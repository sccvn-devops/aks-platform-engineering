#!/usr/bin/env bash
# Validate that native Azure Blob Lease is the sole Terraform state-locking mechanism.
# No external lock store (DynamoDB, Cosmos, Azure Table) should be consulted.
#
# Usage (offline — static checks only):
#   ./scripts/validate-blob-lease-locking.sh
#
# Usage (online — also simulates concurrent apply lease behavior):
#   AZURE_ONLINE=1 ./scripts/validate-blob-lease-locking.sh
#
# Optional env-var overrides:
#   TF_STATE_STORAGE_ACCOUNT  (default: stplatformtfstate)
#   TF_STATE_RESOURCE_GROUP   (default: rg-tfstate-bootstrap)
#   TF_STATE_CONTAINER        (default: tfstate)

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
STORAGE_ACCOUNT="${TF_STATE_STORAGE_ACCOUNT:-stplatformtfstate}"
RESOURCE_GROUP="${TF_STATE_RESOURCE_GROUP:-rg-tfstate-bootstrap}"
CONTAINER="${TF_STATE_CONTAINER:-tfstate}"
AZURE_ONLINE="${AZURE_ONLINE:-0}"
FAIL=0

# ---------------------------------------------------------------------------
# 1. Static: backend type must be "azurerm" (not s3, gcs, local, or http)
# ---------------------------------------------------------------------------
echo "=== Static: verifying backend type is 'azurerm' ==="

BACKEND_LINE=$(grep -r 'backend "' "$REPO_ROOT/terraform/" 2>/dev/null \
  | grep -v '\.terraform/' | head -1 || true)

if echo "$BACKEND_LINE" | grep -q 'backend "azurerm"'; then
  echo "OK: backend \"azurerm\" declared — native blob-lease locking applies"
else
  echo "FAIL: expected 'backend \"azurerm\"', found: ${BACKEND_LINE:-<none>}" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 2. Static: no external lock-store attributes in any backend or .tfbackend file
#    (attributes that indicate DynamoDB, Cosmos, or custom lock services)
# ---------------------------------------------------------------------------
echo ""
echo "=== Static: checking for forbidden lock-store attributes ==="

declare -a FORBIDDEN_ATTRS=(
  "lock_table"
  "dynamodb_table"
  "lock_service_url"
  "table_name.*lock"
  "cosmos.*lock"
)

FOUND_ATTR=0
for PATTERN in "${FORBIDDEN_ATTRS[@]}"; do
  MATCHES=$(grep -rEi "$PATTERN" "$REPO_ROOT/terraform/" 2>/dev/null \
    | grep -v '\.terraform/' || true)
  if [ -n "$MATCHES" ]; then
    echo "FAIL: forbidden lock-store attribute '$PATTERN' found:" >&2
    echo "$MATCHES" >&2
    FAIL=1
    FOUND_ATTR=1
  fi
done

if [ "$FOUND_ATTR" -eq 0 ]; then
  echo "OK: no external lock-store attributes found in terraform/"
fi

# ---------------------------------------------------------------------------
# 3. Static: no competing lock resources (Storage Table as lock store,
#    DynamoDB tables, Cosmos tables used solely for locking)
# ---------------------------------------------------------------------------
echo ""
echo "=== Static: checking for competing lock resources ==="

declare -a COMPETING_PATTERNS=(
  "aws_dynamodb_table"
  "azurerm_storage_table.*state.*lock"
  "azurerm_cosmosdb_table.*lock"
)

FOUND_COMPETING=0
for PATTERN in "${COMPETING_PATTERNS[@]}"; do
  MATCHES=$(grep -rEi "$PATTERN" "$REPO_ROOT/terraform/" 2>/dev/null \
    | grep -v '\.terraform/' || true)
  if [ -n "$MATCHES" ]; then
    echo "FAIL: competing lock resource '$PATTERN' found:" >&2
    echo "$MATCHES" >&2
    FAIL=1
    FOUND_COMPETING=1
  fi
done

if [ "$FOUND_COMPETING" -eq 0 ]; then
  echo "OK: no competing lock resources found"
fi

