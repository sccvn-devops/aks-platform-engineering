#!/usr/bin/env bash
# Validate that the Terraform state Storage Account uses Microsoft-managed key (MSE)
# encryption with no CMK references, as required by ADR-023-v3 decision #5.
#
# Usage:
#   ./scripts/validate-mse-encryption.sh
#
# Requires 'az login' — the check reads live Azure properties.
#
# Optional env-var overrides:
#   TF_STATE_STORAGE_ACCOUNT  (default: stplatformtfstate)
#   TF_STATE_RESOURCE_GROUP   (default: rg-tfstate-bootstrap)

set -euo pipefail

STORAGE_ACCOUNT="${TF_STATE_STORAGE_ACCOUNT:-stplatformtfstate}"
RESOURCE_GROUP="${TF_STATE_RESOURCE_GROUP:-rg-tfstate-bootstrap}"
FAIL=0

# ---------------------------------------------------------------------------
# Pre-flight: az authentication
# ---------------------------------------------------------------------------
if ! az account show --query id -o tsv &>/dev/null; then
  echo "ERROR: not authenticated — run 'az login' first" >&2
  exit 1
fi

echo "Checking encryption configuration of storage account: $STORAGE_ACCOUNT"
echo ""

# ---------------------------------------------------------------------------
# 1. Blob encryption enabled
# ---------------------------------------------------------------------------
BLOB_ENC=$(az storage account show \
  --name "$STORAGE_ACCOUNT" \
  --resource-group "$RESOURCE_GROUP" \
  --query "encryption.services.blob.enabled" \
  --output tsv 2>/dev/null)

if [ "$BLOB_ENC" = "true" ]; then
  echo "OK: encryption.services.blob.enabled = true"
else
  echo "FAIL: encryption.services.blob.enabled = $BLOB_ENC (expected true)" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 2. Key type is Microsoft-managed (keySource = Microsoft.Storage)
# ---------------------------------------------------------------------------
KEY_SOURCE=$(az storage account show \
  --name "$STORAGE_ACCOUNT" \
  --resource-group "$RESOURCE_GROUP" \
  --query "encryption.keySource" \
  --output tsv 2>/dev/null)

if [ "$KEY_SOURCE" = "Microsoft.Storage" ]; then
  echo "OK: encryption.keySource = Microsoft.Storage (Microsoft-managed key)"
else
  echo "FAIL: encryption.keySource = $KEY_SOURCE (expected Microsoft.Storage)" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 3. No CMK Key Vault URI configured
# ---------------------------------------------------------------------------
CMK_URI=$(az storage account show \
  --name "$STORAGE_ACCOUNT" \
  --resource-group "$RESOURCE_GROUP" \
  --query "encryption.keyVaultProperties.keyVaultUri" \
  --output tsv 2>/dev/null || true)

if [ -z "$CMK_URI" ] || [ "$CMK_URI" = "null" ] || [ "$CMK_URI" = "None" ]; then
  echo "OK: no CMK Key Vault URI configured"
else
  echo "FAIL: CMK Key Vault URI is set: $CMK_URI — remove per ADR-023-v3 decision #5" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 4. No CMK key name configured
# ---------------------------------------------------------------------------
CMK_KEY=$(az storage account show \
  --name "$STORAGE_ACCOUNT" \
  --resource-group "$RESOURCE_GROUP" \
  --query "encryption.keyVaultProperties.keyName" \
  --output tsv 2>/dev/null || true)

if [ -z "$CMK_KEY" ] || [ "$CMK_KEY" = "null" ] || [ "$CMK_KEY" = "None" ]; then
  echo "OK: no CMK key name configured"
else
  echo "FAIL: CMK key name is set: $CMK_KEY — remove per ADR-023-v3 decision #5" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 5. Static check: no CMK references in Terraform source for the state SA
# ---------------------------------------------------------------------------
REPO_ROOT="$(git rev-parse --show-toplevel)"
echo ""
echo "=== Static: checking Terraform source for CMK references on state SA ==="

CMK_IN_TF=$(grep -rEi \
  "customer_managed_key|key_vault_key_id|infrastructure_encryption|identity_type.*UserAssigned.*encryption" \
  "$REPO_ROOT/terraform/provider.tf" 2>/dev/null || true)

if [ -z "$CMK_IN_TF" ]; then
  echo "OK: no CMK configuration in terraform/provider.tf"
else
  echo "FAIL: CMK-related configuration found in terraform/provider.tf:" >&2
  echo "$CMK_IN_TF" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
echo ""
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: FAIL — MSE encryption validation failed" >&2
  exit 1
else
  echo "RESULT: PASS — encryption.services.blob.enabled=true, keyType=Microsoft-managed, no CMK"
  echo "         State SA satisfies ADR-023-v3 decision #5 (CMK migration deferred to roadmap)"
fi
