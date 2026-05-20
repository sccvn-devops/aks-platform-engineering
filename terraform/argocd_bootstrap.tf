locals {
  argocd_registered_clusters = {
    "mgmt-ne" = {
      environment    = "control-plane"
      env            = "control-plane"
      region         = var.secondary_location
      role           = "management"
      lease_status   = "standby"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["mgmt-ne"].host
      ca_data        = module.aks_clusters["mgmt-ne"].cluster_ca_certificate
      cert_data      = module.aks_clusters["mgmt-ne"].client_certificate
      key_data       = module.aks_clusters["mgmt-ne"].client_key
    }
    "aks-dev-we" = {
      environment    = "dev"
      env            = "dev"
      region         = var.location
      role           = "workload"
      lease_status   = "unmanaged"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["aks-dev-we"].host
      ca_data        = module.aks_clusters["aks-dev-we"].cluster_ca_certificate
      cert_data      = module.aks_clusters["aks-dev-we"].client_certificate
      key_data       = module.aks_clusters["aks-dev-we"].client_key
    }
    "aks-staging-we" = {
      environment    = "staging"
      env            = "staging"
      region         = var.location
      role           = "workload"
      lease_status   = "unmanaged"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["aks-staging-we"].host
      ca_data        = module.aks_clusters["aks-staging-we"].cluster_ca_certificate
      cert_data      = module.aks_clusters["aks-staging-we"].client_certificate
      key_data       = module.aks_clusters["aks-staging-we"].client_key
    }
    "aks-prod-we" = {
      environment    = "prod"
      env            = "prod"
      region         = var.location
      role           = "workload"
      lease_status   = "unmanaged"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["aks-prod-we"].host
      ca_data        = module.aks_clusters["aks-prod-we"].cluster_ca_certificate
      cert_data      = module.aks_clusters["aks-prod-we"].client_certificate
      key_data       = module.aks_clusters["aks-prod-we"].client_key
    }
    "aks-prod-ne" = {
      environment    = "prod"
      env            = "prod"
      region         = var.secondary_location
      role           = "workload"
      lease_status   = "unmanaged"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["aks-prod-ne"].host
      ca_data        = module.aks_clusters["aks-prod-ne"].cluster_ca_certificate
      cert_data      = module.aks_clusters["aks-prod-ne"].client_certificate
      key_data       = module.aks_clusters["aks-prod-ne"].client_key
    }
    "seed-wus" = {
      environment    = "dr"
      env            = "dr"
      region         = var.dr_location
      role           = "bootstrap"
      lease_status   = "unmanaged"
      enable_argocd  = "false"
      enable_kyverno = "false"
      host           = module.aks_clusters["seed-wus"].host
      ca_data        = module.aks_clusters["seed-wus"].cluster_ca_certificate
      cert_data      = module.aks_clusters["seed-wus"].client_certificate
      key_data       = module.aks_clusters["seed-wus"].client_key
    }
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
    }
    annotations = merge(local.cluster_metadata, {
      cluster_name        = each.key
      environment         = each.value.environment
      region              = each.value.region
      role                = each.value.role
      mgmt_lease_blob_url = azurerm_storage_blob.mgmt_active.url
      }, each.value.role == "management" ? {
      mgmt_lease_identity_client_id = azurerm_user_assigned_identity.mgmt_cluster[each.key].client_id
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
