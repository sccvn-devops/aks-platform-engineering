resource "azurerm_user_assigned_identity" "saas_token_rotator" {
  name                = "uami-saas-token-rotator"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "saas-token-rotator", purpose = "kv-secret-write" })
}

resource "azurerm_federated_identity_credential" "saas_token_rotator" {
  name                = "saas-token-rotator-mgmt-we"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.saas_token_rotator.id
  subject             = "system:serviceaccount:saas-token-rotator:saas-token-rotator"
}

resource "azurerm_role_assignment" "saas_rotator_mgmt_kv" {
  scope                = azurerm_key_vault.management_ci.id
  role_definition_name = "Key Vault Secrets Officer"
  principal_id         = azurerm_user_assigned_identity.saas_token_rotator.principal_id
}

resource "azurerm_role_assignment" "saas_rotator_prod_we_kv" {
  scope                = azurerm_key_vault.platform["prod-we"].id
  role_definition_name = "Key Vault Secrets Officer"
  principal_id         = azurerm_user_assigned_identity.saas_token_rotator.principal_id
}

resource "azurerm_role_assignment" "saas_rotator_prod_ne_kv" {
  scope                = azurerm_key_vault.platform["prod-ne"].id
  role_definition_name = "Key Vault Secrets Officer"
  principal_id         = azurerm_user_assigned_identity.saas_token_rotator.principal_id
}
