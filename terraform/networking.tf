locals {
  hub_networks = {
    we = {
      name                   = "vnet-hub-we"
      location               = var.location
      address_space          = ["10.0.0.0/16"]
      firewall_subnet_cidr   = ["10.0.0.0/24"]
      bastion_subnet_cidr    = ["10.0.1.0/26"]
      private_endpoints_cidr = ["10.0.2.0/24"]
      shared_services_cidr   = ["10.0.3.0/24"]
    }
    ne = {
      name                   = "vnet-hub-ne"
      location               = var.secondary_location
      address_space          = ["10.8.0.0/16"]
      firewall_subnet_cidr   = ["10.8.0.0/24"]
      bastion_subnet_cidr    = ["10.8.1.0/26"]
      private_endpoints_cidr = ["10.8.2.0/24"]
      shared_services_cidr   = ["10.8.3.0/24"]
    }
  }

  spoke_networks = {
    "mgmt-we" = {
      name                   = "vnet-mgmt-we"
      location               = var.location
      hub                    = "we"
      address_space          = ["10.1.0.0/16"]
      aks_subnet_cidr        = ["10.1.0.0/22"]
      private_endpoints_cidr = ["10.1.4.0/24"]
    }
    "aks-prod-we" = {
      name                   = "vnet-aks-prod-we"
      location               = var.location
      hub                    = "we"
      address_space          = ["10.2.0.0/16"]
      aks_subnet_cidr        = ["10.2.0.0/22"]
      private_endpoints_cidr = ["10.2.4.0/24"]
    }
    "aks-staging-we" = {
      name                   = "vnet-aks-staging-we"
      location               = var.location
      hub                    = "we"
      address_space          = ["10.3.0.0/16"]
      aks_subnet_cidr        = ["10.3.0.0/22"]
      private_endpoints_cidr = ["10.3.4.0/24"]
    }
    "aks-dev-we" = {
      name                   = "vnet-aks-dev-we"
      location               = var.location
      hub                    = "we"
      address_space          = ["10.4.0.0/16"]
      aks_subnet_cidr        = ["10.4.0.0/22"]
      private_endpoints_cidr = ["10.4.4.0/24"]
    }
    "mgmt-ne" = {
      name                   = "vnet-mgmt-ne"
      location               = var.secondary_location
      hub                    = "ne"
      address_space          = ["10.5.0.0/16"]
      aks_subnet_cidr        = ["10.5.0.0/22"]
      private_endpoints_cidr = ["10.5.4.0/24"]
    }
    "aks-prod-ne" = {
      name                   = "vnet-aks-prod-ne"
      location               = var.secondary_location
      hub                    = "ne"
      address_space          = ["10.6.0.0/16"]
      aks_subnet_cidr        = ["10.6.0.0/22"]
      private_endpoints_cidr = ["10.6.4.0/24"]
    }
    "seed-wus" = {
      name                   = "vnet-seed-wus"
      location               = var.dr_location
      hub                    = "we"
      address_space          = ["10.7.0.0/16"]
      aks_subnet_cidr        = ["10.7.0.0/22"]
      private_endpoints_cidr = ["10.7.4.0/24"]
    }
  }

  private_dns_zones = {
    akv        = "privatelink.vaultcore.azure.net"
    sql        = "privatelink.database.windows.net"
    cosmos     = "privatelink.documents.azure.com"
    servicebus = "privatelink.servicebus.windows.net"
    acr        = "privatelink.azurecr.io"
    blob       = "privatelink.blob.core.windows.net"
  }
}

resource "azurerm_virtual_network" "hubs" {
  for_each            = local.hub_networks
  name                = each.value.name
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  address_space       = each.value.address_space
  tags                = merge(var.tags, { networkRole = "hub", region = each.key })
}

resource "azurerm_virtual_network" "spokes" {
  for_each            = local.spoke_networks
  name                = each.value.name
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  address_space       = each.value.address_space
  tags                = merge(var.tags, { networkRole = "spoke", cluster = each.key })
}

resource "azurerm_subnet" "hub_firewall" {
  for_each             = local.hub_networks
  name                 = "AzureFirewallSubnet"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.hubs[each.key].name
  address_prefixes     = each.value.firewall_subnet_cidr
}

resource "azurerm_subnet" "hub_bastion" {
  for_each             = local.hub_networks
  name                 = "AzureBastionSubnet"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.hubs[each.key].name
  address_prefixes     = each.value.bastion_subnet_cidr
}

