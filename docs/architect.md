# IDP GitOps Platform — Engineering Guide

> Authoritative operational and architectural reference for the CityOS Internal Developer Platform.
> Synthesized from: IDP-GitOps-Blueprint-v2.md, IDP-GitOps-ADRs-v2.md, IDP-GitOps-Blueprint-PRD.md,
> IDP-GitOps-Blueprint-PRD-v3.md, and **IDP-GitOps-Blueprint-PRD-v4.md** (architecture deepening).
> Last updated: 2026-05-25.
>
> **Alignment:** Aligned with **PRD-v4** (2026-05-25). Supersedes the PRD-v3 alignment archived at
> [`archived/architect.v3.md`](../archived/architect.v3.md). PRD-v4 is an additive addendum:
> all v2 (FR-1..25, US-001..025) and v3 (FR-V3-NN, US-V3-NN) requirements remain in force verbatim.
> v4 additions live in the `FR-V4-NN` / `US-V4-NN` namespace and surface throughout this guide
> (cluster registry §3.4, workload-identity module §15.5, reusable TF workflows §18.5,
> Go HTTP transport seam §7.5, service-seed split §11.4, hybrid Helm→ESO §6.5, robustness §15.6,
> CI hygiene §18.6). New ADR: **ADR-031-v4** (cluster topology lives in a single committed registry).

---

## Table of Contents

