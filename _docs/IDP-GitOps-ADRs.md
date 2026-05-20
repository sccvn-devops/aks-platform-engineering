# Architecture Decision Records — IDP GitOps Blueprint

**Companion document to:** `IDP-GitOps-Blueprint-v2.md`
**Programme:** Internal Developer Platform on Azure AKS (GitOps + Platform Engineering)
**Owner:** Principal Enterprise Architect, Platform Engineering
**Last updated:** 2026-05-19

This document captures every load-bearing architectural decision made during the design of the IDP GitOps blueprint. Each ADR is self-contained and follows the same template (Context → Decision → Options → Trade-offs → Consequences → Action Items). When two ADRs interact, the relationship is called out under "Related ADRs" so reviewers can trace dependencies.

## Index

| # | Title | Status |
|---|---|---|
| ADR-001 | CI Engine — Jenkins over Bamboo | Superseded by ADR-001-v2 |
| ADR-002 | Cloud Control Plane — Crossplane over Terraform/CAPZ | Accepted |
| ADR-003 | Cluster Topology — Dedicated Management Cluster + Per-Environment Workload Clusters | Superseded by ADR-003-v2 |
| ADR-004 | Multi-Region Strategy — Active-Passive Management, Active-Active Data Plane | Superseded by ADR-004-v2 |
| ADR-005 | Secret Management — External Secrets Operator + Azure Key Vault | Superseded by ADR-005-v2 |
| ADR-006 | Identity Model — Azure Workload Identity Federation for All Cluster ↔ Azure Calls | Superseded by ADR-006-v2 |
| ADR-007 | GitOps Engine — ArgoCD with ApplicationSet (Matrix Generators) | Accepted |
| ADR-008 | Supply Chain — Cosign Image Signing with AKV-Backed Key + Gatekeeper Enforcement | Superseded by ADR-008-v2 |
| ADR-009 | Network Posture — Private AKS + Private Endpoints + Hub-Spoke VNets | Accepted |
| ADR-010 | Race-Free GitOps Update — Idempotent kustomize edit + Rebase Loop + Author Filter | Superseded by ADR-010-v2 |
| ADR-011 | Source of Truth — Monorepo `platform-gitops` for All Cluster State | Accepted |
| ADR-012 | Drift & Rollback Visibility — ArgoCD Notifications → Jira Bridge | Accepted |
| ADR-013 | Sync Ordering — Sync Waves with Custom `health.lua` for Crossplane XRs | Superseded by ADR-013-v2 |
| ADR-014 | CI Agents — Ephemeral Pods in a Dedicated AKS Node Pool | Accepted |
| ADR-015 | Container Build Tool — Buildah (Rootless) over Docker-in-Docker | Accepted |

---

# ADR-001: CI Engine — Jenkins over Bamboo

**Status:** Superseded by ADR-001-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, Security Lead, Head of Engineering

## Context

The blueprint requires a CI engine that (a) integrates cleanly with Bitbucket webhooks and Jira automation, (b) runs ephemeral agents inside AKS, (c) can update the GitOps repository safely, and (d) does **not** share a failure domain with Atlassian SaaS (Jira/Bitbucket). The two candidates explicitly named by the programme are Jenkins (OSS) and Bamboo (Atlassian).

A key non-functional constraint: the platform's #1 design goal is failure-domain isolation. An outage of one component must not freeze every other component. Since the issue tracker and the version control system are *already* Atlassian SaaS, adding Bamboo would put three out of three "developer experience" components on the same vendor's status page.

## Decision

Select **Jenkins LTS** as the CI engine. Run it as a 2-replica StatefulSet inside the management AKS cluster with Azure Files as the shared `JENKINS_HOME` PVC. Use the `kubernetes-plugin` to spawn per-job ephemeral pods in a dedicated `cipool` node pool.

## Options Considered

### Option A: Jenkins LTS (OSS)

| Dimension | Assessment |
|---|---|
| Complexity | Medium — well-trodden patterns, large community |
| Cost | Low — free; pay only for compute |
| Scalability | High — kubernetes-plugin scales to hundreds of concurrent builds |
| Failure-domain isolation | **Excellent** — independent of Atlassian SaaS |
| Team familiarity | High |
| HA story | Active-passive with shared PVC + leader election; documented and stable |

**Pros**
- Decouples CI from the Atlassian failure domain (the decisive factor)
- Mature `kubernetes-plugin` for ephemeral agents
- Jenkinsfile-as-code is widely understood; portable to other orgs
- Free; no per-agent licensing

**Cons**
- Plugin sprawl can become a maintenance burden if not curated
- True active-active is not supported (we mitigate with leader election)
- Older UI compared to modern CI services

### Option B: Bamboo Data Center

| Dimension | Assessment |
|---|---|
| Complexity | Medium — but Atlassian-flavoured Java specs are less ergonomic |
| Cost | High — per-agent licensing |
| Scalability | Medium — Remote Agents in pods work but feel bolted on |
| Failure-domain isolation | **Poor** — same vendor as Jira + Bitbucket SaaS |
| Team familiarity | Medium |
| HA story | Bamboo Data Center provides active-passive; smaller install base |

**Pros**
- Native integration with Jira (Bamboo issue panel, deployment views in Jira)
- First-party Bitbucket webhooks
- Less plugin maintenance burden

**Cons**
- **Couples CI failure domain to Jira/Bitbucket Cloud** — single Atlassian incident impairs all three
- Per-agent licensing scales costs unpredictably
- Smaller plugin ecosystem for Crossplane / Cosign / Trivy / ACR (requires more shell-out)
- Less GitOps-idiomatic pipeline definition

### Option C (rejected without deep evaluation): GitHub Actions, Tekton, Azure DevOps Pipelines, Drone

These were excluded by the programme's stack constraint (Jenkins or Bamboo only). Recorded here so the next review knows we did not silently broaden the search.

## Trade-off Analysis

The decisive factor is failure-domain isolation. Bamboo's deep coupling to Atlassian's product family means a single Atlassian Cloud incident would impair Jira, Bitbucket Cloud webhooks, and Bamboo simultaneously — taking out the entire DX plane. Jenkins running inside our own AKS cluster is reachable, controllable, and recoverable through the same DR procedures we already maintain for the management cluster.

We pay for this independence with a small operational tax (plugin curation, leader-election HA instead of active-active). That tax is acceptable because the alternative — three-of-three vendor concentration — would violate the programme's primary design goal.

## Consequences

