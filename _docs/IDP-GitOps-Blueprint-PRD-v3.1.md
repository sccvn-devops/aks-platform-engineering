# PRD: IDP GitOps Blueprint v3.1 — Pre-v4 Hardening Patch

**Version:** 3.1
**Date:** 2026-05-26
**Author:** Platform Engineering
**Status:** Draft — pending v3.1 readiness review (must close before PRD-v4 P0 begins)

---

## Purpose

PRD-v3.1 is a small **maintenance patch** to `IDP-GitOps-Blueprint-PRD-v3.md`
(v3.0). It closes a single FR-V3 gap surfaced during the PRD-v4 readiness
verification on 2026-05-26: `FR-V3-12` (AKV-native quarterly rotation
policies) was never implemented in Terraform.

PRD-v3.1 is additive — it does not modify any v2/v3 ADR or any v3 functional
requirement. It introduces one new `FR-V3.1-NN` / `US-V3.1-NN` namespace
entry covering the rotation-policy closure.

PRD-v3.1 must land before PRD-v4 P0 begins. PRD-v4's `FR-V4-36`..`FR-V4-40`
(Helm → ESO migration) assume AKV-native rotation is in place for
operator-rotated secrets; without v3.1, the v4 secrets migration story
would inherit the v3 gap rather than close it.

**v3.1 now also covers TLS history purge (`FR-V3-05`).** The PRD-v4
readiness verification (2026-05-26) found that the `git filter-repo`
history rewrite was never performed — `BEGIN PRIVATE KEY` blobs remain
reachable on `origin/main` in commits `7b07f95`, `9d04b71`, and
`de9253b`. The user decision on 2026-05-26 (OQ-V4-07) chose the full
remediation path: destructive history rewrite + force push + cert
re-revocation confirmation + downstream re-clone runbook. This work
is captured in `FR-V3.1-06..09` and `US-V3.1-02` below.

---

## Scope

### In Scope (v3.1 additions only)

| Area | Addition |
|---|---|
| AKV rotation policies | Declarative `rotation_policy` blocks on platform-managed AKV secrets and certificates (TLS, third-party tokens, system passwords) so that AKV rotates them on a quarterly schedule independent of the `saas-token-rotator` runtime path |
| TLS history purge | Destructive `git filter-repo` rewrite removing `backstage/etc/tls/tls.key` and `backstage/etc/tls/tls.crt` from all refs; coordinated force-push; cert re-revocation confirmation at issuing CA; downstream re-clone runbook |

### Out of Scope (v3.1)

| Area | Reason |
|---|---|
| Anything from PRD-v4 (FR-V4-NN) | v4 is a separate document; v3.1 is strictly a v3 patch |
| Crossplane UAMI scope-down | Accepted as documented exception via ADR-024-v3 Amendment 2026-05-26; not a v3.1 item |
| Replacing `saas-token-rotator` | The tool handles SaaS-side rotation (Jira, Bitbucket); AKV-native policies cover AKV-side rotation. They are complementary, not overlapping |
| Re-issuing a new cert with new key material | The original certificate is already revoked per FR-V3-05's step 2; v3.1 confirms the revocation but does not issue a replacement (that flow already exists via FR-V3-06 AKV-issued certs) |

---

## Background

A PRD-v4 readiness verification on 2026-05-26 ran an automated audit
across all v3 functional requirements and discovered two gaps:

1. **`FR-V3-12` — AKV-native quarterly rotation: ABSENT.**
   `terraform/keyvaults.tf` (~120 lines) contains `azurerm_key_vault`
   resources but **zero `rotation_policy` blocks** on any
   `azurerm_key_vault_certificate` resource and zero `expiration_date` /
   rotation metadata on any `azurerm_key_vault_secret`. The
   `saas-token-rotator` tool handles application-level token rotation at
   runtime (Bitbucket/Jira OAuth flows) but the AKV-native quarterly
   rotation policy for platform secrets and certificates was never
   provisioned via Terraform.

