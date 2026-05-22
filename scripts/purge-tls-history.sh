#!/usr/bin/env bash
# purge-tls-history.sh — Remove committed TLS key/cert material from all branches.
#
# ADR-030-v3 | US-V3-05 | P0 finding F-06
#
# PREREQUISITES:
#   - git-filter-repo installed: pip install git-filter-repo
#   - All collaborators must re-clone or run:
#       git fetch origin && git reset --hard origin/<branch>
#   - Coordinate with team before running (rewrites all branch history)
#   - Old certificate must be revoked at the CA after purge (see CA_REVOCATION_STEPS below)
#
# USAGE (run once from repo root, with no uncommitted changes):
#   bash scripts/purge-tls-history.sh [--dry-run]
#
# CA_REVOCATION_STEPS (perform after successful purge + force-push):
#   1. Identify the CA that issued the compromised cert (CN=backstage-pe-demo.com, self-signed in this case)
#   2. For self-signed certs: the cert has no CRL/OCSP entry — just replace and stop trusting the old cert
#   3. Provision a new certificate in AKV (kv-platform-mgmt-we) under secret names:
#        backstage-tls-crt  (PEM certificate)
#        backstage-tls-key  (PEM private key)
#   4. Run: terraform apply -var backstage_tls_crt="$(az keyvault secret show ...)" ...
#   5. Verify new cert is live: openssl s_client -connect <backstage-host>:443 | openssl x509 -noout -issuer -dates

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
DRY_RUN=false

for arg in "$@"; do
  [[ "$arg" == "--dry-run" ]] && DRY_RUN=true
done

TLS_PATHS=(
  "terraform/tls.key"
  "terraform/tls.crt"
  "backstage/etc/tls/tls.key"
  "backstage/etc/tls/tls.crt"
)

echo "=== TLS History Purge ==="
echo "Repository: $REPO_ROOT"
echo "Dry-run: $DRY_RUN"
echo ""

# Verify git-filter-repo is installed
if ! command -v git-filter-repo &>/dev/null; then
  echo "ERROR: git-filter-repo not found. Install with: pip install git-filter-repo"
  exit 1
fi

# Check for uncommitted changes
if ! git diff --quiet HEAD; then
  echo "ERROR: Uncommitted changes detected. Commit or stash before purging."
  exit 1
fi

# Build --path args for git-filter-repo
FILTER_ARGS=()
for path in "${TLS_PATHS[@]}"; do
  FILTER_ARGS+=(--path "$path")
done

echo "Paths to purge from history:"
for path in "${TLS_PATHS[@]}"; do
  REMAINING=$(git log --all --oneline -- "$path" | wc -l)
  echo "  $path (appears in $REMAINING commits)"
done
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[DRY-RUN] Would run:"
  echo "  git filter-repo ${FILTER_ARGS[*]} --invert-paths --force"
  echo ""
  echo "[DRY-RUN] After purge, would force-push all refs:"
  echo "  git push origin --force --all"
  echo "  git push origin --force --tags"
  exit 0
fi

echo "WARNING: This rewrites ALL branch history. Press Ctrl+C to cancel or Enter to continue."
read -r

cd "$REPO_ROOT"
git filter-repo "${FILTER_ARGS[@]}" --invert-paths --force

echo ""
echo "=== History rewrite complete ==="
echo ""
echo "Verifying purge..."
FOUND=false
for path in "${TLS_PATHS[@]}"; do
  REMAINING=$(git log --all --oneline -- "$path" 2>/dev/null | wc -l)
  if [[ "$REMAINING" -gt 0 ]]; then
    echo "FAIL: $path still appears in $REMAINING commits"
    FOUND=true
  else
    echo "PASS: $path purged from history"
  fi
done

if [[ "$FOUND" == "true" ]]; then
  echo ""
  echo "ERROR: Some paths were not fully purged. Investigate before force-pushing."
  exit 1
fi

echo ""
echo "=== Purge verified. Next steps ==="
echo "1. Force-push all branches to origin:"
echo "   git push origin --force --all"
echo "   git push origin --force --tags"
echo ""
echo "2. Notify all collaborators to re-clone or reset:"
echo "   git fetch origin && git reset --hard origin/<branch>"
echo ""
echo "3. Revoke the old certificate at the CA (see CA_REVOCATION_STEPS in this script)"
echo ""
echo "4. Provision new backstage TLS cert in AKV (backstage-tls-crt, backstage-tls-key)"
echo "   then: terraform apply -var backstage_tls_crt=... -var backstage_tls_key=..."
