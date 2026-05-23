#!/usr/bin/env bash
# Validate US-V3-09: AKV-native quarterly rotation policies on all v3 keys/certs.
# Offline (default): static checks against Terraform source.
# Online (AZURE_ONLINE=1): live AKV inspection via az CLI.
set -euo pipefail

TERRAFORM_DIR="$(cd "$(dirname "$0")/../terraform" && pwd)"
PASS=0
FAIL=0

ok()   { echo "[PASS] $*"; ((PASS++)) || true; }
fail() { echo "[FAIL] $*"; ((FAIL++)) || true; }

# ── Static checks ──────────────────────────────────────────────────────────────

echo "=== Static: cosign key rotation_policy ==="
if grep -q 'rotation_policy' "${TERRAFORM_DIR}/jenkins.tf"; then
  ok "rotation_policy block present in jenkins.tf (management_ci_cosign key)"
else
  fail "rotation_policy missing from jenkins.tf — add rotation_policy block to management_ci_cosign"
fi

if grep -q 'expire_after.*"P90D"' "${TERRAFORM_DIR}/jenkins.tf"; then
  ok "expire_after = P90D (quarterly) on cosign key"
else
  fail "expire_after P90D not found in jenkins.tf rotation_policy"
fi

echo ""
echo "=== Static: platform TLS cert quarterly validity ==="
if grep -q 'validity_in_months\s*=\s*3' "${TERRAFORM_DIR}/external_secrets.tf"; then
  ok "validity_in_months = 3 (quarterly) on platform_tls certificate"
else
  fail "validity_in_months is not 3 on platform_tls certificate — update to validity_in_months = 3"
fi

if grep -q 'action_type\s*=\s*"AutoRenew"' "${TERRAFORM_DIR}/external_secrets.tf"; then
  ok "AutoRenew lifetime_action present on platform_tls certificate"
else
  fail "AutoRenew lifetime_action missing from platform_tls certificate"
fi

echo ""
echo "=== Static: no UAMI client secrets ==="
if grep -rn "azurerm_service_principal_password\|azuread_application_password" \
    "${TERRAFORM_DIR}"/*.tf 2>/dev/null | grep -v '.terraform/' | grep -q .; then
  fail "Static credential (SP password) found in Terraform — all identities must use Workload Identity federation"
else
  ok "No service principal passwords found — all identities use federated credentials"
fi

# Count UAMIs vs federated credentials to confirm federation coverage
UAMI_COUNT=$(grep -h 'resource "azurerm_user_assigned_identity"' "${TERRAFORM_DIR}"/*.tf 2>/dev/null | wc -l || echo 0)
FIC_COUNT=$(grep -h 'resource "azurerm_federated_identity_credential"' "${TERRAFORM_DIR}"/*.tf 2>/dev/null | wc -l || echo 0)
echo "    UAMIs declared: ${UAMI_COUNT}, federated credentials declared: ${FIC_COUNT}"
if [ "${FIC_COUNT}" -ge 1 ]; then
  ok "At least one federated identity credential exists (Workload Identity pattern in use)"
else
  fail "No federated identity credentials found — Workload Identity federation not configured"
fi

# ── Online checks (requires az login) ──────────────────────────────────────────

if [[ "${AZURE_ONLINE:-0}" == "1" ]]; then
  KV_NAME="${AKV_MGMT_NAME:-kv-platform-mgmt-we}"
  KEY_NAME="cosign-signing-key"
  echo ""
  echo "=== Online: live AKV key rotation policy for ${KV_NAME}/${KEY_NAME} ==="

  POLICY=$(az keyvault key rotation-policy show \
    --vault-name "${KV_NAME}" \
    --name "${KEY_NAME}" \
    --output json 2>/dev/null || echo '{}')

  if echo "${POLICY}" | grep -q '"timeAfterCreate"\|"timeBeforeExpiry"'; then
    ok "Rotation policy attached to ${KEY_NAME} in ${KV_NAME}"
    NEXT_ROTATION=$(echo "${POLICY}" | \
      python3 -c "import sys,json; p=json.load(sys.stdin); \
        attrs=p.get('attributes',{}); print(attrs.get('expiresOn','unknown'))" 2>/dev/null || echo "unknown")
    echo "    Next expiry/rotation timestamp: ${NEXT_ROTATION}"
    if [[ "${NEXT_ROTATION}" != "unknown" && "${NEXT_ROTATION}" != "null" ]]; then
      ok "Next rotation timestamp is visible: ${NEXT_ROTATION}"
    else
      fail "Could not read next rotation timestamp from policy"
    fi
  else
    fail "No rotation policy returned for ${KEY_NAME} — policy may not be applied yet"
  fi
else
  echo ""
  echo "(Skipping live AKV checks — set AZURE_ONLINE=1 with az login to enable)"
fi

# ── Summary ────────────────────────────────────────────────────────────────────

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
if [ "${FAIL}" -gt 0 ]; then
  exit 1
fi
echo "All rotation-policy checks passed."
