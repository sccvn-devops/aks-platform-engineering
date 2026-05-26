#!/usr/bin/env bash
# tls-history-purge-mirror.sh — PRD-v3.1 US-V3.1-02 / FR-V3.1-07
#
# Executes the exact filter-repo command mandated by FR-V3.1-07 on a
# fresh MIRROR CLONE (never the working dev copy) and runs the two
# verification commands mandated by US-V3.1-02 AC2. Exits non-zero on
# any verification failure so the operator does NOT proceed to the
# force-push step.
#
# Companion runbook: docs/tls-history-purge-2026-05-runbook.md §2.
#
# PREREQUISITES (operator workstation):
#   - bash >= 4
#   - git >= 2.40
#   - git-filter-repo >= 2.38 on PATH (pip install git-filter-repo)
#   - jq (for §2.2 snapshot)
#
# USAGE:
#   bash scripts/tls-history-purge-mirror.sh \
#     --remote https://github.com/sccvn-devops/aks-platform-engineering.git \
#     [--workdir /tmp/tls-purge-XXXX] \
#     [--keep]
#
#   --remote   Canonical remote URL (default: this repo's origin)
#   --workdir  Directory to create the mirror in (default: mktemp)
#   --keep     Do NOT delete the mirror after success (needed for §3 force-push)
#
# The script does NOT push. After it exits 0, the operator runs the
# §3 force-push steps from the runbook using the printed mirror path.

set -euo pipefail

DEFAULT_REMOTE="https://github.com/sccvn-devops/aks-platform-engineering.git"
REMOTE="${DEFAULT_REMOTE}"
WORKDIR=""
KEEP=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote)  REMOTE="$2"; shift 2 ;;
    --workdir) WORKDIR="$2"; shift 2 ;;
    --keep)    KEEP=true; shift ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; exit 2 ;;
  esac
done

# Preflight
if ! command -v git-filter-repo >/dev/null 2>&1; then
  echo "ERROR: git-filter-repo not on PATH (install: pip install git-filter-repo)" >&2
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git not on PATH" >&2
  exit 1
fi

if [[ -z "$WORKDIR" ]]; then
  WORKDIR=$(mktemp -d -t tls-purge-XXXX)
fi
mkdir -p "$WORKDIR"
cd "$WORKDIR"

cleanup() {
  local rc=$?
  if [[ "$KEEP" == "false" && $rc -eq 0 ]]; then
    rm -rf "$WORKDIR"
  fi
  exit $rc
}
trap cleanup EXIT

echo "=== TLS history mirror-clone purge (US-V3.1-02 / FR-V3.1-07) ==="
echo "Remote:  $REMOTE"
echo "Workdir: $WORKDIR"
echo ""

# §2.1 — Fresh mirror clone
echo "[2.1] Mirror cloning..."
git clone --mirror "$REMOTE" repo.git
cd repo.git

# §2.2 — Snapshot pre-rewrite state
echo "[2.2] Snapshotting pre-rewrite state..."
git log --all -S"BEGIN PRIVATE KEY" --oneline > "$WORKDIR/tls-purge-before.txt"
git for-each-ref --format='%(refname) %(objectname)' \
  > "$WORKDIR/tls-purge-refs-before.txt"

BEFORE_COUNT=$(wc -l < "$WORKDIR/tls-purge-before.txt")
echo "    pre-rewrite commits matching BEGIN PRIVATE KEY: $BEFORE_COUNT"
if [[ "$BEFORE_COUNT" -eq 0 ]]; then
  echo ""
  echo "NOTE: origin already shows zero matching commits. No rewrite needed."
  echo "      Mirror retained at: $WORKDIR/repo.git (for audit)"
  KEEP=true
  exit 0
fi

# §2.3 — The rewrite (exact command per FR-V3.1-07)
echo "[2.3] Running git filter-repo..."
git filter-repo \
  --invert-paths \
  --path backstage/etc/tls/tls.key \
  --path backstage/etc/tls/tls.crt \
  --refs HEAD --refs refs/heads --refs refs/tags