2. **`FR-V3-05` — TLS history purge: RED.**
   - Working tree is clean (`backstage/etc/tls/` does not exist).
   - `.gitignore` includes `*.key` and `*.crt`.
   - **But** `git log --all -S"BEGIN PRIVATE KEY"` returns three commits:
     - `7b07f95` — original "Backstage integration" commit that introduced
       the TLS material.
     - `9d04b71` — `feat: [US-V3-05] - Purge TLS Material from Git History`
       — a `git rm` commit. **This is not a history rewrite.** The diff
       still contains the key material, and the blob is reachable via
       `git show 7b07f95:backstage/etc/tls/tls.key`.
     - `de9253b` — the v3 merge commit that inherits both above.
   - Net: the historical blob remains recoverable from the canonical
     remote (`origin/main`) on `https://github.com/sccvn-devops/aks-platform-engineering`.
     The certificate associated with the leaked key was revoked per
     FR-V3-05 step 2, but the recoverable blob still violates the
     intent of the original FR.

The user decision on 2026-05-26 (OQ-V4-07) chose **full remediation**
over accept-with-revoked-cert. PRD-v3.1 carries that closure as
`FR-V3.1-06..09`.

The v3 PRD's `FR-V3-12` text mandated "AKV-native quarterly rotation for
platform-managed secrets and certificates" without specifying which
resources fall under "platform-managed." This ambiguity contributed to the
gap; PRD-v3.1 closes both the implementation and the definition.

---

## Functional Requirements

### AKV-Native Rotation (closes `FR-V3-12`)

- **FR-V3.1-01** — Every platform-managed AKV certificate (TLS for
  Backstage, TLS for Jenkins, TLS for any DX tool exposed via private
  endpoint) must declare an `azurerm_key_vault_certificate` resource
  with a `certificate_policy.lifetime_action` block of action
  `AutoRenew`, triggered `lifetime_percentage = 75` (i.e., renew at 75%
  of validity). Validity period must be 90 days for short-lived service
  certs and 365 days for root/intermediate material.

- **FR-V3.1-02** — Every platform-managed AKV secret (Jenkins admin
  password, PostgreSQL admin password, internal service tokens) must
  declare an `azurerm_key_vault_secret` resource with an
  `expiration_date` set to a quarterly boundary 90 days from creation,
  and a companion `azurerm_role_assignment` granting the rotation
  workload identity (`uami-saas-token-rotator` or its successor) the
  necessary write permission on a *per-secret-name-prefix* basis (never
  vault-wide).

- **FR-V3.1-03** — Operator-rotated secrets (third-party API tokens not
  generated by the platform — e.g., Atlassian Cloud API tokens) must
  declare an `azurerm_key_vault_secret` with `expiration_date` set
  quarterly and an `azurerm_monitor_metric_alert` that fires 14 days
  before expiry, routed to the platform on-call channel. AKV does not
  auto-rotate operator-owned material; the alert closes the loop.

- **FR-V3.1-04** — A CI assertion (added to
  `.github/workflows/terraform-ci.yml` or extended from the existing
  scope-check job) must run `az keyvault secret list --vault-name ...
  --query "[?attributes.expires==null]"` against every platform AKV in
  every environment and **fail the CI build** if any platform-tagged
  secret returns a null expiry date. The assertion is advisory in
  sprint-1 (after merge of US-V3.1-01) and blocking from sprint-2
  onward — matching the v3 phased-blocking-gates pattern (`ADR-025-v3`).

- **FR-V3.1-05** — `terraform/keyvaults.tf` must add a top-of-file
  comment block listing every secret/certificate name the platform
  declares, its mode (`auto-rotated` / `operator-rotated`), its
  expiry-policy reference, and the role-assignment-scope that rotates
  it. This is a static catalogue, not a code abstraction — it
  documents the contract a reviewer enforces at PR time.

### TLS History Purge (closes `FR-V3-05`)

- **FR-V3.1-06** — A pre-cutover window of **at least 48 hours** must
  be announced to all active contributors and to every CI integration
  consuming the repo before the rewrite executes. The announcement
  must specify: the exact cutover time, the affected refs (all
  branches + tags + `refs/pull/*` that contain the offending blobs),
  the freeze window during which no merges may happen, and a link to
  the re-clone runbook (FR-V3.1-09).

