#!/usr/bin/env bash
# setup-gha-environments.sh — create GitHub Environments with reviewer gates (US-V3-14, ADR-025-v3)
#
# Idempotent: safe to re-run.  Creates or updates each of the 7 env-state Environments.
#
# Requires:
#   - gh CLI authenticated with repo admin rights (gh auth login)
#   - The @platform-team GitHub team must exist in the organisation
#
# Usage:
#   bash scripts/setup-gha-environments.sh [--repo owner/repo] [--team org/team-slug]
#
# What it does per environment:
#   - Creates the GitHub Environment if it does not exist
#   - Sets deployment branch restriction to `main` only (blocks PR-branch deploys)
#   - Configures required reviewers: the @platform-team group
#   - Sets a 5-minute wait timer (gives reviewers time to respond before auto-cancel)
#
# After running this script, also configure per-environment OIDC subjects in Entra ID:
#   Federated credential subject for each environment:
#     repo:<org>/<repo>:environment:<env-name>
#   The 'gha-platform-ci' UAMI should have one federated credential per environment.
#   See ADR-030-v3 for the full OIDC credential model.

set -euo pipefail

REPO="${GITHUB_REPOSITORY:-}"
TEAM_SLUG="${GHA_REVIEWER_TEAM:-platform-team}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)  REPO="$2";      shift 2 ;;
    --team)  TEAM_SLUG="$2"; shift 2 ;;
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

ORG="${REPO%%/*}"
ENVIRONMENTS=(mgmt-we mgmt-ne dev staging prod-we prod-ne seed-wus)

# Reviewer wait timers (minutes) per environment risk level
declare -A WAIT_TIMER
WAIT_TIMER[mgmt-we]=10
WAIT_TIMER[mgmt-ne]=10
WAIT_TIMER[dev]=0
WAIT_TIMER[staging]=5
WAIT_TIMER[prod-we]=10
WAIT_TIMER[prod-ne]=10
WAIT_TIMER[seed-wus]=5

echo "Configuring GitHub Environments for ${REPO} ..."
echo "  Reviewer team: ${ORG}/${TEAM_SLUG}"
echo ""

# Resolve the team node_id for the reviewer payload
TEAM_NODE_ID="$(gh api "orgs/${ORG}/teams/${TEAM_SLUG}" -q '.node_id' 2>/dev/null || true)"
if [[ -z "${TEAM_NODE_ID}" ]]; then
  echo "WARNING: team '${ORG}/${TEAM_SLUG}' not found — environments will be created without reviewers."
  echo "  Manually add required reviewers in: https://github.com/${REPO}/settings/environments"
  TEAM_NODE_ID=""
fi

for ENV in "${ENVIRONMENTS[@]}"; do
  echo "  → ${ENV} (wait: ${WAIT_TIMER[$ENV]}m)"

  # Build reviewers payload (empty array if team not found)
  if [[ -n "${TEAM_NODE_ID}" ]]; then
    REVIEWERS_PAYLOAD="[{\"type\":\"Team\",\"reviewer\":{\"node_id\":\"${TEAM_NODE_ID}\"}}]"
  else
    REVIEWERS_PAYLOAD="[]"
  fi

  # Create or update the Environment via GitHub API
  gh api \
    --method PUT \
    --header "Accept: application/vnd.github+json" \
    "repos/${REPO}/environments/${ENV}" \
    --input - <<EOF
{
  "wait_timer": ${WAIT_TIMER[$ENV]},
  "prevent_self_review": true,
  "reviewers": ${REVIEWERS_PAYLOAD},
  "deployment_branch_policy": {
    "protected_branches": false,
    "custom_branch_policies": true
  }
}
EOF

  # Add main-only deployment branch policy
  gh api \
    --method POST \
    --header "Accept: application/vnd.github+json" \
    "repos/${REPO}/environments/${ENV}/deployment-branch-policies" \
    --field name="main" \
    --field type="branch" \
    2>/dev/null || true  # idempotent — 409 if already exists is fine

  echo "    done"
done

echo ""
echo "Environments configured. Next steps:"
echo ""
echo "  1. Provision per-environment OIDC subjects in Entra ID:"
echo "     For each env, add a federated credential on the 'gha-platform-ci' UAMI:"
echo "       subject: repo:${REPO}:environment:<env-name>"
echo "       issuer:  https://token.actions.githubusercontent.com"
echo "       audience: api://AzureADTokenExchange"
echo ""
echo "  2. Configure required status checks (day-1 gates):"
echo "     bash scripts/setup-branch-protection.sh --repo ${REPO}"
echo ""
echo "  3. Verify: open a PR against main and confirm the apply workflow"
echo "     cannot be triggered from a non-main branch."
