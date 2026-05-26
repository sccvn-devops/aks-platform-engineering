# saas-token-rotator workload identity — uses the workload_identity module
# (US-V4-02 / FR-V4-05..09).  Was previously 5 hand-rolled resources; now ~15
# lines of structured input.

module "saas_token_rotator_identity" {
  source = "./modules/workload_identity"

  name                = "uami-saas-token-rotator"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "saas-token-rotator", purpose = "kv-secret-write" })

  federated_credentials = {
    "saas-token-rotator-mgmt-we" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "saas-token-rotator"
      service_account_name      = "saas-token-rotator"
    }
  }

  role_assignments = {
    "mgmt_kv" = {
      scope                = azurerm_key_vault.management_ci.id
      role_definition_name = "Key Vault Secrets Officer"
    }
    "prod_we_kv" = {
      scope                = azurerm_key_vault.platform["prod-we"].id
      role_definition_name = "Key Vault Secrets Officer"
    }
    "prod_ne_kv" = {
      scope                = azurerm_key_vault.platform["prod-ne"].id
      role_definition_name = "Key Vault Secrets Officer"
    }
  }
}

# State migration: keep existing resources in state when refactoring into the
# module.  Without these `moved` blocks, terraform plan would propose to
# destroy and recreate the UAMI (rotating the principal_id and breaking every
# downstream role assignment).
moved {
  from = azurerm_user_assigned_identity.saas_token_rotator
  to   = module.saas_token_rotator_identity.azurerm_user_assigned_identity.this
}

moved {
  from = azurerm_federated_identity_credential.saas_token_rotator
  to   = module.saas_token_rotator_identity.azurerm_federated_identity_credential.this["saas-token-rotator-mgmt-we"]
}

moved {
  from = azurerm_role_assignment.saas_rotator_mgmt_kv
  to   = module.saas_token_rotator_identity.azurerm_role_assignment.this["mgmt_kv"]
}

moved {
  from = azurerm_role_assignment.saas_rotator_prod_we_kv
  to   = module.saas_token_rotator_identity.azurerm_role_assignment.this["prod_we_kv"]
}

moved {
  from = azurerm_role_assignment.saas_rotator_prod_ne_kv
  to   = module.saas_token_rotator_identity.azurerm_role_assignment.this["prod_ne_kv"]
}