- **FR-V3.1-07** — The history rewrite must run on a **fresh mirror
  clone** (not the working developer copy), using `git filter-repo
  --invert-paths --path backstage/etc/tls/tls.key --path
  backstage/etc/tls/tls.crt --refs HEAD --refs refs/heads --refs
  refs/tags`. Verification after the rewrite: `git log --all -S"BEGIN
  PRIVATE KEY"` must return **zero** commits, and `git rev-list --all
  | xargs git grep -l "BEGIN PRIVATE KEY" 2>/dev/null` must return
  **zero** matches. Only after both verifications return clean may
  the force-push proceed.

- **FR-V3.1-08** — The force-push must target the canonical remote
  (`origin` on `https://github.com/sccvn-devops/aks-platform-engineering`)
  for **every branch** that previously contained the offending blobs
  (minimum: `main`, `feat/prd-v3-strengthen-tf-code-quality`,
  `feat/prd-v4-deepening-improvement`, plus any active feature
  branches). Open PRs whose source branches are affected must be
  rebased after the force-push by their owners. Tags carrying the
  offending blobs must be re-pointed (delete + re-create).

- **FR-V3.1-09** — Post-cutover obligations within the same sprint:
  - **Cert revocation re-verified** at the issuing CA via OCSP/CRL
    query; revocation status recorded in `docs/seed-cluster-dr-runbook.md`
    or a new `docs/tls-history-purge-2026-05-runbook.md`.
  - **Re-clone runbook published** describing the exact local steps
    every contributor and every CI runner must execute (`rm -rf` of
    local clones + fresh `git clone`).
  - **GitHub Actions cache invalidation**: any cached refs/objects
    referencing the old SHAs must be purged (`gh cache delete --all`
    or equivalent per-workflow cache resets).
  - **Branch-protection ruleset audit**: re-confirm that branch
    protection on `main` still applies after the force-push (force-pushes
    require admin override; protection must be re-instated immediately
    after the cutover).

---

## User Stories

All acceptance criteria use **Given / When / Then** per the standing
PRD-v4 convention; v3.1 inherits the same testing discipline.

### US-V3.1-01: AKV-Native Quarterly Rotation Policy (FR-V3.1-01..05)

**Description:** As a security-conscious platform engineer, I want
AKV-native quarterly rotation policies declared in Terraform for every
platform secret and certificate, so that key rotation is enforced by AKV
itself (not dependent on `saas-token-rotator` runtime availability) and
verified at PR time by a CI assertion.

**Acceptance Criteria:**

- **Given** `terraform/keyvaults.tf` declares
  `azurerm_key_vault_certificate` resources for every platform TLS
  certificate (Backstage, Jenkins, any DX tool with a private endpoint),
  **When** `terraform apply` runs in any environment,
  **Then** each cert resource carries a `certificate_policy.lifetime_action`
  block of action `AutoRenew` at `lifetime_percentage = 75` and the
  rendered certificate carries the AKV-issued thumbprint observable via
  `az keyvault certificate show`.

- **Given** `terraform/keyvaults.tf` declares
  `azurerm_key_vault_secret` resources for every platform-generated
  secret (Jenkins admin password, PostgreSQL admin password,
  internal-service tokens),
  **When** `terraform plan` runs,
  **Then** every secret resource has `expiration_date` set to a
  quarterly boundary 90 days from creation, and the rotation UAMI's
  role assignment is scoped by AKV secret-name prefix (not vault-wide).

- **Given** the CI assertion from FR-V3.1-04 runs in advisory mode for
  the first sprint after US-V3.1-01 lands,
  **When** the assertion finds a platform-tagged secret with
  null expiry,
  **Then** the CI job posts a warning comment on the PR but does not
  fail; from sprint-2 onward the same finding fails the PR.

- **Given** an operator-rotated secret reaches 14 days before its
  declared expiry,
  **When** the `azurerm_monitor_metric_alert` fires,
  **Then** the platform on-call channel receives the alert with the
  secret name, vault name, expiry date, and a link to the rotation
  runbook.

