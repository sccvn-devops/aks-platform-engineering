#!/usr/bin/env bash
# fetch-tf-secrets.sh — source this script before running terraform plan/apply in CI.
#
# Authenticates to Azure via OIDC federation (GitHub Actions or equivalent) and reads
# every sensitive Terraform variable from the management Key Vault, exporting them as
# TF_VAR_* environment variables.  Values are masked in GitHub Actions logs via
# ::add-mask:: so they never appear in plain text in CI output.
#
# Required environment variables (set by GitHub Actions OIDC or operator):
#   ARM_CLIENT_ID       — platform CI user-assigned managed identity client ID
#   ARM_TENANT_ID       — Azure AD tenant ID
#   ARM_SUBSCRIPTION_ID — Azure subscription ID
#
# For GitHub Actions OIDC, the workflow must have:
#   permissions:
#     id-token: write
#     contents: read
#
# Usage:
#   source scripts/fetch-tf-secrets.sh
#   terraform -chdir=terraform init -backend-config=backends/mgmt-we.tfbackend
#   terraform -chdir=terraform plan ...

set -euo pipefail

KV_NAME="kv-platform-mgmt-we"

# ---------------------------------------------------------------------------
# Detect execution environment and authenticate
# ---------------------------------------------------------------------------
if [[ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ]]; then
  # GitHub Actions — exchange the OIDC token for an Azure access token
  echo "Authenticating to Azure via GitHub Actions OIDC federation..."
  OIDC_TOKEN=$(curl -fsSL \
    -H "Authorization: bearer ${ACTIONS_ID_TOKEN_REQUEST_TOKEN}" \
    "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=api://AzureADTokenExchange" \
    | jq -r '.value')

  az login \
    --service-principal \
    --username "${ARM_CLIENT_ID}" \
    --federated-token "${OIDC_TOKEN}" \
    --tenant "${ARM_TENANT_ID}" \
    --output none

  az account set --subscription "${ARM_SUBSCRIPTION_ID}" --output none
else
  # Local / non-GHA — expect az login to already be active
  echo "Non-GHA environment: skipping federated login, using current az login context."
fi

# ---------------------------------------------------------------------------
# Helper: fetch a secret and export it as TF_VAR_*, masking in CI logs
# ---------------------------------------------------------------------------
fetch_secret() {
  local secret_name="$1"
  local tf_var_name="$2"

  local value
  value=$(az keyvault secret show \
    --vault-name "${KV_NAME}" \
    --name "${secret_name}" \
    --query "value" \
    --output tsv 2>/dev/null || true)

  if [[ -z "${value}" ]]; then
    echo "WARNING: AKV secret '${secret_name}' not found in ${KV_NAME} — ${tf_var_name} will be unset." >&2
    return
  fi

  # Mask in GitHub Actions logs
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::add-mask::${value}"
  fi

  export "${tf_var_name}=${value}"
}

# ---------------------------------------------------------------------------
# Fetch all sensitive Terraform variables from AKV
# ---------------------------------------------------------------------------
echo "Fetching sensitive Terraform variables from ${KV_NAME}..."

fetch_secret "jenkins-admin-password"                   "TF_VAR_jenkins_admin_password"
fetch_secret "bitbucket-workspace-token"                "TF_VAR_jenkins_bitbucket_workspace_token"
fetch_secret "jira-service-account-token"               "TF_VAR_jenkins_jira_service_account_token"
fetch_secret "jenkins-webhook-https-keystore"           "TF_VAR_jenkins_webhook_https_keystore_base64"
fetch_secret "jenkins-webhook-https-keystore-password"  "TF_VAR_jenkins_webhook_https_keystore_password"
fetch_secret "backstage-postgres-password"              "TF_VAR_postgres_password"
fetch_secret "backstage-tls-crt"                        "TF_VAR_backstage_tls_crt"
fetch_secret "backstage-tls-key"                        "TF_VAR_backstage_tls_key"

echo "Sensitive variables exported. Proceeding with Terraform..."
