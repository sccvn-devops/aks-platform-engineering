#!/usr/bin/env bash
# verify-tls-history-clean.sh — PRD-v3.1 US-V3.1-02 AC2 / AC3 / AC5
#
# Run from a FRESH clone of origin to confirm the TLS history purge
# has been applied to the canonical remote. Exits 0 on green, 1 on
# any finding.
#
# Companion runbook: docs/tls-history-purge-2026-05-runbook.md §3.4 and §5.
#
# USAGE:
#   bash scripts/verify-tls-history-clean.sh
#
# Recommended invocation (CI runners / re-clone validation):
#   git clone https://github.com/sccvn-devops/aks-platform-engineering.git /tmp/verify
#   bash /tmp/verify/scripts/verify-tls-history-clean.sh
#
# CI integration: add this script as a non-blocking job in
# terraform-ci.yml for the first sprint after cutover (advisory),
# then flip to blocking from sprint-2 onward.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

RED=$'\e[31m'
GREEN=$'\e[32m'
RESET=$'\e[0m'
if [[ ! -t 1 ]]; then RED=""; GREEN=""; RESET=""; fi

echo "=== TLS history verification (US-V3.1-02 AC2/AC3/AC5) ==="
echo "Repository: $REPO_ROOT"
echo "Origin:     $(git config --get remote.origin.url 2>/dev/null || echo '(no origin)')"
echo "HEAD:       $(git rev-parse HEAD 2>/dev/null || echo '(unknown)')"
echo ""

# Marker note: the PRD AC2 quotes "BEGIN PRIVATE KEY" (no dashes) as
# the search string. The PRDs and this runbook themselves contain that
# literal phrase as documentation, so the bare form returns false
# positives. The PEM boundary `-----BEGIN PRIVATE KEY-----` (5 dashes
# either side) uniquely identifies actual PEM material and is what the
# AC actually intends. We run BOTH:
#   - the AC-literal form (with docs-exclusion filter for known doc paths)
#   - the PEM-boundary form (no exclusions; this is the security signal)
DOC_EXCLUDE='_docs/.*\.md|docs/tls-history-purge-2026-05-runbook\.md|scripts/(verify-tls-history-clean|tls-history-purge-mirror|purge-tls-history)\.sh|prd\.json|progress\.txt'

fail=0

# Check 1 — PEM boundary content-search via git log -S (security signal)
echo "[1] git log --all -S\"-----BEGIN PRIVATE KEY-----\" (PEM boundary) ..."
matches_log_pem=$(git log --all -S'-----BEGIN PRIVATE KEY-----' --oneline 2>/dev/null || true)
if [[ -n "$matches_log_pem" ]]; then
  echo "${RED}FAIL${RESET}: commits still reference an actual PEM private-key block:"
  echo "$matches_log_pem" | sed 's/^/    /'
  fail=1
else
  echo "${GREEN}PASS${RESET}: zero commits with PEM blocks"
fi

# Check 2 — PEM boundary blob content via rev-list | grep (security signal)
echo "[2] git rev-list --all | xargs git grep -l \"-----BEGIN PRIVATE KEY-----\" ..."
matches_grep_pem=$(git rev-list --all 2>/dev/null \
  | xargs -r git grep -l -- '-----BEGIN PRIVATE KEY-----' 2>/dev/null || true)
if [[ -n "$matches_grep_pem" ]]; then
  echo "${RED}FAIL${RESET}: blobs still contain a PEM private-key block (first 5):"
  echo "$matches_grep_pem" | head -5 | sed 's/^/    /'
  fail=1
else
  echo "${GREEN}PASS${RESET}: zero blob matches with PEM boundary"
fi

# Check 2b — AC-literal form with docs-exclusion (audit confirmation)
echo "[2b] AC-literal git log --all -S\"BEGIN PRIVATE KEY\" minus known doc paths ..."
matches_log_lit=$(git log --all -S"BEGIN PRIVATE KEY" --oneline 2>/dev/null || true)
matches_grep_lit=$(git rev-list --all 2>/dev/null \
  | xargs -r git grep -l "BEGIN PRIVATE KEY" 2>/dev/null \
  | grep -vE ":($DOC_EXCLUDE)\$" || true)
if [[ -n "$matches_grep_lit" ]]; then
  echo "${RED}FAIL${RESET}: literal-phrase matches outside known doc paths (first 5):"
  echo "$matches_grep_lit" | head -5 | sed 's/^/    /'
  fail=1
else
  # Report how many literal matches we filtered (informational only)
  lit_total=$(echo "$matches_log_lit" | grep -c . || true)
  echo "${GREEN}PASS${RESET}: zero non-doc blob matches (filtered $lit_total commit refs in known doc paths)"
fi

# Check 3 — the original v3 incident commits MUST NOT be reachable
echo "[3] Checking original v3 incident commits 7b07f95, 9d04b71, de9253b ..."
for sha in 7b07f95 9d04b71 de9253b; do
  if git cat-file -e "$sha^{commit}" 2>/dev/null; then
    # Reachable from any ref?
    reachable_refs=$(git for-each-ref --contains "$sha" --format='%(refname)' 2>/dev/null || true)
    if [[ -n "$reachable_refs" ]]; then
      echo "${RED}FAIL${RESET}: $sha is reachable from:"
      echo "$reachable_refs" | sed 's/^/    /'
      fail=1
    else
      echo "    NOTE: $sha exists as a dangling object but is unreachable from any ref (GC will collect)"
    fi
  else
    echo "${GREEN}PASS${RESET}: $sha not present in object store"
  fi
done

echo ""
if [[ $fail -eq 0 ]]; then
  echo "${GREEN}=== ALL CHECKS PASSED ===${RESET}"
  echo "FR-V3-05 closure verified against this clone."
  exit 0
else
  echo "${RED}=== VERIFICATION FAILED ===${RESET}"
  echo "Origin still contains TLS history. Re-run scripts/tls-history-purge-mirror.sh"
  echo "or, if origin is already purged, re-clone (see runbook §5)."
  exit 1
fi
