# TLS History Purge Runbook (PRD-v3.1 US-V3.1-02)

**Owner**: Platform Security Lead + Platform On-Call
**Closes**: `FR-V3-05` (originally v3 P0 finding F-06), `FR-V3.1-06..09`, OQ-V4-07
**Scope**: Destructive `git filter-repo` rewrite of the canonical remote
`https://github.com/sccvn-devops/aks-platform-engineering` to remove the
historical `backstage/etc/tls/tls.key` and `backstage/etc/tls/tls.crt`
blobs reachable from commits `7b07f95`, `9d04b71`, and `de9253b`.

> ⚠️ This runbook describes a destructive history rewrite + force-push
> on the canonical remote. The operator executing this MUST hold
> repository-admin rights on `sccvn-devops/aks-platform-engineering`
> and MUST have completed the 48-hour pre-cutover announcement
> (FR-V3.1-06) before starting the cutover steps.

---

## 0. Pre-flight inventory (read-only)

Run these from any working clone. They establish the "before" state
that the post-cutover verification will be compared against.

```bash
# Confirm the blobs are reachable on origin
git log --all -S"BEGIN PRIVATE KEY" --oneline
# Expected (2026-05-26):
#   de9253b Feat/prd v3 strengthen tf code quality (#3)
#   9d04b71 feat: [US-V3-05] - Purge TLS Material from Git History
#   7b07f95 Backstage integration.  (#102)

# Confirm working tree is clean of the blobs (.gitignore guards *.key/*.crt)
ls backstage/etc/tls/ 2>&1 || echo "OK: working tree clean"

# Affected refs (open PR source branches + tags carrying the blobs)
git for-each-ref --format='%(refname)' refs/remotes/origin refs/tags |
  while read -r ref; do
    if git log "$ref" -S"BEGIN PRIVATE KEY" --oneline | grep -q .; then
      echo "AFFECTED: $ref"
    fi
  done
```

Record the affected-ref list as **Appendix A** of the cutover ticket
before proceeding.

---

## 1. Pre-cutover window (FR-V3.1-06) — T-48h to T-0

| Step | Owner | Channel | Deliverable |
|---|---|---|---|
| 1.1 | Platform Security Lead | `#platform-announce`, `#platform-oncall`, email to active contributors (those with a commit in the last 90 days) | Announcement using the template in Appendix B below |
| 1.2 | Platform Security Lead | GitHub PR `dependabot` + CI integrations (Bitbucket → Jenkins webhooks; Backstage catalog importer; Renovate) | Email/Slack with affected refs + new HEAD SHA notification window |
| 1.3 | Platform On-Call | Status page (or pinned `#platform-announce` message) | Freeze banner: "Merges paused `T-0` → `T-0 + 60min` for TLS-history purge — see runbook" |
| 1.4 | All open-PR owners | (acknowledge) | Each open-PR owner acknowledges the rebase requirement in the announcement thread |

The announcement window MUST be **at least 48 hours** between
posting and cutover. Capture announcement timestamps in §6 of this
runbook below.

### Appendix B — Announcement template

```
Subject: [Action required] Git history rewrite + force-push on aks-platform-engineering at <T-0 ISO8601>

What: We are running `git filter-repo --invert-paths` to remove
backstage/etc/tls/tls.key and backstage/etc/tls/tls.crt from all
reachable refs on origin. Commits 7b07f95, 9d04b71, de9253b will be
rewritten; their SHAs WILL change.

When: <T-0 ISO8601>; expected duration 60 minutes; merge freeze
window: T-0 → T-0 + 60min.

Affected refs (minimum): main, feat/prd-v3-strengthen-tf-code-quality,
feat/prd-v4-deepening-improvement, plus open-PR source branches and
any tags reachable to the offending blobs (see Appendix A of the
cutover ticket).

Action required:
  - Open-PR owners: rebase after cutover; you'll get a follow-up
    notification with the new base SHA.
  - All contributors: re-clone or hard-reset after cutover (see
    re-clone runbook §5 below).
  - CI integrations: cache invalidation runs automatically (§4).

Link to runbook: docs/tls-history-purge-2026-05-runbook.md
```

---

## 2. Mirror-clone rewrite (FR-V3.1-07) — T-0 to T-0 + 15min

