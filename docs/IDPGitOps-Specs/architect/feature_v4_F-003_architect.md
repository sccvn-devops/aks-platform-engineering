---
title: Feature Architecture v4 F-003 — Platform invariant gates
id: F-003
kind: architecture
feature: F-003
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Feature Architecture v4 F-003 — Platform invariant gates

> Low-level design for one feature: contracts, schema, sequence, failure modes.

## Design summary

I: the platform's invariants are enforced by small independent processes over the repository text rather than by a framework — basis: eleven standalone scripts, each with its own entry point and exit code, wired into pre-commit individually [D: .pre-commit-config.yaml:62] and sharing no library between them [D: scripts/validate-action-pins.py:135].

I: the Terraform side enforces cross-variable invariants at plan time instead — basis: a dedicated file of `terraform_data` resources whose preconditions fail the plan before any provider work [D: terraform/invariants.tf:7].

Two design properties worth naming: a validator reads what is committed, so it can
be run by anyone at any time; and the rule it enforces is usually written in a
comment beside it, which is the only place the intent survives
[D: scripts/validate-helm-release-secrets.py:75].

## API contracts

Commands and error shape: [`../data/api-contract_v4_F-003.md`](../data/api-contract_v4_F-003.md).

## Data model

[`../data/data-erd_v4_F-003.md`](../data/data-erd_v4_F-003.md). Two record shapes;
everything else is a check over source text.

## Sequence

Pre-commit path: the hook set runs the validators on the staged tree
[D: .pre-commit-config.yaml:48]; a non-zero exit blocks the commit.

CI path: the workflow runs format, validate, tflint, checkov and plan
[D: .github/workflows/terraform-ci.yml:1], and the apply workflow runs post-merge in
a two-phase matrix [D: .github/workflows/terraform-apply.yml:1].

Plan path: Terraform evaluates the invariant preconditions before provider work
[D: terraform/invariants.tf:61], and a violation aborts with a formatted message
[D: terraform/invariants.tf:70].

## Failure modes

| Failure | Detection | Behaviour | Evidence |
| --- | --- | --- | --- |
| Suppression without metadata | Baseline validator | Non-zero exit naming the index and the field | [D: scripts/validate-checkov-baseline.py:91] |
| Version declared twice | Singleton scan | Non-zero exit naming the file | [D: scripts/validate-tool-version-singleton.py:104] |
| Action referenced by tag | Pin validator | Non-zero exit with a file and line annotation | [D: scripts/validate-action-pins.py:72] |
| Registry entry malformed | Schema validation | Non-zero exit naming the entry | [D: scripts/validate-cluster-registry.py:111] |
| Registry key and `aks_name` disagree | Loader check, reused by the validator | Raises before any consumer sees the entry | [D: scripts/validate-cluster-registry.py:41] |
| `region_abbrev` not in the canonical map | Terraform precondition | Plan aborts | [D: terraform/invariants.tf:95] |
| Spoke CIDR equal to a hub CIDR | Terraform precondition | Plan aborts | [D: terraform/invariants.tf:62] |
| Sensitive value through a Helm set-value | Helm validator | Non-zero exit; the curated attribute list is the contract | [D: scripts/validate-helm-release-secrets.py:75] |
| Binary entry point using signals directly | Runner-shape check | Non-zero exit | [D: scripts/validate-mgmt-plane-lock-runners.sh:42] |
| Platform secret with no expiry | Expiry validator, advisory or blocking by environment | Exit depends on the mode | [D: scripts/validate-akv-null-expiry.py:62] |

## Observability

The output is annotations and exit codes
[D: scripts/validate-action-pins.py:50]; there is no metric, no dashboard and no
history of violations over time.

- OPEN: Nothing records how often a gate fires, so the gates' value cannot be measured.
- OPEN: The mixed advisory/blocking mode of the expiry check [D: scripts/validate-akv-null-expiry.py:62] is set by an environment variable; which mode CI uses is not visible from the script.

## Traceability

| Design element | Satisfies |
| --- | --- |
| Baseline metadata validator | F-003-US1 · DOM-003-R1 |
| Version singleton scan | F-003-US2 · DOM-003-R2 |
| Action pin validator | F-003-US3 · DOM-003-R3 |
| Registry schema validation and loader reuse | F-003-US4 · DOM-003-R4 |
| Terraform preconditions | F-003-US4 · DOM-003-R5, R6 |
| Helm secret validator | F-003-US5 · DOM-003-R7 |
| Runner-shape check | F-003-US6 · DOM-003-R8 |
