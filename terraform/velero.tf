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

  # FR-V4-41 / US-V4-10: Velero backup storage retains cluster-state restore
  # points; destroying the account drops every restore point in one step.
  # Removal requires a deliberate two-PR sequence (drop prevent_destroy in
  # PR-1, then destroy in PR-2).
  lifecycle {
    prevent_destroy = true
  }
}

resource "azurerm_storage_container" "mgmt_backup" {
  name                  = "velero"
  storage_account_name  = azurerm_storage_account.mgmt_backup.name
  container_access_type = "private"

  # FR-V4-41 / US-V4-10: the `velero` container holds the Velero backup
  # objects; an accidental destroy invalidates the DR plan.
  lifecycle {
    prevent_destroy = true
  }
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

# Velero workload identity — uses the workload_identity module (US-V4-02 /
# FR-V4-05..09).  RG-scoped Contributor is OK without allow_subscription_scope;
# the precondition only rejects bare /subscriptions/<uuid>.
module "velero_identity" {
  source = "./modules/workload_identity"

  name                = "uami-velero"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { cluster = "mgmt-we", purpose = "backup" })

  federated_credentials = {
    "velero-server-mgmt-we" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "velero"
      service_account_name      = "velero-server"
    }
  }

  role_assignments = {
    "storage_blob_data_contributor" = {
      scope                = azurerm_storage_account.mgmt_backup.id
      role_definition_name = "Storage Blob Data Contributor"
    }
    "rg_contributor" = {
      scope                = azurerm_resource_group.this.id
      role_definition_name = "Contributor"
    }
  }
}

moved {
  from = azurerm_user_assigned_identity.velero
  to   = module.velero_identity.azurerm_user_assigned_identity.this
}

moved {
  from = azurerm_federated_identity_credential.velero
  to   = module.velero_identity.azurerm_federated_identity_credential.this["velero-server-mgmt-we"]
}

moved {
  from = azurerm_role_assignment.velero_storage_blob_data_contributor
  to   = module.velero_identity.azurerm_role_assignment.this["storage_blob_data_contributor"]
}

moved {
  from = azurerm_role_assignment.velero_contributor
  to   = module.velero_identity.azurerm_role_assignment.this["rg_contributor"]
}