# §2.4 — Verification. Per AC2 the canonical commands are:
#   git log --all -S"BEGIN PRIVATE KEY"        → MUST return zero
#   git rev-list --all | xargs git grep -l "BEGIN PRIVATE KEY"  → MUST return zero
# However the PRD documents themselves contain the literal phrase
# "BEGIN PRIVATE KEY" as part of their AC text, so the bare phrase
# will continue to match docs blobs even after a successful purge.
# The PEM boundary form `-----BEGIN PRIVATE KEY-----` (5 dashes either
# side) uniquely identifies actual PEM material and is what the AC
# actually intends. We run both forms here:
#   - PEM-boundary form is the security gate (MUST be zero, blocks force-push)
#   - AC-literal form is reported for audit; PRD-doc blobs are
#     filtered via DOC_EXCLUDE to give a meaningful number
echo "[2.4] Verifying purge..."

DOC_EXCLUDE='_docs/.*\.md|docs/tls-history-purge-2026-05-runbook\.md|scripts/(verify-tls-history-clean|tls-history-purge-mirror|purge-tls-history)\.sh|prd\.json|progress\.txt'

# Security gate — PEM boundary form
git log --all -S'-----BEGIN PRIVATE KEY-----' --oneline \
  > "$WORKDIR/tls-purge-after-log.txt"
LOG_COUNT=$(wc -l < "$WORKDIR/tls-purge-after-log.txt")
if [[ "$LOG_COUNT" -ne 0 ]]; then
  echo ""
  echo "FAIL: git log --all -S'-----BEGIN PRIVATE KEY-----' (PEM boundary) still returns $LOG_COUNT commits:" >&2
  cat "$WORKDIR/tls-purge-after-log.txt" >&2
  echo "" >&2
  echo "Do NOT proceed to the §3 force-push. Investigate." >&2
  KEEP=true
  exit 1
fi
echo "    PASS: git log -S '-----BEGIN PRIVATE KEY-----' returns zero commits"

git rev-list --all \
  | xargs -r git grep -l -- '-----BEGIN PRIVATE KEY-----' 2>/dev/null \
  > "$WORKDIR/tls-purge-after-grep.txt" || true
GREP_COUNT=$(wc -l < "$WORKDIR/tls-purge-after-grep.txt")
if [[ "$GREP_COUNT" -ne 0 ]]; then
  echo ""
  echo "FAIL: git rev-list | git grep -- '-----BEGIN PRIVATE KEY-----' still finds $GREP_COUNT blob matches:" >&2
  cat "$WORKDIR/tls-purge-after-grep.txt" >&2
  echo "" >&2
  echo "Do NOT proceed to the §3 force-push. Investigate." >&2
  KEEP=true
  exit 1
fi
echo "    PASS: git rev-list | git grep -- '-----BEGIN PRIVATE KEY-----' returns zero matches"

# Audit reporting — AC-literal form with docs filter
git log --all -S"BEGIN PRIVATE KEY" --oneline \
  > "$WORKDIR/tls-purge-after-log-literal.txt"
LIT_LOG=$(wc -l < "$WORKDIR/tls-purge-after-log-literal.txt")
git rev-list --all \
  | xargs -r git grep -l "BEGIN PRIVATE KEY" 2>/dev/null \
  | grep -vE ":($DOC_EXCLUDE)\$" \
  > "$WORKDIR/tls-purge-after-grep-non-docs.txt" || true
NON_DOC=$(wc -l < "$WORKDIR/tls-purge-after-grep-non-docs.txt")
if [[ "$NON_DOC" -ne 0 ]]; then
  echo ""
  echo "FAIL: AC-literal phrase 'BEGIN PRIVATE KEY' still found in $NON_DOC non-doc blobs:" >&2
  cat "$WORKDIR/tls-purge-after-grep-non-docs.txt" >&2
  echo "" >&2
  echo "Do NOT proceed to the §3 force-push. Investigate." >&2
  KEEP=true
  exit 1
fi
echo "    PASS: AC-literal form returns zero non-doc blob matches"
echo "    NOTE: $LIT_LOG commits still match the bare phrase 'BEGIN PRIVATE KEY' (PRD docs that quote AC text)"

# Force --keep so the operator can run the §3 force-push from the mirror
KEEP=true

echo ""
echo "=== VERIFICATION CLEAN ==="
echo "Mirror retained at: $WORKDIR/repo.git"
echo "Snapshots at:       $WORKDIR/tls-purge-{before,refs-before,after-log,after-grep}.txt"
echo ""
echo "Next: §3 of docs/tls-history-purge-2026-05-runbook.md"
echo "      (relax branch protection → force-push → re-instate protection)"