Run on a clean operator workstation (NOT the working dev copy). All
commands assume `bash`, `git ≥ 2.40`, and
`pip install git-filter-repo` (≥ 2.38) on PATH.

```bash
# 2.1 — Fresh mirror clone in a scratch directory
WORK=$(mktemp -d -t tls-purge-XXXX)
cd "$WORK"
git clone --mirror https://github.com/sccvn-devops/aks-platform-engineering.git
cd aks-platform-engineering.git

# 2.2 — Record pre-rewrite state (used for §3 and audit log)
git log --all -S"BEGIN PRIVATE KEY" --oneline > /tmp/tls-purge-before.txt
git for-each-ref --format='%(refname) %(objectname)' > /tmp/tls-purge-refs-before.txt

# 2.3 — The rewrite (exact command per FR-V3.1-07)
git filter-repo \
  --invert-paths \
  --path backstage/etc/tls/tls.key \
  --path backstage/etc/tls/tls.crt \
  --refs HEAD --refs refs/heads --refs refs/tags

# 2.4 — Verification (BOTH must return zero before §3)
git log --all -S"BEGIN PRIVATE KEY" --oneline | tee /tmp/tls-purge-after-log.txt
test ! -s /tmp/tls-purge-after-log.txt || { echo "FAIL: log -S still finds commits"; exit 1; }

git rev-list --all | xargs -r git grep -l "BEGIN PRIVATE KEY" 2>/dev/null | tee /tmp/tls-purge-after-grep.txt
test ! -s /tmp/tls-purge-after-grep.txt || { echo "FAIL: grep still finds blobs"; exit 1; }

echo "VERIFICATION CLEAN — proceed to §3"
```

`scripts/tls-history-purge-mirror.sh` automates §2.1 → §2.4 and exits
non-zero on any verification failure. The script MUST be the
authoritative execution path; this section documents the equivalent
manual steps for audit.

---

## 3. Branch-protection relax + force-push (FR-V3.1-08) — T-0 + 15min to T-0 + 35min

### 3.1 Temporarily allow force-push on `main`

```bash
# Snapshot current protection (audit artifact)
gh api repos/sccvn-devops/aks-platform-engineering/branches/main/protection \
  > /tmp/main-protection-before.json

# Toggle allow_force_pushes=true for the cutover window only
gh api -X PUT repos/sccvn-devops/aks-platform-engineering/branches/main/protection \
  --input - <<'JSON'
{
  "required_status_checks": null,
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": true,
  "allow_deletions": false
}
JSON
```

> The admin-override window opens here. Note the timestamp — this is
> the start of the audit-log entry required by AC7.

### 3.2 Force-push the rewritten refs

From the mirror clone (`$WORK/aks-platform-engineering.git`):

```bash
git push --force --all origin
git push --force --tags origin

# If tags were re-pointed (carrying the blobs), explicitly delete + re-push
git for-each-ref --format='%(refname:short)' refs/tags |
  while read -r tag; do
    git push origin ":refs/tags/$tag"
    git push origin "refs/tags/$tag"
  done
```

### 3.3 Re-instate branch protection

```bash
# Restore the pre-cutover protection from the snapshot in §3.1
# (uses jq to drop server-only fields before re-PUT)
jq '{
  required_status_checks: .required_status_checks,
  enforce_admins: .enforce_admins.enabled,
  required_pull_request_reviews: .required_pull_request_reviews,
  restrictions: .restrictions,
  allow_force_pushes: false,
  allow_deletions: false,
  required_linear_history: .required_linear_history.enabled,
  required_conversation_resolution: .required_conversation_resolution.enabled
}' /tmp/main-protection-before.json |
gh api -X PUT repos/sccvn-devops/aks-platform-engineering/branches/main/protection --input -

# Verify re-instated
gh api repos/sccvn-devops/aks-platform-engineering/branches/main/protection \
  | jq '{allow_force_pushes, enforce_admins}'
# Expect: allow_force_pushes.enabled == false, enforce_admins.enabled == true
```

Note the timestamp — this closes the admin-override window for the
audit log.

### 3.4 Post-push verification on a NEW clone

