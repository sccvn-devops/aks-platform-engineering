---
title: Feature Architecture v4 F-004 — Secret and token lifecycle
id: F-004
kind: architecture
feature: F-004
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-21
---

# Feature Architecture v4 F-004 — Secret and token lifecycle

> Low-level design for one feature: contracts, schema, sequence, failure modes.

## Design summary

I: secret material has one write path and one catalogue, and the catalogue records who owns each write — basis: a single `Put`/`Get` interface every caller programs against [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76], and two Terraform maps splitting secrets by write ownership [D: terraform/locals.tf:38] [D: terraform/locals.tf:64].

I: rotation is a small state machine that refuses to run anywhere but the active cluster — basis: the runner's first act after validation is a leadership check [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115], which is also the only coupling between this feature and arbitration.

Expiry is expressed in infrastructure rather than in code: a 90-day rotating
boundary feeds an expiration date onto every platform-managed secret
[D: terraform/keyvaults.tf:58].

## API contracts

[`../data/api-contract_v4_F-004.md`](../data/api-contract_v4_F-004.md).

## Data model

[`../data/data-erd_v4_F-004.md`](../data/data-erd_v4_F-004.md). Owns the catalogue
entry, the version projection and the write record. No value is modelled anywhere
in this hub.

## Sequence

Rotation run [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110]:

1. Validate required fields [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:111].
2. Stop unless this cluster is active [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115].
3. Mint a new credential [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:120].
4. Write it to the west vault with the recovering strategy [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:128].
5. Write the same value to the north vault [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:131].
6. Sleep out the grace period, interruptibly — 24 h as deployed [D: gitops/platform/saas-token-rotator/values.yaml:16].
7. Disable superseded versions in **both** regional vaults, each behind a type assertion for the disabler interface [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:146].
8. Report the resulting secret age [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157].

Write path [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76]: attempt
the write; on a conflict, probe whether the secret is soft-deleted; under the
default strategy return the typed error, and under the recovering strategy recover
and retry [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:16].

## Failure modes

| Failure | Detection | Behaviour | Evidence |
| --- | --- | --- | --- |
| Not the active cluster | Leadership check | Run exits without minting | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115] |
| Mint fails | Error from the minter | Run aborts; no vault write | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:121] |
| West vault write fails | Error from the store | Run aborts before the second write | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:128] |
| North vault write fails | Error from the store | Run aborts with the pair inconsistent; the scheduled job's on-failure retry re-mints and rewrites both, so the divergence is bounded by the retry budget rather than permanent | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:131] [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:23] |
| Shutdown during the grace period | Interruptible sleep | Returns within a second | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:140] |
| Disabling old versions fails | Error from the disabler | Logged, not fatal | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:146] |
| Secret soft-deleted, default strategy | Conflict then probe | Typed error, no revival | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50] |
| Auth failure | 401 or 403 | Typed error | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:93] |
| Server error | 5xx | Redacted error; transport retries 5xx only | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:190] |
| Platform secret with no expiry | Terraform-side validator | Advisory or blocking by mode | [D: scripts/validate-akv-null-expiry.py:62] |

## Observability

| Signal | Where | Evidence |
| --- | --- | --- |
| Signal | Name | Alert and threshold | Evidence |
| --- | --- | --- | --- |
| Secret age after rotation | `saas_token_age_days` | `SaaSTokenAgeExceeded` above 100 days, for 5m, 3600s interval | [D: gitops/platform/saas-token-rotator/templates/prometheus-rule.yaml:19] |
| Dual-write skew between the regional vaults | `akv_dual_write_skew_seconds` | `PerRegionAKVDualWriteSkew` above 600 s, for 5m, 60s interval | [D: gitops/platform/akv-sync-exporter/templates/prometheus-rule.yaml:17] |
| Near-expiry | Event Grid, not a metric | `SecretNearExpiry` / `CertificateNearExpiry`, 30 days ahead | [D: terraform/akv_alerts.tf:63] |

The skew exporter is a Python script mounted from a ConfigMap, not a Go binary
[D: gitops/platform/akv-sync-exporter/templates/configmap.yaml:11].

- OPEN: Nothing records rotation outcomes over time; the age gauge is a level, not a history.
- OPEN: The skew exporter publishes a metric [D: gitops/platform/akv-sync-exporter/templates/configmap.yaml:60] and alerts above 600 s; nothing in the code repairs a skew it detects.
- OPEN: Disabling superseded versions is behind a type assertion on the store [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:147]. A store that does not satisfy the disabler interface is skipped silently, with no log — DOM-004-R8 would then not hold, and nothing would say so. Production wires `*akvwriter.Client`, which does satisfy it [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:317].

## Traceability

| Design element | Satisfies |
| --- | --- |
| Two-mode catalogue | F-004-US1 · DOM-004-R1, R2 |
| Leadership gate before rotating | F-004-US2 · DOM-004-R10 |
| Dual-vault write | F-004-US2 · DOM-004-R3 |
| Single write path with a strategy | F-004-US4 · DOM-004-R4, R5 |
| Typed sentinels | F-004-US4 · DOM-004-R6 |
| Grace period then disable | F-004-US3 · DOM-004-R8, R11 |
| Expiry on every managed secret | F-004-US5 · DOM-004-R7 |
| Redacted errors | F-004-US4 · DOM-004-R9 |
