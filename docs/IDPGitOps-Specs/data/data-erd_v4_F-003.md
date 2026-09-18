---
title: Data Model v4 F-003 — Platform invariant gates
id: F-003
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Data Model v4 F-003 — Platform invariant gates

> The slice of the data model this feature reads and writes, at field precision.

This feature's subject matter is the repository itself. Most of its checks read
source text rather than records — action references in workflow files, version
strings, Terraform expressions — and only two carry a shape worth modelling.

## Scope

| Entity | Access | Evidence |
| --- | --- | --- |
| `checkov_baseline_finding` | own (validate) | [D: scripts/validate-checkov-baseline.py:63] |
| `tool_version_pin` | own (validate) | [D: scripts/validate-tool-version-singleton.py:104] |
| `cluster_entry` | validate (owns the schema, not the content) | [D: scripts/validate-cluster-registry.py:111] |

## Feature ERD

[`schema/erd_v4_F-003.puml`](schema/erd_v4_F-003.puml).

## Field definitions

`checkov_baseline_finding` — the three metadata fields every suppressed finding
must carry. The required list is read from the baseline's own `_schema` header
rather than hardcoded [D: scripts/validate-checkov-baseline.py:63], with a default
used when the header is absent [D: scripts/validate-checkov-baseline.py:31]. Field
meanings are documented in the repository
[D: docs/checkov-baseline-metadata-schema.md:11].

`tool_version_pin` — one line per tool in `.tool-versions`
[D: .tool-versions:17]; the trailing comment is advisory
[D: .tool-versions:12]. A version declared anywhere else fails the scan
[D: scripts/validate-tool-version-singleton.py:66].

`cluster_entry` — validated, not defined, here. Full field table in
[`data-erd_v4_F-001.md`](data-erd_v4_F-001.md).

Checks with no record shape, listed so the gap is visible rather than implied:
action pinning [D: scripts/validate-action-pins.py:72], Helm secret usage
[D: scripts/validate-helm-release-secrets.py:75], state partitioning
[D: scripts/validate-state-partitioning.sh:1], workflow-call contracts
[D: scripts/validate-workflow-call-contract.py:1], runner shape
[D: scripts/validate-mgmt-plane-lock-runners.sh:1] and null secret expiry
[D: scripts/validate-akv-null-expiry.py:62].

## New and changed entities

F-003 introduces `checkov_baseline_finding` and `tool_version_pin`. It changes no
entity another feature owns.

## Migrations

| # | Change | Order | Proof |
| --- | --- | --- | --- |
| M1 | Add a required metadata field to suppressions | Edit the baseline `_schema` header, then backfill every entry | The validator reads the header and passes [D: scripts/validate-checkov-baseline.py:62] |
| M2 | Add a tool to the pinned set | One line in `.tool-versions`; never a second declaration | The singleton scan stays clean [D: scripts/validate-tool-version-singleton.py:104] |
| M3 | Add a required registry field | Schema, loader and every entry together | The registry validator passes [D: scripts/validate-cluster-registry.py:111] |

## Traceability

| Field or constraint | Satisfies |
| --- | --- |
| Three metadata fields per suppression | DOM-003-R1 · F-003-US1 |
| Single version declaration | DOM-003-R2 · F-003-US2 |
| Registry key equals `aks_name` | DOM-003-R4 · F-003-US4 |
| `region_abbrev` matches the canonical map | DOM-003-R5 · F-003-US4 |

## Open questions

- OPEN: `expiry` is a date after which a suppression fails the PR [D: docs/checkov-baseline-metadata-schema.md:13]; nothing in the tree sweeps for suppressions that are already past it outside a PR run.
- OPEN: The baseline currently holds no suppressed findings [D: .checkov.baseline:11]. Is that because none is needed, or because they live elsewhere?
- OPEN: `tool_version_pin.note` carries advisory tag comments [D: .tool-versions:12]; nothing checks that a note still matches its version.
