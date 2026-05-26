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
}

# External Secrets workload identity per workload cluster.  Migrated to the
# workload_identity module (US-V4-02 / FR-V4-05..09); each cluster gets its
# own module instance with one federated credential per ServiceAccount.  The
# duplicated "uami + fic-per-SA + kv_reader role" triple that lived inline is
# now a single ~15-line declaration here.
module "external_secrets_identity" {
  for_each = local.external_secrets_workload_clusters
  source   = "./modules/workload_identity"

  name                = "uami-eso-${each.key}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name

  federated_credentials = {
    for sa_key, sa in local.external_secrets_service_accounts :
    "${each.key}-${sa.namespace}" => {
      issuer                    = module.aks_clusters[each.key].oidc_issuer_url
      service_account_namespace = sa.namespace
      service_account_name      = sa.name
      name                      = "eso-${each.key}-${sa.namespace}"
    }
  }

  role_assignments = {
    "kv_reader" = {
      scope                = azurerm_key_vault.platform[each.value.key_vault_key].id
      role_definition_name = "Key Vault Secrets User"
    }
  }

  depends_on = [module.aks_clusters]
}

moved {
  from = azurerm_user_assigned_identity.external_secrets["aks-dev-we"]
  to   = module.external_secrets_identity["aks-dev-we"].azurerm_user_assigned_identity.this
}
moved {
  from = azurerm_user_assigned_identity.external_secrets["aks-staging-we"]
  to   = module.external_secrets_identity["aks-staging-we"].azurerm_user_assigned_identity.this
}
moved {
  from = azurerm_user_assigned_identity.external_secrets["aks-prod-we"]
  to   = module.external_secrets_identity["aks-prod-we"].azurerm_user_assigned_identity.this
}
moved {
  from = azurerm_user_assigned_identity.external_secrets["aks-prod-ne"]
  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_user_assigned_identity.this
}

# Federated credentials previously keyed as "<cluster>:<sa_key>"; the module
# instance keys them as "<cluster>-<namespace>".
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-dev-we:platform_secrets"]
  to   = module.external_secrets_identity["aks-dev-we"].azurerm_federated_identity_credential.this["aks-dev-we-platform-secrets"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-dev-we:kyverno"]
  to   = module.external_secrets_identity["aks-dev-we"].azurerm_federated_identity_credential.this["aks-dev-we-kyverno"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-staging-we:platform_secrets"]
  to   = module.external_secrets_identity["aks-staging-we"].azurerm_federated_identity_credential.this["aks-staging-we-platform-secrets"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-staging-we:kyverno"]
  to   = module.external_secrets_identity["aks-staging-we"].azurerm_federated_identity_credential.this["aks-staging-we-kyverno"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-prod-we:platform_secrets"]
  to   = module.external_secrets_identity["aks-prod-we"].azurerm_federated_identity_credential.this["aks-prod-we-platform-secrets"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-prod-we:kyverno"]
  to   = module.external_secrets_identity["aks-prod-we"].azurerm_federated_identity_credential.this["aks-prod-we-kyverno"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-prod-ne:platform_secrets"]
  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_federated_identity_credential.this["aks-prod-ne-platform-secrets"]
}
moved {
  from = azurerm_federated_identity_credential.external_secrets["aks-prod-ne:kyverno"]
  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_federated_identity_credential.this["aks-prod-ne-kyverno"]
}

moved {
  from = azurerm_role_assignment.external_secrets_key_vault_reader["aks-dev-we"]
  to   = module.external_secrets_identity["aks-dev-we"].azurerm_role_assignment.this["kv_reader"]
}
moved {
  from = azurerm_role_assignment.external_secrets_key_vault_reader["aks-staging-we"]
  to   = module.external_secrets_identity["aks-staging-we"].azurerm_role_assignment.this["kv_reader"]
}
moved {
  from = azurerm_role_assignment.external_secrets_key_vault_reader["aks-prod-we"]
  to   = module.external_secrets_identity["aks-prod-we"].azurerm_role_assignment.this["kv_reader"]
}
moved {
  from = azurerm_role_assignment.external_secrets_key_vault_reader["aks-prod-ne"]
  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_role_assignment.this["kv_reader"]
}

resource "azurerm_key_vault_secret" "external_secrets_smoke_test" {
  for_each = local.workload_key_vaults

  name            = "eso-smoke-test"
  value           = "synced-from-${each.value.cluster}"
  key_vault_id    = azurerm_key_vault.platform[each.key].id
  expiration_date = local.platform_secret_expiry_rfc3339

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
#
# US-V3.1-01 (FR-V3.1-01): AutoRenew triggers at lifetime_percentage = 75 (i.e. ~22
# days before expiry for the 3-month validity), which guarantees a quarterly
# rotation cadence enforced by AKV itself — independent of saas-token-rotator
# runtime availability. ESO refreshInterval = 60s re-syncs the new PEM bundle
# well within the overlap window.
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
    lifetime_action {
      action {
        action_type = "AutoRenew"
      }
      trigger {
        lifetime_percentage = 75
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
