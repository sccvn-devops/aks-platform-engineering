---
title: Tasks v4 F-004 — Secret and token lifecycle
id: F-004
kind: tasks
feature: F-004
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-21
---

# Tasks v4 F-004 — Secret and token lifecycle

> Ordered, dependency-aware implementation tasks — plan only, no code.

**As-built inventory.**

## Task list

| Task | Description | Depends on | Artifact | Done-when | Status |
| --- | --- | --- | --- | --- | --- |
| F-004-T1 | Single vault write path with a soft-delete strategy and typed sentinels | — | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76] | `go test ./internal/akvwriter/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-004-T2 | Version handling: read the projection, disable superseded versions | F-004-T1 | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65] | `go test ./internal/akvwriter/... -run DisableOldVersions` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-004-T3 | Transport seam with bounded retries and redacted errors | — | [D: tools/mgmt-plane-lock/internal/httpx/httpx.go:1] | `go test ./internal/httpx/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-004-T4 | Rotation runner: leadership gate, mint, dual write, grace period, disable, age report | F-004-T1, F-004-T2, F-004-T3 | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/main.go:19] [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/wiring.go:34] | `go test ./internal/rotation/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-004-T5 | SaaS minters for the source host and the issue tracker | F-004-T3 | `[D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20]`, `tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go` | `go vet ./cmd/saas-token-rotator/...` is clean and every outbound call is built from the shared transport seam, not `http.DefaultClient` [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:18]. Live-vendor behaviour is F-004-T9. | done — go vet ./cmd/saas-token-rotator/... exits 0 here 2026-09-21 and the only http.DefaultClient occurrence is the comment forbidding it; both mint paths use the httpx seam. Live-vendor behaviour is F-004-T9. |
| F-004-T6 | Secret catalogue split by write ownership | — | [D: terraform/locals.tf:41] | `python3 scripts/validate-akv-catalogue.py` exits 0 | done — python3 scripts/validate-akv-catalogue.py exits 0 here 2026-09-18 |
| F-004-T7 | Quarterly expiry boundary applied to every platform-managed secret | F-004-T6 | [D: terraform/keyvaults.tf:58] | `NULL_EXPIRY_MODE=block python3 scripts/validate-akv-null-expiry.py` exits 0 (the mode literal is `block`, not `blocking`) | done — NULL_EXPIRY_MODE=block python3 scripts/validate-akv-null-expiry.py exits 0 here 2026-09-20, 14 secrets scanned |
| F-004-T8 | Dual-write skew exporter and the near-expiry alerts | F-004-T6 | [D: terraform/akv_sync_exporter.tf:1] [D: gitops/platform/akv-sync-exporter/templates/prometheus-rule.yaml:17] [D: gitops/platform/akv-sync-exporter/templates/configmap.yaml:56] [D: gitops/platform/saas-token-rotator/templates/prometheus-rule.yaml:19] [D: terraform/akv_alerts.tf:63] | Both alert rules render from the charts (checkable here), **and** the metric is scraped on a cluster (needs a running platform) | wip — the rules and the exporter exist in the charts; scraping is unverified |
| F-004-T10 | Test the divergent-pair state a mid-rotation failure leaves: the West Europe vault holds the new version, North Europe the old, and the on-failure retry converges them | F-004-T4 | — | F-004-TC15 exists and `go test ./internal/rotation/... -run Diverg` passes | todo — raised by `revise` 2026-09-21 with the DOM-004-R3 decision |
| F-004-T9 | Contract test for the two SaaS minters: a recorded interaction per vendor so a breaking API change fails here rather than at the quarterly run | F-004-T5 | — | A test exists under `tools/mgmt-plane-lock/cmd/saas-token-rotator/` that exercises both mint paths against recorded responses, and `go test ./cmd/saas-token-rotator/...` passes | todo — raised by `revise` 2026-09-20; F-004-T5 cannot otherwise be evidenced |

## Execution order

```
group A   T1 write path · T3 transport seam · T6 catalogue
group B   T2 version handling (T1) · T7 expiry (T6) · T8 observability (T6)
group C   T4 rotation runner (T1,T2,T3) · T5 minters (T3)
group D   T9 minter contract test (T5) · T10 divergent-pair test (T4)
```

## Definition of done

- [ ] The done-when command ran here and passed.
- [ ] No secret value can reach a log, an error or a document.
- [ ] Every new write goes through the single path.
- [ ] A new catalogued secret names its mode and its consumers.

## Open questions

- RESOLVED 2026-09-20: T5's done-when is now the part that is checkable here (the transport-seam constraint); the live-vendor half is tracked as F-004-T9.
- RESOLVED 2026-09-20: T8's done-when is split — the alert rules and the exporter are checkable from a checkout and exist; only scraping needs a running platform.
- OPEN: F-004-T9 and F-004-T10 have no owner and no priority. If a recorded-interaction test is not wanted, F-004-T5 stays permanently `wip` and that should be stated rather than left looking unfinished.
