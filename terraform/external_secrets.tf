locals {
  external_secrets_workload_clusters = {
    for key, vault in local.workload_key_vaults : vault.cluster => {
      key_vault_key = key
      location      = vault.location
    }
  }

  external_secrets_service_accounts = {
    platform_secrets = {
      namespace = "platform-secrets"
      name      = "azure-keyvault-reader"
    }
    kyverno = {
      namespace = "kyverno"
      name      = "azure-keyvault-reader"
    }
  }

  external_secrets_federated_subjects = {
    for pair in setproduct(
      keys(local.external_secrets_workload_clusters),
      keys(local.external_secrets_service_accounts),
      ) : "${pair[0]}:${pair[1]}" => {
      cluster         = pair[0]
      service_account = local.external_secrets_service_accounts[pair[1]]
    }
  }
}

resource "azurerm_user_assigned_identity" "external_secrets" {
  for_each = local.external_secrets_workload_clusters

  name                = "uami-eso-${each.key}"
  resource_group_name = azurerm_resource_group.this.name
  location            = each.value.location
}

resource "azurerm_federated_identity_credential" "external_secrets" {
  for_each = local.external_secrets_federated_subjects

  name                = "eso-${each.value.cluster}-${each.value.service_account.namespace}"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks_clusters[each.value.cluster].oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.external_secrets[each.value.cluster].id
  subject             = "system:serviceaccount:${each.value.service_account.namespace}:${each.value.service_account.name}"

  depends_on = [module.aks_clusters]
}

resource "azurerm_role_assignment" "external_secrets_key_vault_reader" {
  for_each = local.external_secrets_workload_clusters

  scope                = azurerm_key_vault.platform[each.value.key_vault_key].id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.external_secrets[each.key].principal_id
}

resource "azurerm_key_vault_secret" "external_secrets_smoke_test" {
  for_each = local.workload_key_vaults

  name         = "eso-smoke-test"
  value        = "synced-from-${each.value.cluster}"
  key_vault_id = azurerm_key_vault.platform[each.key].id

  depends_on = [
    azurerm_role_assignment.platform_key_vault_admin,
    azurerm_role_assignment.current_operator_key_vault_admin,
  ]
}

resource "azurerm_key_vault_secret" "cosign_public_key" {
  for_each = local.workload_key_vaults

  name         = "cosign-public-key"
  value        = azurerm_key_vault_key.management_ci_cosign.public_key_pem
  key_vault_id = azurerm_key_vault.platform[each.key].id

  depends_on = [
    azurerm_role_assignment.platform_key_vault_admin,
    azurerm_role_assignment.current_operator_key_vault_admin,
    azurerm_key_vault_key.management_ci_cosign,
  ]
}

# Self-signed platform TLS certificate issued by AKV in each workload vault.
# ESO syncs the backing PEM secret (secret/platform-tls) to a kubernetes.io/tls
# Secret in every workload namespace via the addons-akv-tls-cert-sync ApplicationSet.
# content_type = application/x-pem-file ensures the backing secret is PEM-encoded
# so the ESO v2 template regexFind can extract tls.crt and tls.key correctly.
# AKV auto-renews 30 days before expiry; ESO re-syncs within 60 s of the new version.
resource "azurerm_key_vault_certificate" "platform_tls" {
  for_each = local.workload_key_vaults

  name         = "platform-tls"
  key_vault_id = azurerm_key_vault.platform[each.key].id

  certificate_policy {
    issuer_parameters {
      name = "Self"
    }
    key_properties {
      exportable = true
      key_size   = 2048
      key_type   = "RSA"
      reuse_key  = false
    }
    # AKV-native quarterly rotation: cert valid for 3 months, auto-renewed 14 days
    # before expiry so there is always an overlap window for ESO to re-sync the
    # new PEM bundle before the old cert expires (ESO refreshInterval = 60s).
    lifetime_action {
      action {
        action_type = "AutoRenew"
      }
      trigger {
        days_before_expiry = 14
      }
    }
    secret_properties {
      content_type = "application/x-pem-file"
    }
    x509_certificate_properties {
      extended_key_usage = ["1.3.6.1.5.5.7.3.1"]
      key_usage          = ["digitalSignature", "keyEncipherment"]
      subject            = "CN=platform.internal"
      validity_in_months = 3
      subject_alternative_names {
        dns_names = ["*.platform.internal"]
      }
    }
  }

  depends_on = [
    azurerm_role_assignment.platform_key_vault_admin,
    azurerm_role_assignment.current_operator_key_vault_admin,
  ]
}
