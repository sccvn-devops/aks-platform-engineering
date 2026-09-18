---
title: Common Architecture Standards — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Common Architecture Standards — IDPGitOps

> The shared rules every feature and every agent must follow.

**Conventions as practised**, read out of the tree. Where a rule has a machine that
enforces it, the machine is cited; where it is only a habit, it says so.

## Tech stack

| Concern | Choice | Version | Evidence |
| --- | --- | --- | --- |
| Infrastructure as code | Terraform | 1.5.7 | [D: .tool-versions:16] |
| Terraform lint | tflint with the azurerm ruleset | 0.55.0 | [D: .tool-versions:17] |
| Supply-chain scan | checkov | 3.2.345 | [D: .tool-versions:18] |
| Cluster tooling | kubectl 1.31.3, kustomize 5.4.3 | — | [D: .tool-versions:21] |
| Platform controllers | Go | 1.24.0 | [D: tools/mgmt-plane-lock/go.mod:3] |
| Onboarding tooling | Python | ≥ 3.10 | [D: tools/service_seed/pyproject.toml:16] |
| Templating | Jinja2 with strict undefined; cookiecutter for the repo scaffold | Jinja2 ≥3.1 <4 | [D: tools/service_seed/pyproject.toml:28] |
| Developer portal | Backstage on Node | — | [D: backstage/package.json:1] |
| Metrics | Prometheus client | — | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:48] |
| Container bases | node:20-bookworm-slim, golang:1.24.3, python:3.12-slim | — | [D: backstage/Dockerfile:1] |

One version source: `.tool-versions` is the only file allowed to declare these, and
a second declaration fails a scan [D: scripts/validate-tool-version-singleton.py:104].

## Project layout

| Directory | Holds | Evidence |
| --- | --- | --- |
| `terraform/` | Azure provisioning, one file per concern | [D: terraform/locals.tf:1] |
| `terraform/modules/<name>/` | Reusable modules with their own tests | [D: terraform/modules/workload_identity/tests/workload_identity.tftest.hcl:29] |
| `gitops/clusters/` | Topology data and its schema | [D: gitops/clusters/registry.yaml:1] |
| `gitops/bootstrap/`, `gitops/platform/`, `gitops/apps/` | Infra tier, platform controllers, workload tier | [D: gitops/platform/mgmt-plane-lock/Chart.yaml:1] |
| `tools/mgmt-plane-lock/cmd/<binary>/` | Process wiring only | [D: scripts/validate-mgmt-plane-lock-runners.sh:42] |
| `tools/mgmt-plane-lock/internal/<pkg>/` | One concern per package | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:18] |
| `tools/service_seed/` | Parse, render, git, wiring | [D: tools/service_seed/jira_intake.py:19] |
| `scripts/` | Repository invariant validators | [D: .pre-commit-config.yaml:48] |
| `backstage/` | Portal workspace | [D: backstage/package.json:1] |

Dependency rules that are enforced, not merely stated:

- Entry points do not handle signals; they use the shared lifecycle helper [D: scripts/validate-mgmt-plane-lock-runners.sh:42].
- Consumers define the narrow interface they need, and both the real client and the fake satisfy it [D: tools/mgmt-plane-lock/internal/akvwriter/fake_test.go:181].
- Per-cluster identity is read from one catalogue [D: tools/service_seed/cli.py:35].
- A validator reuses the loader of the code it guards [D: scripts/validate-cluster-registry.py:41].

I: the onboarding modules obey a one-direction import rule — basis: intake imports no local module, render imports intake and the loader, git imports render and intake, wiring imports all three [D: tools/service_seed/service_template.py:36] [D: tools/service_seed/gitops_pr.py:31].

## Naming conventions

| Thing | As practised | Evidence |
| --- | --- | --- |
| Terraform identifiers | `snake_case` | [D: terraform/locals.tf:41] |
| Terraform files | one concern per file | [D: terraform/keyvaults.tf:1] |
| Kubernetes and chart directories | `kebab-case` | [D: gitops/platform/mgmt-plane-lock/Chart.yaml:1] |
| Cluster keys | `<role>-<env>-<region>` and equal to `aks_name` | [D: gitops/clusters/registry.yaml:97] |
| Generated service paths | `apps/<slug>/{infra,workload}/{base,overlays/<env>}/` | [D: tools/service_seed/service_template.py:303] |
| Seed branches | `seed/<slug>-<tier>-<key lowercased>` | [D: tools/service_seed/cli.py:388] |
| Go packages | lowercase, single concern | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:1] |
| Go interfaces | named for the role | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:18] |
| Python errors | `<Concern>Error`, carrying the offending field | [D: tools/service_seed/jira_intake.py:35] |
| Validators | `validate-<invariant>.py` or `.sh` | [D: scripts/validate-action-pins.py:1] |
| Entities | `snake_case`, singular | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:25] |

