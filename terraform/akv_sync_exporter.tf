################################################################################
# AKV Dual-Write Sync Exporter — Workload Identity and Vault Access
# Runs on mgmt-we; reads secret metadata from both prod vaults to expose
# akv_dual_write_skew_seconds metric for the PerRegionAKVDualWriteSkew alert.
#
# Migrated to the workload_identity module (US-V4-02 / FR-V4-05..09).  Subject
# format and role-assignment scope guarantees now live inside the module.
################################################################################

module "akv_sync_exporter_identity" {
  source = "./modules/workload_identity"

  name                = "uami-akv-sync-exporter"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "akv-sync-exporter" })

  federated_credentials = {
    "akv-sync-exporter" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "akv-sync-exporter"
      service_account_name      = "akv-sync-exporter"
    }
  }

  role_assignments = {
    "kv_we" = {
      scope                = azurerm_key_vault.platform["prod-we"].id
      role_definition_name = "Key Vault Secrets User"
    }
    "kv_ne" = {
      scope                = azurerm_key_vault.platform["prod-ne"].id
      role_definition_name = "Key Vault Secrets User"
    }
  }
}

moved {
  from = azurerm_user_assigned_identity.akv_sync_exporter
  to   = module.akv_sync_exporter_identity.azurerm_user_assigned_identity.this
}

moved {
  from = azurerm_federated_identity_credential.akv_sync_exporter
  to   = module.akv_sync_exporter_identity.azurerm_federated_identity_credential.this["akv-sync-exporter"]
}

moved {
  from = azurerm_role_assignment.akv_sync_exporter_kv_we
  to   = module.akv_sync_exporter_identity.azurerm_role_assignment.this["kv_we"]
}

moved {
  from = azurerm_role_assignment.akv_sync_exporter_kv_ne
  to   = module.akv_sync_exporter_identity.azurerm_role_assignment.this["kv_ne"]
}
