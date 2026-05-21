locals {
  aks_cluster_definitions = {
    "mgmt-ne" = {
      location                  = var.secondary_location
      subnet_key                = "mgmt-ne"
      sku_tier                  = "Standard"
      enable_auto_scaling       = false
      agents_count              = 1
      agents_min_count          = null
      agents_max_count          = null
      agents_availability_zones = null
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = var.agents_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "mgmt-ne" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "mgmt-ne" }
    }
    "aks-dev-we" = {
      location                  = var.location
      subnet_key                = "aks-dev-we"
      sku_tier                  = "Standard"
      enable_auto_scaling       = true
      agents_count              = null
      agents_min_count          = 1
      agents_max_count          = 3
      agents_availability_zones = ["1"]
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = var.agents_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "aks-dev-we" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "aks-dev-we" }
    }
    "aks-staging-we" = {
      location                  = var.location
      subnet_key                = "aks-staging-we"
      sku_tier                  = "Standard"
      enable_auto_scaling       = true
      agents_count              = null
      agents_min_count          = 1
      agents_max_count          = 5
      agents_availability_zones = ["1", "2", "3"]
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = var.agents_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "aks-staging-we" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "aks-staging-we" }
    }
    "aks-prod-we" = {
      location                  = var.location
      subnet_key                = "aks-prod-we"
      sku_tier                  = "Premium"
      enable_auto_scaling       = true
      agents_count              = null
      agents_min_count          = 3
      agents_max_count          = 10
      agents_availability_zones = ["1", "2", "3"]
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = "Standard_D4s_v3"
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "aks-prod-we" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "aks-prod-we" }
    }
    "aks-prod-ne" = {
      location                  = var.secondary_location
      subnet_key                = "aks-prod-ne"
      sku_tier                  = "Premium"
      enable_auto_scaling       = true
      agents_count              = null
      agents_min_count          = 3
      agents_max_count          = 10
      agents_availability_zones = ["1", "2", "3"]
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = "Standard_D4s_v3"
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "aks-prod-ne" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "aks-prod-ne" }
    }
    "seed-wus" = {
      location                  = var.dr_location
      subnet_key                = "seed-wus"
      sku_tier                  = "Standard"
      enable_auto_scaling       = false
      agents_count              = 1
      agents_min_count          = null
      agents_max_count          = null
      agents_availability_zones = null
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = var.agents_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = "seed-wus" }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = "seed-wus" }
    }
  }
}

module "aks_clusters" {
  for_each                                        = local.aks_cluster_definitions
  source                                          = "Azure/aks/azurerm"
  version                                         = "9.4.1"
  cluster_name                                    = each.key
  resource_group_name                             = azurerm_resource_group.this.name
  location                                        = each.value.location
  kubernetes_version                              = var.kubernetes_version
  orchestrator_version                            = var.kubernetes_version
  role_based_access_control_enabled               = var.role_based_access_control_enabled
  rbac_aad                                        = var.rbac_aad
  prefix                                          = replace(each.key, "-", "")
  network_plugin                                  = var.network_plugin
  vnet_subnet_id                                  = azurerm_subnet.spoke_aks[each.value.subnet_key].id
  os_disk_size_gb                                 = var.os_disk_size_gb
  os_sku                                          = var.os_sku
  sku_tier                                        = each.value.sku_tier
  private_cluster_enabled                         = true
  identity_type                                   = contains(keys(local.mgmt_cluster_storage_identities), each.key) ? "UserAssigned" : "SystemAssigned"
  identity_ids                                    = contains(keys(local.mgmt_cluster_storage_identities), each.key) ? [azurerm_user_assigned_identity.mgmt_cluster[each.key].id] : null
  enable_auto_scaling                             = each.value.enable_auto_scaling
  enable_host_encryption                          = var.enable_host_encryption
  log_analytics_workspace_enabled                 = var.log_analytics_workspace_enabled
  agents_min_count                                = each.value.agents_min_count
  agents_max_count                                = each.value.agents_max_count
  agents_count                                    = each.value.agents_count
  agents_max_pods                                 = var.agents_max_pods
  agents_pool_name                                = each.value.default_nodepool_name
  agents_type                                     = "VirtualMachineScaleSets"
  agents_size                                     = each.value.default_nodepool_vm_size
  agents_availability_zones                       = each.value.agents_availability_zones
  monitor_metrics                                 = {}
  azure_policy_enabled                            = var.azure_policy_enabled
  microsoft_defender_enabled                      = var.microsoft_defender_enabled
  tags                                            = merge(var.tags, { cluster = each.key })
  green_field_application_gateway_for_ingress     = var.green_field_application_gateway_for_ingress
  create_role_assignments_for_application_gateway = var.create_role_assignments_for_application_gateway
  workload_identity_enabled                       = true
  oidc_issuer_enabled                             = true
  agents_labels                                   = each.value.default_nodepool_labels
  agents_tags                                     = each.value.default_nodepool_tags
  network_policy                                  = var.network_policy
  net_profile_dns_service_ip                      = var.net_profile_dns_service_ip
  net_profile_service_cidr                        = var.net_profile_service_cidr
  network_contributor_role_assigned_subnet_ids = {
    aks = azurerm_subnet.spoke_aks[each.value.subnet_key].id
  }

  depends_on = [azurerm_subnet.spoke_aks]
}
