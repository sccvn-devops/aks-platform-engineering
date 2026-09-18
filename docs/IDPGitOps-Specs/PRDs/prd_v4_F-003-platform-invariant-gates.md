---
title: PRD v4 F-003 — Platform invariant gates
id: F-003
kind: prd
feature: F-003
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# PRD v4 F-003 — Platform invariant gates

> Feature-level requirements: what and why, never how.

**Reconstructed from behaviour.** Stories are inferred from the gates that exist.
Value and priority are OPEN:.

## Summary

I: the platform protects a set of repository-wide properties with independent checks that run before a commit and again on a pull request, so a violation is a failing check rather than a review comment — basis: eleven validators wired into the hook set [D: .pre-commit-config.yaml:48] and a plan-time precondition file [D: terraform/invariants.tf:61].

## User stories

- **F-003-US1** — As a security reviewer, I want every accepted supply-chain finding to carry a reason, an owner and an expiry. I: inferred from the baseline validator [D: scripts/validate-checkov-baseline.py:91].
- **F-003-US2** — As a platform engineer, I want one place that declares toolchain versions. I: inferred from the singleton scan [D: scripts/validate-tool-version-singleton.py:104].
- **F-003-US3** — As a security reviewer, I want every action pinned to a commit. I: inferred from the pin validator [D: scripts/validate-action-pins.py:72].
- **F-003-US4** — As a platform engineer, I want a malformed cluster catalogue to fail before anything consumes it. I: inferred from the schema validator and the plan-time preconditions [D: scripts/validate-cluster-registry.py:111].
- **F-003-US5** — As a security reviewer, I want no sensitive value to reach a chart as a set-value. I: inferred from the Helm validator [D: scripts/validate-helm-release-secrets.py:75].
- **F-003-US6** — As a platform engineer, I want binary entry points to stay wiring-only. I: inferred from the runner-shape check [D: scripts/validate-mgmt-plane-lock-runners.sh:42].
- **F-003-US7** — As a reviewer, I want a readable plan on the pull request. I: inferred from the single truncation implementation [D: scripts/truncate-plan-output.py:45].

## Acceptance criteria

**F-003-US4** — the committed registry loads with every required field [D: tools/service_seed/tests/test_registry.py:48]; a key/name mismatch fails [D: tools/service_seed/tests/test_registry.py:100].

**F-003-US5** — a direct sensitive reference is detected [D: scripts/tests/test_validate_helm_release_secrets.py:71]; a one-hop alias is detected [D: scripts/tests/test_validate_helm_release_secrets.py:119]; a Secret name by reference is allowed [D: scripts/tests/test_validate_helm_release_secrets.py:218]; the live tree is clean [D: scripts/tests/test_validate_helm_release_secrets.py:313].

**F-003-US7** — oversize output is truncated with a footer [D: scripts/tests/test_truncate_plan_output.py:45]; exactly-threshold output passes through [D: scripts/tests/test_truncate_plan_output.py:41].

OPEN: F-003-US1, US2, US3 and US6 have no test, so their acceptance criteria are
the validator's own behaviour and nothing verifies it.

## Implementation status

| Story | Status | Carried by | Evidence |
| --- | --- | --- | --- |
| F-003-US1 | wip — validator exits 0 here, but the committed baseline holds no suppression to exercise | F-003-T4 | — |
| F-003-US2 | done — python3 scripts/validate-tool-version-singleton.py exits 0 here 2026-09-18 | F-003-T2 | — |
| F-003-US3 | done — python3 scripts/validate-action-pins.py exits 0 here 2026-09-18 | F-003-T3 | — |
| F-003-US4 | done — python3 scripts/validate-cluster-registry.py exits 0 here 2026-09-18 | F-003-TC4, TC5 · F-003-T1, T6 | — |
| F-003-US5 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | F-003-TC1, TC2, TC3, TC6 · F-003-T5 | — |
| F-003-US6 | done — bash scripts/validate-mgmt-plane-lock-runners.sh exits 0 here 2026-09-18 | F-003-T7 | — |
| F-003-US7 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | F-003-TC7 · F-003-T8 | — |

## Scope boundaries

- No runtime enforcement: every gate runs before or during a pull request, never against a live cluster [D: scripts/validate-cluster-registry.py:111].
- No policy engine: admission-time enforcement is Kyverno's, not this feature's.
- No secret scanning beyond private keys and the Helm set-value path [D: .pre-commit-config.yaml:42].
- No dependency-update automation: version bumps are operator-driven [D: .tool-versions:11].

## Dependencies

| Dependency | Nature | Evidence |
| --- | --- | --- |
| pre-commit | Local execution | [D: .pre-commit-config.yaml:48] |
| GitHub Actions | Pull-request execution | [D: .github/workflows/terraform-ci.yml:1] |
| The pinned toolchain | Terraform, tflint and checkov versions | [D: .tool-versions:17] |
| The service_seed registry loader | Shared by the registry validator | [D: scripts/validate-cluster-registry.py:41] |

## Metrics

OPEN: Nothing counts gate failures, time-to-fix, or suppressions past expiry. The
gates are binary and leave no trace beyond a CI log.

## Open questions

- OPEN: What is this feature worth? No incident record or defect count in the repository connects a gate to a prevented failure.
- OPEN: Who owns a failing gate that blocks an unrelated change — the contributor or the platform team?
- OPEN: Is there an agreed policy for adding a suppression, beyond the three metadata fields?
