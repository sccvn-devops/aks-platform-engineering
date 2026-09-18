---
title: API Contract v4 F-003 — Platform invariant gates
id: F-003
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# API Contract v4 F-003 — Platform invariant gates

> The wire contract for this feature — the readable companion to its OpenAPI and AsyncAPI files.

## Surface summary

No network surface. The contract is a set of commands, each exiting non-zero on a
violation and printing an annotation naming the file and line
[D: scripts/validate-action-pins.py:50]. They run in two places with the same
arguments: pre-commit [D: .pre-commit-config.yaml:62] and CI.

## Endpoints

| Command | Enforces | Evidence |
| --- | --- | --- |
| `python3 scripts/validate-cluster-registry.py` | The registry matches its committed JSON Schema | [D: scripts/validate-cluster-registry.py:111] |
| `python3 scripts/validate-tool-version-singleton.py` | No file but `.tool-versions` declares a pinned version | [D: scripts/validate-tool-version-singleton.py:104] |
| `python3 scripts/validate-action-pins.py` | Every action reference is a commit SHA with a tag comment | [D: scripts/validate-action-pins.py:72] |
| `python3 scripts/validate-checkov-baseline.py` | Every suppression carries its three required metadata fields | [D: scripts/validate-checkov-baseline.py:91] |
| `python3 scripts/validate-checkov-suppressions.py` | In-line suppressions carry the same metadata | [D: scripts/validate-checkov-suppressions.py:1] |
| `python3 scripts/validate-helm-release-secrets.py` | No sensitive value flows through a Helm set-value | [D: scripts/validate-helm-release-secrets.py:75] |
| `python3 scripts/validate-workflow-call-contract.py` | Reusable workflow inputs and callers agree | [D: scripts/validate-workflow-call-contract.py:1] |
| `python3 scripts/validate-akv-null-expiry.py` | Platform-managed secrets carry an expiry | [D: scripts/validate-akv-null-expiry.py:62] |
| `bash scripts/validate-mgmt-plane-lock-runners.sh` | Binary entry points stay wiring-only | [D: scripts/validate-mgmt-plane-lock-runners.sh:42] |
| `bash scripts/validate-state-partitioning.sh` | State files stay per environment and per cluster | [D: scripts/validate-state-partitioning.sh:1] |
| `bash scripts/validate-blob-lease-locking.sh` | Backend files carry only a key line, and the lease is released after the probe | [D: scripts/validate-blob-lease-locking.sh:111] |
| Terraform preconditions | Cross-variable invariants fail the plan up front | [D: terraform/invariants.tf:61] |

## Events

**None** — every validator is a synchronous process
[D: scripts/validate-action-pins.py:169].

## Error model

One shape: a workflow annotation with a level, a file and a line
[D: scripts/validate-action-pins.py:50], plus a non-zero exit. The Terraform side
fails the plan with a formatted message instead
[D: terraform/invariants.tf:70].

I: annotations are formatted for GitHub Actions when that environment variable is present, and printed plainly otherwise — basis: the annotation helper branches on it [D: scripts/validate-action-pins.py:51].

## Versioning and compatibility

| Stable | Changes carefully |
| --- | --- |
| Exit-code semantics: zero means clean | Adding a validator means adding a hook and a CI step, or local and CI verdicts diverge [D: .pre-commit-config.yaml:62] |
| The baseline's `_schema` header contract | Adding a required metadata field invalidates every existing suppression [D: scripts/validate-checkov-baseline.py:63] |

## Spec files

- [`schema/openapi_v4_F-003.json`](schema/openapi_v4_F-003.json) — empty paths, stated.
- [`schema/asyncapi_v4_F-003.json`](schema/asyncapi_v4_F-003.json) — empty.

## Traceability

| Surface | Satisfies |
| --- | --- |
| Registry validator | F-003-US4 · DOM-003-R4, R5 |
| Version singleton | F-003-US2 · DOM-003-R2 |
| Action pinning | F-003-US3 · DOM-003-R3 |
| Baseline metadata | F-003-US1 · DOM-003-R1 |
| Helm secret check | F-003-US5 · DOM-003-R7 |
| Runner shape check | F-003-US6 · DOM-003-R8 |
| Terraform preconditions | F-003-US4 · DOM-003-R6 |

## Open questions

- OPEN: The survey found no CI configuration, yet three workflow files exist [D: .github/workflows/terraform-ci.yml:1]. Which validators actually run in CI as opposed to only in pre-commit? The hook list and the workflow steps are maintained separately.
- OPEN: Nothing enforces that a new validator is added to both places; the parity claim rests on review.
- OPEN: Are the shell validators run anywhere automatically? They are absent from the pre-commit hook list except the runner-shape one [D: .pre-commit-config.yaml:118].
