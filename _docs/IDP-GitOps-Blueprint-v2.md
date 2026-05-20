# Production-Grade GitOps & Platform Engineering Blueprint — **v2**

**Stack:** Jira · Bitbucket · Jenkins · Crossplane (Azure Provider) · Azure AKS · ArgoCD · Azure Key Vault · External Secrets Operator · Argo Rollouts
**Author Role:** Principal Enterprise Architect / Platform Engineering Specialist
**Document Version:** 2.0 (audit revision of v1.0 — 2026-05-19)
**Companion:** `IDP-GitOps-ADRs-v2.md`
**Reference Implementation:** [Azure-Samples/aks-platform-engineering](https://github.com/Azure-Samples/aks-platform-engineering)

---

## Changelog from v1.0

This revision resolves 17 inconsistencies and ambiguities identified in a structured audit of v1.0. The originals (`IDP-GitOps-Blueprint.md`, `IDP-GitOps-ADRs.md`) remain in place for traceability.

| # | Change | Affects |
|---|---|---|
| C-1 | Jenkins HA pattern corrected: single-replica StatefulSet with Velero PVC snapshots (was "2 replicas + leader election" — not a real upstream pattern). | §3.4, ADR-001 |
| C-2 | Data plane re-labeled: "Active-Active **reads**, Active-Passive **writes** (auto-failover)" — matches Cosmos `multipleWriteLocationsEnabled: false`. | §1.2, §4.3, ADR-004 |
| C-3 | "Private endpoint to Bitbucket" removed. Bitbucket Cloud reached via Azure Firewall Premium + FQDN allow-list; inbound webhooks via Front Door + WAF + Atlassian CIDR pin. | §1.4, ADR-009 |
| C-4 | Cloud infrastructure and workload manifests now live in **two separate Applications** per service (`<svc>-infra-<env>`, `<svc>-app-<env>-<cluster>`); long retry budgets on infra, short on workload. | §6.2, §6.3, ADR-013 |
| C-5 | PlatformBot's Bitbucket app password replaced by **workspace access token** stored in AKV, managed by ESO + Reloader, rotated quarterly. Same pattern for Jira service-account token. | §2.1.2, §5.7 (new), ADR-006 |
| C-6 | Formal glossary introduced (§0). "Workload" vs "platform component" defined precisely; Kyverno enforces `tier=platform` namespace label on mgmt clusters. | §0 (new), §1.3, ADR-003, ADR-016 (new) |
| C-7 | Crossplane "read-only mode" in mgmt-ne replaced with explicit **scale-to-zero** of controllers + cluster `role=standby` label gating the ApplicationSet. | §1.2, §7.3, ADR-004, ADR-017 (new) |
| C-8 | SLO class (bronze/silver/gold) re-defined as a single availability tier with explicit RTO/RPO targets; **Cosmos consistency level decoupled** as its own parameter. | §0, §4.0 (new), §4.2-4.5, ADR-018 (new) |
| C-9 | Per-region Azure Key Vault pair (`kv-platform-prod-we`, `kv-platform-prod-ne`); Crossplane dual-writes; ESO points to local vault. | §5.1.1 (new), §4 Compositions, ADR-005, ADR-019 (new) |
| C-10 | ESO `ClusterSecretStore` replaced by **per-namespace `SecretStore`** with per-namespace UAMI and prefix-scoped AKV RBAC. | §5.3, ADR-005, ADR-020 (new) |
| C-11 | Jenkins/`cipool` confirmed mgmt-we-only; CI is paused during mgmt-we DR (acceptable per ADR-001). | §3.4, §7.3, ADR-001 |
| C-12 | Canonical cluster naming `<role>-<env>-<region>` adopted throughout; cluster Secret labels (`env`, `region`, `role`) enforced by Kyverno. | §0, §1.2, §6.2, §7.3 |
| C-13 | Argo Rollouts made mandatory for production workloads; canary 5/25/50/100 with platform-provided AnalysisTemplate per SLO class. | §6.5 (new), ADR-021 (new) |
| C-14 | DR runbook simplified: management-plane promotion automated by an Azure Storage Blob Lease singleton lock. | §1.7 (new), §7.3, ADR-022 (new) |
| C-15 | `[ci skip]` clarified as defensive layer; author filter is the primary commit-loop prevention mechanism. | §3.3, ADR-010 |

---

## Executive Summary

This blueprint defines a **resilient, multi-cluster, multi-region Internal Developer Platform (IDP)** built on the GitOps Bridge pattern. The architecture isolates the *developer experience plane* (Jira, Bitbucket Cloud, Jenkins), the *control plane* (Crossplane + ArgoCD hub in the management cluster), and the *data plane* (workload AKS clusters) so that a failure in any one plane cannot disrupt the other two. Key v2 decisions:

- Jenkins is the CI engine (chosen over Bamboo for failure-domain isolation from Atlassian SaaS).
- Crossplane Azure provider is the sole cloud control plane; XRDs abstract SQL, Cosmos, Service Bus, and DNS behind small platform claims.
- ArgoCD ApplicationSet with Matrix generators, **two-tier**: a slow infrastructure tier (Crossplane XRCs) and a fast workload tier (K8s manifests).
- ESO + per-region Azure Key Vault + per-namespace identity binding give zero-trust secret delivery with multi-tenant isolation.
- Argo Rollouts handles progressive delivery for production; AnalysisTemplate is auto-generated from the service's SLO class.
- Management-plane DR is automatic — an Azure Storage Blob Lease decides which mgmt cluster is active; controllers in the inactive cluster are scaled to zero.

---

# 0. CONVENTIONS — GLOSSARY AND NAMING

> Read this section first. Every section after it uses these terms precisely.

## 0.1 Glossary

| Term | Definition |
|---|---|
| **Workload** | Any application that serves customer-, business-, or end-user-facing traffic. Workloads run in **workload clusters** only. |
| **Platform component** | Any service that supports the developer or operator experience: ArgoCD, Crossplane (core + providers), ESO controllers, Reloader, Velero, Jenkins (controller + ephemeral agents), observability stack, `argocd-jira-bridge`, `mgmt-leader-lease`. Platform components run in **management clusters** only. |
| **Management cluster** | A cluster named `mgmt-<region>`. Hosts only platform components. Kyverno rejects any Pod whose namespace lacks the label `tier=platform`. |
| **Workload cluster** | A cluster named `aks-<env>-<region>`. Hosts only workloads, plus the minimum operators they need (ESO agent, Reloader, Argo Rollouts controller, Kyverno, ArgoCD agent SA). |
| **Seed cluster** | A tiny single-node cluster (`seed-wus`) in a third region used only as a re-build origin for management clusters during catastrophic DR. |
| **Active management cluster** | Whichever mgmt cluster currently holds the Azure Storage Blob Lease (§1.7). Exactly one at any time. |
| **Standby management cluster** | All mgmt clusters that do not hold the lease. Controllers there are scaled to zero. |
| **Control plane** | The set of components in the *active* management cluster that reconcile intent (in Git) to state (in clusters and Azure). |
| **Data plane** | The workload clusters and the Azure PaaS resources their pods talk to. |
| **DX plane** | Jira Cloud, Bitbucket Cloud, Jenkins. Source of intent, not of runtime state. |
| **Active-Active reads** | Front Door routes user traffic to the geographically nearer region's workload cluster; both regions serve reads. |
| **Active-Passive writes** | Writes always reach the Cosmos primary write region (WE during normal operation; NE after auto-failover). Cosmos `multipleWriteLocationsEnabled` is intentionally `false`. |
| **Composite Resource (XR)** / **Composite Resource Claim (XRC)** | Crossplane CRD primitives. An XRC is the user-facing namespaced claim; an XR is the cluster-scoped composite that materializes the claim. |
| **SLO class** | A platform-issued availability tier — `bronze`, `silver`, or `gold` — that determines Azure SKU choices, multi-AZ posture, replication, and required progressive-delivery strategy. See §4.0. |
| **PlatformBot** | The Bitbucket Cloud service account that the Jenkins pipeline uses to write to the `platform-gitops` repo. Its credentials are workspace access tokens stored in AKV, not user app passwords. |

## 0.2 Cluster Naming

| Cluster | Canonical name | Role |
|---|---|---|
| Management, West Europe | `mgmt-we` | Active by default |
| Management, North Europe | `mgmt-ne` | Standby; promotes via lease |
| DR seed, West US 2 | `seed-wus` | Bootstrap origin |
| Workload, dev | `aks-dev-we` | Single-AZ |
| Workload, staging | `aks-staging-we` | Multi-AZ |
| Workload, prod (WE) | `aks-prod-we` | Multi-AZ |
| Workload, prod (NE) | `aks-prod-ne` | Multi-AZ |

Cluster Secrets registered in ArgoCD carry these mandatory labels (enforced by Kyverno `KyvernoRequiredClusterSecretLabels`):

```yaml
labels:
  argocd.argoproj.io/secret-type: cluster
  env: dev | staging | prod | mgmt | seed
  region: westeurope | northeurope | westus2
  role: workload | management | seed
  lease-status: active | standby | n-a    # set by mgmt-leader-lease on mgmt clusters; "n-a" for workload/seed
data:
  name: aks-prod-we    # must equal metadata.name
```

## 0.3 Other Naming Standards

| Resource | Pattern | Example |
|---|---|---|
| Namespace (workload) | `<service-name>` | `payments` |
| Namespace (platform) | `<component>-system` or upstream chart default | `crossplane-system`, `external-secrets` |
| Azure Key Vault | `kv-platform-<env>-<region>` | `kv-platform-prod-we` |
| Resource Group | `rg-<service>-<purpose>` | `rg-payments-sql` |
| ACR | `acr<platform><env>` (no dashes) | `acrplatformprod` |
| UAMI (per namespace) | `uami-<namespace>-<cluster>` | `uami-payments-aks-prod-we` |
| AKV secret (cross-tenant safe) | `<namespace>-<resource>-<purpose>` | `payments-sql-conn`, `payments-cosmos-conn` |

---

# 1. ARCHITECTURAL TOPOLOGY & FAILURE DOMAINS

## 1.1 Plane Model

| Plane | Purpose | Components | Failure Class |
|---|---|---|---|
| Developer Experience (DX) | Author intent | Jira Cloud, Bitbucket Cloud, Jenkins controller | *Write-side critical* — outage stops new changes, not running workloads |
| Control Plane (CP) | Reconcile intent → state | Active mgmt cluster (Crossplane, ArgoCD hub, ESO orchestrator, Velero, leader-lease, jira-bridge) | *Reconciliation-critical* — outage freezes drift correction; workloads keep serving |
| Data Plane (DP) | Run workloads | Workload AKS clusters per env × region | *Runtime-critical* — direct user impact |

**Cardinal rule:** state lives in Git, not in any cluster. Any plane can be destroyed and rebuilt from the Bitbucket Cloud monorepo `platform-gitops`.

## 1.2 Multi-Region, Multi-Cluster Layout

```
                                           REGION: West Europe (Primary mgmt)              REGION: North Europe (Standby mgmt)         REGION: West US 2
┌─────────────────────┐                   ┌────────────────────────────────────────┐      ┌────────────────────────────────────┐    ┌──────────────────┐
│  DX Plane (SaaS)    │                   │  mgmt-we                                │      │  mgmt-ne                            │    │  seed-wus        │
│  ─ Jira Cloud       │  ── webhook ──►   │  Holds Azure Storage Blob Lease         │      │  ─ All controllers @ 0 replicas     │    │  Bootstrap only  │
│  ─ Bitbucket Cloud  │                   │  ─ ArgoCD Hub (HA, 3 controllers)       │ ◄──► │  ─ Polls lease every 5s; idle       │    │  for full DR     │
│  ─ Jenkins ctrl     │  ── kubectl ──►   │  ─ Crossplane + Azure provider          │      │  ─ Crossplane installed but quiet   │    │                  │
│   (mgmt-we only)    │                   │  ─ ESO controller-of-controllers        │      │  ─ ESO orchestrator @ 0 replicas    │    │                  │
└─────────────────────┘                   │  ─ Velero (6h etcd/PV → GRS storage)    │      │  ─ Velero (restore target)          │    │                  │
                                          │  ─ mgmt-leader-lease (single replica)   │      │  ─ mgmt-leader-lease (single)       │    │                  │
                                          └──────────┬──────────────────────────────┘      └────────────────────────────────────┘    └──────────────────┘
                                                     │ syncs (via ApplicationSet labels)
                          ┌──────────────────────────┼──────────────────────────┬───────────────────────────┐
                          ▼                          ▼                          ▼                           ▼
                  ┌───────────────────┐  ┌────────────────────┐  ┌───────────────────────┐  ┌──────────────────────┐
                  │ aks-dev-we        │  │ aks-staging-we     │  │ aks-prod-we           │  │ aks-prod-ne          │
                  │ Single-AZ         │  │ Multi-AZ           │  │ Multi-AZ, Premium SKU │  │ Multi-AZ, Premium    │
                  │ ArgoCD destination│  │ ArgoCD destination │  │ Argo Rollouts canary  │  │ Argo Rollouts canary │
                  │ ESO + SecretStore │  │ ESO + SecretStore  │  │ ESO + SecretStore     │  │ ESO + SecretStore    │
                  │ Argo Rollouts ctrl│  │ Argo Rollouts ctrl │  │ Argo Rollouts ctrl    │  │ Argo Rollouts ctrl   │
                  │ → kv-platform-    │  │ → kv-platform-     │  │ → kv-platform-        │  │ → kv-platform-       │
                  │   dev-we          │  │   staging-we       │  │   prod-we             │  │   prod-ne            │
                  └───────────────────┘  └────────────────────┘  └───────────────────────┘  └──────────────────────┘

Data plane: Active-Active reads via Front Door; Active-Passive writes via Cosmos (WE primary, NE auto-failover).
```

The full C4 Container diagram in v2 is `IDP-C4-Container-v2.drawio` (delivered alongside).

## 1.3 Failure Domain Isolation

| Failure Scenario | What Stops | What Keeps Working | Why |
|---|---|---|---|
| Jira Cloud down | New service requests; drift incidents not auto-tracked | All pipelines, all workloads, ArgoCD reconcile | Jira is *intent-side*; runtime is unaware |
| Bitbucket Cloud down | New commits; ArgoCD pulls fail | Running workloads; in-flight reconciliations using last-known revision | ArgoCD caches last good revision; Crossplane reconciles from in-cluster CRs |
| Jenkins controller down | Image builds; new image-tag PRs | Existing images in ACR; ArgoCD continues syncing whatever is in Git | CI is write-side only |
| mgmt-we down | Drift correction (briefly, until lease expires); active reconciliation | Workload pods, in-region DB connections; mgmt-ne auto-promotes via lease (~120s) | Lease-based singleton lock; mgmt-ne stands by |
| Workload cluster down (single) | Workloads on that cluster | All other clusters, control plane, CI; Front Door routes around if it's a prod cluster | Per-cluster blast radius |
| Entire West Europe region down | WE workloads; mgmt-we; AKV-WE writes | NE workloads (Active-Active reads); mgmt-ne promoted via lease; AKV-NE serves NE pods; Cosmos auto-failover promotes NE to writes | Per-region AKV; auto-promotion |
| mgmt-we ↔ mgmt-ne partition (split-brain risk) | mgmt-we can no longer renew lease; mgmt-ne acquires it; mgmt-we's controller-scaler scales mgmt-we controllers to 0 on lease loss | Workloads; ArgoCD on whichever cluster has lease | Azure Storage Blob Lease is single source of truth |

**Critical design rule:** the management cluster never schedules workloads. Kyverno enforces `tier=platform` on every namespace; non-conformant pods are admission-denied. This means a management-plane outage degrades the *meta-capability* of provisioning, never the *capability* of serving traffic.

## 1.4 Network Topology — Zero-Trust Segmentation (Revised)

```
Hub VNet (10.0.0.0/16, West Europe), peered to North Europe hub
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│  Azure Firewall Premium                                                                  │
│    - Egress allow-list (FQDN): *.bitbucket.org, api.bitbucket.org, *.atlassian.net,      │
│      *.azurecr.io, *.azure.com, *.microsoftonline.com, package-mirror FQDNs              │
│    - TLS inspection on Atlassian-bound traffic                                           │
│    - DNAT: inbound webhooks → Jenkins controller (only from Front Door)                  │
│  Azure Bastion                          (break-glass operator access; no public IPs)     │
│  Private DNS Zones                                                                       │
│    - privatelink.vaultcore.azure.net           (AKV)                                     │
│    - privatelink.database.windows.net          (Azure SQL)                               │
│    - privatelink.documents.azure.com           (Cosmos)                                  │
│    - privatelink.servicebus.windows.net        (Service Bus)                             │
│    - privatelink.azurecr.io                    (ACR)                                     │
│    - privatelink.blob.core.windows.net         (Storage Account hosting the lease blob)  │
└──────────────────────────────────────────────────────────────────────────────────────────┘
        │ VNet peering              │ VNet peering                     │ VNet peering
        ▼                           ▼                                  ▼
┌──────────────────────────┐  ┌──────────────────────────┐  ┌──────────────────────────┐
│ mgmt-we spoke (10.1/16)  │  │ aks-prod-we spoke (10.2)  │  │ aks-staging-we (10.3)    │
│ AKS API: PRIVATE         │  │ AKS API: PRIVATE          │  │ AKS API: PRIVATE         │
│ Crossplane, ArgoCD,      │  │ Workload pods only        │  │ Workload pods only       │
│ ESO orchestr, Jenkins    │  │ Private endpoints to:     │  │ Private endpoints to:    │
│ + cipool node pool       │  │   kv-platform-prod-we,    │  │   kv-platform-staging-we,│
│ Private endpoints to:    │  │   Cosmos prod, SB prod    │  │   SQL staging, SB staging│
│   ACR, every AKV,        │  │ Egress via hub firewall   │  │                          │
│   leader-lease storage   │  │                           │  │                          │
└──────────────────────────┘  └──────────────────────────┘  └──────────────────────────┘
```

**Bitbucket Cloud is NOT reachable via Private Endpoint** (Atlassian doesn't offer Azure Private Link for Bitbucket Cloud). The path is:

- **Inbound webhooks:** Bitbucket → Azure Front Door → WAF (Atlassian's published webhook CIDRs pinned) → Azure Firewall DNAT → Jenkins controller. TLS verified at WAF and at Jenkins.
- **Outbound from Jenkins/ArgoCD:** Azure Firewall Premium with FQDN allow-list (`*.bitbucket.org`, `api.bitbucket.org`, `*.atlassian.net`) and TLS inspection. CIDR-range churn on Atlassian's side is handled by the firewall's FQDN tag (auto-updated by Microsoft).

This residual SaaS dependency is accepted explicitly. ADR-001 trades it away in exchange for failure-domain isolation between Jira/Bitbucket and the CI engine.

## 1.5 Comparison vs. Reference Architecture

Extensions beyond Azure-Samples `aks-platform-engineering` (unchanged from v1.0):

1. Crossplane is the sole provisioning engine (reference treats Crossplane and CAPZ as equivalents).
2. Multi-region management cluster pair with Azure Lease promotion.
3. Private Endpoint everywhere for Azure PaaS; SaaS dependencies via Firewall + WAF + FQDN allow-list.
4. ESO + Workload Identity + per-namespace `SecretStore` as the canonical secret pipeline.

## 1.6 C4 Container Diagram

`IDP-C4-Container-v2.drawio` delivers the same content as v1's diagram with the corrections:
- Bitbucket Cloud relocated out of the "Private Endpoint" mesh into the "Egress via Azure Firewall" path.
- mgmt-ne labeled "Scale-to-Zero Standby (Lease-Awaiting)" instead of "Read-Only Crossplane."
- Per-region AKV pair shown.
- `mgmt-leader-lease` and Storage Blob lease shown as a new platform component.

## 1.7 Management-Plane Singleton Lock (NEW)

A small Go controller named **`mgmt-leader-lease`** runs in both `mgmt-we` and `mgmt-ne`. Its job: acquire and continuously renew an Azure Storage Blob Lease against `stplatleasewe<random>/leases/mgmt-active` (or its NE counterpart — the storage account is geo-redundant via GRS).

**Lease semantics:**
- Lease TTL: 60 seconds.
- Renewal interval: 15 seconds.
- Acquire-when-free: poll every 5 seconds when not holding the lease.
- Operator override: a small CLI `mgmt-cli failback --to mgmt-we --confirm` calls Storage REST to break the lease and reacquire from the target cluster.

**Effect:**
- The lease-holding cluster writes its identity to ConfigMap `mgmt-leader-status` in `kube-system`.
- A second controller, **`controller-scaler`**, watches that ConfigMap and reconciles controller replica counts:
  - On lease-acquisition: ArgoCD application-controller → 3, server → 2, repo-server → 2; Crossplane providers → 1; ESO orchestrator → 1.
  - On lease-loss: all of the above → 0.
  - Additionally, updates the `lease-status` label on the cluster's ArgoCD Secret to `active` (on acquisition) or `standby` (on loss).
- The Storage Account hosting the lease blob has a private endpoint reachable from both regions; its own HA is the standard Azure Storage 99.99% SLA, geo-replicated for catastrophe recovery.

**Why this exists:** prevents split-brain where both mgmt clusters believe they are active simultaneously, which would cause ARM ownership thrash, ARM rate-limit exhaustion, and double-writes to AKVs.

See ADR-022.

---

# 2. JIRA & BITBUCKET PLATFORM ENGINEERING GATEWAYS

## 2.1 "Create New IDP Service" — End-to-End Workflow

Unchanged from v1.0 §2.1 in shape. Two corrections to the Jenkins seed job (§2.1.2):

- The `bitbucket-app-password` Jenkins credential is replaced by `bitbucket-workspace-token` sourced from AKV via ESO (see §5.7).
- The Jira token credential is replaced by `jira-service-account-token` sourced from AKV.

The seed job's Bitbucket API calls now use:

```groovy
withCredentials([string(credentialsId: 'bitbucket-workspace-token', variable: 'BB_TOKEN')]) {
  sh '''
    curl -sS -H "Authorization: Bearer $BB_TOKEN" -X POST \
      "https://api.bitbucket.org/2.0/repositories/${BITBUCKET_WORKSPACE}/${serviceName}" \
      ...
  '''
}
```

The seed job creates **two** subdirectories per service in `platform-gitops` (per the two-tier ApplicationSet design — see §6.2):

```
apps/<serviceName>/
├── infra/
│   ├── base/
│   │   ├── xrc-sql.yaml
│   │   ├── xrc-cosmos.yaml
│   │   ├── xrc-sb.yaml
│   │   └── xrc-namespace-binding.yaml    # per-namespace UAMI + SecretStore
│   └── overlays/{dev,staging,prod}/
└── workload/
    ├── base/
    │   ├── rollout.yaml                  # Argo Rollouts, not Deployment
    │   ├── service.yaml
    │   ├── analysis-template.yaml        # platform-provided per SLO class
    │   ├── ingress.yaml
    │   ├── external-secret-sql.yaml
    │   ├── external-secret-cosmos.yaml
    │   └── external-secret-sb.yaml
    └── overlays/{dev,staging,prod}/
```

For `dev` and `staging`, the seed job's template substitutes `kind: Deployment` for `kind: Rollout` if the developer team prefers (Rollouts is *mandatory* only in prod overlays).

## 2.2 Governance Model

Unchanged from v1.0 — branch protection, CODEOWNERS, mandatory scans. One addition: a Kyverno policy on the `platform-gitops` repo's pre-merge CI rejects PRs that bump `Deployment` (not `Rollout`) in any `overlays/prod/` directory.

## 2.3 Jira Tracking of Drift and Rollbacks

Unchanged from v1.0 §2.3. Note: `argocd-jira-bridge` now runs only in the active mgmt cluster (scaled by `controller-scaler` based on lease status).

## 2.4 Workflow — PlantUML Activity Diagram

Same as v1.0 §2.4 with one updated swimlane note: "Jenkins seed job — creates *two* PRs to platform-gitops: one for infra/, one for workload/, gated by the same review path."

---

# 3. RESILIENT CI PIPELINE ENGINE DESIGN

## 3.1 Jenkins (vs. Bamboo) Recap

Same justification as v1.0 ADR-001. Failure-domain isolation from Atlassian SaaS is the decisive argument.

## 3.2 Resilient Declarative Pipeline (Jenkinsfile Outline)

Same shape as v1.0 §3.2 with two changes:

- **Credential resolution:** `bitbucket-workspace-token` and `jira-service-account-token` are sourced from AKV via ESO + Reloader. When the token rotates (quarterly), Reloader restarts the Jenkins controller; in-flight builds are checkpointed; new builds use the new token.
- **GitOps update step** now updates **only the workload overlay**:

```groovy
stage('GitOps: Update workload overlay (idempotent, race-free)') {
  when { branch 'main' }
  agent { kubernetes { yamlFile 'ci/pod-templates/git.yaml' } }
  environment {
    GITOPS_PATH = "apps/${SERVICE_NAME}/workload/overlays/dev"   # never apps/.../infra/...
  }
  steps {
    sshagent(['gitops-deploy-key']) {
      sh '''
        set -euo pipefail
        git clone --depth 1 ${GITOPS_REPO} gitops
        cd gitops/${GITOPS_PATH}
        kustomize edit set image ${SERVICE_NAME}=${ACR_LOGIN}/${SERVICE_NAME}:${IMAGE_TAG}
        if git -C ../../.. diff --quiet; then
          echo "No image change; skipping commit"; exit 0
        fi
        git -c user.email=ci@platform -c user.name=PlatformBot \
          commit -am "[ci skip] ${SERVICE_NAME}: bump dev workload to ${IMAGE_TAG}"
        for i in 1 2 3 4 5; do
          if git push origin main; then exit 0; fi
          git pull --rebase origin main
        done
        echo "Push failed after 5 retries"; exit 1
      '''
    }
  }
}
```

The Jenkins pipeline never touches `apps/<svc>/infra/`. Infrastructure changes are exclusively human PRs reviewed by Platform Engineers.

## 3.3 Race-Free GitOps — The Loop-Prevention Contract

**Authoritative mechanism: the author filter on the Jenkins multibranch Bitbucket Branch Source trait** — `^(?!PlatformBot).*$`. Commits authored by `PlatformBot` (the Bitbucket workspace service account used by the pipeline) never trigger a Jenkins build.

**Defensive mechanism: the `[ci skip]` commit-message marker.** Some future tool addition (a second CI runner, a GitHub mirror, a downstream Bitbucket Pipelines lint job) may respect this convention. We emit it for forward-compatibility.

**Contract:** the author filter is the primary loop-prevention layer. The `[ci skip]` marker is defensive. **Removing either layer requires a new ADR** (this is captured in ADR-010-v2).

## 3.4 Jenkins HA, Recovery, and DR Posture (Revised)

Jenkins runs **only in `mgmt-we`**, as a **single-replica StatefulSet**:

- `replicas: 1` (single master — upstream Jenkins doesn't support multi-master).
- PVC: ReadWriteOnce Azure Disk Premium SSD for `$JENKINS_HOME`.
- `terminationGracePeriodSeconds: 300` to allow in-flight job draining.
- LivenessProbe on `/login` (HTTP 200), readinessProbe on `/whoAmI/api/json`.
- Velero backs up the PVC every 6 hours to GRS storage; retention 30 days.
- Expected restart RTO: ~60-90s (pod reschedule + plugin load).
- Expected DR RTO (full mgmt-we loss): ~30 minutes — Jenkins is paused until mgmt-we recovers.

This is acceptable because Jenkins is the most write-side and most pause-tolerant of all platform components. ADR-001-v2 documents the trade-off: faster Jenkins DR would require running an idle controller in mgmt-ne and is not justified by the failure model.

**Agent scaling:**
- Dedicated AKS node pool `cipool` in `mgmt-we`, taint `workload=ci:NoSchedule` (which is fine — `tier=platform` namespaces tolerate that taint).
- Per-job pods spawned by the `kubernetes-plugin`; pods are deleted on job completion.
- Autoscaler scales `cipool` between 1 and 30 nodes.

## 3.5 Pipeline Workflow Diagram

Identical to v1.0 §3.5 in structure, with a note added on the "GitOps: Update workload overlay" stage: *"Only touches `apps/<svc>/workload/overlays/dev/`; never the `infra/` subtree."*

---

# 4. CROSSPLANE CLOUD CONTROL PLANE & AZURE CUSTOM RESOURCES

## 4.0 SLO Class Contract (NEW)

`sloClass` on every Composition claim is a single platform-issued availability tier that translates to the right SKU and posture decisions across services. The contract:

| Class | Availability SLO | RTO / RPO | Suitable for | Cost relative |
|---|---|---|---|---|
| **Bronze** | 99.5% | RTO ≤ 4h / RPO ≤ 1h | Dev, sandbox, internal-tools | 1× |
| **Silver** | 99.9% | RTO ≤ 15m / RPO ≤ 5m | Staging, internal-facing prod, low-criticality | 3× |
| **Gold** | 99.95%+ | RTO ≤ 1m / RPO ≤ 30s | Revenue-critical prod, regulated workloads | 8-10× |

The composition translates SLO class to Azure choices on a per-resource basis. **Cosmos consistency level is no longer derived from SLO class** — it's a separate parameter, because consistency is an app-correctness choice, not a reliability tier.

| Resource | Bronze | Silver | Gold |
|---|---|---|---|
| Azure SQL SKU | `GP_S_Gen5_2` (serverless) | `GP_Gen5_4` | `BC_Gen5_8`, zone-redundant, geo-replica |
| Cosmos topology | Single region | Single region + continuous backup | Multi-region replica (auto-failover priority 0=primary, 1=NE) |
| Service Bus | Basic | Standard | Premium |
| Argo Rollouts strategy | None (direct cutover) | Canary 25/100 + success-rate analysis | Canary 5/25/50/100 + success-rate + p99-latency analysis |
| PDB minAvailable | 0 | 1 | 2 |
| HPA min replicas | 1 | 2 | 3 |

Cosmos consistency is its own parameter on the claim:

```yaml
spec:
  parameters:
    sloClass: gold              # availability tier
    consistency: session        # app-correctness choice; eventual|session|bounded|strong
```

## 4.1 Composition Strategy

Unchanged from v1.0 §4.1. Note: every Composition that emits secrets to AKV now emits to **both** regional vaults (see §5.1.1). This is implemented as two `keyvault.azure.upbound.io/v1beta1 Secret` resources per managed secret, with selectors `region=westeurope` and `region=northeurope`.

## 4.2 XRD #1 — Azure SQL Server + Database (Revised)

Same shape as v1.0 §4.2 with the following corrections to the Composition:

```yaml
# Excerpt — the dual-write to per-region AKV
- name: pushConnInfoToKV-we
  base:
    apiVersion: keyvault.azure.upbound.io/v1beta1
    kind: Secret
    spec:
      forProvider:
        contentType: "application/json"
        keyVaultIdSelector:
          matchLabels:
            role: platform-kv
            region: westeurope
  patches:
  - fromFieldPath: spec.parameters.serviceName
    toFieldPath: metadata.name
    transforms:
    - { type: string, string: { fmt: "%s-sql-conn-we" } }
  - fromFieldPath: spec.parameters.serviceName
    toFieldPath: spec.forProvider.name
    transforms:
    - { type: string, string: { fmt: "%s-sql-conn" } }
  - type: CombineFromComposite
    combine:
      variables:
      - fromFieldPath: status.atProvider.fullyQualifiedDomainName
      - fromFieldPath: spec.parameters.serviceName
      strategy: string
      string: { fmt: '{"server":"%s","database":"sql-%s-db","authMode":"AAD-Integrated"}' }
    toFieldPath: spec.forProvider.value

- name: pushConnInfoToKV-ne
  base:
    apiVersion: keyvault.azure.upbound.io/v1beta1
    kind: Secret
    spec:
      forProvider:
        contentType: "application/json"
        keyVaultIdSelector:
          matchLabels:
            role: platform-kv
            region: northeurope
  patches:
  - fromFieldPath: spec.parameters.serviceName
    toFieldPath: metadata.name
    transforms:
    - { type: string, string: { fmt: "%s-sql-conn-ne" } }
  - fromFieldPath: spec.parameters.serviceName
    toFieldPath: spec.forProvider.name
    transforms:
    - { type: string, string: { fmt: "%s-sql-conn" } }     # same logical name in each vault
  # ... same value patch as above
```

`zoneRedundant` is now derived from `sloClass` per the SLO contract above.

## 4.3 XRD #2 — Cosmos DB (Revised)

Same shape as v1.0 §4.3 with two corrections:

1. **`consistency` is its own claim parameter** (not derived from sloClass):

```yaml
parameters:
  serviceName:        { type: string }
  sloClass:           { type: string, enum: [bronze, silver, gold] }
  consistency:        { type: string, enum: [eventual, session, bounded, strong], default: session }
  primaryRegion:      { type: string, default: westeurope }
  failoverRegion:     { type: string, default: northeurope }
  databaseName:       { type: string, default: appdb }
```

2. **`multipleWriteLocationsEnabled` stays `false`** explicitly, with a comment in the Composition declaring "Active-Active reads, Active-Passive writes" as the chosen topology:

```yaml
- name: cosmosAccount
  base:
    apiVersion: cosmosdb.azure.upbound.io/v1beta1
    kind: Account
    spec:
      forProvider:
        kind: GlobalDocumentDB
        offerType: Standard
        consistencyPolicy:
        - consistencyLevel: Session              # patched from spec.parameters.consistency
        automaticFailoverEnabled: true
        multipleWriteLocationsEnabled: false     # Active-Passive writes; see ADR-004-v2
        publicNetworkAccessEnabled: false
        backup:
        - type: Continuous
        geoLocation:
        - location: westeurope
          failoverPriority: 0
        - location: northeurope
          failoverPriority: 1
  patches:
  - fromFieldPath: spec.parameters.consistency
    toFieldPath: spec.forProvider.consistencyPolicy[0].consistencyLevel
    transforms:
    - type: map
      map: { eventual: Eventual, session: Session, bounded: BoundedStaleness, strong: Strong }
```

Connection metadata is dual-written to both AKVs as in §4.2.

## 4.4 XRD #3 — Azure DNS

Unchanged from v1.0 §4.4. DNS records have no SLO class (DNS itself has Azure-wide availability).

## 4.5 XRD #4 — Service Bus (Revised)

Same shape as v1.0 §4.5 with SLO-class mapping per §4.0: bronze→Basic, silver→Standard, gold→Premium. Connection strings are dual-written.

## 4.6 XRD #5 — Namespace Vault Binding (NEW)

Required by the per-namespace ESO isolation pattern (Q10, ADR-020). Each workload namespace gets a `NamespaceVaultBinding` claim that materializes the identity scaffolding for reading secrets.

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xnamespacevaultbindings.platform.example.com
spec:
  group: platform.example.com
  names: { kind: XNamespaceVaultBinding, plural: xnamespacevaultbindings }
  claimNames: { kind: NamespaceVaultBinding, plural: namespacevaultbindings }
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              parameters:
                type: object
                required: [namespaceName, clusterName, region]
                properties:
                  namespaceName: { type: string }
                  clusterName:   { type: string }      # e.g. aks-prod-we
                  region:        { type: string }      # westeurope | northeurope
                  secretPrefix:  { type: string }      # defaults to namespaceName
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: namespace-vault-binding.platform.example.com
spec:
  compositeTypeRef:
    apiVersion: platform.example.com/v1alpha1
    kind: XNamespaceVaultBinding
  resources:
  - name: uami
    base:
      apiVersion: managedidentity.azure.upbound.io/v1beta1
      kind: UserAssignedIdentity
      spec:
        forProvider:
          resourceGroupNameSelector: { matchLabels: { role: identity-rg } }
    patches:
    - fromFieldPath: spec.parameters.namespaceName
      toFieldPath: metadata.name
      transforms:
      - { type: string, string: { fmt: "uami-%s-${CLUSTER}" } }   # CLUSTER substituted by patch from clusterName
    - fromFieldPath: spec.parameters.region
      toFieldPath: spec.forProvider.location
  - name: federatedCredential
    base:
      apiVersion: managedidentity.azure.upbound.io/v1beta1
      kind: FederatedIdentityCredential
      spec:
        forProvider:
          audience: ["api://AzureADTokenExchange"]
          parentIdSelector: { matchControllerRef: true }
    patches:
    - fromFieldPath: spec.parameters.clusterName
      toFieldPath: spec.forProvider.issuer
      transforms:
      - { type: string, string: { fmt: "https://${REGION}.oic.prod-aks.azure.com/<TENANT>/<OIDC_FROM_CLUSTER_STATUS>/" } }
    - type: CombineFromComposite
      combine:
        variables:
        - fromFieldPath: spec.parameters.namespaceName
        strategy: string
        string: { fmt: "system:serviceaccount:%s:eso-sa" }
      toFieldPath: spec.forProvider.subject
  - name: keyVaultAccessPolicy
    base:
      apiVersion: authorization.azure.upbound.io/v1beta1
      kind: RoleAssignment
      spec:
        forProvider:
          # 'Key Vault Secrets User' role, scoped to a Vault with secret-name conditions
          roleDefinitionIdLiteral: "/providers/Microsoft.Authorization/roleDefinitions/4633458b-17de-408a-b874-0445c86b69e6"
          principalIdSelector: { matchControllerRef: true }
          # Condition expression scopes the role to secrets matching <prefix>-* in the regional vault
          condition: |
            (
              !(ActionMatches{'Microsoft.KeyVault/vaults/secrets/getSecret/action'})
              OR
              @Resource[Microsoft.KeyVault/vaults/secrets:Name] LIKE '${PREFIX}-*'
            )
          conditionVersion: "2.0"
    patches:
    - fromFieldPath: spec.parameters.region
      toFieldPath: spec.forProvider.scopeSelector.matchLabels.region
    - { fromFieldPath: spec.parameters.secretPrefix, toFieldPath: spec.forProvider.condition }
```

Note: the `condition` field above uses Azure ABAC (Attribute-Based Access Control) for Key Vault data-plane actions, which supports secret-name pattern matching. This requires the AKV to be in RBAC authorization mode (`enableRbacAuthorization: true`).

The Composition is invoked once per (namespace, cluster) pair. In practice, an ApplicationSet `namespace-vault-bindings-set` generates one `NamespaceVaultBinding` per namespace listed in `platform/namespaces.yaml`. The output is consumed by the namespace's ESO `SecretStore` (see §5.3).

## 4.7 Reconciliation, Drift, and Rate Limits

Unchanged from v1.0 §4.6. One addition: the per-region ProviderConfig sharding now also distributes secret writes across the two regional AKVs.

---

# 5. ZERO-TRUST SECURITY — AZURE KEY VAULT & ESO

## 5.1 End-to-End Secret Lifecycle (Revised)

```
   ┌──────────────────┐   provisions   ┌──────────────────────┐
   │ Crossplane       │ ─────────────► │  Azure SQL / Cosmos  │
   │ (active mgmt)    │                │  / Service Bus       │
   └────────┬─────────┘                └──────────┬───────────┘
            │ writes connection JSON              │
            │ (atomically to BOTH AKVs)           │ used by
            ▼                                     ▼
   ┌──────────────────────┐                 ┌──────────────────────┐
   │ kv-platform-prod-we  │                 │ kv-platform-prod-ne  │
   │ (private endpoint    │                 │ (private endpoint    │
   │  in WE hub)          │                 │  in NE hub)          │
   └──────────────────────┘                 └──────────────────────┘
            │ polled by per-namespace ESO            │ polled by per-namespace ESO
            ▼ in aks-prod-we                         ▼ in aks-prod-ne
   ┌────────────────────────────────────┐    ┌────────────────────────────────────┐
   │ payments namespace                 │    │ payments namespace                 │
   │ SA: eso-sa (UAMI fed)              │    │ SA: eso-sa (UAMI fed)              │
   │ SecretStore → kv-platform-prod-we  │    │ SecretStore → kv-platform-prod-ne  │
   │ ExternalSecret → K8s Secret        │    │ ExternalSecret → K8s Secret        │
   │ Reloader → rollout restart         │    │ Reloader → rollout restart         │
   └────────────────────────────────────┘    └────────────────────────────────────┘
```

## 5.1.1 Per-Region Vault Topology (NEW)

| Cluster | Reads from | Writers |
|---|---|---|
| `aks-dev-we` | `kv-platform-dev-we` | Crossplane (active mgmt) |
| `aks-staging-we` | `kv-platform-staging-we` | Crossplane (active mgmt) |
| `aks-prod-we` | `kv-platform-prod-we` | Crossplane (active mgmt, both AKVs) |
| `aks-prod-ne` | `kv-platform-prod-ne` | Crossplane (active mgmt, both AKVs) |

Each vault has:
- Private endpoint in the corresponding region's hub.
- RBAC mode (`enableRbacAuthorization: true`) so ABAC conditions can scope reads.
- Soft-delete (90 days) + purge-protection enabled.
- Diagnostic logs piped to Azure Monitor for audit.

The `controller-scaler` in the active mgmt cluster ensures that only one Crossplane instance writes to the vault pair at any time (Q16 / ADR-022).

## 5.2 Authentication — Workload Identity (Revised)

Same as v1.0 §5.2 but now scoped per-namespace, not per-cluster. The UAMI for the `payments` namespace in `aks-prod-we` is `uami-payments-aks-prod-we`, federated to `system:serviceaccount:payments:eso-sa`. It is granted `Key Vault Secrets User` on `kv-platform-prod-we`, **conditioned** on the secret name matching `payments-*` (ABAC).

## 5.3 ESO SecretStore (Per-Namespace, Revised)

```yaml
# Created automatically by the NamespaceVaultBinding Composition
apiVersion: external-secrets.io/v1beta1
kind: SecretStore                                  # namespaced — not Cluster
metadata:
  name: azure-keyvault
  namespace: payments
spec:
  provider:
    azurekv:
      authType: WorkloadIdentity
      vaultUrl: https://kv-platform-prod-we.vault.azure.net   # local regional vault
      serviceAccountRef:
        name: eso-sa
      environmentType: PublicCloud
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: eso-sa
  namespace: payments
  annotations:
    azure.workload.identity/client-id: "<UAMI_CLIENT_ID>"     # from NamespaceVaultBinding status
    azure.workload.identity/tenant-id: "<TENANT_ID>"
  labels:
    azure.workload.identity/use: "true"
```

The ESO controller in the cluster is configured with `--namespace-scoped-stores=true` and to honor SecretStore over ClusterSecretStore. No ClusterSecretStore is deployed.

## 5.4 ExternalSecret (Revised)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: payments-sql-conn
  namespace: payments
spec:
  refreshInterval: 1m
  secretStoreRef:
    kind: SecretStore                            # not ClusterSecretStore
    name: azure-keyvault
  target:
    name: payments-sql-conn
    creationPolicy: Owner
    template:
      engineVersion: v2
      type: Opaque
      metadata:
        annotations:
          reloader.stakater.com/match: "true"
      data:
        SQL_SERVER:   "{{ .conn | fromJson | dig "server" "" }}"
        SQL_DATABASE: "{{ .conn | fromJson | dig "database" "" }}"
        SQL_AUTHMODE: "{{ .conn | fromJson | dig "authMode" "" }}"
  data:
  - secretKey: conn
    remoteRef:
      key: payments-sql-conn                      # matches the AKV secret name; ABAC scopes by 'payments-*'
      version: latest
```

A compromised `payments` namespace cannot create an `ExternalSecret` referencing `billing-sql-conn` — the AKV RBAC condition (`secrets:Name LIKE 'payments-*'`) denies the read.

## 5.5 Rotation

Unchanged from v1.0 §5.5. End-to-end MTTR for an automatic rotation: ~90 seconds.

## 5.6 Lifecycle Diagram

Same PlantUML as v1.0 §5.6 with the SecretStore changed from `ClusterSecretStore` to `SecretStore`, and the AKV named per-region.

## 5.7 SaaS API Credentials — Same Pipeline as Cloud Connection Strings (NEW)

Bitbucket Cloud and Jira Cloud don't accept federated identity tokens inbound — their APIs require static tokens. ADR-006-v2 narrows the "no static credentials" promise to **cluster ↔ Azure** paths, and treats SaaS API tokens as residual statics with the following lifecycle:

| Token | Type | Scope | Storage | Rotation |
|---|---|---|---|---|
| Bitbucket workspace access token | Workspace-scoped (not user) | `repository:write`, `pullrequest:write` | `kv-platform-prod-we:bitbucket-workspace-token` | Quarterly via Crossplane CronJob |
| Jira service-account API token | Bound to dedicated svc account, project-scoped to `IDP` | Read+write on IDP project only | `kv-platform-prod-we:jira-sa-token` | Quarterly via Crossplane CronJob |
| Cosign signing key | KMS-backed in AKV (HSM if compliance demands) | Sign images in ACR | `kv-platform-prod-we:cosign-signing-key` | Annually (re-sign in place) |

Rotation procedure for the Bitbucket workspace token:

1. `saas-token-rotator` CronJob (in `mgmt-leader-status`-aware namespace, runs only on lease holder) authenticates to Atlassian via the existing token, mints a new workspace token via `POST /workspaces/{slug}/access-tokens`, writes the new value to AKV (both regions).
2. Reloader detects the new Secret hash in Jenkins's mounted Secret; rolls Jenkins controller pod. In-flight builds are checkpointed by Jenkins's resume-on-restart.
3. After 24 hours, the rotator revokes the previous token via `DELETE /workspaces/{slug}/access-tokens/{id}`.

The rotator emits a Prometheus metric `saas_token_age_days{token="bitbucket-workspace"}` so alerting fires if a rotation is missed.

---

# 6. ARGOCD GITOPS ENGINE CONFIGURATION

## 6.1 Hub-and-Spoke Topology

ArgoCD runs as the hub in the **active** mgmt cluster (per the lease). The controller-scaler keeps mgmt-ne's ArgoCD at zero replicas when it doesn't hold the lease.

HA inside the active cluster: 3 application-controllers (sharded by cluster), 2 repo-servers, 3 redis-ha replicas, 2 server replicas.

## 6.2 Two-Tier ApplicationSet (Revised)

Per Q4 and ADR-013-v2, each service has **two Applications**:

- `<svc>-infra-<env>` — manages slow-lifecycle cloud infrastructure. Owned by `platform-infra-set` ApplicationSet. Long retry budgets.
- `<svc>-app-<env>-<cluster>` — manages fast-lifecycle workload manifests. Owned by `workloads-set` ApplicationSet. Short retry budgets.

### platform-infra-set (slow tier)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: platform-infra-set
  namespace: argocd
spec:
  goTemplate: true
  generators:
  - matrix:
      generators:
      - list:
          elements:
          - { env: dev }
          - { env: staging }
          - { env: prod }
      - git:
          repoURL: git@bitbucket.org:platform-prod/platform-gitops.git
          revision: main
          directories:
          - path: apps/*/infra/overlays/{{.env}}
  template:
    metadata:
      name: '{{ index .path.segments 1 }}-infra-{{ .env }}'
      labels:
        owner: '{{ index .path.segments 1 }}'
        env: '{{ .env }}'
        tier: infra
    spec:
      project: workloads
      destination:
        name: '{{ if eq .env "prod" }}aks-prod-we{{ else }}aks-{{ .env }}-we{{ end }}'   # NE prod handled by sibling Application
        namespace: '{{ index .path.segments 1 }}'
      source:
        repoURL: git@bitbucket.org:platform-prod/platform-gitops.git
        targetRevision: main
        path: '{{ .path.path }}'
      syncPolicy:
        automated:
          prune: false                # never prune cloud resources automatically
          selfHeal: true
        syncOptions:
        - CreateNamespace=true
        - ServerSideApply=true
        - ApplyOutOfSyncOnly=true
        retry:
          limit: 10
          backoff:
            duration: 1m
            factor: 2
            maxDuration: 30m         # accommodates Azure SQL/Cosmos provisioning
```

### workloads-set (fast tier)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: workloads-set
  namespace: argocd
spec:
  goTemplate: true
  generators:
  - matrix:
      generators:
      - clusters:
          selector: { matchLabels: { role: workload } }
      - git:
          repoURL: git@bitbucket.org:platform-prod/platform-gitops.git
          revision: main
          directories:
          - path: apps/*/workload/overlays/*
  template:
    metadata:
      name: '{{ index .path.segments 1 }}-app-{{ index .path.segments 4 }}-{{ .name }}'
      labels:
        owner: '{{ index .path.segments 1 }}'
        env:   '{{ .metadata.labels.env }}'
        tier:  workload
    spec:
      project: workloads
      destination:
        name: '{{ .name }}'                # cluster name
        namespace: '{{ index .path.segments 1 }}'
      source:
        repoURL: git@bitbucket.org:platform-prod/platform-gitops.git
        targetRevision: main
        path: '{{ .path.path }}'
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
        syncOptions:
        - CreateNamespace=true
        - PrunePropagationPolicy=foreground
        - ServerSideApply=true
        retry:
          limit: 5
          backoff:
            duration: 30s
            factor: 2
            maxDuration: 5m
      revisionHistoryLimit: 20
```

The workload Application depends on the infra Application via a **custom AppProject signature key**: the workloads-set generator filters to only overlays whose infra-Application is currently Healthy. Concretely, an admission webhook on ArgoCD adds an annotation `platform.example.com/infra-healthy: true|false` to each workload Application's status; the sync policy blocks if `false`. (Alternatively, a simpler version uses ArgoCD's `dependencies` field if available in the installed version.)

## 6.3 Sync Waves (Within an Application — Revised)

Because Crossplane XRCs now live in the **infra** Application (separate lifecycle), the workload Application's waves are smaller and faster:

| Wave | Resource | Why |
|---|---|---|
| -1 | NetworkPolicy, RBAC | Foundation; runs in seconds |
| 0 | ExternalSecret | ESO fills secrets (Crossplane is already done in the infra App) |
| 1 | ConfigMap, ServiceAccount | Glue |
| 2 | Rollout / Deployment, Service | Pods start |
| 3 | HPA, PDB | Tuning |
| 4 | Ingress | Expose only when pods Ready |

Wave 0 typically completes in under 30 seconds (ESO poll frequency). Wave 2 takes whatever the rollout takes (canary = several minutes for prod; immediate for dev).

The infra Application uses its own sync waves internally:

| Wave | Resource | Why |
|---|---|---|
| 0 | NamespaceVaultBinding XRC | Creates UAMI + FIC + AKV access policy |
| 1 | SQLDatabase, CosmosAccount, ServiceBus, DnsRecord XRCs | Provisions PaaS |

Both Applications use custom `health.lua` for Crossplane XR kinds so wave gating waits on `status.conditions[Ready] == True`.

## 6.4 Self-Healing & Rollback

`syncPolicy.automated.selfHeal: true` on both tiers. Manual rollback for the workload tier is one command:

```bash
argocd app rollback payments-app-prod-aks-prod-we 7
```

The infra tier deliberately disables `prune` to prevent accidental destruction of cloud resources by a misconfigured commit. To delete a cloud resource, the operator removes the XRC manifest from `apps/<svc>/infra/overlays/<env>/` and the Crossplane operator deletes the underlying Azure resource on next reconcile.

## 6.5 Progressive Delivery — Argo Rollouts (NEW)

**Argo Rollouts is mandatory for all workloads in `aks-prod-we` and `aks-prod-ne`.** Dev and staging may use either `Deployment` or `Rollout`. A Kyverno policy `KyvernoProdRequiresRollout` rejects `Deployment` resources in any namespace residing on a prod cluster.

The Argo Rollouts controller runs **locally in each workload cluster** (not in the management cluster), deployed via the bootstrap ApplicationSet. It is not managed by `controller-scaler`.

Default canary strategy per SLO class:

```yaml
# Gold canary
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: payments
  namespace: payments
spec:
  replicas: 3
  strategy:
    canary:
      analysis:
        templates:
        - templateName: gold-analysis
        startingStep: 1
      steps:
      - setWeight: 5
      - pause: { duration: 5m }
      - setWeight: 25
      - pause: { duration: 5m }
      - setWeight: 50
      - pause: { duration: 5m }
      - setWeight: 100
  selector: { matchLabels: { app: payments } }
  template: { ... }       # the pod spec
```

The `AnalysisTemplate` is created by a separate Composition **`NamespaceRolloutPolicy`** (XRD `xnamespacerolloutpolicies.platform.example.com`) based on the service's SLO class. This decouples progressive-delivery concerns from identity/secret concerns (handled by `NamespaceVaultBinding`):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: gold-analysis
  namespace: payments
spec:
  metrics:
  - name: success-rate
    interval: 30s
    successCondition: result[0] >= 0.99
    failureLimit: 2
    provider:
      prometheus:
        address: http://prometheus.observability.svc:9090
        query: |
          sum(rate(http_requests_total{namespace="payments",code!~"5.."}[1m]))
            /
          sum(rate(http_requests_total{namespace="payments"}[1m]))
  - name: p99-latency
    interval: 30s
    successCondition: result[0] <= 0.500
    failureLimit: 2
    provider:
      prometheus:
        address: http://prometheus.observability.svc:9090
        query: |
          histogram_quantile(0.99,
            sum by (le) (rate(http_request_duration_seconds_bucket{namespace="payments"}[1m])))
```

Failed analysis aborts the rollout — the canary ReplicaSet is scaled to zero and the stable ReplicaSet remains at 100% traffic. ArgoCD marks the Application Degraded and `argocd-jira-bridge` opens a Jira incident.

For Bronze: no analysis template; canary collapses to `[ { setWeight: 100 } ]` (effectively a direct cutover, but using `Rollout` for consistency).

## 6.6 Workflow Diagram

Updated PlantUML (same shape as v1.0 §6.5 with the infra/workload split and the AnalysisTemplate gate added between "Rollout starts" and "Rollout complete").

---

# 7. DAY-2 OPERATIONS, OBSERVABILITY & RESILIENCE RUNBOOKS

## 7.1 Observability Stack

Unchanged from v1.0 §7.1. Added alert rules:

```yaml
- alert: MgmtLeaderLeaseLost
  expr: time() - mgmt_leader_lease_renewed_seconds > 30
  for: 1m
  labels: { severity: critical }
  annotations:
    summary: "Active mgmt cluster lost its lease; controller-scaler will scale this cluster down"

- alert: PerRegionAKVDualWriteSkew
  expr: |
    abs(
      sum(kv_secret_last_modified_timestamp{vault="kv-platform-prod-we"})
      -
      sum(kv_secret_last_modified_timestamp{vault="kv-platform-prod-ne"})
    ) > 600
  for: 10m
  labels: { severity: warning }
  annotations:
    summary: "AKV pair has been out-of-sync for 10m; investigate Crossplane dual-write health"

- alert: SaaSTokenAgeExceeded
  expr: saas_token_age_days{token=~"bitbucket-workspace|jira-sa"} > 100
  for: 1h
  labels: { severity: warning }

- alert: RolloutAnalysisFailed
  expr: rollout_phase{phase="Degraded"} == 1
  for: 1m
  labels: { severity: critical }
```

## 7.2 Platform-Loop SLOs

| SLO | Target | Measurement |
|---|---|---|
| Time-to-deploy (workload tier) | p95 ≤ 5 min | Jenkins build + ArgoCD workload sync |
| Time-to-provision (infra tier) | p95 ≤ 30 min | PR merge → SQL/Cosmos Ready |
| Drift-correction-MTTR (workload) | p95 ≤ 3 min | Manual change → reverted |
| Secret freshness | p95 ≤ 90 s | AKV rotation → app restart |
| Mgmt-plane failover RTO | p99 ≤ 120 s | Lease loss → mgmt-ne fully active |

## 7.3 Disaster Recovery — Total Loss of Active Management Cluster (Revised)

**Scope.** `mgmt-we` is destroyed or partitioned. Workload clusters and Azure PaaS are still running.

**Pre-conditions:**
1. `platform-gitops` is replicated (Bitbucket Cloud → GitHub Enterprise mirror via scheduled job).
2. Velero takes 6-hourly snapshots of mgmt-we (etcd + PVs) to GRS storage.
3. The Azure Storage Blob Lease (§1.7) is the source of truth for which cluster is active.
4. Per-region AKVs both hold current secrets (Crossplane dual-write).

**Runbook:**

```
T+0  — ALERT FIRES (MgmtLeaderLeaseLost or AKS API unreachable)
       PagerDuty + Jira SEV1 incident opened automatically.

T+60s — mgmt-we's lease blob TTL expires (60s TTL, renewal every 15s).

T+65s — mgmt-ne's mgmt-leader-lease acquires the lease (polls every 5s).
        ConfigMap mgmt-leader-status in mgmt-ne is written to "we are now leader."
        controller-scaler updates cluster Secret label: lease-status=active.

T+70s — controller-scaler in mgmt-ne reads the ConfigMap; scales up:
          - argocd-application-controller → 3 replicas
          - argocd-server → 2 replicas
          - argocd-repo-server → 2 replicas
          - crossplane-provider-azure → 1 replica
          - external-secrets controller → 1 replica
          - argocd-jira-bridge → 1 replica

T+120s — ArgoCD on mgmt-ne reads the cluster Secrets (preserved in Git);
         re-establishes connections to workload clusters; resumes reconciliation.
         Crossplane re-establishes ownership of cloud resources via Adopt policy.
         (Argo Rollouts controllers in workload clusters are unaffected — they run locally.)

T+5min — On-call engineer validates:
           - kubectl --context mgmt-ne -n argocd get app → all Healthy/Synced
           - Crossplane XRs → Ready=True
           - ESO pulls fresh secrets from kv-platform-prod-ne
           - Front Door health-probes both prod clusters; traffic continues
           - Cosmos auto-failover triggered? (yes, if WE region itself died — RTO ~30-60s)

T+10min — Engineer runs canary deploy through Jenkins-substitute (manual build) to validate
          end-to-end pipeline. (Jenkins itself is offline until mgmt-we recovers — by design.)

T+15min — Jira incident downgraded to SEV2 (workloads stable, control plane on warm standby).

When mgmt-we recovers (anytime later):
- mgmt-we's mgmt-leader-lease polls the storage blob, finds lease held by mgmt-ne, stays passive.
- controller-scaler in mgmt-we keeps all controllers at 0 replicas.
- mgmt-we is now the standby. No split-brain.

For planned failback (after stability period):
- mgmt-cli failback --to mgmt-we --confirm
  - Acquires lease on behalf of mgmt-we; mgmt-ne's lease renewal fails and ne's controllers scale to 0.
- Engineer validates Jenkins recovery via Velero PVC restore: ~10 minutes.
- T+~30min total for full failback.
```

**Worst case — both mgmt-we and mgmt-ne are lost simultaneously** (e.g., Azure-wide outage affecting both regions):

```
1. Bring up seed-wus.
2. kubectl --context seed-wus apply -f bootstrap/control-plane-claim.yaml
   - Crossplane on the seed cluster provisions a new mgmt-we (or mgmt-wus if both EU regions are down).
3. The new mgmt cluster's mgmt-leader-lease acquires the (now-free) blob lease.
4. ArgoCD on the new cluster reads platform-gitops, syncs itself, syncs everything else.
5. Workload clusters resume reconciliation under the new mgmt.
6. RTO: ~45 minutes (driven by AKS provisioning + ArgoCD bootstrap).
7. Workloads themselves: still serving traffic the entire time (workload clusters are independent).
```

## 7.4 PlantUML DR Diagram

Same shape as v1.0 §7.4 with the lease-based auto-promotion as the first branch and the manual seed-rebuild as the second.

---

## Appendix A — Bill of Materials

Same as v1.0, with additions:

| Component | Version | License |
|---|---|---|
| Argo Rollouts | v1.7+ | Apache-2.0 |
| Kyverno | v1.12+ | Apache-2.0 |
| `mgmt-leader-lease` (in-house) | v1.0 | proprietary |
| `controller-scaler` (in-house) | v1.0 | proprietary |
| `argocd-jira-bridge` (in-house) | v1.0 | proprietary |
| `saas-token-rotator` (in-house) | v1.0 | proprietary |

## Appendix B — Folder Layout (platform-gitops repo, Revised)

```
platform-gitops/
├── bootstrap/
│   ├── argocd-values.yaml
│   ├── argocd-bootstrap-applicationset.yaml
│   ├── control-plane-claim.yaml
│   ├── leader-lease/
│   │   ├── storage-account.yaml             # Crossplane XR
│   │   └── lease-controller-deployment.yaml
│   ├── controller-scaler/
│   │   └── deployment.yaml
│   └── clusters/
│       ├── mgmt-we.yaml
│       ├── mgmt-ne.yaml
│       ├── seed-wus.yaml
│       ├── aks-dev-we.yaml
│       ├── aks-staging-we.yaml
│       ├── aks-prod-we.yaml
│       └── aks-prod-ne.yaml
├── platform/
│   ├── crossplane-providers/
│   ├── kyverno-policies/
│   │   ├── required-cluster-labels.yaml
│   │   ├── prod-requires-rollout.yaml
│   │   └── tier-platform-on-mgmt.yaml
│   ├── argocd-projects.yaml
│   └── namespaces.yaml                       # input to namespace-vault-bindings-set
├── apps/
│   └── payments/
│       ├── infra/                            # slow lifecycle
│       │   ├── base/
│       │   │   ├── xrc-sql.yaml
│       │   │   ├── xrc-cosmos.yaml
│       │   │   ├── xrc-sb.yaml
│       │   │   └── xrc-namespace-binding.yaml
│       │   └── overlays/{dev,staging,prod}/
│       └── workload/                         # fast lifecycle
│           ├── base/
│           │   ├── rollout.yaml
│           │   ├── analysis-template.yaml
│           │   ├── service.yaml
│           │   ├── ingress.yaml
│           │   ├── external-secret-sql.yaml
│           │   ├── external-secret-cosmos.yaml
│           │   └── external-secret-sb.yaml
│           └── overlays/{dev,staging,prod}/
└── templates/
    └── node-service/                         # Cookiecutter
```

---

*End of v2 blueprint. Companion: `IDP-GitOps-ADRs-v2.md`. Diagram: `IDP-C4-Container.drawio` (v1.0 diagram remains current — content unchanged in shape, only label corrections needed; a v2 .drawio update can be produced on request).*
