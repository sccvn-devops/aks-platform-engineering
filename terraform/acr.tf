locals {
  aks_kubelet_object_ids = merge(
    {
      "mgmt-we" = module.aks.kubelet_identity[0].object_id
    },
    {
      for key, cluster in module.aks_clusters :
      key => cluster.kubelet_identity[0].object_id
    }
  )
}

resource "azurerm_user_assigned_identity" "jenkins" {
  name                = "uami-jenkins"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "jenkins", purpose = "acr-push" })
}

resource "azurerm_container_registry" "platform" {
  name                          = "acrplatformprod"
  resource_group_name           = azurerm_resource_group.this.name
  location                      = var.location
  sku                           = "Premium"
  admin_enabled                 = false
  public_network_access_enabled = false
  zone_redundancy_enabled       = true
  tags = merge(var.tags, {
    service = "acr"
    region  = "we"
  })

  georeplications {
    location                  = var.secondary_location
    regional_endpoint_enabled = true
    zone_redundancy_enabled   = true
    tags = merge(var.tags, {
      service = "acr-replication"
      region  = "ne"
    })
  }
}

resource "azurerm_private_endpoint" "platform_acr" {
  name                = "pe-${azurerm_container_registry.platform.name}"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints["we"].id
  tags = merge(var.tags, {
    service = "acr-private-endpoint"
    region  = "we"
  })

  private_service_connection {
    name                           = "psc-${azurerm_container_registry.platform.name}"
    private_connection_resource_id = azurerm_container_registry.platform.id
    is_manual_connection           = false
    subresource_names              = ["registry"]
  }

  private_dns_zone_group {
    name                 = "acr-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["acr"].id]
  }
}

resource "azurerm_role_assignment" "aks_acr_pull" {
  for_each = local.aks_kubelet_object_ids

  scope                = azurerm_container_registry.platform.id
  role_definition_name = "AcrPull"
  principal_id         = each.value
}

resource "azurerm_role_assignment" "jenkins_acr_push" {
  scope                = azurerm_container_registry.platform.id
  role_definition_name = "AcrPush"
  principal_id         = azurerm_user_assigned_identity.jenkins.principal_id
}