```bash
TMPCHECK=$(mktemp -d)
cd "$TMPCHECK"
git clone https://github.com/sccvn-devops/aks-platform-engineering.git verify-clone
cd verify-clone

# Security signal — PEM boundary form; this MUST return empty:
git log --all -S'-----BEGIN PRIVATE KEY-----' --oneline
git rev-list --all | xargs -r git grep -l -- '-----BEGIN PRIVATE KEY-----'
```

> **Marker form**: the PRD AC2 quotes the bare phrase `BEGIN PRIVATE
> KEY` (no dashes). That phrase ALSO appears in this runbook and in
> the PRD documents themselves (as quoted-command text), so the bare
> form will still return non-zero against the rewritten origin —
> those hits are documentation references, not key material. The PEM
> boundary form `-----BEGIN PRIVATE KEY-----` (5 dashes either side)
> is what uniquely identifies actual PEM material. Use the PEM
> boundary form as the green/red signal; `scripts/verify-tls-history-clean.sh`
> runs both and applies a docs-exclusion filter when reporting the
> AC-literal form.

Run `scripts/verify-tls-history-clean.sh` from this clone for the
deterministic green/red signal.

### 3.5 Open-PR rebase notification

For every open PR whose source branch was force-pushed:

```bash
gh pr list --state open --json number,headRefName,author |
  jq -r '.[] | "\(.number)\t\(.headRefName)\t\(.author.login)"' |
  while IFS=$'\t' read -r pr branch author; do
    gh pr comment "$pr" --body "🚨 Origin history was rewritten by US-V3.1-02 TLS history purge. Please rebase \`$branch\` onto the new \`main\`. Runbook: docs/tls-history-purge-2026-05-runbook.md. New main HEAD: $(git rev-parse origin/main)."
  done
```

---

## 4. GitHub Actions cache invalidation (FR-V3.1-09) — T-0 + 35min

```bash
# Purge all GHA caches that may reference the old SHAs
gh cache list --limit 1000 --json id |
  jq -r '.[].id' |
  xargs -I{} gh cache delete {} -R sccvn-devops/aks-platform-engineering

# Verify empty
gh cache list -R sccvn-devops/aks-platform-engineering
# Expect: "No caches found"
```

If `gh cache delete --all` is supported by the operator's `gh`
version, the single-command form `gh cache delete --all -R
sccvn-devops/aks-platform-engineering` is equivalent.

---

## 5. Post-cutover re-clone instructions (FR-V3.1-09) — published to contributors

Every contributor and CI runner must execute exactly **one** of the
following. Pin this in `#platform-announce` for at least 7 days.

### Option A — Full re-clone (recommended)

```bash
cd <parent-dir>
rm -rf aks-platform-engineering
git clone https://github.com/sccvn-devops/aks-platform-engineering.git
cd aks-platform-engineering
git log --all -S"BEGIN PRIVATE KEY" --oneline
# Expect: empty output
```

### Option B — Hard reset existing clone

```bash
cd aks-platform-engineering
git fetch origin --prune --prune-tags --tags --force
# For each local branch tracking a rewritten origin branch:
for b in $(git for-each-ref --format='%(refname:short)' refs/heads); do
  if git rev-parse --quiet --verify "origin/$b" >/dev/null; then
    git checkout "$b" && git reset --hard "origin/$b"
  fi
done
# Drop reflog + GC to remove cached old objects
git reflog expire --expire=now --all
git gc --prune=now --aggressive
git log --all -S"BEGIN PRIVATE KEY" --oneline
# Expect: empty output
```

CI runners (GitHub-hosted): runners are ephemeral — no action needed
beyond the cache invalidation in §4. Self-hosted runners must run
Option A.

---

## 6. Cert revocation re-confirmation (FR-V3.1-09) — within sprint

The original leaked certificate (`CN=backstage-pe-demo.com`,
self-signed; or whichever CA-issued cert was provisioned in v3) was
revoked per `FR-V3-05` step 2 in 2026 May. Per AC4 the revocation
status must be re-verified after the history purge and recorded
below.