**Easier**
- Bitbucket / Jira can suffer SaaS outages without freezing the CI ability to release security patches via mirrored repos
- Pipelines are portable Jenkinsfiles in the app repos
- Cost scales with compute, not licenses
- Existing Cosign/Trivy/Crossplane CLIs work out of the box

**Harder**
- Jenkins upgrade cadence and plugin compatibility require active stewardship (monthly maintenance window)
- Active-active scale-out is not available; large bursts route through one active replica
- The team owns more Jenkins operational knowledge than a SaaS would require

**Will need to revisit if**
- Atlassian releases a Bamboo Cloud SKU with strong cross-region isolation
- Jenkins maintenance burden grows beyond two engineer-days per month
- The team adopts a different VCS that makes another CI tool dominant

## Action Items
1. [ ] Provision the `cipool` AKS node pool with taint `workload=ci:NoSchedule`
2. [ ] Install Jenkins HA chart with Azure Files RWX PVC for `JENKINS_HOME`
3. [ ] Curate the plugin allow-list and pin versions in IaC
4. [ ] Document the monthly Jenkins maintenance runbook
5. [ ] Build the platform-seed job (scaffolds Bitbucket repos + ApplicationSet entries)

## Related ADRs
- ADR-014 (Ephemeral Pod Agents) builds on this decision
- ADR-010 (Race-Free GitOps Update) defines what the Jenkins pipeline must do
- ADR-015 (Buildah over Docker) describes how Jenkins builds images

---

# ADR-002: Cloud Control Plane — Crossplane over Terraform/CAPZ

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, Cloud Foundations Lead

## Context

The platform must let development teams self-serve cloud resources (SQL, Cosmos, Service Bus, DNS) through declarative claims, not tickets or imperative scripts. The Azure-Samples `aks-platform-engineering` reference allows a choice between Crossplane and CAPZ; Terraform is also a longstanding candidate.

The chosen tool will define how application teams interact with the platform every day. It must support:

- Declarative composition of multi-resource bundles (an XRC creates SQL + private endpoint + AKV secret in one shot).
- Drift correction without manual intervention.
- Kubernetes-native primitives (CRDs, status conditions, RBAC).
- Integration with ArgoCD's reconciliation model.

## Decision

Adopt **Crossplane v1.16+** with `provider-upjet-azure` as the sole cloud control plane. Terraform is retained only for bootstrapping the first management cluster and is then retired.

## Options Considered

### Option A: Crossplane (Upjet-Azure)

| Dimension | Assessment |
|---|---|
| Self-service abstraction | **Excellent** — XRDs + Compositions hide all Azure detail |
| Drift correction | Continuous (poll every 1-2 min) |
| K8s integration | Native — managed resources are CRDs |
| Multi-resource bundling | First-class via Compositions |
| Ecosystem maturity | Good — Upjet auto-generates from Terraform providers |
| GitOps fit | **Excellent** — pure manifests, ArgoCD-friendly |

**Pros**
- True abstraction layer: dev teams write `kind: SQLDatabase` not 60 lines of Terraform
- Continuous drift correction is built-in, no `terraform plan` cron job needed
- Resources are CRDs with status conditions — ArgoCD can wait on them via custom health checks
- Composition Functions (v1.14+) enable advanced templating in YAML

**Cons**
- Upjet providers can lag the underlying Terraform provider by weeks
- Reconciliation can hit ARM rate limits at scale (mitigated by per-region ProviderConfig sharding)
- Operational knowledge less common than Terraform; team must invest

### Option B: Terraform (run by Atlantis / Spacelift / CI)

| Dimension | Assessment |
|---|---|
| Self-service abstraction | Possible via modules, but no CRD-native UX |
| Drift correction | Periodic only (cron `terraform plan` + alert) |
| K8s integration | None (apply happens outside the cluster) |
| Multi-resource bundling | First-class (modules) |
| Ecosystem maturity | Excellent |
| GitOps fit | Awkward — state lives in a backend, not Git |

**Pros**
- Largest ecosystem and largest team familiarity
- Most stable Azure provider coverage
- Mature SaaS workflow engines (Atlassian, Spacelift, Env0)

**Cons**
- State stored in a remote backend creates a second source of truth alongside Git
- Drift detection is a separate, brittle pipeline; no automatic correction
- Developer-facing self-service requires a wrapper (Backstage Scaffolder, Atlantis) — more moving parts
- Cluster-side cannot easily wait on Terraform-managed resources

### Option C: CAPZ (Cluster API Provider for Azure)

| Dimension | Assessment |
|---|---|
| Self-service abstraction | Cluster-only — does not manage PaaS resources |
| Drift correction | Cluster lifecycle only |
| K8s integration | Native |
| Multi-resource bundling | Not its purpose |
| GitOps fit | Good for clusters |

**Pros**
- Best-in-class for managing AKS cluster lifecycles
- Reference architecture supports it natively
- Smaller surface area than Crossplane

**Cons**
- Does not manage Azure PaaS (SQL, Cosmos, Service Bus, DNS) — this is the bulk of our self-service surface
- Would require running CAPZ *and* Crossplane (or Terraform) in parallel

## Trade-off Analysis

The platform's most important UX promise is "write a YAML claim, get a working environment." Crossplane's CRD-native abstraction model maps directly to that promise; Terraform requires wrapping every consumer surface in a templating engine, and CAPZ does not address PaaS at all.

We accept Crossplane's lag-behind-Terraform-provider risk because the mitigation is well understood: the Upjet generator can be run locally to patch a missing field, and serious gaps can fall back to a small `ProviderRevision` override.

ARM rate limits at scale are mitigated by sharding ProviderConfigs across regions/subscriptions (described in §4.6 of the blueprint), and Crossplane's exponential backoff handles 429s automatically.

## Consequences

**Easier**
- Dev teams write small, readable claims; XRDs evolve independently of consumers
- Drift correction is automatic and continuous
- ArgoCD sync waves can wait on cloud provisioning (see ADR-013)
- No separate state backend to secure, back up, or unlock

**Harder**
- Team must learn Crossplane composition functions and patches
- Provider upgrades must be staged carefully (Upjet generation breaks across major versions)
- ARM rate-limit observability is now a platform-team concern

**Will need to revisit if**
- Crossplane Upjet maintenance lapses or Azure ASO becomes more mature
- The platform's PaaS catalog shrinks dramatically (then CAPZ alone might suffice)

