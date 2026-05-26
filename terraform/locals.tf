# terraform/locals.tf — Helper locals for the Helm → ESO secret migration (US-V4-09, FR-V4-36..40).
#
# Two helper locals declare every secret-bearing reference that may flow through
# a helm_release. Adding a new helm_release or modifying an existing one MUST
# route every secret value through one of the two modes — never through a
# `helm_release.set { name = X, value = var.<sensitive_var> }` shortcut. The
# rendered chart consumes the secret via an ExternalSecret managed under
# gitops/ (which ESO materialises into a K8s Secret in the chart's namespace).
#
# scripts/validate-helm-release-secrets.py is the static guard (the FR-V4-37
# "custom tflint rule" — implemented as a Python validator per the codebase
# convention; tflint plugins require a Go build step that the platform CI
# does not currently provision). It fails the PR if any helm_release block
# contains `set { name = X, value = var.<sensitive_var> }`.
#
# Modes
# ─────
#  secrets_managed_in_tf       Terraform owns the WRITE path: an
#                              azurerm_key_vault_secret resource creates the
#                              AKV entry, and an ExternalSecret in gitops/
#                              syncs it to a K8s Secret consumed by the chart.
#                              Examples: backstage-postgres-password,
#                              backstage-github-token.
#
#  secrets_referenced_only     The AKV entry is owned by an external system
#                              (saas-token-rotator, akv-sync-exporter, an
#                              external operator). Terraform only references
#                              the name so the ExternalSecret manifest can
#                              point at it. Examples:
#                              bitbucket-workspace-token,
#                              jira-service-account-token.
#
# Both keys map to a description; the locals are inspected by
# scripts/validate-helm-release-secrets.py and may be consumed by future
# helm_release wiring that needs the catalogue.

locals {
  # Mode 1 — Terraform writes the secret to AKV; ESO syncs to a K8s Secret.
  # Every entry here MUST also appear as an azurerm_key_vault_secret in
  # terraform/*.tf and in the AKV catalogue header at the top of keyvaults.tf.
  secrets_managed_in_tf = {
    "backstage-postgres-password" = {
      description = "Backstage Postgres admin password — sourced from var.postgres_password at apply time via OIDC federation; written to AKV (mgmt-we) for ESO consumption."
      vault       = "management_ci"
      consumed_by = ["helm_release.backstage"]
    }
    "backstage-github-token" = {
      description = "Backstage GitHub PAT — sourced from var.github_token; written to AKV for ESO consumption (replaces the FR-V4-37-forbidden helm_release.set { value = local.github_token } shortcut)."
      vault       = "management_ci"
      consumed_by = ["helm_release.backstage"]
    }
    "backstage-azure-client-secret" = {
      description = "Backstage AAD app client secret — derived from azuread_service_principal_password; written to AKV for ESO consumption (replaces the in-line helm_release.set value reference)."
      vault       = "management_ci"
      consumed_by = ["helm_release.backstage"]
    }
    "backstage-service-account-token" = {
      description = "Backstage k8s ServiceAccount token — read from the kubernetes_secret bootstrapped by Terraform; written to AKV so the chart can envFrom the ESO-materialised Secret rather than receiving the value through helm_release.set."
      vault       = "management_ci"
      consumed_by = ["helm_release.backstage"]
    }
  }

  # Mode 2 — AKV entry is created elsewhere; Terraform only references the name.
  # No azurerm_key_vault_secret resource for these in terraform/; the platform
  # secret-rotator owns the write path.
  secrets_referenced_only = {
    "bitbucket-workspace-token" = {
      description = "Bitbucket workspace token — rotated quarterly by saas-token-rotator; consumed by Jenkins via an ESO ExternalSecret in gitops/."
      vault       = "management_ci"
      consumed_by = ["jenkins (via ESO)"]
    }
    "jira-service-account-token" = {
      description = "Jira service-account token — rotated quarterly by saas-token-rotator; consumed by Jenkins + argocd-jira-bridge via ESO."
      vault       = "management_ci"
      consumed_by = ["jenkins (via ESO)", "argocd-jira-bridge (via ESO)"]
    }
    "platform-tls" = {
      description = "Per-workload-cluster TLS cert — issued by AKV with AutoRenew at lifetime_percentage = 75; consumed by workload-cluster ingress via ESO."
      vault       = "workload-all"
      consumed_by = ["workload ingress (via ESO)"]
    }
  }

  # Convenience derived view: the union of all secret names referenced by any
  # helm_release in this Terraform root. Used by the validator to confirm the
  # in-tree migration count is zero.
  helm_release_secret_inventory = concat(
    keys(local.secrets_managed_in_tf),
    keys(local.secrets_referenced_only),
  )
}
