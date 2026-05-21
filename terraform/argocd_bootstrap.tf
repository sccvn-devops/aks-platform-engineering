locals {
  argocd_registered_clusters = {
    "mgmt-ne" = {
      environment             = "control-plane"
      env                     = "control-plane"
      region                  = var.secondary_location
      role                    = "management"
      lease_status            = "standby"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "true"
      host                    = module.aks_clusters["mgmt-ne"].host
      ca_data                 = module.aks_clusters["mgmt-ne"].cluster_ca_certificate
      cert_data               = module.aks_clusters["mgmt-ne"].client_certificate
      key_data                = module.aks_clusters["mgmt-ne"].client_key
    }
    "aks-dev-we" = {
      environment             = "dev"
      env                     = "dev"
      region                  = var.location
      role                    = "workload"
      lease_status            = "unmanaged"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "true"
      host                    = module.aks_clusters["aks-dev-we"].host
      ca_data                 = module.aks_clusters["aks-dev-we"].cluster_ca_certificate
      cert_data               = module.aks_clusters["aks-dev-we"].client_certificate
      key_data                = module.aks_clusters["aks-dev-we"].client_key
      oidc_issuer_url         = module.aks_clusters["aks-dev-we"].oidc_issuer_url
    }
    "aks-staging-we" = {
      environment             = "staging"
      env                     = "staging"
      region                  = var.location
      role                    = "workload"
      lease_status            = "unmanaged"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "true"
      host                    = module.aks_clusters["aks-staging-we"].host
      ca_data                 = module.aks_clusters["aks-staging-we"].cluster_ca_certificate
      cert_data               = module.aks_clusters["aks-staging-we"].client_certificate
      key_data                = module.aks_clusters["aks-staging-we"].client_key
      oidc_issuer_url         = module.aks_clusters["aks-staging-we"].oidc_issuer_url
    }
    "aks-prod-we" = {
      environment             = "prod"
      env                     = "prod"
      region                  = var.location
      role                    = "workload"
      lease_status            = "unmanaged"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "true"
      host                    = module.aks_clusters["aks-prod-we"].host
      ca_data                 = module.aks_clusters["aks-prod-we"].cluster_ca_certificate
      cert_data               = module.aks_clusters["aks-prod-we"].client_certificate
      key_data                = module.aks_clusters["aks-prod-we"].client_key
      oidc_issuer_url         = module.aks_clusters["aks-prod-we"].oidc_issuer_url
    }
    "aks-prod-ne" = {
      environment             = "prod"
      env                     = "prod"
      region                  = var.secondary_location
      role                    = "workload"
      lease_status            = "unmanaged"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "true"
      host                    = module.aks_clusters["aks-prod-ne"].host
      ca_data                 = module.aks_clusters["aks-prod-ne"].cluster_ca_certificate
      cert_data               = module.aks_clusters["aks-prod-ne"].client_certificate
      key_data                = module.aks_clusters["aks-prod-ne"].client_key
      oidc_issuer_url         = module.aks_clusters["aks-prod-ne"].oidc_issuer_url
    }
    "seed-wus" = {
      environment             = "dr"
      env                     = "dr"
      region                  = var.dr_location
      role                    = "bootstrap"
      lease_status            = "unmanaged"
      enable_argocd           = "false"
      enable_kyverno          = "true"
      enable_external_secrets = "false"
      host                    = module.aks_clusters["seed-wus"].host
      ca_data                 = module.aks_clusters["seed-wus"].cluster_ca_certificate
      cert_data               = module.aks_clusters["seed-wus"].client_certificate
      key_data                = module.aks_clusters["seed-wus"].client_key
    }
  }

  crossplane_workload_provider_clusters = {
    for name, cluster in local.argocd_registered_clusters : name => {
      host            = cluster.host
      ca_data         = cluster.ca_data
      cert_data       = cluster.cert_data
      key_data        = cluster.key_data
      oidc_issuer_url = try(cluster.oidc_issuer_url, null)
    } if cluster.role == "workload"
  }
}