resource "azurerm_subnet" "hub_private_endpoints" {
  for_each                          = local.hub_networks
  name                              = "snet-private-endpoints"
  resource_group_name               = azurerm_resource_group.this.name
  virtual_network_name              = azurerm_virtual_network.hubs[each.key].name
  address_prefixes                  = each.value.private_endpoints_cidr
  private_endpoint_network_policies = "Disabled"
}

resource "azurerm_subnet" "hub_shared_services" {
  for_each             = local.hub_networks
  name                 = "snet-shared-services"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.hubs[each.key].name
  address_prefixes     = each.value.shared_services_cidr
}

resource "azurerm_subnet" "spoke_aks" {
  for_each             = local.spoke_networks
  name                 = "snet-aks"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.spokes[each.key].name
  address_prefixes     = each.value.aks_subnet_cidr
  service_endpoints    = ["Microsoft.Storage", "Microsoft.KeyVault", "Microsoft.ContainerRegistry"]
}

resource "azurerm_subnet" "spoke_private_endpoints" {
  for_each                          = local.spoke_networks
  name                              = "snet-private-endpoints"
  resource_group_name               = azurerm_resource_group.this.name
  virtual_network_name              = azurerm_virtual_network.spokes[each.key].name
  address_prefixes                  = each.value.private_endpoints_cidr
  private_endpoint_network_policies = "Disabled"
}

resource "azurerm_public_ip" "firewall_we" {
  name                = "pip-fw-hub-we"
  location            = local.hub_networks.we.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  zones               = ["1", "2", "3"]
  tags                = merge(var.tags, { service = "azure-firewall" })
}

resource "azurerm_firewall_policy" "we" {
  name                = "afwp-hub-we"
  location            = local.hub_networks.we.location
  resource_group_name = azurerm_resource_group.this.name
  sku                 = "Premium"
  tags                = merge(var.tags, { service = "azure-firewall-policy" })
}

resource "azurerm_firewall" "we" {
  name                = "afw-hub-we"
  location            = local.hub_networks.we.location
  resource_group_name = azurerm_resource_group.this.name
  sku_name            = "AZFW_VNet"
  sku_tier            = "Premium"
  firewall_policy_id  = azurerm_firewall_policy.we.id
  tags                = merge(var.tags, { service = "azure-firewall" })

  ip_configuration {
    name                 = "primary"
    subnet_id            = azurerm_subnet.hub_firewall["we"].id
    public_ip_address_id = azurerm_public_ip.firewall_we.id
  }
}

resource "azurerm_firewall_policy_rule_collection_group" "we_allowlist" {
  name               = "rcg-platform-fqdns"
  firewall_policy_id = azurerm_firewall_policy.we.id
  priority           = 100

  application_rule_collection {
    name     = "arc-platform-egress"
    priority = 100
    action   = "Allow"

    rule {
      name             = "bitbucket-atlassian"
      source_addresses = ["10.0.0.0/8"]
      destination_fqdns = [
        "bitbucket.org",
        "api.bitbucket.org",
        "auth.atlassian.com",
        "*.atlassian.net",
      ]

      protocols {
        type = "Https"
        port = 443
      }
    }

    rule {
      name             = "azure-container-registry"
      source_addresses = ["10.0.0.0/8"]
      destination_fqdns = [
        "*.azurecr.io",
        "mcr.microsoft.com",
      ]

      protocols {
        type = "Https"
        port = 443
      }
    }

    rule {
      name             = "azure-platform-services"
      source_addresses = ["10.0.0.0/8"]
      destination_fqdns = [
        "management.azure.com",
        "login.microsoftonline.com",
        "packages.microsoft.com",
        "acs-mirror.azureedge.net",
        "*.blob.core.windows.net",
      ]

      protocols {
        type = "Https"
        port = 443
      }
    }
  }
}

resource "azurerm_route_table" "spokes" {
  for_each            = local.spoke_networks
  name                = "rt-${each.key}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { cluster = each.key, routeIntent = "force-tunnel" })
}

resource "azurerm_route" "default_to_firewall" {
  for_each               = local.spoke_networks
  name                   = "default-egress-via-firewall"
  resource_group_name    = azurerm_resource_group.this.name
  route_table_name       = azurerm_route_table.spokes[each.key].name
  address_prefix         = "0.0.0.0/0"
  next_hop_type          = "VirtualAppliance"
  next_hop_in_ip_address = azurerm_firewall.we.ip_configuration[0].private_ip_address
}

