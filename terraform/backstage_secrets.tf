# terraform/backstage_secrets.tf — Backstage helm_release secret migration (US-V4-09, FR-V4-36..40).
#
# US-V4-09 (Appendix A in PRD-v4) inventories the helm_release.backstage in
# main.tf as the only in-tree consumer of `set { value = sensitive }` shortcuts.
# Those four set blocks (env.K8S_SERVICE_ACCOUNT_TOKEN, env.GITHUB_TOKEN,
# env.POSTGRES_PASSWORD, env.AZURE_CLIENT_SECRET) leak sensitive material
# through Terraform plan output, CI logs, and the .tfstate file.
#
# Migration path (TF → AKV → ESO → chart):
#   1. Terraform writes each sensitive value to the management Key Vault
#      (this file).
#   2. The ExternalSecret manifest under
#      gitops/environments/default/addons/backstage/external-secret.yaml
#      tells ESO to materialise a K8s Secret named `backstage-secrets` in the
#      `backstage` namespace.
#   3. The helm_release in main.tf no longer carries any
#      `set { name = "env.X" value = <sensitive> }` block; the chart consumes
#      the env vars via envFrom: secretRef: backstage-secrets (chart-side PR
#      is the "paired PR" the AC references — tracked separately because the
#      Backstage chart is an external OCI artifact).
#
# Catalogue entries for the three new AKV secrets are declared at the top of
# keyvaults.tf; scripts/validate-akv-catalogue.py enforces the round-trip.

resource "azurerm_key_vault_secret" "backstage_github_token" {
  count           = local.build_backstage ? 1 : 0
  name            = "backstage-github-token"
  value           = var.github_token
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "backstage_azure_client_secret" {
  count           = local.build_backstage ? 1 : 0
  name            = "backstage-azure-client-secret"
  value           = azuread_service_principal_password.backstage-sp-password[count.index].value
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

# The k8s ServiceAccount token rotates with the SA, not on a wall-clock cadence,
# so no `expiration_date` is declared (allowlisted in
# scripts/validate-akv-null-expiry.py). Adding expiration_date here would force
# Terraform to recreate the AKV version on every rotate, defeating the point.
resource "azurerm_key_vault_secret" "backstage_service_account_token" {
  count        = local.build_backstage ? 1 : 0
  name         = "backstage-service-account-token"
  value        = kubernetes_secret.backstage_service_account_secret[count.index].data.token
  key_vault_id = azurerm_key_vault.management_ci.id

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}