resource "kubernetes_secret_v1" "argocd_registered_clusters" {
  for_each = local.argocd_registered_clusters

  metadata {
    name      = each.key
    namespace = kubernetes_namespace.argocd_namespace.metadata[0].name
    labels = {
      "argocd.argoproj.io/secret-type" = "cluster"
      "akuity.io/argo-cd-cluster-name" = each.key
      environment                      = each.value.environment
      env                              = each.value.env
      region                           = each.value.region
      role                             = each.value.role
      "lease-status"                   = each.value.lease_status
      enable_argocd                    = each.value.enable_argocd
      enable_kyverno                   = each.value.enable_kyverno
      enable_external_secrets          = each.value.enable_external_secrets
    }
    annotations = merge(local.cluster_metadata, {
      cluster_name        = each.key
      environment         = each.value.environment
      region              = each.value.region
      resource_group_name = azurerm_resource_group.this.name
      role                = each.value.role
      subscription_id     = data.azurerm_subscription.current.subscription_id
      tenant_id           = data.azurerm_client_config.current.tenant_id
      acr_login_server    = azurerm_container_registry.platform.login_server
      mgmt_lease_blob_url = azurerm_storage_blob.mgmt_active.url
      }, each.value.role == "management" ? {
      mgmt_lease_identity_client_id        = azurerm_user_assigned_identity.mgmt_cluster[each.key].client_id
      velero_identity_client_id            = azurerm_user_assigned_identity.velero.client_id
      velero_backup_storage_account_name   = azurerm_storage_account.mgmt_backup.name
      velero_backup_container_name         = azurerm_storage_container.mgmt_backup.name
      velero_backup_resource_group_name    = azurerm_resource_group.this.name
      velero_bsl_access_mode               = each.value.lease_status == "active" ? "ReadWrite" : "ReadOnly"
      velero_schedules_disabled            = each.value.lease_status == "active" ? "false" : "true"
      } : {}, contains(keys(local.external_secrets_workload_clusters), each.key) ? {
      oidc_issuer_url                         = each.value.oidc_issuer_url
      external_secrets_identity_client_id     = azurerm_user_assigned_identity.external_secrets[each.key].client_id
      external_secrets_vault_id               = azurerm_key_vault.platform[local.external_secrets_workload_clusters[each.key].key_vault_key].id
      external_secrets_vault_url              = azurerm_key_vault.platform[local.external_secrets_workload_clusters[each.key].key_vault_key].vault_uri
      external_secrets_smoke_test_secret_name = azurerm_key_vault_secret.external_secrets_smoke_test[local.external_secrets_workload_clusters[each.key].key_vault_key].name
      cosign_public_key_secret_name           = azurerm_key_vault_secret.cosign_public_key[local.external_secrets_workload_clusters[each.key].key_vault_key].name
    } : {})
  }

  data = {
    name   = each.key
    server = each.value.host
    config = jsonencode({
      tlsClientConfig = {
        insecure = false
        caData   = each.value.ca_data
        certData = each.value.cert_data
        keyData  = each.value.key_data
      }
    })
  }

  depends_on = [module.gitops_bridge_bootstrap]
}

resource "kubernetes_secret_v1" "crossplane_workload_kubeconfigs" {
  for_each = local.crossplane_workload_provider_clusters

  metadata {
    name      = "crossplane-kubeconfig-${each.key}"
    namespace = kubernetes_namespace.argocd_namespace.metadata[0].name
    labels = {
      "app.kubernetes.io/managed-by" = "terraform"
      "platform.cityos.io/cluster"   = each.key
    }
  }

  data = {
    kubeconfig = yamlencode({
      apiVersion = "v1"
      kind       = "Config"
      clusters = [
        {
          name = each.key
          cluster = {
            server                       = each.value.host
            "certificate-authority-data" = each.value.ca_data
          }
        }
      ]
      contexts = [
        {
          name = each.key
          context = {
            cluster = each.key
            user    = each.key
          }
        }
      ]
      "current-context" = each.key
      users = [
        {
          name = each.key
          user = {
            "client-certificate-data" = each.value.cert_data
            "client-key-data"         = each.value.key_data
          }
        }
      ]
    })
  }

  depends_on = [kubernetes_secret_v1.argocd_registered_clusters]
}
