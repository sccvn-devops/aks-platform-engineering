---
title: Deployment Strategy — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Deployment Strategy — IDPGitOps

> Environments, pipeline, release mechanics and rollback.

Derived from the manifests, workflows, charts and the repository's own guide.
Commands nobody here has run are marked `I:`.

## Environments

| Environment | Role | Evidence |
| --- | --- | --- |
| `mgmt-we` | Management, catalogued active | [D: gitops/clusters/registry.yaml:40] |
| `mgmt-ne` | Management, catalogued standby | [D: gitops/clusters/registry.yaml:55] |
| `aks-dev-we` | Workload, single zone | [D: gitops/clusters/registry.yaml:64] |
| `aks-staging-we` | Workload, three zones | [D: gitops/clusters/registry.yaml:78] |
| `aks-prod-we` | Workload, three zones, Premium | [D: gitops/clusters/registry.yaml:97] |
| `aks-prod-ne` | Workload, second region | [D: gitops/clusters/registry.yaml:115] |
| `seed-wus` | Seed role | [D: gitops/clusters/registry.yaml:133] |

Each has its own state backend file [D: terraform/backends/mgmt-we.tfbackend:1].

## Local development

No local instance of the platform exists in the tree — there is no compose file and
no local cluster definition. What runs locally is the test and gate set:

```bash
pre-commit install
pip install -e "tools/service_seed[test,render]" && python -m pytest tools/service_seed/tests -q
cd tools/mgmt-plane-lock && go test ./...
```

Both commands were run here and passed
[D: tools/service_seed/pyproject.toml:74] [D: tools/mgmt-plane-lock/go.mod:1]. Full
procedure: [`../runbook/runbook_DEV_RB-001-local-setup.md`](../runbook/runbook_DEV_RB-001-local-setup.md).

## Pipeline stages

| Stage | What runs | Evidence |
| --- | --- | --- |
| Pre-commit | fmt, validate, tflint, checkov, private-key detection, the repository validators | [D: .pre-commit-config.yaml:25] |
| Pull request | Terraform meta-CI: format, validate, lint, scan, plan, comment | [D: .github/workflows/terraform-ci.yml:1] |
| Pull request | Portal static analysis | [D: .github/workflows/sonar.yml:1] |
| Post-merge | Two-phase apply, management plane before workloads | [D: .github/workflows/terraform-apply.yml:1] |
| Continuous | ArgoCD reconciles what is committed | [D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1] |

I: plan output is truncated to a single threshold before being posted — basis: one truncation implementation exists and its contract forbids a caller-supplied limit [D: scripts/truncate-plan-output.py:45].

## Release mechanics

- **State bootstrap** is a one-time script per environment, creating the account and a delete lock [D: scripts/bootstrap-tfstate.sh:59].
- **Provisioning** is `terraform init` against a backend file, then `apply` [D: terraform/backends/prod-we.tfbackend:1]. I: unrun here.
- **Infrastructure provider** is switchable between CAPZ and Crossplane [D: terraform/variables.tf:1]. I: unrun here.
- **Platform controllers** ship as charts [D: gitops/platform/mgmt-plane-lock/Chart.yaml:1].
- **Workload releases** follow the tier's canary steps [D: tools/service_seed/profiles/rollout.yaml:12].
- **Secret expiry** rotates on a 90-day boundary [D: terraform/keyvaults.tf:59].

## Configuration and secrets

| Kind | Where | Evidence |
| --- | --- | --- |
| Cluster identity | The committed registry | [D: gitops/clusters/registry.yaml:1] |
| Addon values | Per-addon overlays | [D: gitops/environments/default/addons/argo-cd/values.yaml:1] |
| Controller settings | Environment variables with validated bounds | [D: tools/mgmt-plane-lock/internal/config/config.go:61] |
| Secret material | Key Vault, catalogued by write ownership | [D: terraform/locals.tf:41] |
| SaaS credentials | Vault, rotated | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] |
| Terraform state | Per environment and cluster | [D: scripts/validate-state-partitioning.sh:1] |

## Open questions

- OPEN: Which workflow actually applies Terraform to which subscription, and who approves it? The apply workflow exists [D: .github/workflows/terraform-apply.yml:1]; the environment protection rules are not in the repository.
- OPEN: No deployment of the Go controllers is visible beyond their charts; what builds and publishes their images?
- OPEN: Rollback is not described anywhere in the tree as a procedure; reverting a commit is implied by GitOps but no runbook states it.
- OPEN: The survey found no CI configuration at all, which is wrong — three workflow files exist. Treat any CI claim here as read from the files themselves, not from the survey.