- **Given** the top-of-file catalogue from FR-V3.1-05 lists every
  platform secret and certificate,
  **When** a PR adds a new `azurerm_key_vault_secret` or
  `azurerm_key_vault_certificate` resource,
  **Then** the PR also updates the catalogue and the CI lints the file
  to confirm the new resource is listed.

- **Given** `tflint`, `terraform validate`, and the CI rotation
  assertion all run,
  **Then** all three pass with zero findings.

### US-V3.1-02: TLS History Purge via `git filter-repo` (FR-V3.1-06..09)

**Description:** As a security engineer, I want the original
`backstage/etc/tls/tls.key` and `backstage/etc/tls/tls.crt` blobs
removed from all reachable refs on the canonical remote via
`git filter-repo`, with cert revocation re-confirmed and a re-clone
runbook published, so that historical key material is no longer
recoverable from `origin` and the FR-V3-05 contract is honored
end-to-end.

**Acceptance Criteria:**

- **Given** the 48-hour pre-cutover window announcement from FR-V3.1-06
  has been sent to all active contributors,
  **When** the cutover time arrives,
  **Then** all merges are paused for the duration of the rewrite +
  force-push window and a status page (or pinned chat message)
  reflects the freeze.

- **Given** the rewrite runs on a fresh mirror clone per FR-V3.1-07,
  **When** the operator runs `git filter-repo --invert-paths --path
  backstage/etc/tls/tls.key --path backstage/etc/tls/tls.crt` followed
  by the verification commands,
  **Then** `git log --all -S"BEGIN PRIVATE KEY"` returns zero commits
  and the verification `git rev-list --all | xargs git grep -l "BEGIN
  PRIVATE KEY" 2>/dev/null` returns zero matches.

- **Given** verification is clean,
  **When** the operator force-pushes to `origin` for every affected
  branch and re-points tags,
  **Then** `git log --all -S"BEGIN PRIVATE KEY"` against a fresh
  clone of `origin` returns zero commits AND open-PR owners receive
  a re-base notification with the runbook link.

- **Given** the cert revocation must be re-confirmed,
  **When** an operator queries OCSP/CRL for the original cert serial
  number,
  **Then** the response is `revoked` and the timestamp + responder
  identity are recorded in the new TLS-purge runbook.

- **Given** the post-cutover runbook is published,
  **When** every CI runner and contributor re-clones,
  **Then** their local working tree contains no commit reachable to
  the original `7b07f95`'s tls blob and `git log` reflects the rewritten
  history.

- **Given** branch protection on `main` was relaxed for the force-push,
  **When** the cutover completes,
  **Then** branch protection is re-instated within the same sprint
  and an audit-log entry confirms the temporary admin-override window.

- **Given** all of the above pass,
  **Then** `FR-V3-05` is marked CLOSED in the v3 readiness verification
  artifact and PRD-v4 §Pre-existing v3 obligations entry for FR-V3-05
  is updated from "pending" to "closed via PRD-v3.1 US-V3.1-02."

---

## Non-Goals

- **No re-architecture of `saas-token-rotator`.** The tool's SaaS-side
  rotation (Atlassian OAuth) is complementary to AKV-native rotation
  declared here; both stay.
- **No customer-managed key (CMK) adoption for AKV itself.** v3 deferred
  CMK to roadmap; v3.1 holds the same deferral.
- **No change to ADR-005-v2** (ESO + per-region AKV pair) — v3.1
  operates entirely within that ADR's bounds.

---

## Technical Considerations

### Phasing

| Phase | Stories | Rationale |
|---|---|---|
| **v3.1 — Single sprint** | US-V3.1-01 (FR-V3.1-01..05) | Atomic; no incremental partial-state |

This is one small story; no multi-phase rollout needed.

### Testing

`terraform validate` + `tflint` clean. The CI rotation assertion lives
as a new job in `.github/workflows/terraform-ci.yml`. No `terraform
test` block is required at v3.1 scope (the assertion provides the
verification). Coverage extends through the existing v3 CI matrix.

