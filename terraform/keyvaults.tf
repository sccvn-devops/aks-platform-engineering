locals {
  workload_key_vaults = {
    "dev-we" = {
      name     = "kv-platform-dev-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-dev-we"
    }
    "staging-we" = {
      name     = "kv-platform-staging-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-staging-we"
    }
    "prod-we" = {
      name     = "kv-platform-prod-we"
      location = var.location
      hub      = "we"
      cluster  = "aks-prod-we"
    }
    "prod-ne" = {
      name     = "kv-platform-prod-ne"
      location = var.secondary_location
      hub      = "ne"
      cluster  = "aks-prod-ne"
    }
  }
}

resource "azurerm_log_analytics_workspace" "platform" {
  name                = "log-platform-secure"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = merge(var.tags, { service = "log-analytics", purpose = "platform-diagnostics" })
}

resource "azurerm_key_vault" "platform" {
  for_each = local.workload_key_vaults

  name                          = each.value.name
  location                      = each.value.location
  resource_group_name           = azurerm_resource_group.this.name
  tenant_id                     = data.azurerm_client_config.current.tenant_id
  sku_name                      = "standard"
  soft_delete_retention_days    = 90
  purge_protection_enabled      = true
  enable_rbac_authorization     = true
  public_network_access_enabled = false
  tags = merge(var.tags, {
    service = "key-vault"
    cluster = each.value.cluster
    region  = each.value.hub
  })

  network_acls {
    default_action             = "Deny"
    bypass                     = "None"
    virtual_network_subnet_ids = [azurerm_subnet.spoke_private_endpoints[each.value.cluster].id]
  }
}

resource "azurerm_role_assignment" "platform_key_vault_admin" {
  for_each = azurerm_key_vault.platform

  scope                = each.value.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = azurerm_user_assigned_identity.akspe.principal_id
}

resource "azurerm_role_assignment" "current_operator_key_vault_admin" {
  for_each = azurerm_key_vault.platform

  scope                = each.value.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = data.azurerm_client_config.current.object_id
}

resource "azurerm_private_endpoint" "platform_key_vault" {
  for_each = local.workload_key_vaults

  name                = "pe-${each.value.name}"
  location            = each.value.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints[each.value.hub].id
  tags = merge(var.tags, {
    service = "key-vault-private-endpoint"
    cluster = each.value.cluster
    region  = each.value.hub
  })

  private_service_connection {
    name                           = "psc-${each.value.name}"
    private_connection_resource_id = azurerm_key_vault.platform[each.key].id
    is_manual_connection           = false
    subresource_names              = ["vault"]
  }

  private_dns_zone_group {
    name                 = "akv-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["akv"].id]
  }
}

resource "azurerm_monitor_diagnostic_setting" "platform_key_vault" {
  for_each = azurerm_key_vault.platform

  name                       = "diag-${each.key}"
  target_resource_id         = each.value.id
  log_analytics_workspace_id = azurerm_log_analytics_workspace.platform.id

  enabled_log {
    category = "AuditEvent"
  }

  metric {
    category = "AllMetrics"
  }
}
