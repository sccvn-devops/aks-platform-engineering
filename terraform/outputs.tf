output "subscription_id" {
  description = "Specifies the subscription id."
  value       = data.azurerm_subscription.current.subscription_id
}

output "tenant" {
  description = "Specifies the tenant id."
  value       = data.azurerm_client_config.current.tenant_id
}

output "akspe_client_id" {
  description = "Specifies the client id used for user MSI to use for workload identity auth with CAPZ/Crossplane."
  value       = azurerm_user_assigned_identity.akspe.client_id
}

output "crossplane_client_id" {
  description = "Specifies the client id used by the Crossplane Azure provider workload identity."
  value       = azurerm_user_assigned_identity.crossplane.client_id
}

output "hub_vnet_ids" {
  description = "Hub VNet resource IDs keyed by region."
  value = {
    for key, network in azurerm_virtual_network.hubs :
    key => network.id
  }
}

output "spoke_vnet_ids" {
  description = "Spoke VNet resource IDs keyed by spoke name."
  value = {
    for key, network in azurerm_virtual_network.spokes :
    key => network.id
  }
}

output "private_dns_zone_ids" {
  description = "Private DNS zone resource IDs keyed by service."
  value = {
    for key, zone in azurerm_private_dns_zone.platform :
    key => zone.id
  }
}

output "key_vault_ids" {
  description = "Key Vault resource IDs keyed by workload environment."
  value = {
    for key, vault in azurerm_key_vault.platform :
    key => vault.id
  }
}

output "key_vault_names" {
  description = "Key Vault names keyed by workload environment."
  value = {
    for key, vault in azurerm_key_vault.platform :
    key => vault.name
  }
}

output "mgmt_lease_storage_account_name" {
  description = "Management-plane lease storage account name."
  value       = azurerm_storage_account.mgmt_lease.name
}

output "mgmt_lease_storage_container_id" {
  description = "Resource Manager ID for the management-plane lease blob container."
  value       = azurerm_storage_container.mgmt_lease.resource_manager_id
}

output "mgmt_lease_blob_url" {
  description = "URL of the singleton management-plane lease blob."
  value       = azurerm_storage_blob.mgmt_active.url
}

output "mgmt_cluster_uami_client_ids" {
  description = "User-assigned managed identity client IDs for the management clusters."
  value = {
    for key, identity in azurerm_user_assigned_identity.mgmt_cluster :
    key => identity.client_id
  }
}

output "log_analytics_workspace_id" {
  description = "Log Analytics workspace ID used for platform diagnostics."
  value       = azurerm_log_analytics_workspace.platform.id
}

output "acr_id" {
  description = "Azure Container Registry resource ID."
  value       = azurerm_container_registry.platform.id
}

output "acr_name" {
  description = "Azure Container Registry name."
  value       = azurerm_container_registry.platform.name
}

output "acr_login_server" {
  description = "Azure Container Registry login server."
  value       = azurerm_container_registry.platform.login_server
}

output "jenkins_uami_client_id" {
  description = "Client ID for the Jenkins user-assigned managed identity."
  value       = azurerm_user_assigned_identity.jenkins.client_id
}

output "hub_we_firewall_private_ip" {
  description = "Private IP address of the West Europe Azure Firewall."
  value       = azurerm_firewall.we.ip_configuration[0].private_ip_address
}

output "hub_we_bastion_id" {
  description = "Azure Bastion host deployed in the West Europe hub."
  value       = azurerm_bastion_host.we.id
}

output "aks_cluster_ids" {
  description = "AKS cluster resource IDs keyed by cluster name."
  value = merge(
    { "mgmt-we" = module.aks.aks_id },
    { for key, cluster in module.aks_clusters : key => cluster.aks_id }
  )
}

output "aks_private_fqdns" {
  description = "Private FQDNs for each AKS cluster keyed by cluster name."
  value = merge(
    { "mgmt-we" = module.aks.cluster_private_fqdn },
    { for key, cluster in module.aks_clusters : key => cluster.cluster_private_fqdn }
  )
}

output "aks_oidc_issuer_urls" {
  description = "OIDC issuer URLs for each AKS cluster keyed by cluster name."
  value = merge(
    { "mgmt-we" = module.aks.oidc_issuer_url },
    { for key, cluster in module.aks_clusters : key => cluster.oidc_issuer_url }
  )
}

output "aks_node_resource_groups" {
  description = "Node resource groups for each AKS cluster keyed by cluster name."
  value = merge(
    { "mgmt-we" = module.aks.node_resource_group },
    { for key, cluster in module.aks_clusters : key => cluster.node_resource_group }
  )
}
