---
title: Test Plan v4 F-003 — Platform invariant gates
id: F-003
kind: test
feature: F-003
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Test Plan v4 F-003 — Platform invariant gates

> Concrete cases traced back to acceptance criteria.

**As-built, and mostly a coverage report.** Eleven validators exist; two have tests.
The cases below are the tests that exist, and the holes are the point.

## Traceability matrix

| Story | Test cases | Level |
| --- | --- | --- |
| F-003-US1 | — (no test) | — |
| F-003-US2 | — (no test) | — |
| F-003-US3 | — (no test) | — |
| F-003-US4 | F-003-TC4, TC5 | unit |
| F-003-US5 | F-003-TC1, TC2, TC3, TC6 | unit |
| F-003-US6 | — (no test) | — |
| F-003-US7 | F-003-TC7, TC8 | unit + terraform |

## Test cases

| Case | What it proves | Cited test |
| --- | --- | --- |
| F-003-TC1 | A direct sensitive-variable reference in a chart value is detected | [D: scripts/tests/test_validate_helm_release_secrets.py:71] |
| F-003-TC2 | A one-hop local alias to a sensitive variable is detected | [D: scripts/tests/test_validate_helm_release_secrets.py:119] |
| F-003-TC3 | A Secret name passed by reference is allowed | [D: scripts/tests/test_validate_helm_release_secrets.py:218] |
| F-003-TC4 | The committed registry loads with every required field | [D: tools/service_seed/tests/test_registry.py:48] |
| F-003-TC5 | A registry entry whose key and cluster name disagree fails | [D: tools/service_seed/tests/test_registry.py:100] |
| F-003-TC6 | The live repository tree is clean under the Helm check | [D: scripts/tests/test_validate_helm_release_secrets.py:313] |
| F-003-TC7 | Plan output over the threshold is truncated with a footer | [D: scripts/tests/test_truncate_plan_output.py:45] |
| F-003-TC8 | A workload identity rejects subscription scope unless explicitly allowed | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:53] |

## Implementation status

| Case | Status | Evidence |
| --- | --- | --- |
| F-003-TC1 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — |
| F-003-TC2 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — |
| F-003-TC3 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — |
| F-003-TC4 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-003-TC5 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-003-TC6 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — |
| F-003-TC7 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — |
| F-003-TC8 | wip — unverified, run terraform test in terraform/modules/workload_identity; terraform not available here | — |

## Edge and negative cases

| Case | Covered by |
| --- | --- |
| A comment describing the forbidden pattern is not flagged | [D: scripts/tests/test_validate_helm_release_secrets.py:268] |
| A non-sensitive variable name is not flagged | [D: scripts/tests/test_validate_helm_release_secrets.py:290] |
| All findings are reported, not just the first | [D: scripts/tests/test_validate_helm_release_secrets.py:240] |
| Exactly-threshold plan output passes through | [D: scripts/tests/test_truncate_plan_output.py:41] |
| A federated subject is built from namespace and service account | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:29] |
| Resource-group scope passes the default guard | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:89] |

## Out of scope

Untested validators, each a hole:

- baseline metadata [D: scripts/validate-checkov-baseline.py:1]
- in-line suppressions [D: scripts/validate-checkov-suppressions.py:1]
- version singleton [D: scripts/validate-tool-version-singleton.py:1]
- action pinning [D: scripts/validate-action-pins.py:1]
- workflow-call contract [D: scripts/validate-workflow-call-contract.py:1]
- AKV catalogue [D: scripts/validate-akv-catalogue.py:1]
- null expiry [D: scripts/validate-akv-null-expiry.py:1]
- state partitioning [D: scripts/validate-state-partitioning.sh:1]
- blob-lease locking [D: scripts/validate-blob-lease-locking.sh:1]
- runner shape [D: scripts/validate-mgmt-plane-lock-runners.sh:1]

`scripts/validate-workflow-call-contract.py` is also the repository's least healthy
file by the index's own scoring, which makes its lack of a test the most
consequential of these.

## Open questions

- OPEN: Is there an agreed rule that a new validator ships with a test? Nothing enforces one.
- OPEN: The two tested validators both assert against the live tree as well as fixtures [D: scripts/tests/test_validate_helm_release_secrets.py:313]; is that the intended pattern for the others?
