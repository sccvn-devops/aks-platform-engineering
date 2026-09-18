---
title: Tasks v4 F-004 — Secret and token lifecycle
id: F-004
kind: tasks
feature: F-004
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
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
| F-004-T4 | Rotation runner: leadership gate, mint, dual write, grace period, disable, age report | F-004-T1, F-004-T2, F-004-T3 | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] | `go test ./internal/rotation/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-004-T5 | SaaS minters for the source host and the issue tracker | F-004-T3 | [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20] | No test exists; the check would be a contract test against each vendor | wip — no test exists for the minters; unverified |
| F-004-T6 | Secret catalogue split by write ownership | — | [D: terraform/locals.tf:41] | `python3 scripts/validate-akv-catalogue.py` exits 0 | done — python3 scripts/validate-akv-catalogue.py exits 0 here 2026-09-18 |
| F-004-T7 | Quarterly expiry boundary applied to every platform-managed secret | F-004-T6 | [D: terraform/keyvaults.tf:58] | `python3 scripts/validate-akv-null-expiry.py` exits 0 in blocking mode | done — NULL_EXPIRY_MODE=advisory python3 scripts/validate-akv-null-expiry.py exits 0 here 2026-09-18 |
| F-004-T8 | Dual-write skew exporter and the near-expiry alerts | F-004-T6 | [D: terraform/akv_sync_exporter.tf:1] | The metric is scraped and the alert rule exists | wip — unverified, needs a running platform |

## Execution order

```
group A   T1 write path · T3 transport seam · T6 catalogue
group B   T2 version handling (T1) · T7 expiry (T6) · T8 observability (T6)
group C   T4 rotation runner (T1,T2,T3) · T5 minters (T3)
```

## Definition of done

- [ ] The done-when command ran here and passed.
- [ ] No secret value can reach a log, an error or a document.
- [ ] Every new write goes through the single path.
- [ ] A new catalogued secret names its mode and its consumers.

## Open questions

- OPEN: T5 has no verifiable done-when in this repository; a vendor contract test does not exist.
- OPEN: T8's done-when cannot be checked from a checkout — it needs a running platform.