```bash
# 6.1 — Identify the leaked cert's serial number from a working tree
#       fixture or AKV history (do NOT recover it from the rewritten
#       repo). Source: the original backstage/etc/tls/tls.crt at
#       commit 7b07f95 as captured in the v3 incident ticket.
SERIAL="<paste from incident ticket>"
CA_OCSP_URI="<paste from incident ticket>"   # blank for self-signed

# 6.2 — For CA-issued certs: OCSP query
if [[ -n "$CA_OCSP_URI" ]]; then
  openssl ocsp \
    -issuer /tmp/issuer.pem \
    -serial "0x$SERIAL" \
    -url "$CA_OCSP_URI" \
    -CAfile /tmp/issuer.pem \
    -resp_text
  # Expected: "Cert Status: revoked"; record Responder ID + thisUpdate timestamp
fi

# 6.3 — For self-signed certs: there is no CRL/OCSP. Confirm AKV no
#       longer holds the leaked cert version:
az keyvault certificate show \
  --vault-name kv-platform-mgmt-we \
  --name backstage-tls-crt \
  --query "x509ThumbprintHex" -o tsv
# Confirm this thumbprint != the leaked cert thumbprint from the v3
# incident ticket.
```

### Revocation record (operator: fill in at cutover)

| Field | Value |
|---|---|
| Cert subject CN | _e.g. backstage-pe-demo.com_ |
| Cert serial number | _hex from incident ticket_ |
| Issuing CA / self-signed | _self-signed_ |
| Revocation timestamp (UTC) | _from v3 incident ticket_ |
| OCSP/CRL re-query timestamp (UTC) | _2026-MM-DDThh:mm:ssZ_ |
| OCSP Responder ID (CA-issued only) | _from openssl ocsp -resp_text_ |
| Re-query response | _revoked / N/A (self-signed)_ |
| AKV current thumbprint | _from az keyvault certificate show_ |
| AKV current thumbprint != leaked | _yes / no_ |
| Operator | _GitHub handle_ |

---

## 7. Cutover audit log (operator: fill in at cutover)

| Event | UTC timestamp | Operator |
|---|---|---|
| Pre-cutover announcement sent | | |
| Merge freeze begins | | |
| Mirror clone created | | |
| `git filter-repo` completes | | |
| `git log -S"BEGIN PRIVATE KEY"` returns zero | | |
| `git rev-list \| git grep` returns zero | | |
| Branch protection on `main` relaxed (admin-override opens) | | |
| Force-push to origin completes | | |
| Open-PR rebase notifications sent | | |
| Branch protection on `main` re-instated (admin-override closes) | | |
| GHA cache purged | | |
| Cert revocation re-confirmed | | |
| Merge freeze lifted | | |

---

## 8. FR-V3-05 closure (AC8)

After §1–§7 are complete and §3.4 verification is clean on a fresh
origin clone, update the following two artifacts in the same PR (or
its immediate follow-up):

1. **PRD-v3.1** (`_docs/IDP-GitOps-Blueprint-PRD-v3.1.md`) — append
   to the "Status" section: `US-V3.1-02 closed <YYYY-MM-DD>; runbook
   §7 audit log at <commit SHA>; FR-V3-05 CLOSED.`

2. **PRD-v4** (`_docs/IDP-GitOps-Blueprint-PRD-v4.md`,
   §"Pre-existing v3 obligations" table) — flip the `FR-V3-05` row
   from `pending` / `v3.1 patch obligation` to `closed via PRD-v3.1
   US-V3.1-02 (<YYYY-MM-DD>)`.

---

## 9. Rollback

The history rewrite is reversible only via `git push --force` of the
pre-rewrite refs from a private operator backup. Before §3.2, the
operator MUST keep the pre-rewrite mirror clone (`$WORK/...`) for at
least the duration of the cutover window. If §3.4 verification or §6
revocation re-check fails, restore as follows:

```bash
cd "$WORK/aks-platform-engineering.git"
# Use the per-ref snapshot from §2.2 — restore each ref to its old SHA
while read -r ref oldsha; do
  case "$ref" in
    refs/heads/*|refs/tags/*)
      git push --force origin "$oldsha:$ref"
      ;;
  esac
done < /tmp/tls-purge-refs-before.txt

# Re-instate branch protection per §3.3
```

A rollback re-exposes the leaked blobs and re-opens `FR-V3-05`. Open
a SEV1 incident if invoked.
