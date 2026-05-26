locals {
  # FR-V4-02: per-cluster autoscaling profile (the only piece NOT in the registry —
  # capacity planning is environment-tier policy, not cluster identity).
  aks_cluster_autoscale_profiles = {
    "mgmt-ne"        = { enable_auto_scaling = false, agents_count = 1, agents_min_count = null, agents_max_count = null, default_nodepool_vm_size = var.agents_size }
    "aks-dev-we"     = { enable_auto_scaling = true, agents_count = null, agents_min_count = 1, agents_max_count = 3, default_nodepool_vm_size = var.agents_size }
    "aks-staging-we" = { enable_auto_scaling = true, agents_count = null, agents_min_count = 1, agents_max_count = 5, default_nodepool_vm_size = var.agents_size }
    "aks-prod-we"    = { enable_auto_scaling = true, agents_count = null, agents_min_count = 3, agents_max_count = 10, default_nodepool_vm_size = "Standard_D4s_v3" }
    "aks-prod-ne"    = { enable_auto_scaling = true, agents_count = null, agents_min_count = 3, agents_max_count = 10, default_nodepool_vm_size = "Standard_D4s_v3" }
    "seed-wus"       = { enable_auto_scaling = false, agents_count = 1, agents_min_count = null, agents_max_count = null, default_nodepool_vm_size = var.agents_size }
  }

  # FR-V4-02: aks_cluster_definitions is derived from the cluster registry.
  # location, sku_tier, agents_availability_zones — sourced from gitops/clusters/registry.yaml.
  # mgmt-we is provisioned by the dedicated `module.aks` in main.tf; it is NOT in this map.
  aks_cluster_definitions = {
    for k, profile in local.aks_cluster_autoscale_profiles : k => {
      location                  = local.cluster_registry[k].region
      subnet_key                = k
      sku_tier                  = local.cluster_registry[k].sku_tier
      enable_auto_scaling       = profile.enable_auto_scaling
      agents_count              = profile.agents_count
      agents_min_count          = profile.agents_min_count
      agents_max_count          = profile.agents_max_count
      agents_availability_zones = length(local.cluster_registry[k].azs) > 0 ? local.cluster_registry[k].azs : null
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = profile.default_nodepool_vm_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = k }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = k }
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
