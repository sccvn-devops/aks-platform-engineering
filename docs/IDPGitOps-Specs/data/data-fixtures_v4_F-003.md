---
title: Fixtures v4 F-003 — Platform invariant gates
id: F-003
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Fixtures v4 F-003 — Platform invariant gates

> The test data this feature is implemented and verified against, and what each fixture is for.

## Fixture sets

| Set | Entities | Scenario | Used by |
| --- | --- | --- | --- |
| F-003-FX1 | `tool_version_pin` ×5 | The committed pin set, copied from the file the singleton check protects [D: .tool-versions:17] | F-003-TC2 |
| F-003-FX2 | `checkov_baseline_finding` | The shape a suppression must carry; an example, since the committed baseline holds none [D: .checkov.baseline:11] | F-003-TC1 |
| F-003-FX3 | `cluster_entry` seed-wus | A catalogued entry copied verbatim, used as the registry-validation subject [D: gitops/clusters/registry.yaml:133] | F-003-TC4 |

[`fixtures/fixtures_v4_F-003.json`](fixtures/fixtures_v4_F-003.json).

## Fixture file

Validated by `route.py` against [`schema/schemas.json`](schema/schemas.json).

## Encoded invariants

| Encoded | Rule | Evidence |
| --- | --- | --- |
| Each pin is one tool and one version, in one file | DOM-003-R2 | [D: scripts/validate-tool-version-singleton.py:66] |
| The example suppression carries all three required fields | DOM-003-R1 | [D: scripts/validate-checkov-baseline.py:63] |
| The seed cluster's key equals its `aks_name` | DOM-003-R4 | [D: scripts/validate-cluster-registry.py:111] |
| `region_abbrev` is `wus`, which must exist in the canonical map | DOM-003-R5 | [D: terraform/invariants.tf:95] |

## Determinism rules

- Every record is copied from a committed file or is an explicitly-labelled example.
- The owner value is a team name, not a person; the expiry is a fixed date.
- No credential can appear: none of these entities has a field for one.

## Loading

The validators read the real repository rather than fixtures
[D: scripts/validate-cluster-registry.py:111], and the two Python test modules build
temporary trees instead [D: scripts/tests/test_validate_helm_release_secrets.py:27].

I: these fixtures are documentation of shape rather than test input — basis: no validator or test in the tree reads a fixture file [D: scripts/tests/test_truncate_plan_output.py:32].

## Traceability

| Set | Test cases |
| --- | --- |
| F-003-FX1 | F-003-TC2 |
| F-003-FX2 | F-003-TC1 |
| F-003-FX3 | F-003-TC4 |

## Open questions

- OPEN: Nine of the eleven validators have no test at all; there is no fixture corpus of known-bad inputs for them.
- OPEN: Should the canonical repository itself be the fixture — several checks already assert the live tree is clean [D: scripts/tests/test_validate_helm_release_secrets.py:313] — or should each validator own a bad-input corpus?