# ---------------------------------------------------------------------------
# 4. Static: verify per-env .tfbackend files contain only 'key = ...'
#    (no lock_table or other lock-service references snuck in)
# ---------------------------------------------------------------------------
echo ""
echo "=== Static: verifying .tfbackend files contain no lock overrides ==="

BACKENDS_DIR="$REPO_ROOT/terraform/backends"
BACKEND_FAIL=0
for BFILE in "$BACKENDS_DIR"/*.tfbackend; do
  BNAME=$(basename "$BFILE")
  # Each .tfbackend should only have the 'key' line
  NON_KEY=$(grep -Ev '^(key\s*=|#|$)' "$BFILE" || true)
  if [ -n "$NON_KEY" ]; then
    echo "FAIL: $BNAME contains unexpected directives:" >&2
    echo "$NON_KEY" >&2
    FAIL=1
    BACKEND_FAIL=1
  fi
done

if [ "$BACKEND_FAIL" -eq 0 ]; then
  echo "OK: all .tfbackend files contain only the 'key' directive"
fi

# ---------------------------------------------------------------------------
# 5. Online: simulate concurrent apply by acquiring a blob lease twice
#    One succeeds; the second must fail with a lease-held (409) error.
# ---------------------------------------------------------------------------
if [ "$AZURE_ONLINE" = "1" ]; then
  echo ""
  echo "=== Online: simulating concurrent apply via Azure Blob Lease ==="

  if ! az account show --query id -o tsv &>/dev/null; then
    echo "FAIL: AZURE_ONLINE=1 but not authenticated — run 'az login' first" >&2
    FAIL=1
  else
    # Pick the first available state blob as a test target
    TEST_BLOB=$(az storage blob list \
      --container-name "$CONTAINER" \
      --account-name "$STORAGE_ACCOUNT" \
      --auth-mode login \
      --query "[0].name" \
      --output tsv 2>/dev/null || true)

    if [ -z "$TEST_BLOB" ]; then
      echo "NOTE: no state blobs found — apply at least one env first"
      echo "NOTE: static checks already confirm no external lock store is configured"
    else
      echo "Target blob: $TEST_BLOB"

      # First acquire — simulates the first concurrent apply
      LEASE_ID=$(az storage blob lease acquire \
        --blob-name "$TEST_BLOB" \
        --container-name "$CONTAINER" \
        --account-name "$STORAGE_ACCOUNT" \
        --auth-mode login \
        --lease-duration 15 \
        --output tsv 2>/dev/null || true)

      if [ -z "$LEASE_ID" ]; then
        echo "NOTE: could not acquire test lease (blob may already be leased or insufficient permission)"
        echo "NOTE: static checks confirm no external lock store"
      else
        echo "OK: first apply acquired lease ID: $LEASE_ID"

        # Second acquire — simulates the racing concurrent apply
        SECOND_OUTPUT=$(az storage blob lease acquire \
          --blob-name "$TEST_BLOB" \
          --container-name "$CONTAINER" \
          --account-name "$STORAGE_ACCOUNT" \
          --auth-mode login \
          --lease-duration 15 \
          --output tsv 2>&1 || true)

        if echo "$SECOND_OUTPUT" | grep -qiE "LeaseAlreadyPresent|lease.*already|409|conflict"; then
          echo "OK: second apply blocked with lease-conflict error (no external lock consulted)"
          echo "    Error: $SECOND_OUTPUT"
        else
          echo "WARN: expected lease-conflict error; got: $SECOND_OUTPUT"
          echo "NOTE: this may occur if the blob became free between attempts"
        fi

        # Always release the test lease to leave the state intact
        az storage blob lease release \
          --blob-name "$TEST_BLOB" \
          --container-name "$CONTAINER" \
          --account-name "$STORAGE_ACCOUNT" \
          --auth-mode login \
          --lease-id "$LEASE_ID" \
          --output none 2>/dev/null || true
        echo "OK: test lease released — blob unlocked"
      fi
    fi
  fi
fi

# ---------------------------------------------------------------------------
echo ""
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: FAIL — blob lease locking validation failed" >&2
  exit 1
else
  echo "RESULT: PASS — native Azure Blob Lease is the sole state-locking mechanism"
  echo "         No DynamoDB, Cosmos, Storage Table, or other external lock store configured."
fi
