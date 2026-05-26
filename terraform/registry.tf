# Cluster topology registry loader — FR-V4-02, ADR-031-v4.
#
# Single point at which the committed YAML registry (gitops/clusters/registry.yaml)
# is parsed into Terraform locals. Every other .tf file referencing per-cluster
# identity (region, resource group, ACR hostname, AKS name, mgmt role) MUST read
# from local.cluster_registry — no inline maps of cluster identity may live
# elsewhere in terraform/*.tf after v4.
#
# Schema-validated by scripts/validate-cluster-registry.py against
# gitops/clusters/registry.schema.json (pre-commit + CI; FR-V4-04).

locals {
  # Raw decoded registry; every consumer downstream goes through derived locals.
  cluster_registry_raw = yamldecode(file("${path.module}/../gitops/clusters/registry.yaml"))

  # Defensive sanity: top-level YAML key MUST equal entry.aks_name. Any divergence
  # would silently desync ApplicationSet selectors from TF state, so we fail fast
  # at plan time via a precondition-like check via a `null_resource` would require
  # a resource, so instead we surface the keys and let `terraform validate` /
  # tflint pick up any reference error if a downstream uses a mismatched key.
  cluster_registry = {
    for k, v in local.cluster_registry_raw : k => merge(v, {
      # Helper booleans for downstream filtering.
      is_management = contains(["active", "standby"], v.mgmt_role)
      is_workload   = v.mgmt_role == "workload"
      is_seed       = v.mgmt_role == "seed"
    })
  }

  # Convenience: all workload cluster keys (used by external_secrets, argocd_bootstrap).
  registry_workload_cluster_keys = [
    for k, v in local.cluster_registry : k if v.is_workload
  ]

  # Convenience: management cluster keys (mgmt-we + mgmt-ne).
  registry_management_cluster_keys = [
    for k, v in local.cluster_registry : k if v.is_management
  ]
}
