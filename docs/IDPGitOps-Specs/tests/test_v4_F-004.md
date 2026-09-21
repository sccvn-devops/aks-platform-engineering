---
title: Test Plan v4 F-004 — Secret and token lifecycle
id: F-004
kind: test
feature: F-004
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-21
---

# Test Plan v4 F-004 — Secret and token lifecycle

> Concrete cases traced back to acceptance criteria.

**As-built inventory.** The write path and the rotation loop are the best-tested
code in the repository; the Terraform side of this feature has no test at all.

## Traceability matrix

| Story | Test cases | Level |
| --- | --- | --- |
| F-004-US1 | — (no `-TC` case) | command — `python3 scripts/validate-akv-catalogue.py`, which enforces the catalogue round-trip |
| F-004-US2 | F-004-TC7, TC8, TC9, TC10, TC15 | unit |
| F-004-US3 | F-004-TC6, TC11, TC12 | unit |
| F-004-US4 | F-004-TC1, TC2, TC3, TC4, TC5 | unit |
| F-004-US5 | — (no `-TC` case) | command — `NULL_EXPIRY_MODE=block python3 scripts/validate-akv-null-expiry.py`, which is what moved the story to done |

## Test cases

| Case | What it proves | Cited test |
| --- | --- | --- |
| F-004-TC1 | A write succeeds and stores the value | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:69] |
| F-004-TC2 | Auth failures are typed, for 401 and 403 | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:93] |
| F-004-TC3 | The default strategy returns a typed error for a soft-deleted secret | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:113] |
| F-004-TC4 | The recovering strategy recovers and writes | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:137] |
| F-004-TC5 | A 5xx returns a redacted error | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:190] |
| F-004-TC6 | All versions but the latest are disabled | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308] |
| F-004-TC7 | A standby cluster does not rotate | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:55] |
| F-004-TC8 | A successful run writes both vaults | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:72] |
| F-004-TC9 | A mint failure surfaces | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:147] |
| F-004-TC10 | A failure on either vault surfaces | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:163] |
| F-004-TC11 | Shutdown during the grace period exits fast | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:114] |
| F-004-TC12 | A disable failure is logged, not fatal | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:218] |
| F-004-TC13 | Required runner fields are validated | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:196] |
| F-004-TC14 | The fake and the real client satisfy one interface | [D: tools/mgmt-plane-lock/internal/akvwriter/fake_test.go:181] |
| F-004-TC15 | A failure between the two vault writes leaves West Europe new and North Europe old, and a re-run converges them | — (not yet written; carried by F-004-T10) |

## Implementation status

| Case | Status | Evidence |
| --- | --- | --- |
| F-004-TC1 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC2 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC5 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC6 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC7 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC8 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC9 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC10 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC11 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC12 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC13 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC14 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-004-TC15 | todo — raised by `revise` 2026-09-21; no test exists yet | — |

## Edge and negative cases

| Case | Covered by |
| --- | --- |
| A conflict that is not a soft delete stays a conflict | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:170] |
| A probe failure falls back to the original conflict | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:369] |
| A recovery failure propagates | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:388] |
| Disabling is skipped when fewer than two versions exist | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:293] |
| A read of a missing secret is typed | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:233] |
| The sentinels are distinct | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:407] |
| 5xx retries, 4xx does not | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:55] |
| Error bodies are redacted unless opted in | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208] |

## Out of scope

- **The Terraform half has no test**: expiry dates, the rotating boundary and the vault topology are asserted nowhere [D: terraform/keyvaults.tf:58].
- **The minters have no test**: both SaaS token functions are untested [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20].
- **The skew exporter has no test** [D: terraform/akv_sync_exporter.tf:1].
- ~~Nothing tests the inconsistent-pair state~~ — now planned as F-004-TC15, carried by F-004-T10 (DOM-004-R3 decision, 2026-09-21). Until it lands, the divergent state is still unexercised.

## Open questions

- OPEN: The minters call live SaaS APIs; is there a contract test or a recorded interaction anywhere outside this repository? F-004-T9 raises one inside it.
- OPEN: What proves, on a running platform, that no secret value has ever been logged? The unit test covers the transport seam only.