resource "azurerm_subnet_route_table_association" "spoke_aks" {
  for_each       = local.spoke_networks
  subnet_id      = azurerm_subnet.spoke_aks[each.key].id
  route_table_id = azurerm_route_table.spokes[each.key].id
}

resource "azurerm_subnet_route_table_association" "spoke_private_endpoints" {
  for_each       = local.spoke_networks
  subnet_id      = azurerm_subnet.spoke_private_endpoints[each.key].id
  route_table_id = azurerm_route_table.spokes[each.key].id
}

resource "azurerm_network_security_group" "spokes" {
  for_each            = local.spoke_networks
  name                = "nsg-${each.key}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { cluster = each.key, securityBoundary = "spoke" })
}

resource "azurerm_subnet_network_security_group_association" "spoke_aks" {
  for_each                  = local.spoke_networks
  subnet_id                 = azurerm_subnet.spoke_aks[each.key].id
  network_security_group_id = azurerm_network_security_group.spokes[each.key].id
}

resource "azurerm_subnet_network_security_group_association" "spoke_private_endpoints" {
  for_each                  = local.spoke_networks
  subnet_id                 = azurerm_subnet.spoke_private_endpoints[each.key].id
  network_security_group_id = azurerm_network_security_group.spokes[each.key].id
}

resource "azurerm_virtual_network_peering" "hub_to_spoke" {
  for_each = local.spoke_networks

  name                         = "hub-${each.value.hub}-to-${each.key}"
  resource_group_name          = azurerm_resource_group.this.name
  virtual_network_name         = azurerm_virtual_network.hubs[each.value.hub].name
  remote_virtual_network_id    = azurerm_virtual_network.spokes[each.key].id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
}

resource "azurerm_virtual_network_peering" "spoke_to_hub" {
  for_each = local.spoke_networks

  name                         = "${each.key}-to-hub-${each.value.hub}"
  resource_group_name          = azurerm_resource_group.this.name
  virtual_network_name         = azurerm_virtual_network.spokes[each.key].name
  remote_virtual_network_id    = azurerm_virtual_network.hubs[each.value.hub].id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
}

resource "azurerm_virtual_network_peering" "hub_we_to_ne" {
  name                         = "hub-we-to-hub-ne"
  resource_group_name          = azurerm_resource_group.this.name
  virtual_network_name         = azurerm_virtual_network.hubs["we"].name
  remote_virtual_network_id    = azurerm_virtual_network.hubs["ne"].id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
}

resource "azurerm_virtual_network_peering" "hub_ne_to_we" {
  name                         = "hub-ne-to-hub-we"
  resource_group_name          = azurerm_resource_group.this.name
  virtual_network_name         = azurerm_virtual_network.hubs["ne"].name
  remote_virtual_network_id    = azurerm_virtual_network.hubs["we"].id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = true
}

resource "azurerm_private_dns_zone" "platform" {
  for_each            = local.private_dns_zones
  name                = each.value
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "private-dns", zone = each.key })
}

resource "azurerm_private_dns_zone_virtual_network_link" "platform" {
  for_each = merge([
    for zone_key, zone_name in local.private_dns_zones : {
      for vnet_key, vnet in merge(
        {
          for hub_key, hub in azurerm_virtual_network.hubs :
          "hub-${hub_key}" => {
            name = hub.name
            id   = hub.id
          }
        },
        {
          for spoke_key, spoke in azurerm_virtual_network.spokes :
          "spoke-${spoke_key}" => {
            name = spoke.name
            id   = spoke.id
          }
        }
      ) :
      "${zone_key}-${vnet_key}" => {
        zone_name = zone_name
        link_name = "link-${zone_key}-${vnet.name}"
        vnet_id   = vnet.id
      }
    }
  ]...)

  name                  = each.value.link_name
  resource_group_name   = azurerm_resource_group.this.name
  private_dns_zone_name = each.value.zone_name
  virtual_network_id    = each.value.vnet_id
  registration_enabled  = false
}

resource "azurerm_public_ip" "bastion_we" {
  name                = "pip-bastion-we"
  location            = local.hub_networks.we.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  zones               = ["1", "2", "3"]
  tags                = merge(var.tags, { service = "azure-bastion" })
}

resource "azurerm_bastion_host" "we" {
  name                = "bas-hub-we"
  location            = local.hub_networks.we.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "azure-bastion" })

  ip_configuration {
    name                 = "configuration"
    subnet_id            = azurerm_subnet.hub_bastion["we"].id
    public_ip_address_id = azurerm_public_ip.bastion_we.id
  }
}