## Action Items
1. [ ] Install Crossplane v1.16+ in the management cluster
2. [ ] Install `provider-upjet-azure` with sharded ProviderConfigs per region
3. [ ] Author the four production XRDs: SQL, Cosmos, Service Bus, DNS (per §4 of the blueprint)
4. [ ] Document the XRD versioning policy (semver, deprecation windows)
5. [ ] Establish a "Crossplane on-call" rotation for ARM-rate-limit and provider-upgrade events

## Related ADRs
- ADR-005 (Secret Management) depends on Crossplane to push connection strings to AKV
- ADR-013 (Sync Waves) depends on Crossplane status conditions

---

# ADR-003: Cluster Topology — Dedicated Management Cluster + Per-Environment Workload Clusters

**Status:** Superseded by ADR-003-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, SRE Lead

## Context

The blueprint must isolate failure domains. A failure in CI, the GitOps engine, or a single team's misconfigured workload must not damage other tenants or environments. Cluster sizing also drives cost, blast radius, and admin complexity.

## Decision

Adopt a **three-tier cluster topology**:

1. One dedicated **management cluster** per region (`mgmt-we`, `mgmt-ne`) running only platform components (Crossplane, ArgoCD hub, ESO controller-of-controllers, Jenkins, Velero, observability). Workloads are forbidden via Gatekeeper.
2. **Per-environment workload clusters** (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`). Each runs its own ESO agent and ArgoCD agent destinations.
3. A **seed cluster** (single-node, third region) maintained for DR-rebuild.

## Options Considered

### Option A: Three-tier (Mgmt + Per-Env Workload + Seed) — chosen

**Pros**
- Strong blast-radius isolation per env (a Dev incident cannot touch Prod)
- Mgmt cluster outage does not stop running workloads
- DR is simple: seed cluster can bootstrap a new mgmt cluster from Git

**Cons**
- Higher base cost (4+ AKS clusters)
- More API endpoints to keep private and authorize

### Option B: Single Multi-Tenant Cluster

**Pros**
- Cheapest
- Simplest networking

**Cons**
- One control plane is a single point of failure for the entire estate
- Noisy-neighbour risk between Dev and Prod workloads
- Compliance segregation (e.g., PCI) becomes painful

### Option C: One Cluster Per Team (no env separation)

**Pros**
- Strong team isolation
- Cost predictable per team

**Cons**
- Inconsistent environments per team — promotion semantics break down
- Explodes cluster count linearly with org size

## Trade-off Analysis

Cost is real but predictable; failure isolation is what the programme is selling. We accept ~4× the baseline AKS cost vs. a single cluster in exchange for a topology where every plane can fail independently and a DR runbook can rebuild any layer from Git.

## Consequences

**Easier**
- Per-env policies and RBAC are clean (no "this cluster is also dev sometimes")
- DR runbook (§7.3 of blueprint) maps 1:1 to clusters
- Cost reporting is per-cluster, which maps to per-environment cost centres

**Harder**
- Four clusters to upgrade in sequence each AKS minor release
- Workload Identity federations and OIDC URLs are per-cluster artefacts in Git
- Operators must always specify `--context` — break-glass procedures must be explicit about which cluster

**Will need to revisit if**
- AKS adds first-class multi-cluster fleet management that materially reduces the upgrade tax
- Cost pressure forces sharing dev+staging on one cluster (acceptable; prod stays alone)

## Action Items
1. [ ] Provision `mgmt-we`, `mgmt-ne`, `aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne` via Crossplane control-plane-claim
2. [ ] Provision seed cluster (manual; documented runbook for re-creation)
3. [ ] Apply Gatekeeper `no-workloads-on-mgmt` policy
4. [ ] Document the cluster-upgrade rotation order: seed → mgmt-ne → mgmt-we → dev → staging → prod-ne → prod-we

## Related ADRs
- ADR-004 (Multi-Region Strategy) builds on this topology
- ADR-007 (ArgoCD Hub) places the hub in the management cluster

---

# ADR-004: Multi-Region Strategy — Active-Passive Management, Active-Active Data Plane

**Status:** Superseded by ADR-004-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, SRE Lead, Business Continuity Lead

## Context

The platform must survive the loss of a single Azure region. Two planes have different needs:

- **Data plane:** user-facing latency-sensitive workloads should fail over fast and ideally serve traffic from both regions during normal operation.
- **Management plane:** drift correction can pause briefly during a region loss without customer impact; what matters is fast recovery, not zero-downtime active-active.

## Decision

Run the **data plane active-active** across West Europe and North Europe with Azure Front Door routing. Run the **management plane active-passive** — `mgmt-we` is primary, `mgmt-ne` is warm-standby with read-only Crossplane and 0-replica ArgoCD controllers ready to scale up.

## Options Considered

### Option A: Both planes active-active

**Pros**
- Lowest RTO for everything
- No promotion step in DR

**Cons**
- Two active ArgoCD hubs reconciling against the same workload clusters create write conflicts
- Crossplane reconciling against the same Azure resources causes ARM contention and race conditions
- Operationally complex: which mgmt cluster owns which XR?

### Option B: Data plane active-active, mgmt plane active-passive — chosen

**Pros**
- Clean ownership: mgmt-we owns reconciliation; mgmt-ne is a recovery target
- Workloads benefit from active-active (low latency, smooth failover)
- DR runbook is short (§7.3): scale up mgmt-ne and re-register clusters

**Cons**
- Brief drift-correction pause during region loss
- Requires Velero to keep mgmt-ne's etcd in sync

### Option C: Single-region everywhere

**Pros**
- Cheapest

**Cons**
- Region outage = total outage
- Unacceptable for any prod SLO above bronze

## Trade-off Analysis

Active-active for stateful platform components is famously hard; we don't get enough operational benefit to justify the complexity. Active-active for stateless workloads is straightforward and we already pay for the second region anyway, so we use it.

## Consequences

**Easier**
- DR scenarios are well-defined and rehearsed
- Data-plane failover does not require operator action (Front Door + Cosmos auto-failover handle it)
- Cost is predictable

**Harder**
- We must keep mgmt-ne current with Velero (6-hour snapshots)
- We must test mgmt-ne promotion at least twice a year (DR game day)

**Will need to revisit if**
- Workload SLOs demand sub-second control-plane recovery (then we need active-active mgmt)
- Azure adds region-pair-aware ArgoCD primitives

## Action Items
1. [ ] Configure Front Door with health-probed origins in WE and NE
2. [ ] Configure Cosmos DB auto-failover (priority 0 = WE, priority 1 = NE)
3. [ ] Stand up Velero schedules for `mgmt-we` etcd + PVCs
4. [ ] Author the `argocd-ha-standby` Helm values for mgmt-ne (0-replica controllers)
5. [ ] Schedule semi-annual DR game day

## Related ADRs
- ADR-003 (Cluster Topology) — provides the clusters this strategy operates on
- ADR-007 (ArgoCD ApplicationSet) — uses cluster labels to discover destinations
- ADR-011 (Source of Truth in Git) — makes mgmt rebuild trivial

---

# ADR-005: Secret Management — External Secrets Operator + Azure Key Vault

**Status:** Superseded by ADR-005-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Platform Lead

## Context

Connection strings for the cloud resources Crossplane provisions (SQL, Cosmos, Service Bus) must reach application pods without ever appearing in Git, in a CI pipeline log, or in a long-lived Kubernetes Secret with static credentials. Rotation must be automatic and observable.

## Decision

Adopt **External Secrets Operator (ESO) v0.10+** in every workload cluster. ESO pulls from a **shared Azure Key Vault** populated by Crossplane Compositions, and projects native Kubernetes Secrets into app namespaces. Rotation is end-to-end automatic via **Stakater Reloader**.

## Options Considered

### Option A: ESO + AKV (chosen)

**Pros**
- ESO authenticates via Workload Identity — no static creds
- Crossplane's `keyvault.azure.upbound.io/v1beta1 Secret` pushes connection JSON automatically
- Standard `Secret` consumption in pods — apps need not know AKV exists
- Mature ecosystem; community-maintained refresh templating

**Cons**
- One more controller per cluster to monitor
- AKV public access must be carefully closed (private endpoints)

### Option B: AKV CSI Driver (Microsoft)

**Pros**
- First-party Microsoft
- Mounts directly as files; no `Secret` object intermediate

**Cons**
- File-mount semantics are awkward for env-var-driven apps
- Sync to a `Secret` object exists but is an optional add-on
- Less granular reload semantics

### Option C: HashiCorp Vault + Vault Agent Injector

**Pros**
- Industry standard; multi-cloud
- Rich policy engine

**Cons**
- Another HA stateful service to operate
- Token-renewal complexity
- We have no other reason to run Vault — AKV is already required

### Option D: SealedSecrets / SOPS in Git

**Pros**
- All state in Git

**Cons**
- Rotation requires a Git commit and a CI pass — not automatic
- Doesn't fit the "Crossplane pushes connection strings" model

## Trade-off Analysis

ESO + AKV is the only option that gives us both **automatic rotation** (Crossplane writes → ESO polls → Reloader restarts) and **zero static credentials** (Workload Identity to AKV). The cost is one operator to manage; we already manage many.

## Consequences

**Easier**
- App teams consume secrets as `envFrom: secretRef:` — no AKV awareness
- Rotation MTTR ~90 seconds end-to-end
- One audit point for cloud secrets (AKV access logs)

**Harder**
- ESO failure means stale secrets — observability is critical
- AKV RBAC must be precise (per-cluster UAMI scoped to a single vault)

**Will need to revisit if**
- AKV CSI driver's `Secret`-sync becomes the platform default
- Compliance demands a Vault-style policy engine

## Action Items
1. [ ] Install ESO in each workload cluster via ArgoCD ApplicationSet
2. [ ] Provision per-cluster UAMI with `Key Vault Secrets User` RBAC
3. [ ] Author the `ClusterSecretStore` resource per cluster
4. [ ] Define the ExternalSecret naming convention (`<service>-<resource>-conn`)
5. [ ] Install Stakater Reloader and document the `reloader.stakater.com/match` annotation requirement

## Related ADRs
- ADR-006 (Workload Identity) — provides the auth mechanism
- ADR-002 (Crossplane) — provides the secret producers
- ADR-013 (Sync Waves) — ensures ESO syncs *after* Crossplane

---

# ADR-006: Identity Model — Azure Workload Identity Federation for All Cluster ↔ Azure Calls

**Status:** Superseded by ADR-006-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Identity Lead

## Context

Every cluster-resident component (Crossplane, ESO, Jenkins agents, app pods) needs to authenticate to Azure (ARM, AKV, ACR, SQL via Entra). Historically this has been done with service-principal client secrets stored as Kubernetes Secrets — long-lived, exfiltratable, hard to rotate. The platform's zero-trust posture requires elimination of static credentials.

## Decision

Use **Azure AD Workload Identity Federation** universally. Every cluster's OIDC issuer is federated to per-purpose User-Assigned Managed Identities (UAMIs). Pods bind to a Kubernetes ServiceAccount annotated `azure.workload.identity/use: "true"`; the workload identity webhook injects a projected, short-lived token. Azure trusts the federation and issues an MSAL access token. **No client secrets exist anywhere.**

## Options Considered

### Option A: Workload Identity Federation (chosen)
**Pros**
- Zero static credentials
- Short-lived (~1 hour) AAD tokens, refreshed automatically
- Per-SA granularity; fine-grained RBAC
- First-party Azure feature; supported in AKS by default

**Cons**
- Initial setup requires correctly issuing FIC objects per SA
- OIDC URL is per-cluster and must be tracked in Git for DR

### Option B: AAD Pod Identity (deprecated)
**Cons**
- Deprecated by Microsoft; do not use

### Option C: Static Service-Principal Secrets
**Cons**
- Long-lived; exfiltration risk
- Manual rotation; no easy audit
- Fails the zero-trust posture

### Option D: AKS Managed Identity assigned to the cluster's kubelet
**Cons**
- One identity shared by every pod on the node — no granularity
- Wrong granularity for multi-tenant workloads

## Trade-off Analysis

Workload Identity is unambiguously the right answer for any new AKS deployment. The only real trade-off is the slight up-front complexity of FIC objects — addressed by automating their creation via Crossplane `FederatedIdentityCredential` XRs.

## Consequences

**Easier**
- No secret rotation procedures for service principals
- AAD audit logs show every token issuance with the SA subject
- DR is simpler — the federation references OIDC URLs that survive cluster destruction

**Harder**
- Cluster OIDC URL must be captured in Git before destructive DR
- AKS API server has additional latency for the first call after a restart while the webhook injects the token

**Will need to revisit if**
- Microsoft launches a successor identity primitive (unlikely soon)
- Compliance requires HSM-backed cluster-resident keys

## Action Items
1. [ ] Enable Workload Identity on every AKS cluster
2. [ ] Crossplane-manage UAMIs and FIC objects
3. [ ] Document the per-cluster OIDC URL list in `bootstrap/clusters/`
4. [ ] Deny `clientSecret` field in Crossplane ProviderConfig via OPA
5. [ ] Rotate any existing service-principal secrets out of the estate before go-live

## Related ADRs
- ADR-005 (ESO) — uses Workload Identity to reach AKV
- ADR-002 (Crossplane) — uses Workload Identity to reach Azure ARM

---

# ADR-007: GitOps Engine — ArgoCD with ApplicationSet (Matrix Generators)

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

The platform must reconcile manifests from Git into N workload clusters across M environments without operators hand-editing per-cluster `Application` objects. The CD tool must support sync ordering (so Crossplane resources finish before workloads start) and provide self-healing.

## Decision

Adopt **ArgoCD v2.11+ in HA mode** running in the management cluster as the hub. Use **ApplicationSet** with **Matrix generators** (cluster generator × Git directory generator) to produce one `Application` per (service × cluster) pair. Use **sync waves** for ordering and **selfHeal + retry** for drift correction.

## Options Considered

### Option A: ArgoCD (chosen)
**Pros**
- ApplicationSet handles multi-cluster fan-out cleanly
- Sync waves give us cross-operator ordering (Crossplane → ESO → Workload)
- Custom `health.lua` lets ArgoCD wait on Crossplane XR readiness
- Mature notifications system for the Jira bridge
- Large community; first-party Azure docs

**Cons**
- Hub model puts mgmt cluster on the critical path for reconciliation (mitigated by ADR-004)
- ApplicationSet templating with Go-templates has some sharp edges

### Option B: Flux v2
**Pros**
- Excellent OCI-as-source support
- Lightweight controllers (no UI by default)
- GitOps Toolkit composability

**Cons**
- No equivalent of ApplicationSet Matrix generators with cluster labels (workarounds exist but are clunkier)
- Notifications less mature than ArgoCD's
- Smaller community for our specific multi-cluster pattern

### Option C: ArgoCD + Argo Rollouts (we will adopt Argo Rollouts anyway)
This is not really an alternative — Argo Rollouts is *complementary*. Captured here so reviewers see we considered the progressive-delivery dimension.

## Trade-off Analysis

ArgoCD's ApplicationSet Matrix is the cleanest path to "one definition produces N targeted Applications across labeled clusters." Flux can do it but requires more glue. The UI is also a meaningful operator-experience win during incidents.

## Consequences

**Easier**
- New clusters auto-pick-up all apps via cluster label matching
- New apps auto-deploy to all matching clusters via directory globbing
- Custom health checks make cross-operator dependencies explicit

**Harder**
- Hub model means mgmt cluster scale matters (sharded controllers across 3 replicas)
- ApplicationSet template debugging requires careful logging

**Will need to revisit if**
- Flux gains feature parity on Matrix generators
- Argo project changes hub architecture significantly

## Action Items
1. [ ] Install ArgoCD HA chart with 3 controllers, 2 servers, 2 repo-servers, redis-ha
2. [ ] Configure cluster generator with labels `env: dev|staging|prod`
3. [ ] Author the ApplicationSet for workloads (per §6.2 of blueprint)
4. [ ] Register custom `health.lua` for Crossplane XR kinds (per §6.3)
5. [ ] Configure notifications → `argocd-jira-bridge`

## Related ADRs
- ADR-013 (Sync Waves) — uses ArgoCD's `argocd.argoproj.io/sync-wave` annotation
- ADR-012 (Drift Bridge) — uses ArgoCD's notifications subsystem

---

# ADR-008: Supply Chain — Cosign Image Signing with AKV-Backed Key + Gatekeeper Enforcement

**Status:** Superseded by ADR-008-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Platform Lead

## Context

The platform must guarantee that every container image admitted to a workload cluster was built by the platform's CI pipeline (i.e., scanned, signed, and traceable to a Bitbucket commit). The threat model includes supply-chain attacks (malicious images pulled from public registries) and insider misuse (operator pushing unscanned images to ACR).

## Decision

Sign every image at CI time with **Cosign** using a key stored in **Azure Key Vault** (KMS-backed). Attach an SBOM (SPDX) attestation. Enforce signature verification at admission in every workload cluster via **OPA/Gatekeeper** with a `K8sRequiredCosignSignature` constraint. Unsigned or invalidly signed images are rejected at the API server.

## Options Considered

### Option A: Cosign (KMS-backed) + Gatekeeper (chosen)
**Pros**
- KMS-backed keys never leave AKV; signing identity auditable
- Gatekeeper is already deployed for other policies; one more constraint
- Sigstore standard; cosign-attest covers SBOM and provenance
- ACR has first-class Cosign integration

**Cons**
- Cosign verification adds ~200ms to first pod start (admission webhook latency)
- Key rotation requires careful coordination (re-sign all live tags)

### Option B: Notary v2 (TUF-based)
**Pros**
- OCI-native and well-specified

**Cons**
- Smaller community than Sigstore
- Less seamless tooling around SBOM/attestation
- Microsoft's own tooling leans Cosign

### Option C: No image signing (rely on private ACR + RBAC)
**Cons**
- Does not defend against insider push
- Fails compliance for many regulated workloads

## Trade-off Analysis

The admission-latency cost is real but small. The compliance and supply-chain integrity benefit is large. Sigstore/Cosign is the de-facto OSS standard and integrates with our existing AKV-as-KMS posture.

## Consequences

**Easier**
- Provenance trail per image (cosign verify shows the signing identity, time, attestations)
- SBOM available cluster-side for vulnerability triage
- Compliance evidence for SLSA-3-style attestations

**Harder**
- Pipeline must always sign — a Cosign step failure blocks deploy
- Key rotation requires re-signing all images currently in use

**Will need to revisit if**
- Sigstore landscape shifts materially (e.g., Notary v2 wins)
- Performance becomes an issue at very high pod-churn rates

## Action Items
1. [ ] Provision Cosign signing key in AKV (HSM-backed if compliance demands)
2. [ ] Add `cosign sign` + `cosign attest --type spdx` steps to Jenkinsfile
3. [ ] Author the Gatekeeper `K8sRequiredCosignSignature` constraint template
4. [ ] Roll out the constraint in dry-run mode first, then enforce
5. [ ] Document the key-rotation runbook (re-sign in place, no tag churn)

## Related ADRs
- ADR-005 (AKV) — provides the key store
- ADR-006 (Workload Identity) — provides admission webhook auth to AKV

---

# ADR-009: Network Posture — Private AKS + Private Endpoints + Hub-Spoke VNets

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Network Lead, Security Lead

## Context

Every component of the platform exchanges sensitive material: GitOps manifests, container images, connection strings, Kubernetes credentials. The default Azure posture (public AKS API server, public-endpoint PaaS) is incompatible with the zero-trust goal. Public exposure is also the most common cause of incidents.

## Decision

Run a **hub-spoke VNet topology** per region. The hub holds Azure Firewall Premium, Azure Bastion, and a battery of Private DNS Zones. Every AKS cluster is **private** (`--enable-private-cluster --disable-public-fqdn`). Every PaaS (AKV, SQL, Cosmos, Service Bus, ACR) has **public network access disabled** and is reached through **Private Endpoints** resolved by hub DNS. Inbound from Bitbucket webhooks is permitted only through Azure Front Door with WAF + source-CIDR pinning to Atlassian's published ranges.

## Options Considered

### Option A: Private everywhere (chosen)
**Pros**
- Reduces public attack surface to Front Door + Bastion + Azure DNS
- Compliance-friendly (PCI / HIPAA / SOC 2 baseline)
- DNS resolution failures are loud, not silent

**Cons**
- Setup is more complex (DNS zones, peerings)
- Operator access requires Bastion (intentional)

### Option B: Public AKS API + Network Restrictions
**Pros**
- Simpler initial setup

**Cons**
- AKS API server is internet-facing; misconfiguration risk
- IP allow-lists are brittle and force agent IPs to be stable

### Option C: Hybrid (Mgmt private, workloads public)
**Cons**
- Inconsistent posture; security audits become harder
- Workload PaaS still leaks data path

## Trade-off Analysis

Private-everywhere adds setup complexity but reduces the most common failure modes (accidentally exposed endpoints, IP allow-list drift). The complexity is one-time; the security benefit is continuous.

## Consequences

**Easier**
- No exposed PaaS endpoints to scan
- Network policy can be a stated invariant ("nothing reaches our PaaS without a private endpoint")
- Compliance audits have fewer questions

**Harder**
- DR rebuild must recreate Private DNS Zones and peerings (codified in Crossplane bootstrap)
- Operator access requires Bastion (one-time UX adjustment)

**Will need to revisit if**
- AKS introduces a private + public hybrid posture that fits our needs
- Front Door is replaced by a different ingress path

## Action Items
1. [ ] Provision hub VNets and spoke VNets via Crossplane control-plane-claim
2. [ ] Provision Private DNS Zones for every privatelink subresource we use
3. [ ] Configure Front Door + WAF + Atlassian CIDR allow-list
4. [ ] Disable public network access on every PaaS resource via Crossplane Composition defaults
5. [ ] Document Bastion break-glass procedure

## Related ADRs
- ADR-003 (Cluster Topology) — places the private clusters
- ADR-004 (Multi-Region) — replicates this posture in NE

---

# ADR-010: Race-Free GitOps Update — Idempotent kustomize edit + Rebase Loop + Author Filter

**Status:** Superseded by ADR-010-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

Multiple CI runs may concurrently want to bump the same overlay's image tag. If we are not careful: (a) commits race and one wins, losing the other's bump; (b) the CI commit itself triggers another CI run, creating an infinite loop; (c) human PRs and bot PRs touch the same files and create merge conflicts.

## Decision

Implement a four-part race-free update protocol in the Jenkins pipeline:

1. Use `kustomize edit set image` (idempotent — same tag twice = no diff).
2. Wrap the push in a rebase-and-retry loop (`for i in 1..5; do push || git pull --rebase`).
3. Tag the commit with `[ci skip]` in the message.
4. Filter Bitbucket-to-Jenkins webhooks to exclude commits authored by `PlatformBot`.

## Options Considered

### Option A: Four-part protocol (chosen)
**Pros**
- Provably terminates (idempotent op + bounded retries)
- No human-vs-bot conflict (the bot only touches files humans never edit directly)
- No infinite loop (author filter is the belt-and-suspenders to `[ci skip]`)

**Cons**
- Slightly more pipeline code than a naive `git push`

### Option B: Centralized PR-bot service (e.g., Renovate)
**Pros**
- Off-the-shelf

**Cons**
- Adds a third-party SaaS to the failure domain (violates ADR-001 logic)
- Heavyweight for the simple "set image tag" use case

### Option C: Push directly, ignore conflicts
**Cons**
- Loses concurrent bumps silently
- Tail risk of stuck reconciliations

## Trade-off Analysis

The pipeline-side complexity is minimal (~20 lines of bash). The robustness benefit eliminates a whole class of "why didn't my deploy happen" incidents that erodes platform trust.

## Consequences

**Easier**
- Trustable deploys: every CI run results in either an accurate bump or an explicit failure
- Concurrent merges to the same overlay are safe
- Human PRs to overlays continue to work (the bot only writes the `images:` block)

**Harder**
- Pipeline contributors must understand the protocol when extending it
- Author-filter regex in webhook config is one more thing to maintain

**Will need to revisit if**
- A different templating tool replaces kustomize (the idempotent op contract must be preserved)

## Action Items
1. [ ] Codify the protocol in Jenkins shared library `updateGitOps()`
2. [ ] Document the `[ci skip]` convention and PlatformBot author filter
3. [ ] Add a regression test: concurrent pipeline runs against the same overlay must converge

## Related ADRs
- ADR-001 (Jenkins) — provides the runtime
- ADR-011 (Monorepo) — provides the GitOps repo this protocol writes to

---

# ADR-011: Source of Truth — Monorepo `platform-gitops` for All Cluster State

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

Cluster state — Crossplane XRCs, ArgoCD Applications, ESO ExternalSecrets, Helm value overlays, network policies — must live somewhere ArgoCD can read and humans can review. Two layouts are common: a single monorepo (`platform-gitops`) or a per-service repo holding both app code and its manifests.

## Decision

Adopt a **monorepo** `platform-gitops` that holds *only* declarative state. App source code lives in per-service repos. The monorepo is structured as in Appendix B of the blueprint: `bootstrap/`, `platform/`, `apps/<svc>/{base,overlays/...}/`, `templates/`.

## Options Considered

### Option A: Monorepo (chosen)
**Pros**
- One ArgoCD repo credential to manage
- ApplicationSet directory generator scales linearly with service count
- Cross-cutting refactors (e.g., new label policy) are one PR
- Single review process for all cluster state

**Cons**
- All services compete for the same merge queue (mitigated by per-directory CODEOWNERS)

### Option B: Per-service repo (app + manifests together)
**Pros**
- Strong service ownership boundary

**Cons**
- N repos to register with ArgoCD
- Cross-cutting changes touch N repos
- The Jenkins seed job becomes more complex (must add to N templates)
- Inconsistent overlay conventions across teams

### Option C: One repo per environment
**Pros**
- Clear blast radius per env

**Cons**
- Triple-write for cross-env changes
- Promotion semantics become awkward

## Trade-off Analysis

The monorepo's "single merge queue" downside is real but addressed with CODEOWNERS scoping and (if needed later) a merge queue tool. The benefits — single source for ArgoCD, single place for platform-wide changes — outweigh the costs at our scale.

## Consequences

**Easier**
- ApplicationSet picks up new apps automatically
- Platform-wide policy changes are one PR
- Audit history is single-stream

**Harder**
- Repo size grows; shallow clones become important
- Cross-team coordination for major refactors

**Will need to revisit if**
- The repo exceeds practical Git ergonomics (rough threshold: ~10GB or ~1M files)

## Action Items
1. [ ] Initialise `platform-gitops` with the Appendix-B structure
2. [ ] Apply CODEOWNERS per `apps/<svc>/` directory
3. [ ] Configure ArgoCD with a single SSH deploy key for this repo
4. [ ] Document the contribution guide (template usage, overlay conventions)

## Related ADRs
- ADR-007 (ArgoCD) — consumes this repo
- ADR-010 (Race-Free Update) — writes to this repo

---

# ADR-012: Drift & Rollback Visibility — ArgoCD Notifications → Jira Bridge

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, SRE Lead

## Context

When ArgoCD detects drift, fails a sync, or rolls back, the right people must know — and that knowledge must land in the system of record for incidents (Jira), not just a Slack channel that may get ignored. Without this, the platform loses its operational accountability story.

## Decision

Deploy a small adapter service, **`argocd-jira-bridge`**, in the management cluster's `argocd` namespace. Configure ArgoCD's `argocd-notifications-cm` to emit webhooks on `on-degraded`, `on-rollback`, `on-recovered`, and `on-sync-failed`. The bridge translates these into Jira issue creates/transitions/comments, scoped to the owning team via the `owner` label on the Application.

## Options Considered

### Option A: ArgoCD Notifications → Jira Bridge (chosen)
**Pros**
- Native ArgoCD feature; no extra controller
- Translation layer (`argocd-jira-bridge`) is ~200 lines of Python
- Per-team routing via Application labels

**Cons**
- One more in-house service to maintain

### Option B: ArgoCD → Slack only
**Cons**
- No durable record; missed messages = missed incidents
- No SLA enforcement

### Option C: ArgoCD → PagerDuty + Jira via PD's webhook
**Cons**
- Adds PagerDuty as a dependency for non-pageable events
- Cost scales with event volume

## Trade-off Analysis

The bridge is small, focused, and lives next to the system it serves. Building it once is cheaper than maintaining a fan-out of provider-specific notification templates.

## Consequences

**Easier**
- Every drift/rollback becomes a tracked Jira incident
- Post-mortems have a definitive timeline
- Per-team ownership is enforceable

**Harder**
- The bridge itself becomes part of the platform's operational surface
- Jira project sprawl must be managed (use one project, label per team)

**Will need to revisit if**
- Jira automation rules become powerful enough to consume the raw ArgoCD webhook directly
- Notification volume grows beyond what humans can triage (then add deduplication)

## Action Items
1. [ ] Implement `argocd-jira-bridge` (Python + Flask + Jira REST v3)
2. [ ] Author the `argocd-notifications-cm` templates and triggers
3. [ ] Configure Jira labels and issue type for drift incidents
4. [ ] Add Prometheus metrics on the bridge (`webhooks_received_total`, `jira_calls_failed_total`)

## Related ADRs
- ADR-007 (ArgoCD) — provides the notification source
- ADR-001 (Jenkins) — separate from Jenkins; failure domains stay isolated

---

# ADR-013: Sync Ordering — Sync Waves with Custom `health.lua` for Crossplane XRs

**Status:** Superseded by ADR-013-v2
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

A workload's `Deployment` cannot start until its `ExternalSecret` has been reconciled. The `ExternalSecret` cannot succeed until Crossplane has provisioned the Azure resource and written the connection string to AKV. Without explicit ordering, ArgoCD applies all manifests roughly in parallel, races appear, pods CrashLoopBackOff while waiting for secrets, and operators have to manually re-sync.

## Decision

Use ArgoCD's **sync waves** annotation (`argocd.argoproj.io/sync-wave`) to encode the dependency graph:

- Wave -1: Namespace, RBAC, NetworkPolicy
- Wave 0: Crossplane XRCs (slowest; cloud provisioning)
- Wave 1: ESO ExternalSecrets
- Wave 2: ConfigMaps, ServiceAccounts
- Wave 3: Deployments, Services
- Wave 4: HPA, PDB
- Wave 5: Ingress

Register a custom `health.lua` for every Crossplane XR kind so ArgoCD treats them as `Healthy` only when `status.conditions[Ready] == True`. This makes wave-0 truly block until cloud provisioning completes.

## Options Considered

### Option A: Sync waves + custom health.lua (chosen)
**Pros**
- Native ArgoCD primitive
- No extra controllers required
- The health check propagates Crossplane semantics into ArgoCD's UI

**Cons**
- Wave assignments must be applied consistently — easy to forget on new manifests
- A stuck wave-0 (Azure outage) blocks the rest until the operator intervenes

### Option B: ArgoCD PreSync hooks running shell scripts that poll Crossplane
**Cons**
- Hook scripts are harder to reason about than declarative waves
- Failure semantics murkier

### Option C: A second operator (Argo Workflows) orchestrating the apply
**Cons**
- More moving parts
- Overkill for the ordering complexity we have

## Trade-off Analysis

The wave system is well-understood and observable in the ArgoCD UI. Custom `health.lua` is a small one-time investment per CRD. The downside (stuck wave-0 during Azure outages) is actually a *feature* — it gives the operator a clear signal rather than a half-deployed app.

## Consequences

**Easier**
- Predictable cross-operator deploys
- Healthy/Degraded states map cleanly to platform reality
- Operators can `argocd app sync --resource` a specific wave during incidents

**Harder**
- Every new XRD needs a health.lua entry (codified in the bootstrap repo)
- Wave assignments must be enforced by lint (CI check)

**Will need to revisit if**
- ArgoCD introduces native CRD health reflection (then health.lua becomes unnecessary)

## Action Items
1. [ ] Add `health.lua` entries to `argocd-cm` for SQLDatabase, CosmosAccount, ServiceBus, DnsRecord kinds
2. [ ] Add a CI lint that fails PRs without `argocd.argoproj.io/sync-wave` on supported kinds
3. [ ] Document the wave assignments in the platform contributing guide

## Related ADRs
- ADR-007 (ArgoCD) — provides the wave mechanism
- ADR-002 (Crossplane) — emits the status conditions
- ADR-005 (ESO) — consumes Crossplane output in wave 1

---

# ADR-014: CI Agents — Ephemeral Pods in a Dedicated AKS Node Pool

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

Long-lived CI agents accumulate state (caches, leaked credentials, polluted toolchains) and complicate version pinning. They are also a security risk: a compromised agent can persist between builds. Static VM agents also fail to elastically match load.

## Decision

Use the Jenkins `kubernetes-plugin` to spawn **one pod per build stage**, scheduled to a dedicated AKS node pool `cipool` with taint `workload=ci:NoSchedule`. Tool images are pre-built (`ci/pod-templates/*.yaml`) and pinned. The node pool autoscales 1 → 30. Pods are deleted on job end.

## Options Considered

### Option A: Ephemeral pods in dedicated node pool (chosen)
**Pros**
- Per-build isolation; no state leakage
- Tool versions pinned per pod template (no agent-wide upgrades)
- Cluster autoscaler handles burst scaling
- Taint isolates CI noise from platform components

**Cons**
- Cold-start adds ~10s per stage (image pulls cached on node)
- Plugin upgrade story has occasional sharp edges

### Option B: Static VM agents (Azure VMSS via Jenkins plugin)
**Cons**
- Bin-packing inefficiency
- Agent OS patching cycle
- Long-lived state

### Option C: External SaaS runners (BuildKite, Buildjet, etc.)
**Cons**
- Adds a vendor dependency (violates failure-isolation goal)
- Egress costs for image pushes

## Trade-off Analysis

The cold-start tax is a few seconds; the security and operational benefits are continuous. Per-pod images mean toolchain upgrades are scoped reviews, not big-bang agent upgrades.

## Consequences

**Easier**
- Every build is reproducible from a versioned tool image
- Cluster autoscaler matches burst load
- Compromise blast radius is one pod

**Harder**
- Pod template inventory must be maintained
- Image pull caching needs node-pool awareness for fast cold starts

**Will need to revisit if**
- AKS adds a dedicated CI-runner service that meets our isolation needs

## Action Items
1. [ ] Provision `cipool` node pool (autoscale 1-30, taint `workload=ci:NoSchedule`)
2. [ ] Build and publish pod-template tool images (node, go, jvm, python, buildah, cosign, trivy, syft, git)
3. [ ] Pin tool image tags in Jenkinsfile templates
4. [ ] Add a monthly job to rebuild tool images with latest CVE patches

## Related ADRs
- ADR-001 (Jenkins) — the runtime that schedules these pods
- ADR-015 (Buildah) — one of the tool images

---

# ADR-015: Container Build Tool — Buildah (Rootless) over Docker-in-Docker

**Status:** Accepted
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Platform Lead

## Context

CI must build OCI images inside ephemeral pods (per ADR-014). The historical pattern of mounting `/var/run/docker.sock` from the node into the build pod (or running Docker-in-Docker with privileged mode) creates a privilege-escalation path: a compromised pipeline obtains node-level root.

## Decision

Use **Buildah in rootless mode** as the container build tool. Pods run as non-root, use the `fuse-overlayfs` driver, and do not require `privileged: true` or access to the Docker socket.

## Options Considered

### Option A: Buildah (rootless, chosen)
**Pros**
- No privileged pods
- No Docker daemon to manage
- Produces OCI-format images directly
- Same Dockerfile syntax developers already know

**Cons**
- Slightly less Docker-tooling muscle memory in the team
- Some Dockerfile features (e.g., `--mount=type=secret` semantics) behave differently

### Option B: Kaniko (Google)
**Pros**
- Also rootless; no privileged

**Cons**
- Cache layering less ergonomic
- Maintenance has slowed

### Option C: Docker-in-Docker with privileged: true
**Cons**
- Privileged escalation surface
- Disqualified by security review

### Option D: BuildKit (rootless)
**Pros**
- Best-in-class build performance and cache reuse

**Cons**
- More setup complexity for rootless mode
- We chose Buildah for ease; BuildKit is a reasonable future swap

## Trade-off Analysis

Buildah hits the sweet spot of "no privilege, no daemon, Dockerfile-compatible." BuildKit may be revisited if cache performance becomes a bottleneck.

## Consequences

**Easier**
- Builds run in unprivileged pods — security review passes
- No daemon means no leaked state
- OCI output is consumable by Cosign, Trivy, Syft directly

**Harder**
- Some edge-case Dockerfile features (BuildKit-specific) won't work
- Developers occasionally need help porting Docker idioms

**Will need to revisit if**
- BuildKit's rootless story becomes our standard
- A specific service's build truly needs BuildKit-only features

## Action Items
1. [ ] Add `buildah` tool image to `ci/pod-templates/buildah.yaml`
2. [ ] Document the Dockerfile compatibility caveats
3. [ ] Add a CI lint that rejects `privileged: true` in any pod template
4. [ ] Measure build time pre/post; revisit if regression > 30%

## Related ADRs
- ADR-014 (Ephemeral Pod Agents) — the runtime
- ADR-008 (Cosign Signing) — consumes the build output

---

## Appendix — How to Maintain This Document

- One ADR per file under `docs/adrs/` is the canonical layout when these decisions are migrated into the `platform-gitops` repository. This combined view exists to give reviewers and new joiners a single read.
- Status transitions: `Proposed` → `Accepted` → (optionally `Deprecated` or `Superseded by ADR-NNN`). Never delete an ADR; supersede it.
- When proposing a new ADR, copy the template (Context / Decision / Options / Trade-off / Consequences / Action Items / Related ADRs) and assign the next free number.
- Quarterly: re-review the "Will need to revisit if" clauses. If any trigger fires, open a new ADR rather than amending the old one.

*End of ADR pack.*
