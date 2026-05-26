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

# Jenkins workload identity — uses the workload_identity module
# (US-V4-02 / FR-V4-05..09).  Owns the UAMI + the two federated credentials
# (controller + agent) + the two Azure role grants (AcrPush on the platform
# ACR + Key Vault Crypto User on the management Key Vault).
#
# The ACR + management KV resources are still declared in this file / jenkins.tf
# respectively; the module collapses what was previously 5 inline TF resources
# into one structured call.  Scope arguments retain references to the existing
# resource addresses so Terraform's dependency graph still sees the edges.
module "jenkins_identity" {
  source = "./modules/workload_identity"

  name                = "uami-jenkins"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { service = "jenkins", purpose = "acr-push" })

  federated_credentials = {
    "jenkins-controller-mgmt-we" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "jenkins"
      service_account_name      = "jenkins-controller"
    }
    "jenkins-agent-mgmt-we" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "jenkins"
      service_account_name      = "jenkins-agent"
    }
  }

  role_assignments = {
    "acr_push" = {
      scope                = azurerm_container_registry.platform.id
      role_definition_name = "AcrPush"
    }
    "mgmt_kv_crypto_user" = {
      scope                = azurerm_key_vault.management_ci.id
      role_definition_name = "Key Vault Crypto User"
    }
  }

  depends_on = [module.aks]
}

moved {
  from = azurerm_user_assigned_identity.jenkins
  to   = module.jenkins_identity.azurerm_user_assigned_identity.this
}

moved {
  from = azurerm_federated_identity_credential.jenkins_controller
  to   = module.jenkins_identity.azurerm_federated_identity_credential.this["jenkins-controller-mgmt-we"]
}

moved {
  from = azurerm_federated_identity_credential.jenkins_agent
  to   = module.jenkins_identity.azurerm_federated_identity_credential.this["jenkins-agent-mgmt-we"]
}

moved {
  from = azurerm_role_assignment.jenkins_acr_push
  to   = module.jenkins_identity.azurerm_role_assignment.this["acr_push"]
}

moved {
  from = azurerm_role_assignment.jenkins_management_ci_crypto_user
  to   = module.jenkins_identity.azurerm_role_assignment.this["mgmt_kv_crypto_user"]
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
