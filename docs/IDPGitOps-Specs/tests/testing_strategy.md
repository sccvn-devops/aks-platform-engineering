---
title: Testing Strategy — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Testing Strategy — IDPGitOps

> Test levels, tooling, coverage rules and pipeline gates.

**As practised.** The levels below are the ones the repository actually has; where a
level is absent, it says so rather than describing what a mature suite would do.

## Test levels

| Level | Scope | Tool | Command | Evidence |
| --- | --- | --- | --- | --- |
| Unit — Go | Arbitration, scaling, status, config, transport, vault writer, rotation, drift bridge | `go test` with injected fakes | `go test ./...` in `tools/mgmt-plane-lock` | [D: tools/mgmt-plane-lock/go.mod:1] |
| Unit — Python, onboarding | Intake, render, git, registry, robustness | stdlib `unittest`, run under pytest | `pytest -q` in `tools/service_seed` | [D: tools/service_seed/pyproject.toml:74] |
| Unit — Python, scripts | Two validators only | stdlib `unittest` | `python -m pytest scripts/tests` | [D: scripts/tests/test_validate_helm_release_secrets.py:27] |
| Infrastructure | One module's guards | `terraform test` | `terraform test` in the module | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:29] |
| Portal | One end-to-end check | Playwright | `npm test` in `backstage` | [D: backstage/packages/app/e2e-tests/app.test.ts:19] |
| Repository invariants | Eleven gates over the committed tree | standalone scripts | `pre-commit run --all-files` | [D: .pre-commit-config.yaml:48] |
| DR drill | Failover and lease locking | shell scripts | run by hand | [D: scripts/dr-validation/validate-mgmt-failover.sh:1] |

Absent levels, stated: there is **no integration test against a live vendor API**,
**no end-to-end test of the onboarding orchestrator**, and **no test that runs
against a real cluster**.

## Coverage policy

OPEN: No coverage threshold is configured anywhere. The Python package declares a
coverage plugin and calls it an acceptance gate
[D: tools/service_seed/pyproject.toml:38] without setting a number; the Go module
sets none.

What is measurable today: 94 Python cases in the onboarding suite and 48 Go test
functions across the arbitration and secret packages; nine of the eleven repository
validators have no test at all.

## Test data

Fixtures live in [`../data/fixtures/`](../data/fixtures/) and are documented per
feature. They are **documentation of shape**: no test in the tree reads them, and
each suite builds its data inline [D: tools/service_seed/tests/test_seed_job.py:11].
Whether they should become the suites' input is an open question recorded in each
fixtures document.

Rules that the existing suites already follow: fixed identifiers, no clock reads,
fakes at the narrow interface rather than recorded transcripts
[D: tools/mgmt-plane-lock/internal/akvwriter/fake_test.go:181], and temporary
directories per case [D: tools/service_seed/tests/test_service_template.py:112].

## Pipeline gates

| Gate | Where | Evidence |
| --- | --- | --- |
| Format, validate, lint, scan | pre-commit and CI | [D: .pre-commit-config.yaml:25] |
| Registry schema | pre-commit and CI | [D: .pre-commit-config.yaml:86] |
| Action pinning, version singleton, baseline metadata | pre-commit | [D: .pre-commit-config.yaml:62] |
| Plan on the pull request | CI | [D: .github/workflows/terraform-ci.yml:1] |
| Two-phase apply after merge | CI | [D: .github/workflows/terraform-apply.yml:1] |
| Static analysis of the portal | CI | [D: .github/workflows/sonar.yml:1] |

- OPEN: Which unit suites run in CI? The workflow files cover Terraform and the portal; nothing in them was found to run the Go or Python suites.
- OPEN: Nothing enforces that a hook and a CI step stay in step.

## Non-functional testing

| Property | Checked by | Evidence |
| --- | --- | --- |
| Timeouts on every outbound call | A dedicated robustness suite | [D: tools/service_seed/tests/test_robustness.py:41] |
| Path confinement | The same suite | [D: tools/service_seed/tests/test_robustness.py:186] |
| Credential redaction | Transport tests | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208] |
| Retry policy | Transport tests | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:55] |
| Identity scope guards | Terraform module test | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:53] |

OPEN: No performance, load, soak or accessibility testing exists anywhere in the
repository.