1. [Glossary and Naming Conventions](#1-glossary-and-naming-conventions)
2. [Architecture Overview](#2-architecture-overview)
3. [Cluster Topology](#3-cluster-topology) (incl. **§3.4 Cluster Topology Registry — PRD-v4**)
4. [Network Topology](#4-network-topology)
5. [GitOps Patterns](#5-gitops-patterns)
6. [Secrets Management](#6-secrets-management) (incl. **§6.5 Hybrid Helm → ESO — PRD-v4**)
7. [Management-Plane Singleton Lock](#7-management-plane-singleton-lock) (incl. **§7.5 Go Tooling Architecture — PRD-v4**)
8. [CI Pipeline](#8-ci-pipeline) (incl. **§8.5 Reusable Workflows + Composite Action — PRD-v4**)
9. [Supply Chain Security](#9-supply-chain-security)
10. [Progressive Delivery](#10-progressive-delivery)
11. [Self-Service Service Seed Workflow](#11-self-service-service-seed-workflow) (incl. **§11.4 Three-Module Split + Jinja — PRD-v4**)
12. [Observability and Alerting](#12-observability-and-alerting)
13. [DR Runbooks](#13-dr-runbooks)
14. [Key ADR Decisions](#14-key-adr-decisions) (incl. **ADR-031-v4**)
15. [Terraform State Management](#15-terraform-state-management) (incl. **§15.5 Workload-Identity Module + §15.6 Robustness Pass — PRD-v4**)
16. [Secret Material Lifecycle](#16-secret-material-lifecycle)
17. [RBAC Scope-Down (Post-v3)](#17-rbac-scope-down-post-v3)
18. [Quality Gates (Pre-commit + CI)](#18-quality-gates-pre-commit--ci)
19. [Variable Validation](#19-variable-validation)
20. [Supply-Chain Scanning (Checkov)](#20-supply-chain-scanning-checkov)
21. [Static Analysis (SonarQube)](#21-static-analysis-sonarqube)
22. [ADR-016 Amendment Note](#22-adr-016-amendment-note)
23. [**CI Hygiene (PRD-v4)**](#23-ci-hygiene-prd-v4)
24. [**PRD-v4 Mapping (Quick Reference)**](#24-prd-v4-mapping-quick-reference)

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

> **Naming convention.** Terraform resource files use `snake_case.tf`; Go binary names and Helm chart names use `kebab-case`. This is intentional and not a drift signal.

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

### 3.4 Cluster Topology Registry (PRD-v4, FR-V4-01..04, ADR-031-v4)

PRD-v4 collapses the previous three-way encoding of cluster topology (Terraform locals + ArgoCD ApplicationSet locals + `seed_job.py` hardcoded maps) into a **single committed registry**:

| Artifact | Path | Role |
|---|---|---|
| Registry data | `gitops/clusters/registry.yaml` | Sole authoritative source of cluster identity. Day-1: 7 entries (`mgmt-we`, `mgmt-ne`, `aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`, `seed-wus`). |
| Registry schema | `gitops/clusters/registry.schema.json` | JSON Schema enforced in pre-commit + CI. |
| Terraform consumer | `terraform/locals.tf` (and dependent `*.tf`) | Reads via `yamldecode(file("${path.module}/../gitops/clusters/registry.yaml"))`. No literal subscription IDs / regions / RG names / ACR hostnames may remain in `terraform/*.tf` after v4. |
| ArgoCD consumer | ApplicationSet locals under `gitops/clusters/` | Derive from the registry; no hand maintenance. |
| Python consumer | `tools/service_seed/cli.py` | Loads the registry once; passes data into `service_template.render(...)` and `gitops_pr.compose(...)` (see §11.4). |

Each registry entry carries: `subscription_id`, `region`, `region_abbrev`, `resource_group`, `acr_hostname`, `aks_name`, `mgmt_role` (`active|standby|workload|seed`), `azs` (list), `sku_tier`, `gitops_addons` (map of `enable_*` flags). Adding a cluster is a **one-PR** edit (the registry); the schema validator blocks malformed PRs at pre-commit time and in CI.

The decision to use a committed YAML rather than a CRD on `mgmt-we` is documented in **ADR-031-v4** (rejected alternative during grilling: CRD-based registry).

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

### 6.5 Hybrid Helm → ESO Migration for Sensitive Values (PRD-v4, FR-V4-36..40)

PRD-v4 eliminates the residual `helm_release.set { name = X, value = <sensitive_var> }` pattern that survived v3. Every secret-bearing chart value must arrive via an `ExternalSecret`-projected Kubernetes Secret. A two-mode helper local in `terraform/locals.tf` (or equivalent) classifies each call site:

| Mode | Semantics | When to use |
|---|---|---|
| `secrets_managed_in_tf` | Terraform generates the secret (`random_password`) → writes to AKV → ESO syncs into the workload namespace | System-generated material owned end-to-end by TF (e.g., internal admin passwords) |
| `secrets_referenced_only` | Terraform reads the secret via `data "azurerm_key_vault_secret"`; never generates it | Operator-rotated material pre-populated in AKV (e.g., Bitbucket workspace token, AKV-issued TLS) |

Enforcement:

- A custom **tflint** rule (FR-V4-37) fails CI when any `helm_release.set.value` references a variable declared `sensitive = true`.
- Charts that cannot natively consume an `ExternalSecret` must add the indirection before migration (FR-V4-38).
- Pre-existing call sites are inventoried in PRD-v4 §Appendix A and migrated iteratively across P5 (FR-V4-39).

Combined with the Go HTTP transport seam (§7.5, FR-V4-18), secret material is also absent from error strings — closing the Q7 credential-leak vector for free.

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

> **Note (PRD-v4, OQ-V4-04):** The future of `mgmt-cli` is an open question — it may merge into `mgmt-leader-lease` or remain as an operator escape hatch. Decision is targeted before P3 begins.

### 7.5 Go Tooling Architecture (PRD-v4, FR-V4-15..26)

PRD-v4 deepens four shallow seams across the `tools/mgmt-plane-lock/` Go binaries: HTTP transport policy, AKV writer consolidation, lifecycle plumbing, and per-binary run-loops.

#### 7.5.1 HTTP Transport Seam — `internal/httpx/` (FR-V4-15..18)

A single package owns HTTP policy across the binary set. Every `http.Client` is constructed by `httpx.NewTransport(opts) http.RoundTripper`; no `http.DefaultClient` survives in `cmd/argocd-jira-bridge`, `cmd/saas-token-rotator`, or `internal/akvwriter`.

`httpx.Options` declares:

- **Timeout** — per-request, mandatory.
- **Retry policy** — max attempts + exponential backoff with jitter; retry-on covers 5xx + 429 + network errors.
- **Redaction policy** — response bodies are **never** embedded in error strings by default. Callers opt in via `httpx.WithBodyOnError(maxBytes)`, which still passes the body through a configurable redactor. This single switch closes the Q7 credential-leak class for the platform.

Every transport call carries a `context.Context`; the package rejects nil contexts. A context-aware sleep helper replaces the three legacy blocking `time.Sleep(...)` calls during shutdown grace periods.

#### 7.5.2 AKV Secret Writer — `internal/akvwriter` (FR-V4-19..22)

`internal/akvwriter` becomes the **single AKV write path** across the binary set. `cmd/saas-token-rotator/main.go` stops open-coding AKV PUT calls. The writer builds its `http.Client` via `httpx.NewTransport(...)` (FR-V4-20) — no bespoke retry or timeout remains inside the package.

API shape:

```go
err := akvw.Put(ctx, name, value, akvwriter.Overwrite)        // hard-overwrite latest
err := akvw.Put(ctx, name, value, akvwriter.NewVersionOnly)   // never write if name absent
err := akvw.Put(ctx, name, value, akvwriter.RecoverIfSoftDeleted)
```

Typed errors replace the v3-era generic `error` wrapping of HTTP body strings: `ErrSecretNotFound`, `ErrAuthFailed`, `ErrConflict`, `ErrSoftDeletedSecretExists`. An in-memory fake adapter at `akvwriter/fake_test.go` is test-only and backs unit tests for every consumer.

#### 7.5.3 Lifecycle Plumbing — `internal/bootstrap/` + Per-Binary `Runner` Types (FR-V4-23..26)

A new `internal/bootstrap/` package owns process lifecycle:

- `bootstrap.SignalContext() context.Context` — SIGTERM/SIGINT-aware context creation.
- `bootstrap.ServeMetrics(ctx, addr, registry)` — the metrics HTTP server pattern.

`bootstrap` does **not** own flag parsing, config loading, or DI wiring. It is a thin, testable seam.

Run-loops migrate **out of `main.go`** and into the binary's existing `internal/` package as a named `Runner` type:

| Binary | Run-loop home |
|---|---|
| `controller-scaler` | `internal/scaling.Runner.Run(ctx) error` |
| `mgmt-leader-lease` | `internal/bloblease.LeaseRunner.Run(ctx) error` |
| `saas-token-rotator` | `internal/rotation.Runner.Run(ctx) error` (new package) |
| `argocd-jira-bridge` | no runner — lifecycle from `internal/bootstrap` only |

No shared `Runner` interface is introduced (anti-pattern explicitly rejected during grilling); each binary's runner is consumer-defined. Each `main.go` compresses to **≤40 lines**: flag parsing, config load, runner construction, single `runner.Run(ctx)` call.

The blocking `time.Sleep(...)` in `cmd/saas-token-rotator/main.go` (rotation grace period) converts to `select { case <-time.After(...): case <-ctx.Done(): }` so SIGTERM is honored within 1 second.

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

### 8.5 Platform-Repo CI: Reusable Workflows + Composite Action (PRD-v4, FR-V4-10..14)

> Application CI continues on Jenkins (ADR-001-v2). This section covers **platform-repo** CI on GitHub Actions, which v4 deepens.

PRD-v4 collapses the two copy-pasted Terraform workflows from v3 into a **reusable-workflow** layout. The reusable workflows live under a `reusable/` sub-directory so their filenames do not collide with the existing top-level callers; the existing `.github/workflows/terraform-ci.yml` and `.github/workflows/terraform-apply.yml` at the top level are rewritten as **thin callers** that invoke the reusable workflows. No third workflow file is introduced.

```
.github/
├── actions/
│   └── setup-tf/action.yml                       # Composite action: install TF from .tool-versions,
│                                                  #   Azure OIDC login, set TF_IN_AUTOMATION / TF_INPUT=false
└── workflows/
    ├── reusable/
    │   ├── terraform-plan.yml                    # on: workflow_call:
    │   │                                          # permissions: id-token write, contents read, pull-requests write
    │   │                                          # owns: init, fmt-check, validate, tflint, checkov,
    │   │                                          #       plan, JSON-diff render, PR comment (60KB truncation)
    │   └── terraform-apply.yml                   # on: workflow_call:
    │                                              # permissions: id-token write, contents read (NO pull-requests)
    │                                              # owns: init, apply
    ├── terraform-ci.yml                          # PR entrypoint — thin caller of reusable/terraform-plan.yml
    └── terraform-apply.yml                       # main entrypoint — thin caller of reusable/terraform-apply.yml
                                                   #   with the two-phase matrix declared here
```

**Two-phase apply matrix** (FR-V4-11):

- **Phase 1:** `[mgmt-we, mgmt-ne]`
- **Phase 2:** `[dev, staging, prod-we, prod-ne, seed-wus]` with `needs: [phase-1]`

Phase 2 jobs never start until phase 1 succeeds. Each matrix entry retains its dedicated GitHub Environment protection (§18.4) — the seven environments configured by v3 are unchanged.

**Plan-output truncation logic** lives in exactly one file (inside `terraform-plan.yml`), at one threshold (60KB). Output exceeding 60KB is truncated with a link to the full artifact (FR-V4-13).

**Composite action `.github/actions/setup-tf`** owns the shared bootstrap (TF install + Azure OIDC login + TF env vars). Both reusable workflows consume it; no inline duplication.

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

Source (PRD-v3): `tools/service_seed/seed_job.py`.
Source (PRD-v4): split into three modules + thin CLI — see §11.4.

### 11.3 End-to-End Timeline (target p95)

```
Jira ticket created
  → Jenkins seed job (~2 min): repo created, PRs opened
  → PR review and merge (~human-gated)
  → ArgoCD syncs platform-infra-set (~30 min): Crossplane provisions SQL/Cosmos/SB
  → ArgoCD syncs workloads-set (~3 min): workload pods running in dev

Total target: ≤ 45 minutes (infra provision is the long pole)
```

### 11.4 service_seed Three-Module Split + Jinja Templates (PRD-v4, FR-V4-27..35)

PRD-v4 retires the 981-line `seed_job.py` god module in favor of three concerns + a thin CLI, all template-driven:

| Module | Role | Inputs | Outputs |
|---|---|---|---|
| `tools/service_seed/jira_intake.py` | Jira fetch + parse → `ServiceRequest` dataclass | Jira API client, issue key | Validated frozen `@dataclass` (no I/O after fetch) |
| `tools/service_seed/service_template.py` | Render rollout/infra manifests to a working tree | `ServiceRequest`, cluster registry data, Jinja env | Filesystem write only |
| `tools/service_seed/gitops_pr.py` | Compose Bitbucket branch + PR | Working tree, `BitbucketClient` | Pushed branch, opened PR |
| `tools/service_seed/cli.py` | ≤40-line wiring layer | argparse, env-vars, registry path | Wired pipeline run |

Cross-module contract: **`ServiceRequest`** is a frozen `@dataclass` whose `__post_init__` validates SLO class, service name, owner team, and required fields. It is the **only** shared type — no shared mutable state. Cluster registry data (loaded by `cli.py` from `gitops/clusters/registry.yaml`, §3.4) is **passed as data** into `render(...)` and `compose(...)`; neither module re-reads the registry.

`BitbucketClient` lives inside `gitops_pr.py` only — the consumer-defined `RemoteRepo` protocol is mocked at the call site in tests. It is **not** promoted to a shared module until a second consumer exists (explicit rejection of premature abstraction).

#### Jinja Templates + SLO/Rollout Profile YAMLs (FR-V4-32..35)

All YAML emission migrates from Python string templates into Jinja2:

```
tools/service_seed/
├── templates/
│   ├── infra/                       # XRCs (SQL, Cosmos, Service Bus, namespace-binding)
│   ├── workload/base/               # rollout.yaml.j2, service.yaml.j2, ingress.yaml.j2, ...
│   └── workload/overlays/{dev,staging,prod}/
└── profiles/
    ├── slo.yaml                     # gold|silver|bronze → success_rate, p99_ms, probe_interval
    └── rollout.yaml                 # gold|silver|bronze → canary step strategy
```

- Jinja2 runs with **`StrictUndefined`** — missing template variables fail rendering loudly.
- **No SLO numeric literal** (e.g., `0.99`, `500`, `5m`) may remain inside `service_template.py`. Profiles are the source of truth.
- **No YAML literal** may remain inside `service_template.py` — every output file is template-driven.
- CI runs `kubeconform` against rendered output for a `ServiceRequest` fixture of each SLO class. Schema drift fails the PR.

Packaging: `tools/service_seed/pyproject.toml` declares the package and pins dependencies (`jinja2`, `pyyaml`, `cookiecutter`); `pip install -e tools/service_seed` exposes the CLI as a `console_scripts` entry point.

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

| ADR | Decision | Why | Status |
|---|---|---|---|
| ADR-001-v2 | Jenkins: single-replica StatefulSet in `mgmt-we` only | Upstream Jenkins is single-master. CI pauses ~30m during WE outage — acceptable for failure-domain isolation from Atlassian SaaS. | Accepted (v2) |
| ADR-004-v2 | Active-Active reads, Active-Passive writes (Cosmos) | `multipleWriteLocationsEnabled: false`. Multi-master Cosmos requires conflict-aware app design. Auto-failover handles write-region loss automatically. | Accepted (v2) |
| ADR-005-v2 | Per-region AKV pair + per-namespace SecretStore | Single vault = single failure domain. `ClusterSecretStore` = cluster-wide trust. Per-namespace UAMI provides IAM-level isolation per workload. | Accepted (v2) |
| ADR-008-v2 | Cosign + AKV-backed key + Kyverno (not Gatekeeper) | Gatekeeper replaced by Kyverno as sole policy engine. Cosign provides image provenance; Kyverno verifies at admission on every cluster. | Accepted (v2) |
| ADR-013-v2 | Two-tier ApplicationSets (infra + workload) | Infra lifecycle (Crossplane XRCs, 30m provision) and workload lifecycle (K8s manifests, 3m sync) differ fundamentally. Separate retry policies and prune behavior prevent infra churn from blocking workload deploys. | Accepted (v2) |
| ADR-017 | Standby mgmt controllers scaled to zero | Eliminates ARM ownership thrash and double-writes to AKV. Lease arbitration (ADR-022) is the sole authority for which cluster is active. | Accepted (v2) |
| ADR-020 | Per-namespace UAMI isolation (not ABAC prefix conditions) | Azure Key Vault data-plane RBAC does not support attribute-based conditions on secret names. Per-namespace UAMI binding is the maximum isolation Azure supports for Key Vault secret-plane access. | Accepted (v2) |
| ADR-021 | Argo Rollouts mandatory in production | `kyverno-prod-requires-rollout` rejects `Deployment` on prod clusters. AnalysisTemplate auto-generated from SLO class via `NamespaceRolloutPolicy` Composition (`xnamespacerolloutpolicies.platform.cityos.io`). | Accepted (v2) |
| ADR-022 | Azure Storage Blob Lease as singleton lock | Lease TTL=60s, renewal=15s. Geo-replicated GRS storage with private endpoint. Single source of truth preventing split-brain. | Accepted (v2) |
| ADR-023-v3 | Remote Terraform state in Azure Blob with native lease locking | Replaces v2 local-state anti-pattern; per-cluster state keys bound blast radius (§15). | Accepted (PRD-v3) |
| ADR-024-v3 | Scoped RBAC for `akspe` / Velero / `gha-platform-ci` UAMIs | Removes subscription-Owner anti-pattern; CI gate fails any subscription-scoped or `Owner` assignment (§17). | Accepted (PRD-v3) |
| ADR-025-v3 | Pre-commit + GHA quality gates with phased blocking | Single `.checkov.yaml` consumed locally and in CI; phased advisory → blocking ratchet (§18). | Accepted (PRD-v3) |
| ADR-026-v3 | Inline `validation {}` + `tflint-ruleset-azurerm` + custom unvalidated-var rule | Day-1 critical path validates inline; full coverage ratchets to blocking (§19). | Accepted (PRD-v3) |
| ADR-027-v3 | Checkov with inline ticket-referenced expiring suppressions + CODEOWNERS-protected baseline | No silent merges (§20). | Accepted (PRD-v3) |
| ADR-028-v3 | Sonar covers Backstage TS + Dockerfiles only; HCL excluded | Avoids double-coverage with tflint/checkov (§21). | Accepted (PRD-v3) |
| ADR-016-v3-amendment | `cipool` on `mgmt-we` is an allowed DX-plane tooling host; `systempool` and workload clusters are not | Ratifies Jenkins-on-`cipool` reality and gates `var.sonar_hosting = "cipool"` (§22). | Accepted (PRD-v3) |
| **ADR-031-v4** | **Cluster topology lives in a single committed registry (YAML at `gitops/clusters/registry.yaml`)** | **Eliminates three-way encoding across Terraform locals + ArgoCD ApplicationSet locals + `seed_job.py` hardcoded maps. Pre-commit + CI JSON-schema validation. CRD alternative explicitly rejected — registry is human-curated and PR-reviewed. Underpins FR-V4-01..04 (§3.4).** | **Accepted (PRD-v4)** |

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

### 15.5 Workload Identity Terraform Module (PRD-v4, FR-V4-05..09)

PRD-v4 collapses the copy-pasted UAMI + federated-credential + role-assignment pattern (repeated across `jenkins.tf`, `external_secrets.tf`, `akv_sync_exporter.tf`, `saas_token_rotator.tf`, `velero.tf`) into a single reusable module at **`terraform/modules/workload_identity/`** with a **single-identity interface** (no maps inside — multiplicity is HCL `for_each` at the call site).

Inputs:

```hcl
module "workload_identity_jenkins" {
  source = "./modules/workload_identity"

  name                          = "jenkins"
  resource_group                = local.rg_mgmt_we
  location                      = "westeurope"
  kubernetes_namespace          = "jenkins"
  kubernetes_service_account    = "jenkins"
  oidc_issuer_url               = local.mgmt_we.oidc_issuer_url
  role_assignments              = [
    { scope = data.azurerm_key_vault.mgmt_we.id, role_definition_name = "Key Vault Secrets User" },
    { scope = data.azurerm_key_vault.mgmt_we.id, role_definition_name = "Key Vault Crypto User" },
  ]
  tags                          = local.common_tags
  # allow_subscription_scope     = false   # default; module asserts no subscription-scoped role unless explicit
}
```

Module ownership boundary:

- **Owns:** `azurerm_user_assigned_identity`, `azurerm_federated_identity_credential` (with audience/issuer/subject formatted inside), and the role assignments.
- **Does NOT own:** Key Vault access policies, Helm release wiring, secret material. Wiring stays at the call site so the module is reusable across charts.

The duplicated Key Vault role-assignment loops in `keyvaults.tf:64–78` and `jenkins.tf:64–68` collapse into a single loop driven by the module's outputs (FR-V4-08).

`terraform test` blocks at `terraform/modules/workload_identity/tests/` assert:

- Federated-credential subject equals `system:serviceaccount:<ns>:<sa>`.
- Role assignments live at the requested scope only.
- **No role assignment at subscription scope** unless `allow_subscription_scope = true` (default `false`). This guard reinforces ADR-024-v3 at module level.

Success metric: PR diff for adding a new workload identity is ≥60% smaller than the equivalent v3-era PR.

### 15.6 Robustness Pass (PRD-v4, FR-V4-41..44)

PRD-v4 closes four robustness gaps surfaced by the architecture review:

| Gap | Closing requirement | Resource(s) |
|---|---|---|
| Accidental destruction of stateful storage | `lifecycle { prevent_destroy = true }` blocks | `terraform/storage.tf` (mgmt-plane lease blob container), `terraform/velero.tf` (Velero backup container). Removal requires a deliberate two-PR sequence: lift the lifecycle, then destroy. |
| Cross-variable invariants asserted as advisory | Convert `check{}` advisories to `precondition{}` blocks on the consuming resources | Day-1 invariants: spoke CIDR non-overlap with hub CIDR (`networking.tf`); `region_abbrev` consistency with `region` per registry (§3.4); Bitbucket CIDR list provenance comment + last-verified date in `variables.tf`. |
| Python hangs on network/subprocess calls | Explicit timeouts everywhere | `urlopen(..., timeout=30)` for HTTP; `subprocess.run(..., timeout=300)` for git. Identical defaults across `jira_intake.py`, `gitops_pr.py`, `service_template.py`. Token material never appears in git remote URLs (use credential helpers or `.netrc`). |
| Cookiecutter path traversal | Resolved-path containment assertion | `service_template.py` asserts `target.resolve().is_relative_to(destination.resolve())` before writing each file. |

These changes apply to v4-touched surfaces only; legacy untouched code is grandfathered.

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

---

## 23. CI Hygiene (PRD-v4)

> Codifies PRD-v4 FR-V4-45..49 — `QW-CI` items Q3, Q4, Q5, Q10.

### 23.1 Single Source of Truth for Tool Versions (`.tool-versions`)

A single `.tool-versions` file at the repository root declares versions for `terraform`, `tflint`, `checkov`, `terragrunt`, `kubectl`, `kustomize`. CI workflows source versions from this file via `asdf-vm/actions/install@<sha>` (or the `mise` equivalent). Pre-commit hooks read the same file via `additional_dependencies` templating or a small bootstrap script.

**Exactly one** file in the repo declares those tool versions — drift is impossible by construction.

Contributors without `asdf`/`mise` use `scripts/install-tools.sh` (R-V4-7 mitigation), which reads `.tool-versions` and installs via `tfenv` / direct download.

### 23.2 SHA-Pinned GitHub Actions (Dependabot-Managed)

All `uses:` references in `.github/workflows/*.yml` and `.github/actions/*/action.yml` pin to a **40-character commit SHA** with the human-readable tag in a comment:

```yaml
- uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11  # v4.1.1
```

Dependabot opens a weekly PR upgrading the pinned SHAs together with comment tags. PRs are auto-mergeable subject to standard review.

### 23.3 Checkov Baseline Per-Finding Metadata Schema

`.checkov.yaml` and `.checkov.baseline` enforce per-finding metadata: each baseline entry carries `rationale` (non-empty string), `owner` (GitHub team or @-handle), and `expiry` (ISO date ≤ 180 days from entry creation). Findings missing any field, or whose `expiry` lapses, fail the CI Checkov job with a pointer to the offending entry.

CODEOWNERS protection on `.checkov.baseline` (v3, §20.4) is retained.

### 23.4 Pre-commit + CI Scope Parity

Pre-commit Checkov scope **matches** CI Checkov scope. Both scan `terraform/`, `backstage/**/Dockerfile*`, and any hand-authored Kubernetes manifest under `gitops/` (templates excluded). The pre-commit tflint hook passes `--config=.tflint.hcl` explicitly — identical to CI.

A CI job runs `pre-commit run --all-files` after a clean CI run and asserts a zero diff. Drift between pre-commit and CI is a regression-tested failure mode (FR-V4-49).

---

## 24. PRD-v4 Mapping (Quick Reference)

> Quick index for engineers and agents reading this guide alongside PRD-v4.

| PRD-v4 area | FR range | User story | Guide section |
|---|---|---|---|
| Cluster topology registry | FR-V4-01..04 | US-V4-01 | §3.4 |
| Workload-identity TF module | FR-V4-05..09 | US-V4-02 | §15.5 |
| Reusable TF workflows + composite action | FR-V4-10..14 | US-V4-03 | §8.5 |
| Go HTTP transport seam | FR-V4-15..18 | US-V4-04 | §7.5.1 |
| AKV writer consolidation | FR-V4-19..22 | US-V4-05 | §7.5.2 |
| Go binary lifecycle + per-binary runners | FR-V4-23..26 | US-V4-06 | §7.5.3 |
| service_seed three-module split | FR-V4-27..31 | US-V4-07 | §11.4 |
| Jinja templates + SLO/rollout profiles | FR-V4-32..35 | US-V4-08 | §11.4 |
| Hybrid Helm → ESO secret migration | FR-V4-36..40 | US-V4-09 | §6.5 |
| Robustness pass | FR-V4-41..44 | US-V4-10 | §15.6 |
| CI hygiene | FR-V4-45..49 | US-V4-11 | §23 |
| Cluster topology ADR | n/a | n/a | ADR-031-v4 (§14) |

**prd.json priority → PRD-v4 phase mapping**

| prd.json priority | PRD-v4 phase | Stories |
|---|---|---|
| 1 | P0 — Foundation | US-V4-11 |
| 2 | P1 — Registry + workflows | US-V4-01, US-V4-03 |
| 3 | P2 — Self-contained TF + Robustness | US-V4-02, US-V4-10 |
| 4 | P3 — Go deepening | US-V4-04, US-V4-05, US-V4-06 |
| 5 | P4 — Python deepening | US-V4-07, US-V4-08 |
| 6 | P5 — Secrets migration | US-V4-09 |