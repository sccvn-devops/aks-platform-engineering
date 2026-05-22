#!/usr/bin/env bash
# Bootstrap the Terraform remote-state storage account out-of-band (Azure CLI only).
# Run once per subscription before any `terraform init`. Safe to re-run (idempotent).
#
# Usage:
#   export AZURE_SUBSCRIPTION_ID=<subscription-id>
#   ./scripts/bootstrap-tfstate.sh
#
# Optional overrides (env vars):
#   TF_STATE_STORAGE_ACCOUNT  – storage account name  (default: stplatformtfstate)
#   TF_STATE_RESOURCE_GROUP   – resource group name   (default: rg-tfstate-bootstrap)
#   TF_STATE_LOCATION         – Azure region          (default: westeurope)

set -euo pipefail

RESOURCE_GROUP="${TF_STATE_RESOURCE_GROUP:-rg-tfstate-bootstrap}"
STORAGE_ACCOUNT="${TF_STATE_STORAGE_ACCOUNT:-stplatformtfstate}"
LOCATION="${TF_STATE_LOCATION:-westeurope}"
CONTAINER="tfstate"
LOCK_NAME="prevent-tfstate-deletion"

# ---------------------------------------------------------------------------
# Pre-flight: ensure az CLI is authenticated
# ---------------------------------------------------------------------------
if ! az account show --query id -o tsv &>/dev/null; then
  echo "ERROR: not authenticated — run 'az login' first" >&2
  exit 1
fi

SUBSCRIPTION_ID="${AZURE_SUBSCRIPTION_ID:-$(az account show --query id -o tsv)}"
echo "Subscription : $SUBSCRIPTION_ID"
echo "Resource group: $RESOURCE_GROUP"
echo "Storage account: $STORAGE_ACCOUNT ($LOCATION)"
echo ""

# ---------------------------------------------------------------------------
# 1. Resource group (idempotent — create only if absent)
# ---------------------------------------------------------------------------
if ! az group show --name "$RESOURCE_GROUP" &>/dev/null; then
  echo "Creating resource group $RESOURCE_GROUP ..."
  az group create \
    --name "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --output none
else
  echo "Resource group $RESOURCE_GROUP already exists — skipping."
fi

# ---------------------------------------------------------------------------
# 2. CanNotDelete management lock on the RG (idempotent)
# ---------------------------------------------------------------------------
if ! az lock show \
      --name "$LOCK_NAME" \
      --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "Creating management lock $LOCK_NAME ..."
  az lock create \
    --name "$LOCK_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --lock-type CanNotDelete \
    --output none
else
  echo "Management lock $LOCK_NAME already exists — skipping."
fi

# ---------------------------------------------------------------------------
# 3. Storage account: GRS, StorageV2, TLS 1.2, no public blob access (idempotent)
# ---------------------------------------------------------------------------
if ! az storage account show \
      --name "$STORAGE_ACCOUNT" \
      --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "Creating storage account $STORAGE_ACCOUNT ..."
  az storage account create \
    --name "$STORAGE_ACCOUNT" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --sku Standard_GRS \
    --kind StorageV2 \
    --allow-blob-public-access false \
    --min-tls-version TLS1_2 \
    --output none
else
  echo "Storage account $STORAGE_ACCOUNT already exists — skipping."
fi

# ---------------------------------------------------------------------------
# 4. Enable blob versioning (idempotent — update is safe to repeat)
# ---------------------------------------------------------------------------
echo "Ensuring blob versioning is enabled ..."
az storage account blob-service-properties update \
  --account-name "$STORAGE_ACCOUNT" \
  --resource-group "$RESOURCE_GROUP" \
  --enable-versioning true \
  --output none

# ---------------------------------------------------------------------------
# 5. State container (idempotent — create only if absent)
# ---------------------------------------------------------------------------
CONTAINER_EXISTS=$(az storage container exists \
  --name "$CONTAINER" \
  --account-name "$STORAGE_ACCOUNT" \
  --auth-mode login \
  --query exists \
  --output tsv)

if [ "$CONTAINER_EXISTS" != "true" ]; then
  echo "Creating blob container $CONTAINER ..."
  az storage container create \
    --name "$CONTAINER" \
    --account-name "$STORAGE_ACCOUNT" \
    --auth-mode login \
    --output none
else
  echo "Container $CONTAINER already exists — skipping."
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
cat <<EOF

Terraform state backend is ready.

  Resource group : $RESOURCE_GROUP  (CanNotDelete lock: $LOCK_NAME)
  Storage account: $STORAGE_ACCOUNT
  Container      : $CONTAINER

Next step — initialise each environment with its partitioned state key:

  terraform -chdir=terraform init \\
    -backend-config=backends/mgmt-we.tfbackend

See terraform/backends/ for all seven environment config files.
EOF
