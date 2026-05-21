################################################################################
# AKV Dual-Write Sync Exporter — Workload Identity and Vault Access
# Runs on mgmt-we; reads secret metadata from both prod vaults to expose
# akv_dual_write_skew_seconds metric for the PerRegionAKVDualWriteSkew alert.
################################################################################

resource "azurerm_user_assigned_identity" "akv_sync_exporter" {
  name                = "uami-akv-sync-exporter"
  resource_group_name = azurerm_resource_group.this.name
  location            = var.location
  tags                = merge(var.tags, { service = "akv-sync-exporter" })
}

# Grant Secrets User on both prod vaults so the exporter can read secret metadata
resource "azurerm_role_assignment" "akv_sync_exporter_kv_we" {
  scope                = azurerm_key_vault.platform["prod-we"].id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.akv_sync_exporter.principal_id
}

resource "azurerm_role_assignment" "akv_sync_exporter_kv_ne" {
  scope                = azurerm_key_vault.platform["prod-ne"].id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.akv_sync_exporter.principal_id
}

# Federated identity for the akv-sync-exporter ServiceAccount on mgmt-we
resource "azurerm_federated_identity_credential" "akv_sync_exporter" {
  name                = "akv-sync-exporter"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.akv_sync_exporter.id
  subject             = "system:serviceaccount:akv-sync-exporter:akv-sync-exporter"
}
