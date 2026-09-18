---
title: Product Backlog — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Product Backlog — IDPGitOps

> Prioritized feature/story list. The one place feature IDs are minted.

Feature IDs are minted here. The four rows below are the confirmed clusters of this
reverse pass, sized by the surface each one actually has. Value and priority are
OPEN: — a repository cannot state either.

## Backlog

| Feature | Title | Surface in tree | Size | Value | Priority | PRD |
| --- | --- | --- | --- | --- | --- | --- |
| F-001 | Service onboarding pipeline | 4 Python modules, 7 test modules, 94 cases, 18 templates, 2 profiles [D: tools/service_seed/cli.py:340] | L | OPEN: | OPEN: | [prd_v4_F-001](../../IDPGitOps-Specs/PRDs/prd_v4_F-001-service-onboarding-pipeline.md) |
| F-002 | Management plane arbitration | 5 Go packages, 4 binaries, the repository's only served route [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139] | L | OPEN: | OPEN: | [prd_v4_F-002](../../IDPGitOps-Specs/PRDs/prd_v4_F-002-management-plane-arbitration.md) |
| F-003 | Platform invariant gates | 11 validators, 1 precondition file, 2 tested [D: .pre-commit-config.yaml:48] | M | OPEN: | OPEN: | [prd_v4_F-003](../../IDPGitOps-Specs/PRDs/prd_v4_F-003-platform-invariant-gates.md) |
| F-004 | Secret and token lifecycle | 3 Go packages, 2 Terraform catalogues, an expiry boundary, a skew exporter [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] | M | OPEN: | OPEN: | [prd_v4_F-004](../../IDPGitOps-Specs/PRDs/prd_v4_F-004-secret-and-token-lifecycle.md) |

## Prioritization rationale

OPEN: Priority is not recoverable. What the code shows is a dependency direction,
not an importance order: onboarding and rotation both read the catalogue
[D: tools/service_seed/service_template.py:270], rotation depends on arbitration
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115], and the gates depend on
nothing [D: scripts/validate-action-pins.py:135].

## Deferred

Capabilities present in the tree that this pass did **not** document as features,
with the reason — each is a candidate row for a later pass:

| Candidate | Why deferred | Evidence |
| --- | --- | --- |
| Drift-to-ticket bridge | Three test cases; thin evidence for a full feature set | [D: tools/mgmt-plane-lock/internal/jirabridge/jirabridge_test.go:9] |
| Developer portal | One end-to-end test; a 48-file workspace with almost no platform-specific logic | [D: backstage/packages/app/e2e-tests/app.test.ts:19] |
| GitOps bootstrap and the two-tier layout | Configuration rather than code; it would document YAML, not behaviour | [D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1] |
| Seed-cluster recovery | A catalogue entry and two shell scripts | [D: scripts/dr-validation/validate-seed-cluster-dr.sh:1] |
| Network and identity topology | Terraform only, with one module test | [D: terraform/networking.tf:1] |
