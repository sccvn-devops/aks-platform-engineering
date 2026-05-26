<!--
Archived 2026-05-25 - represents PRD-v3 state; superseded by PRD-v4.
See _docs/IDP-GitOps-Blueprint-PRD-v4.md for the current authoritative PRD.
Current live document: docs/architect.md (aligned with PRD-v4).
-->

# IDP GitOps Platform — Engineering Guide

> Authoritative operational and architectural reference for the CityOS Internal Developer Platform.
> Synthesized from: IDP-GitOps-Blueprint-v2.md, IDP-GitOps-ADRs-v2.md, IDP-GitOps-Blueprint-PRD.md.
> Last updated: 2026-05-22.

---

## Table of Contents

1. [Glossary and Naming Conventions](#1-glossary-and-naming-conventions)
2. [Architecture Overview](#2-architecture-overview)
3. [Cluster Topology](#3-cluster-topology)
4. [Network Topology](#4-network-topology)
5. [GitOps Patterns](#5-gitops-patterns)
6. [Secrets Management](#6-secrets-management)
7. [Management-Plane Singleton Lock](#7-management-plane-singleton-lock)
8. [CI Pipeline](#8-ci-pipeline)
9. [Supply Chain Security](#9-supply-chain-security)
10. [Progressive Delivery](#10-progressive-delivery)
11. [Self-Service Service Seed Workflow](#11-self-service-service-seed-workflow)
12. [Observability and Alerting](#12-observability-and-alerting)
13. [DR Runbooks](#13-dr-runbooks)
14. [Key ADR Decisions](#14-key-adr-decisions)

---

## 1. Glossary and Naming Conventions

### 1.1 Core Terms

| Term | Definition |
|---|---|
| **Workload** | Any application serving customer- or business-facing traffic. Runs in workload clusters only. |
| **Platform component** | Any service supporting developer or operator experience: ArgoCD, Crossplane, ESO controllers, Reloader, Velero, Jenkins, observability stack, `argocd-jira-bridge`, `mgmt-leader-lease`. Runs in management clusters only. |
| **Management cluster** | A cluster named `mgmt-<region>`. Hosts only platform components. Kyverno rejects any Pod whose namespace lacks `tier=platform`. |
| **Workload cluster** | A cluster named `aks-<env>-<region>`. Hosts workloads plus minimum operators (ESO agent, Reloader, Argo Rollouts controller, Kyverno). |
| **Seed cluster** | `seed-wus` — a single-node cluster in West US 2 used only as rebuild origin for management clusters during catastrophic DR. |
| **Active management cluster** | Whichever mgmt cluster currently holds the Azure Storage Blob Lease. Exactly one at any time. |
| **Standby management cluster** | All mgmt clusters not holding the lease. All controllers scaled to zero replicas. |
| **DX plane** | Jira Cloud, Bitbucket Cloud, Jenkins. Source of intent, not runtime state. |
| **Control plane** | Active management cluster components that reconcile Git intent → cluster/Azure state. |
| **Data plane** | Workload AKS clusters and Azure PaaS resources they consume. |
| **Active-Active reads** | Front Door routes user traffic to the geographically nearer prod cluster; both regions serve reads. |
| **Active-Passive writes** | Cosmos has a single write region (WE primary). `multipleWriteLocationsEnabled: false`. Auto-failover to NE on WE loss (`automaticFailoverEnabled: true`). |
| **SLO class** | Platform availability tier — `bronze`, `silver`, or `gold` — determining Azure SKU, multi-AZ posture, and required progressive-delivery strategy. |
| **PlatformBot** | Bitbucket service account used by Jenkins for GitOps commits. Uses workspace access tokens stored in AKV, never user app passwords. |

### 1.2 Naming Standards

| Resource type | Pattern | Example |
|---|---|---|
| Cluster | `<role>-<region>` or `<role>-<env>-<region>` | `mgmt-we`, `aks-prod-we` |
| Azure Key Vault | `kv-platform-<env>-<region>` | `kv-platform-prod-we` |
| ACR | `acr<platform><env>` (no dashes) | `acrplatformprod` |
| UAMI (per namespace) | `uami-<namespace>-<cluster>` | `uami-payments-aks-prod-we` |
| AKV secret | `<namespace>-<resource>-<purpose>` | `payments-sql-conn` |
| ArgoCD Application (infra) | `<svc>-infra-<env>` | `myapp-infra-prod` |
| ArgoCD Application (workload) | `<svc>-app-<env>-<cluster>` | `myapp-app-prod-aks-prod-we` |
| Crossplane XRD group | `platform.cityos.io` | — |

---

## 2. Architecture Overview

The platform isolates three failure planes so that an outage in one cannot disrupt the others:

| Plane | Components | Failure impact |
|---|---|---|
| **DX (Developer Experience)** | Jira Cloud, Bitbucket Cloud, Jenkins controller | Stops new changes. Running workloads unaffected. |
| **Control** | Active mgmt cluster: Crossplane, ArgoCD hub, ESO, Velero, `mgmt-leader-lease` | Pauses drift correction for ≤120s. Workloads keep serving. |
| **Data** | `aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne` | Direct user impact on that cluster only. |

**Cardinal rule:** all state lives in Git (`platform-gitops` on Bitbucket Cloud). Every cluster can be destroyed and rebuilt from the monorepo.

### Infrastructure Provider

Crossplane (Upbound `provider-azure`) is the **sole** cloud provisioning engine. Terraform provisions bootstrap infrastructure (VNets, AKS clusters, storage accounts, ACR) and registers clusters in ArgoCD. All subsequent cloud resources are provisioned declaratively via Crossplane XRDs.

---

## 3. Cluster Topology

### 3.1 Cluster Inventory

| Cluster | Region | Role | AZs | SKU |
|---|---|---|---|---|
| `mgmt-we` | West Europe | Active control plane | Single | Standard |
| `mgmt-ne` | North Europe | Standby (controllers at 0 replicas) | Single | Standard |
| `aks-dev-we` | West Europe | Dev workloads | Single | Standard |
| `aks-staging-we` | West Europe | Staging workloads | Multi | Standard |
| `aks-prod-we` | West Europe | Production workloads | Multi | Premium |
| `aks-prod-ne` | North Europe | Production replica | Multi | Premium |
| `seed-wus` | West US 2 | Catastrophic DR bootstrap | Single (1 node) | Standard |

### 3.2 Node Pools

`mgmt-we` has two node pools:

- **`systempool`** — system workloads (ArgoCD, Crossplane, ESO, etc.)
- **`cipool`** — Jenkins controller and ephemeral agent pods; taint `workload=ci:NoSchedule`, autoscaler 1–30 nodes. Exists only in `mgmt-we`.

All clusters run with: private API server, Workload Identity enabled, OIDC issuer enabled.

### 3.3 ArgoCD Cluster Secret Labels

Every registered cluster Secret carries mandatory labels enforced by Kyverno policy `KyvernoRequiredClusterSecretLabels`:

```yaml
labels:
  argocd.argoproj.io/secret-type: cluster
  env: dev | staging | prod | mgmt | seed
  region: westeurope | northeurope | westus2
  role: workload | management | seed
  lease-status: active | standby | n-a   # "n-a" for workload/seed clusters
```

`controller-scaler` updates `lease-status` on management cluster Secrets when the blob lease changes hands.

---

## 4. Network Topology

Hub-and-spoke VNet architecture with Azure Firewall Premium as the egress/DNAT gateway:

```
Hub VNet (10.0.0.0/16, West Europe)  ←──peered──→  Hub VNet (North Europe)
│  Azure Firewall Premium (egress FQDN allow-list + TLS inspection)
│  Azure Bastion (break-glass operator access; no public IPs on any cluster)
│  Private DNS Zones: AKV, SQL, Cosmos, Service Bus, ACR, Blob Storage
│
├── mgmt-we spoke    (10.1/16) — AKS private API; Jenkins + cipool
├── aks-prod-we      (10.2/16)
├── aks-staging-we   (10.3/16)
├── aks-dev-we       (10.4/16)
├── mgmt-ne          (10.5/16)
└── aks-prod-ne      (10.6/16)
```

**Bitbucket Cloud** has no Azure Private Link. Paths:

- **Inbound webhooks:** Bitbucket → Azure Front Door → WAF (Atlassian CIDR pin) → Firewall DNAT → Jenkins controller (internal Azure LB).
- **Outbound from Jenkins/ArgoCD:** Azure Firewall FQDN allow-list (`*.bitbucket.org`, `*.atlassian.net`).

No AKS cluster has a public IP on its API server.

**Workload user traffic routing** (Active-Active reads) is application-team responsibility. Each service team provisions their own Front Door profile or Traffic Manager via a Crossplane Composition claim. The platform provides the Active-Active prod cluster pair and the Crossplane Kubernetes provider.

---

## 5. GitOps Patterns

### 5.1 App-of-Apps Bootstrap

Terraform applies a single `cluster-addons` ApplicationSet (`terraform/bootstrap/addons.yaml`) to `mgmt-we`. This ApplicationSet recursively syncs everything under `gitops/bootstrap/control-plane/addons/oss/` via `directory.recurse: true`.

All addon ApplicationSets carry sync-wave annotations. ArgoCD waits for each wave to be fully Healthy before advancing:

| Wave | Resources |
|---|---|
| 10 | `kube-prometheus-stack` |
| 27–29 | Crossplane platform XRDs + ProviderConfigs, `mgmt-plane-lock` |
| 30–33 | ESO operator + bootstrap, Kyverno cosign key + policies |
| 34–38 | Jenkins secrets + pipeline, Velero, `jira-bridge`, `saas-token-rotator` |
| **40** | **`platform-infra-set` ApplicationSet** — ArgoCD waits here until all generated infra Applications are Healthy |
| **50** | **`workloads-set` ApplicationSet** — only applied after wave 40 is Healthy |

### 5.2 Two-Tier ApplicationSets

Every service gets two independent ArgoCD Applications:

| Tier | ApplicationSet | Naming | Sync policy | Retry |
|---|---|---|---|---|
| **Infra** | `platform-infra-set` (wave 40) | `<svc>-infra-<env>` | `prune: false`, `selfHeal: true` | limit 10, max backoff 30m |
| **Workload** | `workloads-set` (wave 50) | `<svc>-app-<env>-<cluster>` | `prune: true`, `selfHeal: true` | limit 5, max backoff 5m |

The wave-40/50 ordering enforces that all infra Applications are Healthy before any workload Application is created or synced. Both ApplicationSets use `ServerSideApply: true` and `ApplyOutOfSyncOnly: true`.

### 5.3 GitOps Repository Layout

```
gitops/
├── bootstrap/control-plane/addons/oss/   # All addon ApplicationSets + AppProjects
├── environments/default/addons/          # Per-addon Helm values overlays
├── platform/                             # Platform controller Helm charts
│   ├── crossplane/                       # XRDs + Compositions
│   ├── external-secrets-bootstrap/       # Per-cluster SecretStore + ExternalSecret
│   ├── kyverno-policies/                 # Policy overlays per cluster role
│   ├── mgmt-plane-lock/                  # mgmt-leader-lease Helm chart
│   ├── controller-scaler/                # controller-scaler Helm chart
│   └── argocd-jira-bridge/               # jira-bridge Helm chart
└── apps/<svc>/
    ├── infra/base/                       # Crossplane XRCs
    ├── infra/overlays/{dev,staging,prod}/
    ├── workload/base/                    # Rollout, Service, Ingress, ExternalSecrets
    └── workload/overlays/{dev,staging,prod}/
```

---

## 6. Secrets Management

### 6.1 Architecture

Zero-trust secret delivery via three layers:

```
Azure Key Vault (per region per env)
       ↑  Crossplane dual-write (both regional vaults simultaneously)
ESO SecretStore (per namespace; reads local regional vault only)
       ↓  ExternalSecret → Kubernetes Secret
Application Pod (mounts Secret; Reloader restarts pod on Secret hash change)
```

### 6.2 Per-Region AKV Pairs

| Vault | Region | Consumers |
|---|---|---|
| `kv-platform-dev-we` | West Europe | `aks-dev-we` |
| `kv-platform-staging-we` | West Europe | `aks-staging-we` |
| `kv-platform-prod-we` | West Europe | `aks-prod-we` |
| `kv-platform-prod-ne` | North Europe | `aks-prod-ne` |

All vaults: RBAC authorization mode (`enableRbacAuthorization: true`), soft-delete 90 days, purge protection enabled, public access disabled, private endpoint in regional hub.

Crossplane Compositions for SQL, Cosmos, and Service Bus **dual-write** connection secrets to both prod regional vaults using separate `keyvault.azure.upbound.io/v1beta1/Secret` resources bound to `azure-westeurope` and `azure-northeurope` ProviderConfigs. The `PerRegionAKVDualWriteSkew` alert fires if the two vaults drift more than 10 minutes apart.

### 6.3 Per-Namespace Identity Binding

No `ClusterSecretStore` is deployed anywhere (`processClusterStore: false`, `crds.createClusterSecretStore: false` in ESO Helm values).

Each workload namespace gets isolated identity via the `NamespaceVaultBinding` Crossplane Composition (`xnamespacevaultbindings.platform.cityos.io`), which creates:

- UAMI `uami-<namespace>-<cluster>`
- Federated identity credential for the namespace's `eso-sa` ServiceAccount
- `Key Vault Secrets User` role assignment on the local regional vault
- Namespace-scoped `SecretStore` and `ServiceAccount`

**Architecture note:** Azure Key Vault data-plane RBAC does not support attribute-based conditions (ABAC) on secret names. Isolation is enforced by per-namespace UAMI binding — each namespace's identity is only granted access to its own secrets. This is the maximum isolation Azure Key Vault supports for data-plane access (ADR-020).

### 6.4 SaaS Token Management

Bitbucket workspace token and Jira service-account token cannot use federated identity (SaaS providers do not accept Azure federated tokens inbound). They are:

- Stored exclusively in `kv-platform-mgmt-we`
- Delivered to Jenkins via ESO + Reloader
- Rotated quarterly by `saas-token-rotator` CronJob (lease-aware; only runs on active mgmt cluster)
- Old versions revoked 24 hours after rotation

---

## 7. Management-Plane Singleton Lock

### 7.1 Components

| Component | Location | Purpose |
|---|---|---|
| `mgmt-leader-lease` | `tools/mgmt-plane-lock/cmd/mgmt-leader-lease/` | Acquire/renew Azure Storage Blob Lease |
| `controller-scaler` | `tools/mgmt-plane-lock/cmd/controller-scaler/` | Watch ConfigMap; scale controllers up/down; update `lease-status` label |
| `mgmt-cli` | `tools/mgmt-plane-lock/cmd/mgmt-cli/` | Operator CLI for controlled failback and emergency break-lease |

### 7.2 Lease Semantics

```
TTL: 60s  |  Renewal interval: 15s  |  Acquire poll (when free): every 5s
Lease blob: stplatformmgmtlease / leases / mgmt-active  (GRS, private endpoint)
```

On **lease acquisition**:

1. `mgmt-leader-lease` writes `kube-system/mgmt-leader-status` ConfigMap (`status: active`)
2. `controller-scaler` detects change → scales: ArgoCD controllers 3/2/2, Crossplane 1, ESO 1, `jira-bridge` 1
3. Updates cluster Secret label `lease-status: active`

On **lease loss**:

1. Updates ConfigMap (`status: standby`)
2. `controller-scaler` scales all above → 0
3. Updates cluster Secret label `lease-status: standby`

### 7.3 Metrics and Alerting

- Metric: `mgmt_leader_lease_renewed_seconds` (Prometheus)
- Alert: `MgmtLeaderLeaseLost` — fires if renewal > 30s stale for 1 minute

### 7.4 Operator Commands

```bash
# Controlled failback after mgmt-we recovers
mgmt-cli failback --to mgmt-we --confirm

# Emergency: break a stuck lease (only if mgmt-leader-lease is not running)
mgmt-cli break-lease --confirm
```

---

## 8. CI Pipeline

### 8.1 Jenkins Deployment

Jenkins runs as a **single-replica StatefulSet** in `mgmt-we` only (ADR-001-v2):

```yaml
replicas: 1
nodeSelector: { nodepool: cipool }
tolerations: [{ key: workload, value: ci, effect: NoSchedule }]
terminationGracePeriodSeconds: 300
volumeClaimTemplates:
  - ReadWriteOnce, Azure Disk Premium SSD   # $JENKINS_HOME
livenessProbe: GET /login
readinessProbe: GET /whoAmI/api/json
```

CI is **paused during `mgmt-we` loss** (~30 min RTO via DR runbook). Drift correction continues via `mgmt-ne` promotion. This trade-off is explicit and accepted (ADR-001-v2).

Velero backs up `$JENKINS_HOME` every 6 hours to GRS storage with 30-day retention.

### 8.2 Credential Sourcing

All Jenkins credentials come from AKV via ESO + Reloader. No static credentials stored in Jenkins. On token rotation, Reloader detects the Secret hash change and restarts the Jenkins controller pod.

| Jenkins credential | AKV secret | Purpose |
|---|---|---|
| `bitbucket-workspace-token` | `jenkins-bitbucket-workspace-token` | Bitbucket API + repo access |
| `jira-service-account-token` | `jenkins-jira-service-account-token` | Jira ticket creation |

### 8.3 Pipeline Loop Prevention (ADR-010-v2)

Two-layer defense against PlatformBot commit loops:

1. **Primary (SCM trait):** `committersToIgnore("PlatformBot")` + `BitbucketBranchCommitSkipTrait` in Bitbucket Branch Source configuration. Jenkins ignores webhooks for commits by PlatformBot.
2. **Defensive:** `[ci skip]` marker in all PlatformBot commit messages.

The GitOps update stage only modifies `apps/<svc>/workload/overlays/dev/` — never `infra/`. Push retry: 5 attempts with `git pull --rebase` between each.

### 8.4 Image Build and Signing

```
git push → Bitbucket webhook → Front Door → WAF → Firewall DNAT → Jenkins
Jenkins:
  1. az acr build → pushes image to acrplatformprod (private endpoint)
  2. cosign sign --key azurekms://kv-platform-mgmt-we.vault.azure.net/cosign-signing-key
     (uses Jenkins Workload Identity federated token; no static key material)
  3. Signature attestation stored alongside image in ACR
  4. kustomize edit set image → commit [ci skip] → push with retry
```

---

## 9. Supply Chain Security

### 9.1 Signing

HSM-backed `cosign-signing-key` in `kv-platform-mgmt-we`. Jenkins UAMI has `Key Vault Crypto User`. Every image built in the pipeline is signed; the signature is verified in the same pipeline step before the GitOps commit proceeds.

### 9.2 Verification at Admission (Kyverno)

`cosign-public-key` is synced from the regional AKV to each workload cluster's `kyverno` namespace via ESO `ExternalSecret`. Kyverno `ClusterPolicy` verifies the Cosign signature at admission:

| Cluster | Verification mode |
|---|---|
| `aks-dev-we` | `Audit` (warn, don't block) |
| `aks-staging-we` | `Enforce` |
| `aks-prod-we`, `aks-prod-ne` | `Enforce` |

System namespaces (`kube-system`, `kyverno`, `external-secrets`) are excluded from verification.

### 9.3 Full Admission Policy Stack

| Policy | Scope | Effect |
|---|---|---|
| `kyverno-required-cluster-secret-labels` | All clusters | Enforces mandatory labels on ArgoCD cluster Secrets |
| `kyverno-mgmt-tier-platform` | Management clusters | Rejects Pods in namespaces without `tier=platform` label |
| `kyverno-prod-requires-rollout` | Prod workload clusters | Rejects `kind: Deployment`; must use `kind: Rollout` |
| `kyverno-cosign-verify` | All workload clusters | Verifies Cosign image signature at admission |

---

## 10. Progressive Delivery

### 10.1 Argo Rollouts Deployment

Argo Rollouts controllers run **only on workload clusters** (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`). The `addons-argo-rollouts` ApplicationSet selects on both `role: workload` AND `enable_argo_rollouts: 'true'`. Management clusters are explicitly excluded.

`kyverno-prod-requires-rollout` rejects `kind: Deployment` on prod clusters — all production workloads must use `kind: Rollout`.

### 10.2 SLO Classes and AnalysisTemplates

AnalysisTemplates are auto-generated per namespace via the `NamespaceRolloutPolicy` Crossplane Composition (`xnamespacerolloutpolicies.platform.cityos.io`):

| SLO class | Canary steps | Analysis metrics | Abort behavior |
|---|---|---|---|
| **Gold** | 5% → 25% → 50% → 100%, 5m pauses | success-rate ≥ 99%, p99-latency ≤ 500ms | Either metric fails → canary 0%, stable 100% |
| **Silver** | 25% → 100% | success-rate ≥ 99% | Analysis fail → abort |
| **Bronze** | `setWeight: 100` (direct cutover) | None | N/A |

### 10.3 Rollout Failure Handling

On rollout failure (automatic chain):

1. Argo Rollouts aborts → restores 100% stable traffic
2. ArgoCD marks Application `Degraded`
3. `argocd-jira-bridge` detects `Degraded` → opens `[SEV1]` Jira issue
4. `RolloutAnalysisFailed` PrometheusRule fires within 60 seconds

---

## 11. Self-Service Service Seed Workflow

### 11.1 Trigger

A Jira ticket of type **"IDP Service Request"** triggers a Jenkins webhook (`generic-webhook-trigger` plugin) → `platform-service-seed` pipeline job.

### 11.2 Seed Job Steps

1. Validates Jira issue type; extracts service name and SLO class from the ticket
2. Creates Bitbucket repository from Cookiecutter template (`tools/service_seed/cookiecutter-service/`)
3. Generates `apps/<svc>/infra/` scaffold (Crossplane XRCs):
   - `xrc-sql.yaml`, `xrc-cosmos.yaml`, `xrc-sb.yaml`, `xrc-namespace-binding.yaml`
   - Kustomize overlays for `dev`, `staging`, `prod`
4. Generates `apps/<svc>/workload/` scaffold:
   - `rollout.yaml` (`kind: Rollout` for prod overlay, `kind: Deployment` for dev/staging)
   - `analysis-template.yaml` (auto-selected from SLO class via `NamespaceRolloutPolicy` claim)
   - `service.yaml`, `ingress.yaml`, `external-secret-sql.yaml`, `external-secret-cosmos.yaml`, `external-secret-sb.yaml`
   - Kustomize overlays for `dev`, `staging`, `prod`
5. Pushes infra and workload branches to `platform-gitops`
6. Opens **two PRs** — one for `infra/`, one for `workload/`

Source: `tools/service_seed/seed_job.py`

### 11.3 End-to-End Timeline (target p95)

```
Jira ticket created
  → Jenkins seed job (~2 min): repo created, PRs opened
  → PR review and merge (~human-gated)
  → ArgoCD syncs platform-infra-set (~30 min): Crossplane provisions SQL/Cosmos/SB
  → ArgoCD syncs workloads-set (~3 min): workload pods running in dev

Total target: ≤ 45 minutes (infra provision is the long pole)
```

---

## 12. Observability and Alerting

### 12.1 Stack

`kube-prometheus-stack` (Prometheus + Grafana) deployed on `mgmt-we` via `addons-kube-prometheus-stack-appset.yaml`. Managed by `controller-scaler` (scales to 0 on standby cluster). PagerDuty integration for critical alerts.

### 12.2 Platform Alerts

| Alert | Condition | Severity |
|---|---|---|
| `MgmtLeaderLeaseLost` | `mgmt_leader_lease_renewed_seconds` stale > 30s for 1m | Critical |
| `PerRegionAKVDualWriteSkew` | Vault pair out-of-sync > 10m | Warning |
| `SaaSTokenAgeExceeded` | Token age > 100 days | Warning |
| `RolloutAnalysisFailed` | `rollout_phase == Degraded` for any Application | Critical |

### 12.3 Platform SLO Dashboard (Grafana)

| SLO | Target |
|---|---|
| Time-to-deploy (workload tier, p95) | ≤ 5 minutes |
| Time-to-provision (infra tier, p95) | ≤ 30 minutes |
| Drift-correction MTTR (p95) | ≤ 3 minutes |
| Secret freshness (p95) | ≤ 90 seconds |
| Management-plane failover RTO (p99) | ≤ 120 seconds |

---

## 13. DR Runbooks

### 13.1 Management-Plane Failover (Automatic, RTO ≤ 120s)

**Trigger:** `mgmt-we` loses the Azure Blob Lease (cluster failure, network partition, or operator action).

**Automatic sequence:**

```
t=0s    mgmt-we stops renewing lease (crash or network loss)
t=60s   Lease TTL expires
t=65s   mgmt-ne polls, acquires free lease
t=65s   mgmt-ne writes ConfigMap kube-system/mgmt-leader-status → active
t=70s   controller-scaler on mgmt-ne scales up:
          ArgoCD application-controller → 3, repo-server → 2, server → 2
          Crossplane → 1, ESO → 1, argocd-jira-bridge → 1
t=120s  ArgoCD re-establishes connections to all cluster Secrets
        All workload Applications return to Healthy/Synced
```

Workload clusters are unaffected throughout. In-flight Argo Rollouts canaries continue independently.

**Validation:** `scripts/dr-validation/validate-mgmt-failover.sh`

**Operator failback after `mgmt-we` recovery:**

```bash
mgmt-cli failback --to mgmt-we --confirm
```

**Full runbook:** `docs/management-plane-failover-runbook.md`

### 13.2 Catastrophic Recovery via Seed Cluster (RTO ≤ 45 min)

**Trigger:** Both `mgmt-we` and `mgmt-ne` are unrecoverable.

**Steps:**

1. Connect to `seed-wus` (single-node, always running Crossplane)
2. Apply `bootstrap/control-plane-claim.yaml` → Crossplane provisions a new AKS management cluster
3. New cluster acquires the free blob lease
4. ArgoCD bootstraps itself from `platform-gitops` (Bitbucket Cloud)
5. `cluster-addons` ApplicationSet reconciles waves 10 → 38 (platform addons), then wave 40 (infra), then wave 50 (workloads)

Workload clusters continue serving traffic throughout — data plane is independent of control plane.

**Validation:** `scripts/dr-validation/validate-seed-cluster-dr.sh`

**Full runbook:** `docs/seed-cluster-dr-runbook.md`

---

## 14. Key ADR Decisions

| ADR | Decision | Why |
|---|---|---|
| ADR-001-v2 | Jenkins: single-replica StatefulSet in `mgmt-we` only | Upstream Jenkins is single-master. CI pauses ~30m during WE outage — acceptable for failure-domain isolation from Atlassian SaaS. |
| ADR-004-v2 | Active-Active reads, Active-Passive writes (Cosmos) | `multipleWriteLocationsEnabled: false`. Multi-master Cosmos requires conflict-aware app design. Auto-failover handles write-region loss automatically. |
| ADR-005-v2 | Per-region AKV pair + per-namespace SecretStore | Single vault = single failure domain. `ClusterSecretStore` = cluster-wide trust. Per-namespace UAMI provides IAM-level isolation per workload. |
| ADR-008-v2 | Cosign + AKV-backed key + Kyverno (not Gatekeeper) | Gatekeeper replaced by Kyverno as sole policy engine. Cosign provides image provenance; Kyverno verifies at admission on every cluster. |
| ADR-013-v2 | Two-tier ApplicationSets (infra + workload) | Infra lifecycle (Crossplane XRCs, 30m provision) and workload lifecycle (K8s manifests, 3m sync) differ fundamentally. Separate retry policies and prune behavior prevent infra churn from blocking workload deploys. |
| ADR-017 | Standby mgmt controllers scaled to zero | Eliminates ARM ownership thrash and double-writes to AKV. Lease arbitration (ADR-022) is the sole authority for which cluster is active. |
| ADR-020 | Per-namespace UAMI isolation (not ABAC prefix conditions) | Azure Key Vault data-plane RBAC does not support attribute-based conditions on secret names. Per-namespace UAMI binding is the maximum isolation Azure supports for Key Vault secret-plane access. |
| ADR-021 | Argo Rollouts mandatory in production | `kyverno-prod-requires-rollout` rejects `Deployment` on prod clusters. AnalysisTemplate auto-generated from SLO class via `NamespaceRolloutPolicy` Composition (`xnamespacerolloutpolicies.platform.cityos.io`). |
| ADR-022 | Azure Storage Blob Lease as singleton lock | Lease TTL=60s, renewal=15s. Geo-replicated GRS storage with private endpoint. Single source of truth preventing split-brain. |

---

## 15. Terraform State Management

> Codifies PRD-v3 FR-V3-01..FR-V3-04 and ADR-023-v3. Replaces the v2-era "local state on operator laptops" anti-pattern.

### 15.1 Bootstrap Storage Account (Out-of-Band)

The state backend is itself infrastructure, so Terraform cannot create it without a chicken-and-egg problem. The Storage Account is provisioned **out of band** by an idempotent Azure CLI script (`/scripts/bootstrap-tfstate.sh`) into a dedicated resource group `rg-tfstate-bootstrap` that carries a `CanNotDelete` management lock.

| Property | Value |
|---|---|
| Resource group | `rg-tfstate-bootstrap` (West Europe; `CanNotDelete` lock) |
| Storage Account | `stplatformtfstate` (GRS, blob versioning ON, soft-delete 30d) |
| Container | `tfstate` (private; public access disabled) |
| Encryption | Microsoft-managed keys (MSE) at v3; CMK is roadmap (see §15.4) |
| Authorization | RBAC only; `allowSharedKeyAccess: false` (no SAS / no account keys) |
| Network | Private endpoint in the West Europe hub VNet; firewall denies public traffic |

Re-running the bootstrap script must be a no-op once the SA exists; idempotence is the acceptance test.

### 15.2 Per-Environment State Key Convention

All state files live in the single container under the strict key pattern:

```
tfstate/<env>/<cluster>.tfstate
```

Day-1 keys (one per cluster — no consolidation, no shared state):

```
tfstate/mgmt-we/mgmt-we.tfstate
tfstate/mgmt-ne/mgmt-ne.tfstate
tfstate/dev/aks-dev-we.tfstate
tfstate/staging/aks-staging-we.tfstate
tfstate/prod-we/aks-prod-we.tfstate
tfstate/prod-ne/aks-prod-ne.tfstate
tfstate/seed-wus/seed-wus.tfstate
```

A botched apply, corrupted lock, or accidental `terraform state rm` is bounded to one cluster — the blast radius is one row of the table.

### 15.3 Native Azure Blob Lease Locking

State locking uses the `azurerm` backend's built-in **blob lease** (the same primitive ADR-022 uses for the management-plane singleton — operational experience transfers). No external lock store (DynamoDB, Cosmos, custom table) is permitted.

```
+--------------------+        +-------------------------+        +----------------------------+
|  terraform apply   | -----> | stplatformtfstate (SA)  | -----> | Blob lease on tfstate/...  |
|  (any env)         |        | container: tfstate      |        | TTL ~ 60s, auto-renewed    |
+--------------------+        +-------------------------+        +----------------------------+
        |                                                                 |
        +---- if lease held by another run -> wait or fail w/ clear msg --+
```

Concurrent `terraform apply` against different env-states never block each other because the lease is scoped to the per-env blob.

### 15.4 Encryption Posture (MSE Now, CMK Roadmap)

v3 ships with Microsoft-managed keys for the state SA — the minimum bar for data-at-rest protection without the operational cost of running a CMK rotation. AKV-backed CMK is on the roadmap and tracked under OQ-V3-04 of PRD-v3; retrofitting CMK in v3 without a follow-up ADR is explicitly forbidden (FR-V3-04). The migration plan (in-place rotate vs. mirror-to-new-SA-and-cutover) will be authored alongside the CMK ADR.

---

## 16. Secret Material Lifecycle

> Codifies PRD-v3 FR-V3-05..FR-V3-12 and ADR-030-v3. Complements §6 (Secrets Management) by covering the **Terraform-time** and **history-purge** dimensions §6 did not address.

### 16.1 TLS Certificates — AKV-Issued, ESO-Synced, Never Committed

Every TLS certificate consumed by the platform is **issued by Azure Key Vault** and **synced into Kubernetes by ESO**. No private key or PEM file ever lives in git.

```
   AKV cert policy (issuer + auto-rotate quarterly)
            │
            ▼
   AKV certificate object (kv-platform-<env>-<region>)
            │  ExternalSecret (per namespace; uses local SecretStore)
            ▼
   Kubernetes Secret  (kind: kubernetes.io/tls)
            │  Reloader watches; restarts pods on hash change
            ▼
   Workload pod  (mounts /etc/tls)
```

A `detect-private-key` pre-commit hook (FR-V3-07) blocks any new commit containing PEM-encoded key material — regardless of filename — from reintroducing the F-06 class of defect.

### 16.2 Terraform-Time Secret Sourcing (AKV + OIDC `TF_VAR_*`)

Sensitive Terraform inputs (subscription IDs, client secrets, SAS tokens, SaaS API tokens, connection strings) must be sourced via `data "azurerm_key_vault_secret"` blocks. **No sensitive variable may carry a `default = "..."` value.** No plaintext `*.tfvars` containing sensitive material may be committed.

CI consumes the **already-provisioned** OIDC federated credential (subject pinned per repo + protected `main` GitHub Environment, audience `api://AzureADTokenExchange`) on the `gha-platform-ci` user-assigned managed identity — v3 references this credential, does not re-provision it. The workflow fetches AKV secrets and exports them as `TF_VAR_*` for the duration of the job, masked in logs:

```yaml
# .github/workflows/terraform.yml (excerpt)
permissions:
  id-token: write           # required for OIDC token exchange
  contents: read
jobs:
  plan:
    environment: prod-we    # GitHub Environment with required reviewers
    steps:
      - uses: azure/login@v2
        with:
          client-id: ${{ vars.GHA_PLATFORM_CI_CLIENT_ID }}
          tenant-id: ${{ vars.AZURE_TENANT_ID }}
          subscription-id: ${{ vars.AZURE_SUBSCRIPTION_ID }}
      - name: Fetch TF_VAR_* from AKV
        run: |
          echo "::add-mask::$(az keyvault secret show -n bb-token --vault-name kv-platform-mgmt-we --query value -o tsv)"
          echo "TF_VAR_bitbucket_token=$(az keyvault secret show -n bb-token --vault-name kv-platform-mgmt-we --query value -o tsv)" >> "$GITHUB_ENV"
```

### 16.3 Credential Rotation Model

- **AKV keys and certificates** — quarterly rotation via AKV-native rotation policies. No code, no CronJob, no operator action.
- **Workload Identity and federated credentials** — federation has no rotating secret. Trust is established via OIDC issuer + subject claim; there is nothing to rotate.
- **SaaS tokens** (Bitbucket, Jira — outside AKV's rotation scope) — rotated by the existing `saas-token-rotator` CronJob (§6.4), unchanged by v3.

The net effect: v3 introduces zero new manual rotation toil.

### 16.4 Historical TLS Purge

A `tls.key` was discovered in the v2 audit. The remediation:

1. `git filter-repo --path '**/*.key' --path '**/*.crt' --invert-paths` removes every blob across history.
2. Force-push to the canonical remote during a coordinated window (OQ-V3-03 of PRD-v3).
3. The compromised certificate is **rotated** at the issuing CA and the old certificate **revoked** (added to the CRL).
4. `*.key` and `*.crt` are added to `.gitignore`.
5. The `detect-private-key` hook (§16.1) prevents recurrence at commit-time.

This is a one-time operation; the runbook lives in `docs/runbooks/tls-history-purge.md` (to be authored as part of P0).

---

## 17. RBAC Scope-Down (Post-v3)

> Codifies PRD-v3 FR-V3-10..FR-V3-11 and ADR-024-v3. Removes the development-time "subscription-Owner everywhere" anti-pattern.

### 17.1 The Anti-Pattern v3 Removes

Early v2 development granted the `akspe` user-assigned managed identity **Owner at subscription scope** because it was the path of least resistance for "make Terraform work end-to-end." This is wildly over-privileged:

- Any pod with access to the `akspe` UAMI's federated token could mutate **any** resource in the subscription, including other teams' production data.
- A compromised CI run could create, delete, or re-tag networking, identity, billing — anything.
- It violates least-privilege at the foundational layer; every higher control (Kyverno, ESO per-namespace UAMI) becomes window-dressing.

### 17.2 Scoped Assignments (v3)

| Identity | Role | Scope |
|---|---|---|
| `akspe` UAMI | `Contributor` | Each AKS resource group (`rg-aks-<env>-<region>`) — never the subscription |
| `akspe` UAMI | `User Access Administrator` | Same AKS resource groups (needed for federated-credential lifecycle) |
| Velero UAMI | `Contributor` | Velero backup resource group only |
| Velero UAMI | `Storage Blob Data Contributor` | Velero backup Storage Account only |
| `gha-platform-ci` UAMI | `Key Vault Secrets User` | Each per-env AKV (read-only at TF-time) |

A CI assertion runs `az role assignment list --assignee <UAMI>` and **fails the build** if any assignment is at subscription scope or carries `Owner`. The assertion is a hard gate; suppressions require an ADR.

### 17.3 Verification Cadence

- Pre-commit: `tflint` custom rule warns on any new `azurerm_role_assignment` with `scope = data.azurerm_subscription.current.id`.
- CI: the role-assignment audit job runs on every PR and on a nightly schedule against live state.
- Quarterly: the Platform team reviews assignments as part of the standard FinOps + security review.

See **ADR-024-v3** for the full decision and consequences.

---

## 18. Quality Gates (Pre-commit + CI)

> Codifies PRD-v3 FR-V3-13..FR-V3-17 and ADR-025-v3.

### 18.1 The pre-commit Framework

The repo adopts the Python `pre-commit` framework as the canonical local hook runner. A single `.pre-commit-config.yaml` at the repo root is the source of truth — there are no per-subdirectory hook configs.

```yaml
# .pre-commit-config.yaml (v3 minimal hook set — FR-V3-14)
repos:
  - repo: https://github.com/antonbabenko/pre-commit-terraform
    rev: v1.96.0
    hooks:
      - id: terraform_fmt
      - id: terraform_validate
      - id: terraform_tflint
        args: [--args=--config=__GIT_WORKING_DIR__/.tflint.hcl]
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.6.0
    hooks:
      - id: detect-private-key
      - id: end-of-file-fixer
      - id: trailing-whitespace
  - repo: https://github.com/bridgecrewio/checkov
    rev: 3.2.0
    hooks:
      - id: checkov
        args: [--config-file, .checkov.yaml]
```

Additional hooks may be proposed via ADR but are not required at v3 GA.

### 18.2 GitHub Actions Layout

Platform-repo meta-CI runs on GitHub Actions. App CI continues on Jenkins (ADR-001-v2) — v3 introduces no app-CI change.

```
.github/
└── workflows/
    ├── terraform-plan.yml       # PRs: matrix(env-state) -> fmt + validate + tflint + checkov + plan
    ├── terraform-apply.yml      # main only: matrix(env-state) -> apply (Environment-gated)
    └── backstage.yml            # PRs + main: yarn lint/test/build, sonar-scanner, docker build
```

### 18.3 Blocking vs Advisory Matrix

| Check | Day-1 | Sprint-1 | Sprint-2+ |
|---|---|---|---|
| `terraform_fmt` | Blocking | Blocking | Blocking |
| `terraform_validate` | Blocking | Blocking | Blocking |
| `tflint` | Blocking | Blocking | Blocking |
| `detect-private-key` | Blocking | Blocking | Blocking |
| `checkov` | Advisory | Advisory | **Blocking** |
| Sonar (Backstage) | Advisory | Advisory | **Blocking** (`packages/backend`) |
| Custom tflint "unvalidated var" rule | Advisory | Advisory | **Blocking** once 100% coverage |

### 18.4 GitHub Environments + Required Reviewers (Apply Gate)

`terraform apply` on `main` is **never** automatic. Each env-state has a dedicated GitHub Environment (`dev`, `staging`, `prod-we`, `prod-ne`, `mgmt-we`, `mgmt-ne`, `seed-wus`), and each Environment is configured with:

- At least two required reviewers drawn from `@<org>/platform-team`.
- Deployment branches restricted to `main`.
- OIDC subject claim scoped to that Environment, so a workflow in a non-`main` branch literally cannot obtain a token for `prod-we`.

The reviewer's approval is recorded against the GHA run; the apply log is the auditable record of "who approved what change to which env-state at what time."

---

## 19. Variable Validation

> Codifies PRD-v3 FR-V3-18..FR-V3-20 and ADR-026-v3.

### 19.1 Inline `validation {}` for the Critical Path

Critical-path variables carry inline `validation {}` blocks from day-1. The day-1 set:

- `region`
- `cluster_name`
- `sku_tier`
- `cidr_block`
- `environment` (regex `^(dev|staging|prod)$`)

```hcl
variable "environment" {
  type        = string
  description = "Workload environment selector."
  validation {
    condition     = can(regex("^(dev|staging|prod)$", var.environment))
    error_message = "environment must be one of: dev, staging, prod."
  }
}

variable "sonar_hosting" {
  type        = string
  description = "Sonar hosting selector. SaaS or self-hosted on cipool."
  default     = "saas"
  validation {
    condition     = contains(["saas", "cipool"], var.sonar_hosting)
    error_message = "sonar_hosting must be one of: saas, cipool."
  }
}
```

### 19.2 Resource-Level Rules via `tflint-ruleset-azurerm`

Cloud-resource constraints (naming, SKU restrictions, required tags) live in `tflint-ruleset-azurerm` — not duplicated inside `validation {}` blocks. The split is deliberate: inline validations cover input shape; tflint covers resource shape.

```hcl
# .tflint.hcl
plugin "azurerm" {
  enabled = true
  version = "0.27.0"
  source  = "github.com/terraform-linters/tflint-ruleset-azurerm"
}
```

### 19.3 Custom tflint Rule — Unvalidated-Var Detector

A small custom tflint rule walks every `variable` declaration and flags any that lacks a `validation {}` block and is not on an explicit allow-list. The rule is **advisory while the 100%-coverage ratchet is in progress** and **blocking once 100% is declared** (FR-V3-20).

### 19.4 Rollout Order

1. Day-1: critical-path five carry `validation {}`. Custom rule is advisory.
2. Sprint-1: ratchet to ~50% of variables.
3. Sprint-2: ratchet to 100%; custom rule flips to blocking; CI gates fail any new unvalidated variable.

The ratchet is owned by the Platform team — application teams inherit the gate on their next PR.

---

## 20. Supply-Chain Scanning (Checkov)

> Codifies PRD-v3 FR-V3-21..FR-V3-23 and ADR-027-v3.

### 20.1 Config Layout

```
/.checkov.yaml            # single source of truth — consumed by pre-commit AND CI
/.checkov.baseline        # accepted findings; CODEOWNERS-protected
/CODEOWNERS               # .checkov.baseline -> @<org>/platform-team (interim)
```

The same `.checkov.yaml` is consumed locally and in CI — local failures are reproducible.

```yaml
# .checkov.yaml (excerpt)
framework:
  - terraform
  - dockerfile
soft-fail: false
output: cli,sarif
baseline: .checkov.baseline
skip-path:
  - .terraform/
  - examples/
```

### 20.2 Suppression Syntax (Inline, Ticket-Referenced, Expiring)

Inline suppressions are of the form:

```hcl
resource "azurerm_storage_account" "example" {
  # checkov:skip=CKV_AZURE_33:CITYOS-1234 — tracking AKV-backed CMK migration, expires 2026-09-30
  ...
}
```

The referenced Jira ticket (`CITYOS-1234`) **must exist and must carry an expiry date**. A CI step queries Jira and fails the build if any suppression lacks a ticket, references a closed ticket, or references a ticket whose expiry has passed. Suppressions without a ticket are a build failure — never a silent merge.

### 20.3 Pre-commit + CI Dual Integration

Checkov runs in **both** the pre-commit hook (catches issues before `git push`) and the CI pipeline (catches issues that bypassed the hook). Both use the same `.checkov.yaml`; producing the same exit code and finding set for the same commit SHA is a hard SLO.

### 20.4 Baseline Lifecycle

- New finding lands → CI fails on PR.
- If the finding is genuine: fix the code.
- If the finding is acceptable and ticketed: add inline suppression with ticket reference + expiry.
- If a class of findings needs accepting at baseline scope: PR mutates `.checkov.baseline`; CODEOWNERS routes the review to `@<org>/platform-team`.

The baseline is read-only to all other contributors by branch protection.

---

## 21. Static Analysis (SonarQube)

> Codifies PRD-v3 FR-V3-24..FR-V3-26 and ADR-028-v3.

### 21.1 Scope

Sonar analyzes **Backstage TypeScript** (`backstage/packages/`) and **Dockerfiles** (`backstage/**/Dockerfile*`) only. HCL is **excluded** — that surface is covered by `tflint` (style + provider rules) and `checkov` (security posture). Double-coverage adds CI time without adding signal.

```properties
# backstage/sonar-project.properties (greenfield onboarding — FR-V3-24 / ADR-028-v3)
sonar.projectKey=cityos_backstage
sonar.organization=cityos
sonar.sources=packages
sonar.exclusions=**/*.test.ts,**/*.spec.ts,**/dist/**,**/node_modules/**
sonar.javascript.lcov.reportPaths=packages/backend/coverage/lcov.info,packages/app/coverage/lcov.info
sonar.typescript.tsconfigPaths=packages/backend/tsconfig.json,packages/app/tsconfig.json
```

### 21.2 Hosting Toggle (`var.sonar_hosting`)

| Value | Endpoint | Where it runs |
|---|---|---|
| `"saas"` (default) | `sonarcloud.io` | Hosted by Sonar — no in-cluster footprint |
| `"cipool"` | In-cluster service in `mgmt-we` | `cipool` node pool only; never `systempool` |

The variable is consumed by both the Terraform module that conditionally provisions the Sonar Helm release and the GHA Backstage workflow that selects which endpoint `sonar-scanner` posts to.

```hcl
variable "sonar_hosting" {
  type    = string
  default = "saas"
  validation {
    condition     = contains(["saas", "cipool"], var.sonar_hosting)
    error_message = "sonar_hosting must be one of: saas, cipool."
  }
}
```

When `"cipool"` is selected, the in-cluster Sonar lands with:

```yaml
nodeSelector:
  nodepool: cipool
tolerations:
  - key: workload
    value: ci
    effect: NoSchedule
```

This placement is permitted by **ADR-016-v3-amendment** (§22) — `cipool` is an allowed host for DX-plane tooling.

### 21.3 Quality Gate

- Profile: **Sonar Way** (the upstream default; we do not maintain a fork).
- Override: **new-code coverage must be greater than or equal to 80%**.
- Phasing: advisory for one sprint after Sonar lights up; blocking from sprint-2 onward, scoped to `packages/backend`.

---

## 22. ADR-016 Amendment Note

> Forward-looking marker for the amendment authored in `_docs/IDP-GitOps-ADRs-v2.md` as `ADR-016-v3-amendment` (Status: Proposed — amends ADR-016).

ADR-016 (Platform Terminology) is widely read as "no DX-plane tooling on management clusters." That reading was always inexact — Jenkins has run on `mgmt-we`'s `cipool` since v2 GA (ADR-001-v2). PRD-v3 makes the inexactness explicit: when an operator selects `var.sonar_hosting = "cipool"`, a self-hosted Sonar lands on the same `cipool` node pool. The forthcoming **ADR-016-v3-amendment** ratifies this distinction:

- Management cluster **`systempool`** (on both `mgmt-we` and `mgmt-ne`) remains **platform-only** — Kyverno's `tier=platform` requirement is unchanged.
- Management cluster **`cipool`** (which exists only on `mgmt-we`) is an **allowed host for DX-plane tooling** — Jenkins today, optionally Sonar from v3.
- Workload clusters remain entirely off-limits to DX tooling (no Jenkins, no Sonar).

Until the amendment is `Accepted`, `var.sonar_hosting` must remain at its default `"saas"`. Switching to `"cipool"` is gated on the amendment's ratification (tracked in PRD-v3 as OQ-V3-05).