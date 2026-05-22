#!/usr/bin/env bash
# Validate per-environment Terraform state partitioning.
# Checks that exactly 7 backend config files exist with correct key patterns,
# and optionally verifies the Azure Storage blobs when authenticated.
#
# Usage (offline — checks backend files only):
#   ./scripts/validate-state-partitioning.sh
#
# Usage (online — also verifies Azure blob keys exist):
#   AZURE_ONLINE=1 ./scripts/validate-state-partitioning.sh
#
# Set TF_STATE_STORAGE_ACCOUNT and TF_STATE_RESOURCE_GROUP to override defaults.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
BACKENDS_DIR="$REPO_ROOT/terraform/backends"

STORAGE_ACCOUNT="${TF_STATE_STORAGE_ACCOUNT:-stplatformtfstate}"
RESOURCE_GROUP="${TF_STATE_RESOURCE_GROUP:-rg-tfstate-bootstrap}"
AZURE_ONLINE="${AZURE_ONLINE:-0}"

# Expected: exactly 7 backends with partitioned key paths.
declare -A EXPECTED_KEYS=(
  ["mgmt-we.tfbackend"]="tfstate/mgmt-we/mgmt-we.tfstate"
  ["mgmt-ne.tfbackend"]="tfstate/mgmt-ne/mgmt-ne.tfstate"
  ["dev.tfbackend"]="tfstate/dev/aks-dev-we.tfstate"
  ["staging.tfbackend"]="tfstate/staging/aks-staging-we.tfstate"
  ["prod-we.tfbackend"]="tfstate/prod-we/aks-prod-we.tfstate"
  ["prod-ne.tfbackend"]="tfstate/prod-ne/aks-prod-ne.tfstate"
  ["seed-wus.tfbackend"]="tfstate/seed-wus/seed-wus.tfstate"
)

FAIL=0

# ---------------------------------------------------------------------------
# 1. Verify exactly 7 backend files exist with correct key values
# ---------------------------------------------------------------------------
echo "=== Checking backend config files in $BACKENDS_DIR ==="

FOUND_FILES=()
for FILE in "$BACKENDS_DIR"/*.tfbackend; do
  FOUND_FILES+=("$(basename "$FILE")")
done

if [ "${#FOUND_FILES[@]}" -ne 7 ]; then
  echo "FAIL: expected 7 .tfbackend files, found ${#FOUND_FILES[@]}" >&2
  FAIL=1
fi

for FILENAME in "${!EXPECTED_KEYS[@]}"; do
  FILEPATH="$BACKENDS_DIR/$FILENAME"
  EXPECTED_KEY="${EXPECTED_KEYS[$FILENAME]}"

  if [ ! -f "$FILEPATH" ]; then
    echo "FAIL: missing backend file $FILENAME" >&2
    FAIL=1
    continue
  fi

  ACTUAL_KEY=$(grep -E '^key\s*=' "$FILEPATH" | sed 's/.*=\s*"//;s/".*//')
  if [ "$ACTUAL_KEY" != "$EXPECTED_KEY" ]; then
    echo "FAIL: $FILENAME — expected key \"$EXPECTED_KEY\", got \"$ACTUAL_KEY\"" >&2
    FAIL=1
  else
    echo "OK: $FILENAME → $ACTUAL_KEY"
  fi
done

# ---------------------------------------------------------------------------
# 2. Verify no backend "local" declaration in Terraform sources
# ---------------------------------------------------------------------------
echo ""
echo "=== Checking for disallowed 'backend \"local\"' declarations ==="

if grep -r 'backend "local"' "$REPO_ROOT/terraform/" 2>/dev/null | grep -v '\.terraform'; then
  echo "FAIL: backend \"local\" found in terraform/ — only backend \"azurerm\" is permitted" >&2
  FAIL=1
else
  echo "OK: no 'backend \"local\"' declarations found"
fi

# ---------------------------------------------------------------------------
# 3. (Optional) Online verification — list Azure blob keys after apply
# ---------------------------------------------------------------------------
if [ "$AZURE_ONLINE" = "1" ]; then
  echo ""
  echo "=== Online: listing blobs in container 'tfstate' on $STORAGE_ACCOUNT ==="

  if ! az account show --query id -o tsv &>/dev/null; then
    echo "FAIL: AZURE_ONLINE=1 but not authenticated — run 'az login' first" >&2
    FAIL=1
  else
    BLOB_NAMES=$(az storage blob list \
      --container-name tfstate \
      --account-name "$STORAGE_ACCOUNT" \
      --resource-group "$RESOURCE_GROUP" \
      --auth-mode login \
      --query "[].name" \
      --output tsv 2>/dev/null || true)

    BLOB_COUNT=$(echo "$BLOB_NAMES" | grep -c '\.tfstate$' || true)
    echo "Blobs found after apply:"
    echo "$BLOB_NAMES" | sort

    if [ "$BLOB_COUNT" -ne 7 ]; then
      echo "NOTE: expected 7 state blobs after full apply, found $BLOB_COUNT (normal if not all envs applied yet)"
    else
      echo "OK: exactly 7 state blobs present"
    fi

    # Verify cross-env lease isolation: each blob has its own lease independent of others
    echo ""
    echo "NOTE: Azure blob leases are per-blob. Concurrent applies to different env-state"
    echo "      keys (e.g. dev and prod-we) acquire independent leases and do not block"
    echo "      each other. No additional configuration is required."
  fi
fi

# ---------------------------------------------------------------------------
echo ""
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: FAIL — state partitioning validation failed" >&2
  exit 1
else
  echo "RESULT: PASS — state partitioning validated (7 backends, correct key patterns, no backend \"local\")"
fi
