---
title: Tasks v4 F-003 — Platform invariant gates
id: F-003
kind: tasks
feature: F-003
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Tasks v4 F-003 — Platform invariant gates

> Ordered, dependency-aware implementation tasks — plan only, no code.

**As-built inventory.** One row per gate that exists.

## Task list

| Task | Description | Depends on | Artifact | Done-when | Status |
| --- | --- | --- | --- | --- | --- |
| F-003-T1 | Cluster registry schema validation, reusing the consumer's loader | — | [D: scripts/validate-cluster-registry.py:111] | `python3 scripts/validate-cluster-registry.py` exits 0 | done — python3 scripts/validate-cluster-registry.py exits 0 here 2026-09-18 |
| F-003-T2 | Toolchain version singleton scan across the tree | — | [D: scripts/validate-tool-version-singleton.py:104] | `python3 scripts/validate-tool-version-singleton.py` exits 0 | done — python3 scripts/validate-tool-version-singleton.py exits 0 here 2026-09-18 |
| F-003-T3 | Action SHA-pinning check with tag-comment requirement | — | [D: scripts/validate-action-pins.py:72] | `python3 scripts/validate-action-pins.py` exits 0 | done — python3 scripts/validate-action-pins.py exits 0 here 2026-09-18 |
| F-003-T4 | Checkov baseline metadata validation driven by the file's own schema header | — | [D: scripts/validate-checkov-baseline.py:63] | `python3 scripts/validate-checkov-baseline.py` exits 0 | done — python3 scripts/validate-checkov-baseline.py exits 0 here 2026-09-18 |
| F-003-T5 | Helm set-value secret check with a curated attribute list | — | [D: scripts/validate-helm-release-secrets.py:75] | `python3 -m pytest scripts/tests/test_validate_helm_release_secrets.py -q` passes | done — pytest scripts/tests -q, 21 passed here 2026-09-18 |
| F-003-T6 | Terraform cross-variable preconditions for region codes and network ranges | — | [D: terraform/invariants.tf:61] | `terraform -chdir=terraform validate` passes and a plan evaluates the preconditions | wip — unverified, run terraform validate; terraform not available here |
| F-003-T7 | Runner-shape check forbidding direct signal handling in entry points | — | [D: scripts/validate-mgmt-plane-lock-runners.sh:42] | `bash scripts/validate-mgmt-plane-lock-runners.sh` exits 0 | done — bash scripts/validate-mgmt-plane-lock-runners.sh exits 0 here 2026-09-18 |
| F-003-T8 | Plan-output truncation with a single threshold | — | [D: scripts/truncate-plan-output.py:45] | `python3 -m pytest scripts/tests/test_truncate_plan_output.py -q` passes | done — pytest scripts/tests -q, 21 passed here 2026-09-18 |
| F-003-T9 | Secret-expiry and AKV catalogue checks | — | [D: scripts/validate-akv-null-expiry.py:62] | Both scripts exit 0 in the mode CI uses | done — validate-akv-catalogue.py and validate-akv-null-expiry.py exit 0 here 2026-09-18 (advisory mode) |
| F-003-T10 | Hook set wiring so local and CI verdicts match | F-003-T1…T9 | [D: .pre-commit-config.yaml:48] | `pre-commit run --all-files` passes | wip — unverified, run pre-commit run --all-files; pre-commit not installed here |
| F-003-T11 | CI workflows: plan on pull request, two-phase apply after merge | F-003-T10 | [D: .github/workflows/terraform-ci.yml:1] | A pull request shows the plan comment and every gate green | wip — unverified, needs a pull-request run |

## Execution order

The gates are independent by construction — no validator imports another
[D: scripts/validate-action-pins.py:135] — so T1…T9 are parallel, T10 collects them
and T11 mirrors them into CI.

## Definition of done

- [ ] The done-when command ran here and passed.
- [ ] The gate is wired into both pre-commit and CI, or the asymmetry is recorded.
- [ ] The rule the gate enforces is stated in prose beside it.
- [ ] A new gate ships with a test, or its absence is recorded as an open question.

## Open questions

- OPEN: T11 cannot be verified from a checkout; the survey's own CI detection missed these workflows, so "every gate green" is an assertion nobody here has watched.
- OPEN: Nine gates have no test (see the test plan). Which of them would fail open if broken?
