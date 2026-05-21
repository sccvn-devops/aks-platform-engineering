locals {
  external_secrets_workload_clusters = {
    for key, vault in local.workload_key_vaults : vault.cluster => {
      key_vault_key = key
      location      = vault.location
    }
  }

  external_secrets_service_accounts = {
    platform_secrets = {
      namespace = "platform-secrets"
      name      = "azure-keyvault-reader"
    }
    kyverno = {
      namespace = "kyverno"
      name      = "azure-keyvault-reader"
    }
  }

  external_secrets_federated_subjects = {
    for pair in setproduct(
      keys(local.external_secrets_workload_clusters),
      keys(local.external_secrets_service_accounts),
      ) : "${pair[0]}:${pair[1]}" => {
      cluster         = pair[0]
      service_account = local.external_secrets_service_accounts[pair[1]]
    }
  }
}

resource "azurerm_user_assigned_identity" "external_secrets" {
  for_each = local.external_secrets_workload_clusters

  name                = "uami-eso-${each.key}"
  resource_group_name = azurerm_resource_group.this.name
  location            = each.value.location
}

resource "azurerm_federated_identity_credential" "external_secrets" {
  for_each = local.external_secrets_federated_subjects

  name                = "eso-${each.value.cluster}-${each.value.service_account.namespace}"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks_clusters[each.value.cluster].oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.external_secrets[each.value.cluster].id
  subject             = "system:serviceaccount:${each.value.service_account.namespace}:${each.value.service_account.name}"

  depends_on = [module.aks_clusters]
}

resource "azurerm_role_assignment" "external_secrets_key_vault_reader" {
  for_each = local.external_secrets_workload_clusters

  scope                = azurerm_key_vault.platform[each.value.key_vault_key].id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.external_secrets[each.key].principal_id
}

resource "azurerm_key_vault_secret" "external_secrets_smoke_test" {
  for_each = local.workload_key_vaults

  name         = "eso-smoke-test"
  value        = "synced-from-${each.value.cluster}"
  key_vault_id = azurerm_key_vault.platform[each.key].id

  depends_on = [
    azurerm_role_assignment.platform_key_vault_admin,
    azurerm_role_assignment.current_operator_key_vault_admin,
  ]
}

resource "azurerm_key_vault_secret" "cosign_public_key" {
  for_each = local.workload_key_vaults

  name         = "cosign-public-key"
  value        = azurerm_key_vault_key.management_ci_cosign.public_key_pem
  key_vault_id = azurerm_key_vault.platform[each.key].id

  depends_on = [
    azurerm_role_assignment.platform_key_vault_admin,
    azurerm_role_assignment.current_operator_key_vault_admin,
    azurerm_key_vault_key.management_ci_cosign,
  ]
}
