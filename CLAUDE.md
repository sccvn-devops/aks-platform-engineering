# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repo Is

A production-grade **Internal Developer Platform (IDP)** on Azure AKS using the [GitOps Bridge Pattern](https://github.com/gitops-bridge-dev/gitops-bridge). Three logical planes:

- **DX Plane** — Jira + Bitbucket Cloud + Jenkins (CI, isolated from Azure)
- **Control Plane** — ArgoCD hub on `mgmt-we`/`mgmt-ne` + Crossplane (infra provisioning)
- **Data Plane** — Workload AKS clusters (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`)

Infrastructure provider is switchable via `var.infrastructure_provider`: `capz` (default) or `crossplane`.

## Repository Layout

| Directory | Purpose |
|---|---|
| `terraform/` | Provisions AKS clusters, VNets, AKV, ACR, Jenkins, ArgoCD bootstrap |
| `gitops/bootstrap/control-plane/addons/` | ArgoCD App-of-Apps for control-plane addons (argo-cd, kargo, kyverno, ESO, velero, …) |
| `gitops/bootstrap/workloads/infra/` | ArgoCD infra project + Argo Rollouts for workload clusters |
| `gitops/environments/default/addons/` | Per-addon Helm values overlays |
| `gitops/clusters/` | CAPZ `AzureManagedCluster` and ArgoCD ApplicationSet definitions |
| `gitops/platform/` | Platform controllers: Crossplane compositions, ESO bootstrap, Kyverno policies, mgmt-plane-lock, namespace-vault-binding-claim |
| `gitops/apps/` | Sample workload app (`myapp`) |
| `gitops/hooks/` | Identity and sleep hooks |
| `backstage/` | Backstage IDP portal (pnpm workspace — `packages/app`, `packages/backend`) |
| `scripts/ralph/` | Automation scripts for the `ralph` branch workflow |
| `_docs/` | Architecture PRD, ADRs, and Blueprint documents |
| `docs/` | Operational guides (onboarding, capz-vs-crossplane, Backstage) |

## Commands

### Terraform (run from `terraform/`)

```bash
terraform init -upgrade
terraform fmt -recursive && terraform validate

# Provision with capz (default)
terraform apply -var gitops_addons_org=https://github.com/<your-org> --auto-approve

# Provision with crossplane
terraform apply -var gitops_addons_org=https://github.com/<your-org> \
                -var infrastructure_provider=crossplane --auto-approve
```

Copy `terraform/tfvars` to `terraform/terraform.tfvars` and edit for persistent config.

### Access Control Plane

```bash
export KUBECONFIG=<repo>/terraform/kubeconfig
kubectl get secrets argocd-initial-admin-secret -n argocd --template="{{index .data.password | base64decode}}"
kubectl port-forward svc/argo-cd-argocd-server -n argocd 8080:443
```

### Backstage (run from `backstage/`)

```bash
yarn install
yarn dev              # frontend + backend together
yarn build:all
yarn test             # unit tests
yarn test:all         # with coverage
yarn test:e2e         # Playwright
yarn lint:all
yarn prettier:check
```

## Key Architecture Decisions

See `_docs/IDP-GitOps-ADRs-v2.md` for the authoritative ADR set. Highlights:

- **ADR-007**: ArgoCD with ApplicationSet is the GitOps engine. App-of-Apps pattern: Terraform applies one ApplicationSet to the cluster; ArgoCD syncs everything under `gitops/bootstrap/control-plane/addons/`.
- **ADR-001-v2**: Jenkins is a **single-replica StatefulSet** in `mgmt-we` only — not HA. CI pauses during `mgmt-we` outage (~30 min RTO); drift correction continues via `mgmt-ne`.
- **ADR-004-v2**: Management plane is Active-Passive (`mgmt-we` primary, `mgmt-ne` standby with controllers scaled to zero — ADR-017). Data plane is Active-Active reads / Active-Passive writes.
- **ADR-022**: Management-plane singleton lock uses Azure Storage Blob lease (`leases/mgmt-active`) — only one mgmt cluster active at a time.
- **ADR-005-v2 / ADR-019 / ADR-020**: Secrets via ESO + per-region AKV pair + per-namespace UAMI with prefix-scoped RBAC. No cluster-wide secret stores.
- **ADR-008-v2**: Supply chain — Cosign + AKV + Kyverno policy enforcement (not Gatekeeper).
- **ADR-021**: Argo Rollouts is mandatory for production progressive delivery (canary with SLO analysis).
- **ADR-013-v2**: Two-tier GitOps layout — infra tier (`gitops/bootstrap/`) then workload tier (`gitops/apps/`), with sync waves within each.

## Cluster Topology

| Cluster | Region | Role |
|---|---|---|
| `mgmt-we` | West Europe | Active control plane — ArgoCD hub, Jenkins CI, Crossplane |
| `mgmt-ne` | North Europe | Standby — controllers at zero replicas; promoted on `mgmt-we` loss |
| `aks-dev-we` | West Europe | Dev workloads, single-AZ |
| `aks-staging-we` | West Europe | Staging, multi-AZ |
| `aks-prod-we` | West Europe | Production, multi-AZ, Premium SKU |
| `aks-prod-ne` | North Europe | Production replica, multi-AZ, Premium SKU |
| `seed-wus` | West US 2 | DR seed — single-node, catastrophic bootstrap only |

## GitOps Addon Labels

Terraform labels the AKS cluster metadata with which addons to enable (e.g., `enable_argocd=true`, `enable_kyverno=true`). ArgoCD ApplicationSet uses those labels to determine which apps to install. Changing an addon's enabled state means changing `terraform/main.tf` locals and re-applying.

## Naming Conventions

- Terraform resources: snake_case
- Kubernetes manifests: kebab-case, directory-scoped (`gitops/environments/default/addons/<addon-name>/`)
- YAML: 2-space indent
- Backstage: PascalCase for React components, camelCase for functions, `*.test.tsx` / `*.spec.ts` for tests
- Conventional commits preferred; include issue/PR ref when available

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `sccvn-devops/aks-platform-engineering`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default canonical label names (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context repo. ADRs live at `_docs/IDP-GitOps-ADRs-v2.md` (not `docs/adr/`). See `docs/agents/domain.md`.