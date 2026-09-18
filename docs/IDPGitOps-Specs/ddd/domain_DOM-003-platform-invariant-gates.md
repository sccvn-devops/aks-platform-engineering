---
title: Domain — Platform invariant gates
id: DOM-003
kind: domain
feature: F-003
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Domain — Platform invariant gates

> Business rules, process and user flow for this domain, in the language of the business.

## Ubiquitous language

| Term | Definition | Source |
| --- | --- | --- |
| Invariant | A property of the repository that must hold at every commit | [D: terraform/invariants.tf:1] |
| Gate | A process that fails when an invariant is violated | [D: scripts/validate-action-pins.py:169] |
| Pin | A version or commit reference fixed in one place | [D: .tool-versions:11] |
| Suppression | A supply-chain finding accepted with recorded metadata | [D: .checkov.baseline:6] |
| Baseline | The file holding suppressions and the schema for their metadata | [D: .checkov.baseline:2] |
| Annotation | A gate's output: level, file, line, message | [D: scripts/validate-action-pins.py:50] |
| Precondition | A Terraform-side invariant evaluated before provider work | [D: terraform/invariants.tf:61] |

## Actors

| Actor | Role | Evidence |
| --- | --- | --- |
| Contributor | Runs the gates locally through the hook set | [D: .pre-commit-config.yaml:48] |
| CI | Runs them again on the pull request | [D: .github/workflows/terraform-ci.yml:1] |
| Terraform | Evaluates the cross-variable invariants at plan time | [D: terraform/invariants.tf:61] |
| Reviewer | Accepts a suppression by approving its metadata | [D: docs/checkov-baseline-metadata-schema.md:12] |

## Business rules

- **DOM-003-R1** — A supply-chain finding may be suppressed only with a rationale, an owner and an expiry date. [D: scripts/validate-checkov-baseline.py:91]
- **DOM-003-R2** — Exactly one file declares a toolchain version; a second declaration anywhere fails. [D: scripts/validate-tool-version-singleton.py:104]
- **DOM-003-R3** — Every workflow action reference is a commit SHA carrying a tag comment. [D: scripts/validate-action-pins.py:72]
- **DOM-003-R4** — Every cluster catalogue entry matches the committed schema, and its key equals its cluster name. [D: scripts/validate-cluster-registry.py:111]
- **DOM-003-R5** — A cluster's short region code must match the canonical map for its region. [D: terraform/invariants.tf:95]
- **DOM-003-R6** — A spoke network range may not equal a hub range. [D: terraform/invariants.tf:62]
- **DOM-003-R7** — No sensitive value reaches a chart through a set-value; the list of sensitive attribute names is curated rather than guessed. [D: scripts/validate-helm-release-secrets.py:75]
- **DOM-003-R8** — A binary's entry point does not handle signals itself; it uses the shared lifecycle helper. [D: scripts/validate-mgmt-plane-lock-runners.sh:42]
- **DOM-003-R9** — A validator and the code it guards share one loader, so the two cannot disagree about what is parseable. [D: scripts/validate-cluster-registry.py:41]
- **DOM-003-R10** — A platform-managed secret carries an expiry date. [D: scripts/validate-akv-null-expiry.py:89]

## Process flow

A contributor commits; the hook set runs the gates on the staged tree
[D: .pre-commit-config.yaml:48]. The pull request runs format, validate, lint, scan
and plan [D: .github/workflows/terraform-ci.yml:1]. After merge, the apply workflow
runs in two phases [D: .github/workflows/terraform-apply.yml:1]. At plan time
Terraform evaluates its own invariants and aborts on a violation
[D: terraform/invariants.tf:70].

## Invariants

- A gate reads only committed state, so any contributor can reproduce its verdict [D: scripts/validate-cluster-registry.py:111].
- A gate's failure names a file and a line [D: scripts/validate-action-pins.py:50].
- The rule a gate enforces is written beside it in prose [D: scripts/validate-helm-release-secrets.py:75].

## Implementation status

| Rule | Status | Evidence | Covering test in tree |
| --- | --- | --- | --- |
| DOM-003-R1 | wip — no suppression exists in the baseline to exercise the rule | — | — |
| DOM-003-R2 | done — python3 scripts/validate-tool-version-singleton.py exits 0 here 2026-09-18 | — | — |
| DOM-003-R3 | done — python3 scripts/validate-action-pins.py exits 0 here 2026-09-18 | — | — |
| DOM-003-R4 | done — python3 scripts/validate-cluster-registry.py exits 0 here 2026-09-18 | — | [D: tools/service_seed/tests/test_registry.py:48] |
| DOM-003-R5 | wip — unverified, the precondition is evaluated only during terraform plan | — | — |
| DOM-003-R6 | wip — unverified, the precondition is evaluated only during terraform plan | — | — |
| DOM-003-R7 | done — pytest scripts/tests -q, 21 passed here 2026-09-18 | — | [D: scripts/tests/test_validate_helm_release_secrets.py:71] |
| DOM-003-R8 | done — bash scripts/validate-mgmt-plane-lock-runners.sh exits 0 here 2026-09-18 | — | — |
| DOM-003-R9 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_registry.py:131] |
| DOM-003-R10 | done — NULL_EXPIRY_MODE=advisory python3 scripts/validate-akv-null-expiry.py exits 0 here 2026-09-18 | — | — |

Seven of ten rules have no test naming them. The gates run, but only two of the
eleven validators are themselves tested
[D: scripts/tests/test_validate_helm_release_secrets.py:27]
[D: scripts/tests/test_truncate_plan_output.py:32] — a validator with a bug fails
open, and nothing would notice.

## Open questions

- OPEN: Which gates run in CI as opposed to only in pre-commit? The hook list and the workflow steps are maintained separately, and nothing enforces parity.
- OPEN: The expiry gate runs advisory or blocking depending on an environment variable [D: scripts/validate-akv-null-expiry.py:62]; which mode is used where?
- OPEN: Nothing sweeps for suppressions whose expiry has passed outside a pull-request run.
- OPEN: Nine validators have no test. Is that an accepted risk or an unfilled backlog item?
