locals {
  mgmt_cluster_storage_identities = {
    "mgmt-we" = {
      location = var.location
    }
    "mgmt-ne" = {
      location = var.secondary_location
    }
  }
}

resource "azurerm_user_assigned_identity" "mgmt_cluster" {
  for_each = local.mgmt_cluster_storage_identities

  name                = "uami-${each.key}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { cluster = each.key, purpose = "mgmt-lease" })
}

resource "azurerm_storage_account" "mgmt_lease" {
  name                     = "stplatformmgmtlease"
  resource_group_name      = azurerm_resource_group.this.name
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "GRS"
  account_kind             = "StorageV2"
  min_tls_version          = "TLS1_2"

  # Blob/container creation still uses storage data-plane calls in this provider,
  # so public network access remains enabled for now while access is enforced via RBAC.
  public_network_access_enabled   = true
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = true

  blob_properties {
    versioning_enabled = true
  }

  tags = merge(var.tags, {
    service = "mgmt-lease-storage"
    region  = "we"
  })

  # FR-V4-41 / US-V4-10: the storage account underpins the management-plane
  # singleton lease (ADR-022) and Velero backup lifecycle.  Destroying it
  # accidentally would split-brain the active/standby contract.  Removal
  # requires a deliberate two-PR sequence: PR-1 removes this block, PR-2
  # destroys.
  lifecycle {
    prevent_destroy = true
  }
}

resource "azurerm_storage_container" "mgmt_lease" {
  name                  = "leases"
  storage_account_name  = azurerm_storage_account.mgmt_lease.name
  container_access_type = "private"

  # FR-V4-41 / US-V4-10: the `leases` container holds the mgmt-active blob
  # whose lease arbitrates active vs. standby control planes (ADR-022).  Loss
  # of the container is loss of the lease history.
  lifecycle {
    prevent_destroy = true
  }
}

resource "azurerm_storage_blob" "mgmt_active" {
  name                   = "mgmt-active"
  storage_account_name   = azurerm_storage_account.mgmt_lease.name
  storage_container_name = azurerm_storage_container.mgmt_lease.name
  type                   = "Block"
  source_content         = "standby"
}

resource "azurerm_private_endpoint" "mgmt_lease_blob" {
  name                = "pe-${azurerm_storage_account.mgmt_lease.name}-blob"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints["we"].id
  tags = merge(var.tags, {
    service = "mgmt-lease-storage-private-endpoint"
    region  = "we"
  })

  private_service_connection {
    name                           = "psc-${azurerm_storage_account.mgmt_lease.name}-blob"
    private_connection_resource_id = azurerm_storage_account.mgmt_lease.id
    is_manual_connection           = false
    subresource_names              = ["blob"]
  }

  private_dns_zone_group {
    name                 = "blob-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["blob"].id]
  }
}

resource "azurerm_role_assignment" "mgmt_lease_blob_data_contributor" {
  for_each = azurerm_user_assigned_identity.mgmt_cluster

  scope                = azurerm_storage_container.mgmt_lease.resource_manager_id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = each.value.principal_id
}
