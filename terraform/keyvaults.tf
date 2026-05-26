# ─────────────────────────────────────────────────────────────────────────────
# Platform AKV catalogue (US-V3.1-01 / FR-V3.1-01..05 / closes FR-V3-12)
#
# This catalogue is the single source of truth for every platform-managed AKV
# secret and certificate. Adding a new azurerm_key_vault_secret /
# azurerm_key_vault_certificate resource to terraform/ MUST be accompanied by a
# new entry here; CI lint script scripts/validate-akv-catalogue.py enforces
# that every resource is listed (and vice versa).
#
# Columns:
#   name            — AKV object name
#   mode            — operator-supplied | platform-generated | akv-issued
#   vault           — mgmt-we | workload (per-cluster) | workload-all (every workload vault)
#   expiry-policy   — time_rotating reference (secrets) or lifetime_percentage (certs)
#   rotation-scope  — UAMI that may rewrite this object (per-purpose UAMI is the
#                     maximum isolation available — AKV data-plane RBAC does NOT
#                     support ABAC prefix conditions per grilling-session 2026-05-22,
#                     ADR-020 update; per-purpose UAMI is the equivalent control).
#
# ── Certificates (AKV-issued, AutoRenew at lifetime_percentage = 75) ────────
#   platform-tls            akv-issued   workload-all   lifetime_percentage=75   akspe (Key Vault Administrator)
#   backstage-internal-tls  akv-issued   mgmt-we        lifetime_percentage=75   akspe
#   jenkins-webhook-tls     akv-issued   mgmt-we        lifetime_percentage=75   akspe
#
# ── Platform-generated secrets (90-day quarterly expiry via time_rotating) ──
#   jenkins-admin-username                    operator-supplied      mgmt-we   no-expiry   akspe
#   jenkins-admin-password                    operator-supplied      mgmt-we   quarterly   akspe + saas-token-rotator (vault-scope; ABAC prefix not supported)
#   bitbucket-workspace-token                 platform-generated     mgmt-we   quarterly   saas-token-rotator
#   jira-service-account-token                platform-generated     mgmt-we   quarterly   saas-token-rotator
#   jenkins-webhook-https-keystore            operator-supplied      mgmt-we   quarterly   akspe
#   jenkins-webhook-https-keystore-password   operator-supplied      mgmt-we   quarterly   akspe
#   backstage-postgres-password               operator-supplied      mgmt-we   quarterly   akspe
#   backstage-github-token                    operator-supplied      mgmt-we   quarterly   akspe (US-V4-09: replaces helm_release.set { value = local.github_token })
#   backstage-azure-client-secret             platform-generated     mgmt-we   quarterly   akspe (US-V4-09: replaces helm_release.set { value = azuread_service_principal_password... })
#   backstage-service-account-token           platform-generated     mgmt-we   no-expiry   akspe (US-V4-09: k8s SA token; rotates with the SA itself, not with a clock — see secrets_managed_in_tf in locals.tf)
#   backstage-tls-crt                         operator-supplied      mgmt-we   quarterly   akspe (legacy; consume backstage-internal-tls cert when ready)
#   backstage-tls-key                         operator-supplied      mgmt-we   quarterly   akspe (legacy; consume backstage-internal-tls cert when ready)
#   eso-smoke-test                            platform-generated     workload-all   quarterly   akspe
#   cosign-public-key                         platform-derived       workload-all   no-expiry (driven by cosign-signing-key rotation_policy in mgmt-we) akspe
#
# CI assertions:
#   scripts/validate-akv-null-expiry.py     — fails the PR if any platform-tagged
#                                              secret is missing expiration_date.
#                                              Advisory in sprint-1, blocking from
#                                              sprint-2 (set NULL_EXPIRY_MODE=block).
#   scripts/validate-akv-catalogue.py       — fails the PR if a Terraform-declared
#                                              azurerm_key_vault_secret /
#                                              azurerm_key_vault_certificate is
#                                              missing from the catalogue above
#                                              (or vice versa).
# ─────────────────────────────────────────────────────────────────────────────

# Quarterly boundary for platform-generated secret expiry. The time_rotating
# resource refreshes its `rotation_rfc3339` attribute every 90 days, which is
# fed into expiration_date on every platform-managed secret. When the boundary
# rotates, Terraform plan recreates the affected secret versions with a new
# expiry 90 days ahead, satisfying FR-V3.1-02.
resource "time_rotating" "platform_secret_quarterly" {
  rotation_days = 90
}

locals {
  # Computed expiry for platform-generated secrets: always 90 days ahead of the
  # current rotation boundary. Used as expiration_date on every platform-managed
  # azurerm_key_vault_secret resource (FR-V3.1-02).
  platform_secret_expiry_rfc3339 = timeadd(time_rotating.platform_secret_quarterly.rotation_rfc3339, "2160h")

  workload_key_vaults = {
    "dev-we" = {
      name     = "kv-platform-dev-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-dev-we"
    }
    "staging-we" = {
      name     = "kv-platform-staging-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-staging-we"
    }
    "prod-we" = {
      name     = "kv-platform-prod-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-prod-we"
    }
    "prod-ne" = {
      name     = "kv-platform-prod-ne"
      location = var.secondary_location
      hub      = "ne"
      cluster  = "aks-prod-ne"
    }
  }
}

resource "azurerm_log_analytics_workspace" "platform" {
  name                = "log-platform-secure"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = merge(var.tags, { service = "log-analytics", purpose = "platform-diagnostics" })
}

resource "azurerm_key_vault" "platform" {
  for_each = local.workload_key_vaults

  name                          = each.value.name
  location                      = each.value.location
  resource_group_name           = azurerm_resource_group.this.name
  tenant_id                     = data.azurerm_client_config.current.tenant_id
  sku_name                      = "standard"
  soft_delete_retention_days    = 90
  purge_protection_enabled      = true
  enable_rbac_authorization     = true
  public_network_access_enabled = false
  tags = merge(var.tags, {
    service = "key-vault"
    cluster = each.value.cluster
    region  = each.value.hub
  })

  network_acls {
    default_action             = "Deny"
    bypass                     = "None"
    virtual_network_subnet_ids = [azurerm_subnet.spoke_private_endpoints[each.value.cluster].id]
  }
}

resource "azurerm_role_assignment" "platform_key_vault_admin" {
  for_each = azurerm_key_vault.platform

  scope                = each.value.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = azurerm_user_assigned_identity.akspe.principal_id
}

resource "azurerm_role_assignment" "current_operator_key_vault_admin" {
  for_each = azurerm_key_vault.platform

  scope                = each.value.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = data.azurerm_client_config.current.object_id
}

resource "azurerm_private_endpoint" "platform_key_vault" {
  for_each = local.workload_key_vaults

  name                = "pe-${each.value.name}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints[each.value.hub].id
  tags = merge(var.tags, {
    service = "key-vault-private-endpoint"
    cluster = each.value.cluster
    region  = each.value.hub
  })

  private_service_connection {
    name                           = "psc-${each.value.name}"
    private_connection_resource_id = azurerm_key_vault.platform[each.key].id
    is_manual_connection           = false
    subresource_names              = ["vault"]
  }

  private_dns_zone_group {
    name                 = "akv-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["akv"].id]
  }
}

resource "azurerm_monitor_diagnostic_setting" "platform_key_vault" {
  for_each = azurerm_key_vault.platform

  name                       = "diag-${each.key}"
  target_resource_id         = each.value.id
  log_analytics_workspace_id = azurerm_log_analytics_workspace.platform.id

  enabled_log {
    category = "AuditEvent"
  }

  metric {
    category = "AllMetrics"
  }
}
