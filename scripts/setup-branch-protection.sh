#!/usr/bin/env bash
# setup-branch-protection.sh — configure GitHub branch protection for phased gates (US-V3-13, ADR-025-v3)
#
# Idempotent: safe to re-run.  Re-running updates the rule to the current config.
#
# Requires: gh CLI authenticated with repo admin rights.
#   gh auth login
#   bash scripts/setup-branch-protection.sh [--repo owner/repo] [--sprint2]
#
# Without --sprint2 (default — sprint-1 advisory phase):
#   Required checks: Terraform Format, Terraform Validate, TFLint
#   Checkov is advisory (NOT a required check; warnings visible in Checks tab).
#
# With --sprint2 (sprint-2 blocking phase):
#   Required checks: Terraform Format, Terraform Validate, TFLint, Checkov Security Scan
#   Checkov failures block merge.  Also flip soft-fail in .checkov.yaml → false.

set -euo pipefail

REPO="${GITHUB_REPOSITORY:-}"
SPRINT2=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)    REPO="$2";    shift 2 ;;
    --sprint2) SPRINT2=true; shift   ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

if [[ -z "${REPO}" ]]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [[ -z "${REPO}" ]]; then
  echo "ERROR: cannot determine repo. Set GITHUB_REPOSITORY or pass --repo owner/repo"
  exit 1
fi

echo "Configuring branch protection for ${REPO} main ..."
echo "  Sprint-2 mode: ${SPRINT2}"

# Day-1 required checks (always blocking per ADR-025-v3 decision #4)
REQUIRED_CHECKS='["Terraform Format","Terraform Validate","TFLint"]'

if [[ "${SPRINT2}" == "true" ]]; then
  # Sprint-2: add Checkov as a required check (blocking)
  # Also ensure soft-fail: false is set in .checkov.yaml
  REQUIRED_CHECKS='["Terraform Format","Terraform Validate","TFLint","Checkov Security Scan"]'
  echo ""
  echo "  REMINDER: also flip soft-fail in .checkov.yaml from true → false and commit."
  echo ""
fi

# Build the branch protection payload using gh api
gh api \
  --method PUT \
  --header "Accept: application/vnd.github+json" \
  "repos/${REPO}/branches/main/protection" \
  --input - <<EOF
{
  "required_status_checks": {
    "strict": true,
    "contexts": ${REQUIRED_CHECKS}
  },
  "enforce_admins": false,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": true
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_linear_history": false,
  "required_conversation_resolution": true
}
EOF

echo ""
echo "Branch protection applied."
echo "Required checks (blocking):   ${REQUIRED_CHECKS}"
if [[ "${SPRINT2}" != "true" ]]; then
  echo "Advisory only (not required): Checkov Security Scan"
  echo ""
  echo "To promote to sprint-2 blocking:"
  echo "  1. Run: checkov -d terraform/ --config-file .checkov.yaml --create-baseline"
  echo "  2. Commit updated .checkov.baseline (PR reviewed by @platform-team via CODEOWNERS)"
  echo "  3. In .checkov.yaml: change soft-fail: true → soft-fail: false"
  echo "  4. Run: bash scripts/setup-branch-protection.sh --sprint2"
fi