## Design patterns

Sanctioned, each visible in the tree:

- Data over code for topology [D: gitops/clusters/registry.yaml:1].
- Parse once at the boundary into a frozen value [D: tools/service_seed/jira_intake.py:47].
- Consumer-defined interfaces with injected fakes [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:18].
- Thin entry points, orchestration in a runner [D: scripts/validate-mgmt-plane-lock-runners.sh:42].
- Template-driven output with strict undefined [D: tools/service_seed/service_template.py:112].
- One write path per external system [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76].
- Fail closed on ambiguity [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91].
- Curated lists over heuristics where a false negative is expensive [D: scripts/validate-helm-release-secrets.py:75].

OPEN: Which of these are intentional standards and which are accidents of one
author's style is not recorded anywhere; 127 of 131 git-attributed files have a
single owner.

## Code quality

| Rule | Enforcement |
| --- | --- |
| Strict typing over the Python package | [D: tools/service_seed/pyproject.toml:70] |
| Terraform format and validate | [D: .pre-commit-config.yaml:25] |
| tflint from the pinned version | [D: .pre-commit-config.yaml:53] |
| checkov, with a baseline whose entries carry required metadata | [D: .pre-commit-config.yaml:34] |
| Entry points ≤ 40 lines, no direct signal use | [D: scripts/validate-mgmt-plane-lock-runners.sh:42] |
| Explicit timeout on every outbound call | [D: tools/service_seed/jira_intake.py:27] |
| Bounded subprocesses | [D: tools/service_seed/gitops_pr.py:34] |
| Typed errors carrying the actionable field | [D: tools/service_seed/jira_intake.py:35] |
| No filesystem effect before a path check | [D: tools/service_seed/service_template.py:192] |

OPEN: No coverage threshold is configured anywhere, in either language, despite a
test extra that calls coverage an acceptance gate [D: tools/service_seed/pyproject.toml:38].

## Security baseline

| Rule | Enforcement |
| --- | --- |
| Private keys never committed | [D: .pre-commit-config.yaml:42] |
| No sensitive value through a chart set-value | [D: scripts/validate-helm-release-secrets.py:75] |
| Credentials read from the environment by name | [D: tools/service_seed/cli.py:441] |
| Error bodies redacted by default | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208] |
| Actions pinned to commit SHAs | [D: scripts/validate-action-pins.py:72] |
| Suppressions carry their three required metadata fields | [D: scripts/validate-checkov-baseline.py:91] |
| Platform secrets carry an expiry | [D: terraform/keyvaults.tf:66] |
| Rendered paths confined to their destination | [D: tools/service_seed/service_template.py:150] |
| State partitioned per environment and cluster | [D: scripts/validate-state-partitioning.sh:1] |
| Dependencies pinned to a major | [D: tools/service_seed/pyproject.toml:20] |

## Review checklist

A checklist is a team practice, not a code fact, so this one is assembled from the
gates that already exist rather than invented:

- [ ] `pre-commit run --all-files` passes [D: .pre-commit-config.yaml:48].
- [ ] No toolchain version declared outside `.tool-versions` [D: scripts/validate-tool-version-singleton.py:104].
- [ ] No cluster identity hardcoded outside the catalogue [D: tools/service_seed/cli.py:35].
- [ ] No SLO numeric outside the tier profiles [D: tools/service_seed/service_template.py:95].
- [ ] Every new outbound call has an explicit timeout [D: tools/service_seed/jira_intake.py:27].
- [ ] No new error path can carry a credential [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208].
- [ ] A touched entry point is still wiring-only [D: scripts/validate-mgmt-plane-lock-runners.sh:42].
- [ ] A new validator ships with a test, or the gap is recorded.

OPEN: No review checklist exists in the repository — `CONTRIBUTING.md` and the
pull-request template do not carry one. This list is derived from the gates and
should be confirmed or replaced by the team's own.
