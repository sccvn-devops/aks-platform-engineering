---
page_type: sample
languages:
- bash
- terraform
- yaml
- json
- go
- python
- typescript
products:
- azure
- azure-resource-manager
- azure-kubernetes-service
- azure-container-registry
- azure-storage
- azure-blob-storage
- azure-storage-accounts
- azure-monitor
- azure-log-analytics
- azure-virtual-machines
name: Building a Platform Engineering Environment on Azure Kubernetes Service (AKS)
description: Production-grade Internal Developer Platform (IDP) on Azure AKS using the GitOps Bridge pattern — multi-cluster, multi-region, with progressive delivery, supply-chain verification, and disaster recovery built in.
urlFragment: aks-platform-engineering
---

# IDP GitOps Platform — Multi-Cluster AKS with the GitOps Bridge Pattern

This repository builds a production-grade **Internal Developer Platform (IDP)** on Azure AKS using the [GitOps Bridge Pattern](https://github.com/gitops-bridge-dev/gitops-bridge). It is forked from the upstream [Azure-Samples/aks-platform-engineering](https://github.com/Azure-Samples/aks-platform-engineering) sample and extends it with a complete control plane (mgmt-leader-lease, controller-scaler, saas-token-rotator, argocd-jira-bridge), a service-onboarding pipeline (`tools/service_seed/`), per-namespace secret isolation (ESO + per-region AKV + UAMI prefix RBAC), and progressive delivery via Argo Rollouts gated by SLO class.

The repo's authoritative architecture documents live at:

- [`docs/architect.md`](docs/architect.md) — comprehensive architectural reference (start here)
- [`walkthrough.md`](walkthrough.md) — runnable top-to-bottom code walkthrough (built with `uvx showboat`)
- [`_docs/IDP-GitOps-ADRs-v2.md`](_docs/IDP-GitOps-ADRs-v2.md) — 32 ADRs, authoritative *why* for every decision
- [`_docs/IDP-GitOps-Blueprint-PRD.md`](_docs/IDP-GitOps-Blueprint-PRD.md) + [v3](_docs/IDP-GitOps-Blueprint-PRD-v3.md), [v3.1](_docs/IDP-GitOps-Blueprint-PRD-v3.1.md), [v4](_docs/IDP-GitOps-Blueprint-PRD-v4.md) — functional contracts

## The three planes

| Plane | What lives there | Failure isolation |
|---|---|---|
| **DX Plane** | Jira (intake), Bitbucket Cloud (git remote), Jenkins on mgmt-we (CI single-replica per ADR-001-v2) | Atlassian-SaaS-bounded; outage pauses CI only |
| **Control Plane** | `mgmt-we` (active) + `mgmt-ne` (standby, controllers at zero) — ArgoCD hub, Crossplane, mgmt-plane-lock binaries, secret rotators, observability | Active-Passive — Azure Storage Blob lease arbitrates active cluster (ADR-022) |
| **Data Plane** | `aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne` workload clusters; `seed-wus` DR bootstrap | Active-Active reads + Active-Passive writes (ADR-004-v2) |

## Architecture diagram

![Platform Engineering on AKS Architecture Diagram](./images/AKS-platform-engineering-architecture.png)

## Cluster topology

Authoritative topology lives in [`gitops/clusters/registry.yaml`](gitops/clusters/registry.yaml) — a single committed YAML registry consumed by Terraform (`yamldecode`), ArgoCD ApplicationSet locals, and `tools/service_seed/` (ADR-031-v4). Schema enforced by `gitops/clusters/registry.schema.json`.

| Cluster | Region | Role |
|---|---|---|
| `mgmt-we` | West Europe | **Active control plane** — ArgoCD hub, Jenkins, Crossplane |
| `mgmt-ne` | North Europe | Standby — controllers at zero replicas (ADR-017) |
| `aks-dev-we` | West Europe | Dev workloads, single-AZ |
| `aks-staging-we` | West Europe | Staging, multi-AZ |
| `aks-prod-we` | West Europe | Production, multi-AZ, Premium SKU |
| `aks-prod-ne` | North Europe | Production replica, multi-AZ, Premium SKU |
| `seed-wus` | West US 2 | DR seed — single-node catastrophic bootstrap |

## Module map

### Infrastructure as Code — `terraform/`

| File / module | Role |
|---|---|
| `terraform/registry.tf` | Loads `gitops/clusters/registry.yaml` into `local.cluster_registry` (ADR-031-v4) |
| `terraform/clusters.tf` | Provisions workload AKS clusters via `Azure/aks/azurerm` module, parameterized by the registry |
| `terraform/networking.tf` | Hub-spoke VNets, Azure Firewall (Premium), per-region UDR forcing egress through firewall |
| `terraform/keyvaults.tf` | Per-region AKV pair in RBAC mode (ADR-019); private endpoints in hub |
| `terraform/external_secrets.tf` | ESO UAMI per namespace per workload cluster (ADR-005-v2 + ADR-020) |
| `terraform/argocd_bootstrap.tf` | Installs ArgoCD via Helm, applies App-of-Apps ApplicationSet |
| `terraform/modules/workload_identity/` | Reusable workload-identity module: UAMI + federated credential + role assignments (FR-V4-05..09) |
| `terraform/jenkins.tf`, `terraform/acr.tf`, `terraform/storage.tf`, `terraform/velero.tf` | Single-purpose resource files for each addon's Azure dependencies |

### Go tooling — `tools/mgmt-plane-lock/`

The Control Plane's lifecycle controllers. Each binary's `main.go` is ≤40 lines (FR-V4-26); orchestration lives in `internal/`.

| Binary | Role | Internal package |
|---|---|---|
| **mgmt-leader-lease** | Holds the Azure Storage Blob lease that designates the active mgmt cluster | `bloblease.LeaseRunner` |
| **controller-scaler** | Scales Crossplane/ArgoCD to 0 on standby; restores on lease change | `scaling.Runner` + `kube` |
| **saas-token-rotator** | Quarterly rotation of Bitbucket OAuth + Jira API tokens → AKV | `rotation.Runner` + `akvwriter` + `httpx` |
| **argocd-jira-bridge** | Opens Jira ticket when an ArgoCD Application enters Degraded (ADR-012) | `jirabridge` + `httpx` |
| **mgmt-cli** | Operator escape hatch (fate under review — OQ-V4-04) | — |

Shared internal packages:

| Package | Purpose |
|---|---|
| `internal/httpx` | Transport seam — timeouts, retries, redacted errors (FR-V4-15..18) |
| `internal/akvwriter` | Single AKV write path with typed errors + Strategy enum (FR-V4-19..22) |
| `internal/bootstrap` | SIGTERM-aware context + metrics server (FR-V4-23) |
| `internal/config` | Env-var-based configuration loader |
| `internal/kube` | Kubernetes client wiring used by `scaling` |

### Python tooling — `tools/service_seed/`

Service onboarding pipeline. Per US-V4-07 the original 981-line `seed_job.py` has split into focused modules:

| Module | Role |
|---|---|
| `jira_intake.py` | Jira fetch → typed `ServiceRequest` dataclass; pure parse/validate |
| `service_template.py` | Render templates → working tree on disk; consumes SLO/rollout profiles from `profiles/{slo,rollout}.yaml` |
| `gitops_pr.py` | Compose + push GitOps PR via Bitbucket; owns the git subprocess + Bitbucket client |
| `cli.py` | Thin orchestrator — argparse + env-var load + cluster-registry load + wire three modules |
| `cookiecutter-service/` | Cookiecutter template for the new service repo |
| `templates/`, `profiles/` | Jinja2 templates and SLO/rollout profile YAML (FR-V4-32..35) |

### GitOps content — `gitops/`

ArgoCD-managed content, organised by the two-tier infra→workload pattern (ADR-013-v2).

| Path | Tier | Content |
|---|---|---|
| `gitops/clusters/registry.yaml` | data | Cluster topology (ADR-031-v4) |
| `gitops/bootstrap/control-plane/addons/azure/` | infra | Crossplane Azure provider configs |
| `gitops/bootstrap/control-plane/addons/oss/` | infra | ESO, Kyverno, Argo Rollouts, Jenkins, Kargo, Velero, etc. |
| `gitops/bootstrap/workloads/infra/` | infra | Per-workload-cluster infra (namespaces, Argo Rollouts policies) |
| `gitops/environments/default/addons/` | infra | Per-addon Helm values overlays |
| `gitops/platform/` | infra | Crossplane compositions, ESO bootstrap, Kyverno policies, mgmt-plane-lock manifests |
| `gitops/apps/<service>/` | workload | Per-service kustomize overlays, Rollouts, Claims |

### CI / quality gates — `.github/`, top-level

| Workflow / file | Purpose |
|---|---|
| `.github/workflows/terraform-ci.yml` | PR-time pipeline: fmt → validate → tflint → checkov → plan → PR comment |
| `.github/workflows/terraform-apply.yml` | Post-merge apply, two-phase matrix (mgmt-first, workloads-after; FR-V4-10..14) |
| `.github/workflows/sonar.yml` | SonarQube for Backstage TypeScript + Dockerfiles (ADR-028-v3) |
| `.github/workflows/reusable/` | `workflow_call:` reusable workflows for plan + apply |
| `.pre-commit-config.yaml` | terraform_fmt, terraform_validate, terraform_checkov, tflint, detect-private-key |
| `.tflint.hcl` | tflint with `tflint-ruleset-azurerm` |
| `.checkov.yaml`, `.checkov.baseline` | Supply-chain scanning with per-finding rationale schema |
| `prd.json` | Ralph autonomous-agent execution queue — 13 stories (PRD-v3.1 + PRD-v4) |

## Domain glossary (quick reference)

| Term | Meaning |
|---|---|
| **IDP** | Internal Developer Platform — the full system this repo provisions |
| **mgmt cluster** | Management cluster — `mgmt-we` (active), `mgmt-ne` (standby) |
| **workload cluster** | AKS cluster running tenant workloads — `aks-{dev,staging,prod-we,prod-ne}` |
| **App-of-Apps** | ArgoCD pattern where one ApplicationSet manages child ApplicationSets/Applications |
| **ESO** | External Secrets Operator — syncs secrets from AKV into Kubernetes |
| **UAMI** | User-Assigned Managed Identity — per-namespace Azure identity for AKV access |
| **AKV** | Azure Key Vault |
| **SLO class** | bronze / silver / gold — tier of progressive delivery strictness |
| **XRD** / **Claim** | Crossplane Composite Resource Definition / developer-facing Claim |
| **mgmt-leader-lease** | Go controller holding the blob lease for active mgmt cluster |
| **controller-scaler** | Go controller scaling Crossplane/ArgoCD to zero on standby |
| **seed cluster** | `seed-wus` — West US 2 single-node DR bootstrap |
| **CAPZ** | Cluster API Provider Azure — alternative infra provider (default is Crossplane) |

Full glossary in [`docs/agents/domain.md`](docs/agents/domain.md).

## End-to-end onboarding flow

1. **Jira ticket** for a new service → `jira_intake.parse` produces typed `ServiceRequest`
2. **Cluster registry** consulted → cluster identity (subscription, RG, ACR) resolved
3. **Service repo** scaffolded via cookiecutter → pushed to Bitbucket
4. **GitOps PR** composed by `gitops_pr.py` → kustomize overlays + Argo `Rollout` + Crossplane Claims
5. **PR merges** → ArgoCD's App-of-Apps picks up → ApplicationSet fans out to target cluster
6. **Crossplane** reconciles Claims → provisions Azure resources (SQL, Cosmos, Service Bus)
7. **ESO** projects per-namespace secrets from per-region AKV
8. **Argo Rollouts** canaries new version per SLO class (gold/silver/bronze)
9. **argocd-jira-bridge** opens Jira ticket if rollout degrades
10. **mgmt-leader-lease + controller-scaler** ensure only one mgmt cluster reconciles at a time

## Prerequisites

- An active Azure subscription
- Azure CLI 2.60.0+
- Terraform 1.5.x (pinned per ADR-029-v3 — see [`.tool-versions`](.tool-versions) for the full toolchain)
- `kubectl` 1.28.9+
- `pre-commit` (run `pre-commit install` after clone)

## Getting started

### Bootstrap remote Terraform state (one-time, per environment)

```bash
./scripts/bootstrap-tfstate.sh
```

This provisions the state Storage Account in `rg-tfstate-bootstrap` with a `CanNotDelete` management lock (ADR-023-v3 / FR-V3-01). State files use the pattern `tfstate/<env>/<cluster>.tfstate`.

### Provision the control plane

```bash
cd terraform
terraform init -backend-config=backends/mgmt-we.tfbackend -upgrade

# With CAPZ (default)
terraform apply -var gitops_addons_org=https://github.com/sccvn-devops --auto-approve

# With Crossplane
terraform apply -var gitops_addons_org=https://github.com/sccvn-devops \
                -var infrastructure_provider=crossplane --auto-approve
```

Terraform creates the AKS mgmt cluster, installs ArgoCD via Helm, and applies the App-of-Apps ApplicationSet that targets `gitops/bootstrap/control-plane/addons/`. From that point ArgoCD reconciles every addon, workload cluster, and tenant app.

### Access the Control Plane

```bash
export KUBECONFIG=<repo>/terraform/kubeconfig
kubectl get secrets argocd-initial-admin-secret -n argocd \
  --template="{{index .data.password | base64decode}}"
kubectl port-forward svc/argo-cd-argocd-server -n argocd 8080:443
```

Open https://localhost:8080, username `admin`.

### Run the developer-tooling test suites

```bash
# Go
cd tools/mgmt-plane-lock && go test -race ./...

# Python service-seed
cd tools/service_seed && pytest --cov

# Terraform module tests
cd terraform/modules/workload_identity && terraform test
```

## Onboarding a new development team

See [`docs/Onboard-New-Dev-Team.md`](docs/Onboard-New-Dev-Team.md) — covers cluster handoff + ArgoCD access for tenant developers.

## DR runbooks

- [`docs/management-plane-failover-runbook.md`](docs/management-plane-failover-runbook.md) — `mgmt-we` → `mgmt-ne` failover
- [`docs/seed-cluster-dr-runbook.md`](docs/seed-cluster-dr-runbook.md) — catastrophic recovery from `seed-wus`

## What's in flight

| PRD | Status | Theme |
|---|---|---|
| [PRD-v4](_docs/IDP-GitOps-Blueprint-PRD-v4.md) | Approved 2026-05-26 | Architecture deepening + hardening (11 user stories) |
| [PRD-v3.1](_docs/IDP-GitOps-Blueprint-PRD-v3.1.md) | Draft — pre-v4 gate | AKV-native rotation + TLS history purge (must merge before v4 P0) |
| [PRD-v3](_docs/IDP-GitOps-Blueprint-PRD-v3.md) | Accepted 2026-05-26 | Terraform code-quality + supply-chain hardening |

ADR-031-v4 (cluster topology registry) was Accepted 2026-05-26 alongside PRD-v4.

## Choosing the infrastructure provider

Set `var.infrastructure_provider` to `capz` (default) or `crossplane`. See [`docs/capz-or-crossplane.md`](docs/capz-or-crossplane.md) for the trade-off comparison. The Azure Service Operator (ASO) install bundled with CAPZ can be customised by editing [`gitops/environments/default/addons/cluster-api-provider-azure/values.yaml`](gitops/environments/default/addons/cluster-api-provider-azure/values.yaml).

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft trademarks or logos is subject to and must follow Microsoft's Trademark & Brand Guidelines. Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship. Any use of third-party trademarks or logos are subject to those third-party's policies.
