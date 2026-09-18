---
title: Data Model v4 F-004 — Secret and token lifecycle
id: F-004
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Data Model v4 F-004 — Secret and token lifecycle

> The slice of the data model this feature reads and writes, at field precision.

No secret **value** appears in this hub. The model describes names, modes,
timestamps and outcomes; the value field exists in the projection the code returns
and is deliberately absent from every fixture.

## Scope

| Entity | Access | Evidence |
| --- | --- | --- |
| `akv_secret_entry` | own (catalogue) | [D: terraform/locals.tf:41] |
| `akv_secret_version` | own (read) | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65] |
| `secret_write` | own (write path) | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76] |

## Feature ERD

[`schema/erd_v4_F-004.puml`](schema/erd_v4_F-004.puml).

## Field definitions

`akv_secret_entry` — two catalogues, distinguished by who owns the write path.
`terraform_managed` entries must also exist as a Terraform resource
[D: terraform/locals.tf:38]; `referenced_only` entries are written by the rotator
[D: terraform/locals.tf:64]. Each carries a description and its consumers
[D: terraform/locals.tf:42].

`akv_secret_version` — the projection a read returns: name, value, update time and
enabled flag [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65]. The
update time drives the age signal
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157]. Superseded versions
are disabled rather than removed
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65].

`secret_write` — the single write surface, with a strategy that decides what happens
against a soft-deleted secret [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:44].
`Overwrite` refuses to revive one and returns a typed error
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50]; the rotator chooses
the recovering strategy [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:128].
Outcomes are the four typed sentinels
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58].

Expiry is Terraform-side, not modelled as an entity field: a 90-day rotating
boundary feeds `expiration_date` on every platform-managed secret
[D: terraform/keyvaults.tf:58] [D: terraform/keyvaults.tf:66].

## New and changed entities

F-004 introduces all three entities above.

## Migrations

| # | Change | Order | Proof |
| --- | --- | --- | --- |
| M1 | Move a secret from `referenced_only` to `terraform_managed` | Add the Terraform resource, then move the catalogue entry | The catalogue check and the expiry check both pass [D: scripts/validate-akv-catalogue.py:1] |
| M2 | Add a sensitive attribute name to the forbidden set | One entry in the curated list | The Helm check flags the new pattern [D: scripts/validate-helm-release-secrets.py:75] |
| M3 | Change the expiry window | Edit the rotating boundary | Plan shows new expiry dates on the managed secrets [D: terraform/keyvaults.tf:58] |

## Traceability

| Field or constraint | Satisfies |
| --- | --- |
| Two catalogue modes | DOM-004-R1 · F-004-US1 |
| `terraform_managed` implies a Terraform resource | DOM-004-R2 · F-004-US1 |
| Strategy enum, `Overwrite` default | DOM-004-R5 · F-004-US4 |
| Typed outcomes | DOM-004-R6 · F-004-US4 |
| Dual-vault write | DOM-004-R3 · F-004-US2 |
| Expiry on every managed secret | DOM-004-R7 · F-004-US5 |

## Open questions

- OPEN: `akv_secret_version.value` exists in the projection and is never used outside a write; is a read of the value needed at all, or could the interface drop it?
- OPEN: Nothing models who may read a given secret; that lives in role assignments outside this model.
- OPEN: The catalogue records `consumed_by` as free text [D: terraform/locals.tf:45]; nothing checks that the named consumer exists.
