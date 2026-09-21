---
title: PRD v4 F-004 — Secret and token lifecycle
id: F-004
kind: prd
feature: F-004
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-20
---

# PRD v4 F-004 — Secret and token lifecycle

> Feature-level requirements: what and why, never how.

**Reconstructed from behaviour.**

## Summary

I: every platform secret is declared in one catalogue, written through one path, carries an expiry, and — where it is a SaaS credential — is minted and replaced on a schedule by the active management cluster only — basis: the two catalogues [D: terraform/locals.tf:41], the single write interface [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76], the quarterly expiry boundary [D: terraform/keyvaults.tf:58] and the rotation runner's leadership gate [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115].

## User stories

- **F-004-US1** — As a security reviewer, I want to see every platform secret and who writes it. I: inferred from the two catalogues [D: terraform/locals.tf:64].
- **F-004-US2** — As a platform engineer, I want SaaS credentials replaced on a schedule without anyone handling them. I: inferred from the rotation runner and its minters [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:120].
- **F-004-US3** — As a consumer, I want a rotation not to break me mid-flight. I: inferred from the grace period before superseded versions are disabled [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:140].
- **F-004-US4** — As a platform engineer, I want one write path with predictable failures. I: inferred from the interface and its typed sentinels [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58].
- **F-004-US5** — As a security reviewer, I want no platform secret to sit without an expiry. I: inferred from the computed expiry and the validator [D: scripts/validate-akv-null-expiry.py:89].

## Acceptance criteria

**F-004-US2** — a standby cluster does not rotate [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:55]; a successful run writes both vaults [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:72]; a mint failure surfaces [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:147].

**F-004-US3** — shutdown during the grace period exits fast [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:114]; a disable failure is logged rather than fatal [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:218].

**F-004-US4** — a write succeeds [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:69]; auth failures are typed [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:93]; a soft-deleted target is refused by default [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:113] and recovered on request [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:137]; a 5xx is redacted [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:190].

OPEN: F-004-US1 and F-004-US5 have no `-TC` case. Both are now evidenced by a
validator that runs in CI — `akv-catalogue` and `akv-null-expiry`
[D: .github/workflows/terraform-ci.yml:214] [D: .github/workflows/terraform-ci.yml:192] —
so the run conditions are visible after all. What is still open is that the expiry
job runs advisory, so US5's guarantee holds today by inspection rather than by a
gate that would stop a regression.

## Implementation status

| Story | Status | Carried by | Evidence |
| --- | --- | --- | --- |
| F-004-US1 | done — python3 scripts/validate-akv-catalogue.py exits 0 here 2026-09-18 | F-004-T6 | — |
| F-004-US2 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-004-TC7, TC8, TC9, TC10 · F-004-T4, T5 | — |
| F-004-US3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-004-TC6, TC11, TC12 · F-004-T2, T4 | — |
| F-004-US4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-004-TC1, TC2, TC3, TC4, TC5 · F-004-T1, T3 | — |
| F-004-US5 | done — NULL_EXPIRY_MODE=block python3 scripts/validate-akv-null-expiry.py exits 0 here 2026-09-20 — 14 secrets scanned, every non-allowlisted one declares expiration_date | F-004-T7 | — |

## Scope boundaries

- No projection into clusters: that is the secrets operator's job, referenced by the catalogue [D: terraform/locals.tf:70].
- No access control: role assignments live in infrastructure, not here.
- No certificate issuance: certificate material is referenced, not minted [D: terraform/locals.tf:78].
- No repair of a skewed vault pair: the skew is exported, not corrected [D: terraform/akv_sync_exporter.tf:1].

## Dependencies

| Dependency | Nature | Evidence |
| --- | --- | --- |
| Azure Key Vault, two regional vaults | Hard | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:73] |
| Arbitration (F-002) | Hard — rotation runs only where the cluster is active | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115] |
| SaaS APIs for minting | Hard | [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20] |
| The shared transport seam | Runtime | [D: tools/mgmt-plane-lock/internal/httpx/httpx.go:1] |

## Metrics

Two signals exist, and both now carry a committed threshold:

| Signal | Threshold | Alert | Evidence |
| --- | --- | --- | --- |
| Secret age after rotation | 100 days | `SaaSTokenAgeExceeded`, for 5m | [D: gitops/platform/saas-token-rotator/values.yaml:37] |
| Dual-write skew between the regional vaults | 600 s | `PerRegionAKVDualWriteSkew`, for 5m | [D: gitops/platform/akv-sync-exporter/values.yaml:36] |

OPEN: the two numbers are alert thresholds someone chose, not targets anyone
agreed. 100 days against a 90-day rotation leaves a 10-day margin; nothing states
whether that is the intent.

## Open questions

- RESOLVED 2026-09-20: The rotation interval is quarterly — `schedule: "0 3 1 */3 *"`, 03:00 on the first of every third month [D: gitops/platform/saas-token-rotator/values.yaml:13], rendered into one CronJob per token type with `concurrencyPolicy: Forbid` [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:12]. It matches the 90-day expiry boundary [D: terraform/keyvaults.tf:59].
- OPEN: What is the business consequence of a leaked SaaS token, and does it justify a shorter interval?
- OPEN: Who is accountable for a secret whose consumer has been decommissioned? The catalogue records consumers but nothing prunes.
