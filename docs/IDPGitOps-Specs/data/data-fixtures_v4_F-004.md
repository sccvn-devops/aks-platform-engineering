---
title: Fixtures v4 F-004 — Secret and token lifecycle
id: F-004
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Fixtures v4 F-004 — Secret and token lifecycle

> The test data this feature is implemented and verified against, and what each fixture is for.

**No secret value appears in this set, and the model gives one nowhere to live.**

## Fixture sets

| Set | Entities | Scenario | Used by |
| --- | --- | --- | --- |
| F-004-FX1 | `akv_secret_entry` ×3 | One Terraform-managed entry and two rotator-owned ones, copied from the catalogues [D: terraform/locals.tf:41] | F-004-TC1 |
| F-004-FX2 | `akv_secret_version` ×2 | Latest enabled, predecessor disabled — the post-rotation state [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308] | F-004-TC6 |
| F-004-FX3 | `secret_write` ×2 | A recovering write that succeeds, and an overwriting write that refuses to revive a soft-deleted secret [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50] | F-004-TC3, TC4 |

[`fixtures/fixtures_v4_F-004.json`](fixtures/fixtures_v4_F-004.json).

## Fixture file

Validated by `route.py` against [`schema/schemas.json`](schema/schemas.json).

## Encoded invariants

| Encoded | Rule | Evidence |
| --- | --- | --- |
| Rotator-owned entries are `referenced_only` | DOM-004-R1 | [D: terraform/locals.tf:64] |
| A Terraform-managed entry names its consumer | DOM-004-R2 | [D: terraform/locals.tf:45] |
| The older version is disabled, not deleted | DOM-004-R8 | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308] |
| The default strategy refuses to revive a soft-deleted secret | DOM-004-R5 | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50] |
| No record carries a value | DOM-004-R9 | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65] |

## Determinism rules

- Fixed timestamps 90 days apart, mirroring the quarterly boundary [D: terraform/keyvaults.tf:59].
- Secret names are the committed catalogue's own; no value, no vault URL, no token.

## Loading

The Go tests inject an in-memory store satisfying the same narrow interface as the
real client [D: tools/mgmt-plane-lock/internal/akvwriter/fake_test.go:181], and the
rotation tests drive the runner with fake minters and stores
[D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:72].

## Traceability

| Set | Test cases |
| --- | --- |
| F-004-FX1 | F-004-TC1 |
| F-004-FX2 | F-004-TC6 |
| F-004-FX3 | F-004-TC3, TC4 |

## Open questions

- OPEN: No fixture represents the half-written pair (west succeeded, north failed); the case is tested in Go [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:180] but its resulting state is not modelled here.
- OPEN: Should the catalogue fixtures be generated from `terraform/locals.tf` rather than copied, so they cannot drift?