### Risks

| ID | Risk | Mitigation |
|---|---|---|
| R-V3.1-1 | **Rotation triggers chart restart loop** — newly rotated AKV cert propagates via ESO and triggers a `helm_release` re-roll | `ExternalSecret.refreshInterval` set conservatively (15m); chart `values.yaml` uses `existingSecret` not inline values; restart on rotation is documented behavior, not regression |
| R-V3.1-2 | **Operator misses the 14-day expiry alert** | Alert routes to a paged channel (not silent); runbook is in `docs/management-plane-failover-runbook.md` (extended in US-V3.1-01); quarterly DR drill exercises rotation |
| R-V3.1-3 | **CI assertion blocks legitimate ad-hoc secrets** | Annotation convention: secrets tagged `platform-managed=false` are exempt from the assertion; explicit, reviewer-readable opt-out |
| R-V3.1-4 | **Force-push breaks existing clones and downstream automation** when purging `tls.key` history | FR-V3.1-06 mandates a 48h advance window with re-clone runbook (FR-V3.1-09); freeze merges during the window; CI runners cache-invalidated via `gh cache delete --all`; branch protection re-instated post-cutover. Mirrors PRD-v3 R-3 mitigation pattern |
| R-V3.1-5 | **Force-push collides with v4 P0 branches** mid-rewrite | Cutover scheduled before any v4 P0 PR opens; freeze window blocks new branches from being pushed during the rewrite; v3.1 explicitly precedes v4 P0 in sequencing |
| R-V3.1-6 | **Branch protection cannot be relaxed temporarily** without admin coordination | Admin contact for the canonical remote is identified in the announcement (FR-V3.1-06); admin-override window is logged in GitHub audit log and re-locked within the same sprint per FR-V3.1-09 |

---

## Success Metrics

- **Platform-tagged AKV secrets with `null` expiry:** 0 after sprint-2
  blocking gate (FR-V3.1-04 enforced).
- **AKV certificate auto-renew failures** (observed via Azure Activity
  Log): 0 in any quarterly cycle.
- **Mean time from cert expiry to renewed cert deployed via ESO:** <30
  minutes (auto-renew + 15m ESO `refreshInterval`).
- **Operator-rotated secret 14-day alerts triggered ≥1 quarter before
  expiry:** 100% (no surprise expiries).
- **`git log --all -S"BEGIN PRIVATE KEY"` against a fresh clone of
  the canonical remote:** 0 commits post-US-V3.1-02 closure.
- **Cert revocation OCSP/CRL response for the historical cert:**
  `revoked` (re-verified post-cutover).
- **Branch protection on `main` re-instated within:** same sprint as
  the cutover (admin-override window <72h).

---

## Open Questions

- **OQ-V3.1-01** — Whether to extend the rotation discipline to
  `azurerm_key_vault_key` resources (HSM-backed signing keys for
  Cosign per `ADR-008-v2`). Currently out of v3.1 scope; tracked here
  for future sweep.

---

## Relationship to PRD-v3 and PRD-v4

- **PRD-v3:** v3.1 closes `FR-V3-12` which v3 declared but never
  implemented. v3's other FRs remain in force verbatim.
- **PRD-v4:** v4 references `FR-V3-12` as a prerequisite for its
  Helm → ESO migration (US-V4-09). v4 P0 cannot begin until v3.1
  closes. PRD-v4 OQ-V4-07 is resolved by US-V3.1-02 (TLS history
  purge); PRD-v4 §Pre-existing v3 obligations entry for FR-V3-05
  flips from "pending" to "v3.1 patch obligation" once this PRD
  drafts and to "closed" once US-V3.1-02 ships.
- **No ADR amendment is required by v3.1.** AKV rotation policies
  operate within the existing `ADR-005-v2` (ESO + AKV) and
  `ADR-006-v2` (Workload Identity for Azure) decisions. Should
  `OQ-V3.1-01` (HSM-backed key rotation) move to closure, an ADR
  amendment to `ADR-008-v2` would be required at that time.
