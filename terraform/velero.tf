resource "azurerm_storage_account" "mgmt_backup" {
  name                     = "stplatformmgmtbackup"
  resource_group_name      = azurerm_resource_group.this.name
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "GRS"
  account_kind             = "StorageV2"
  min_tls_version          = "TLS1_2"

  public_network_access_enabled   = true
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = true

  blob_properties {
    versioning_enabled = true
  }

  tags = merge(var.tags, {
    service = "velero-backup-storage"
    region  = "we"
  })
}

resource "azurerm_storage_container" "mgmt_backup" {
  name                  = "velero"
  storage_account_name  = azurerm_storage_account.mgmt_backup.name
  container_access_type = "private"
}

resource "azurerm_private_endpoint" "mgmt_backup_blob" {
  name                = "pe-${azurerm_storage_account.mgmt_backup.name}-blob"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints["we"].id
  tags = merge(var.tags, {
    service = "velero-backup-storage-private-endpoint"
    region  = "we"
  })

  private_service_connection {
    name                           = "psc-${azurerm_storage_account.mgmt_backup.name}-blob"
    private_connection_resource_id = azurerm_storage_account.mgmt_backup.id
    is_manual_connection           = false
    subresource_names              = ["blob"]
  }

  private_dns_zone_group {
    name                 = "blob-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["blob"].id]
  }
}

resource "azurerm_user_assigned_identity" "velero" {
  name                = "uami-velero"
  resource_group_name = azurerm_resource_group.this.name
  location            = var.location
  tags                = merge(var.tags, { cluster = "mgmt-we", purpose = "backup" })
}

resource "azurerm_federated_identity_credential" "velero" {
  name                = "velero-server-mgmt-we"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.velero.id
  subject             = "system:serviceaccount:velero:velero-server"

  depends_on = [module.aks]
}

resource "azurerm_role_assignment" "velero_storage_blob_data_contributor" {
  scope                = azurerm_storage_account.mgmt_backup.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_user_assigned_identity.velero.principal_id
}

resource "azurerm_role_assignment" "velero_contributor" {
  scope                = azurerm_resource_group.this.id
  role_definition_name = "Contributor"
  principal_id         = azurerm_user_assigned_identity.velero.principal_id
}
