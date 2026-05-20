# PRD: IDP GitOps Blueprint v2 — Full Platform Implementation

## Introduction/Overview

This PRD defines the implementation plan for a **production-grade, multi-cluster, multi-region Internal Developer Platform (IDP)** based on the GitOps Bridge pattern as specified in `IDP-GitOps-Blueprint-v2.md`. The platform isolates the Developer Experience plane (Jira, Bitbucket Cloud, Jenkins), the Control Plane (Crossplane + ArgoCD hub), and the Data Plane (workload AKS clusters) so that a failure in any one plane cannot disrupt the other two.

The implementation is a **greenfield build** leveraging existing Terraform and GitOps foundations in this repository, delivered in **five phased milestones**. The target team is a mixed group of platform engineers, DevOps engineers, and application developers.

**Key technologies:** Azure AKS, Crossplane (Azure Provider), ArgoCD, Jenkins, External Secrets Operator (ESO), Argo Rollouts, Azure Key Vault, Azure Front Door, Kyverno/Gatekeeper.

**Bootstrapping strategy:** Semi-automated — Terraform provisions base infrastructure (VNets, AKS clusters, storage accounts, ACR); ArgoCD is manually bootstrapped once, after which the platform becomes self-managing via GitOps.

---

## Goals

- Deliver a fully operational multi-region IDP with automatic management-plane failover (RTO ≤ 90s)
- Provide zero-trust secret delivery via per-namespace ESO + Workload Identity + ABAC-scoped AKV
- Enable progressive delivery (Argo Rollouts) with SLO-class-aware canary analysis for production workloads
- Achieve failure-domain isolation: DX/Control/Data planes fail independently
- Establish a self-service "Create New IDP Service" workflow from Jira through to running workloads
- Support Active-Active reads and Active-Passive writes across West Europe and North Europe regions
- Provide Day-2 operational tooling: observability, alerting, DR runbooks, and automated token rotation
- Build on existing `terraform/` and `gitops/` directories as the starting point

---

## User Stories

### Phase 1: Foundation Infrastructure

#### US-001: Provision Hub-and-Spoke Network Topology
**Description:** As a platform engineer, I want the hub-and-spoke VNet topology provisioned via Terraform so that all clusters have zero-trust network segmentation with Azure Firewall Premium egress filtering.

**Acceptance Criteria:**
- [ ] Hub VNet (10.0.0.0/16) in West Europe with Azure Firewall Premium deployed
- [ ] Hub VNet in North Europe peered to WE hub
- [ ] Spoke VNets for mgmt-we (10.1/16), aks-prod-we (10.2/16), aks-staging-we (10.3/16), aks-dev-we (10.4/16), aks-prod-ne (10.5/16)
- [ ] Azure Firewall FQDN allow-list for Bitbucket/Atlassian, ACR, Azure services
- [ ] Private DNS Zones for AKV, SQL, Cosmos, Service Bus, ACR, Blob Storage
- [ ] Azure Bastion deployed for break-glass operator access
- [ ] No public IP on any AKS cluster API server
- [ ] Terraform plan applies cleanly with no errors

#### US-002: Provision AKS Clusters with Correct Naming and Configuration
**Description:** As a platform engineer, I want all AKS clusters provisioned according to the canonical naming convention so that the multi-cluster layout matches the blueprint topology.

**Acceptance Criteria:**
- [ ] `mgmt-we`: Private API, system pool + `cipool` (taint `workload=ci:NoSchedule`), autoscaler 1-30 nodes on cipool
- [ ] `mgmt-ne`: Private API, system pool only (standby — minimal node count)
- [ ] `aks-dev-we`: Single-AZ, private API
- [ ] `aks-staging-we`: Multi-AZ, private API
- [ ] `aks-prod-we`: Multi-AZ, Premium SKU, private API
- [ ] `aks-prod-ne`: Multi-AZ, Premium SKU, private API
- [ ] `seed-wus`: Single-node cluster in West US 2 for catastrophic DR bootstrap
- [ ] All clusters have Workload Identity enabled
- [ ] All clusters have OIDC issuer enabled
- [ ] Private endpoints from spokes to hub services configured
- [ ] Terraform plan applies cleanly

#### US-003: Provision Per-Region Azure Key Vaults
**Description:** As a platform engineer, I want per-region Azure Key Vaults provisioned so that each workload cluster reads secrets from a local vault with RBAC authorization mode.

**Acceptance Criteria:**
- [ ] `kv-platform-dev-we` created with RBAC mode, soft-delete (90 days), purge-protection
- [ ] `kv-platform-staging-we` created with same settings
- [ ] `kv-platform-prod-we` created with same settings
- [ ] `kv-platform-prod-ne` created with same settings
- [ ] Private endpoints in each respective regional hub
- [ ] Diagnostic logs piped to Azure Monitor
- [ ] Terraform plan applies cleanly

#### US-004: Provision Azure Storage Account for Management Lease
**Description:** As a platform engineer, I want a geo-redundant storage account provisioned for the management-plane singleton lock so that exactly one management cluster is active at any time.

**Acceptance Criteria:**
- [ ] Storage account with GRS replication created
- [ ] Blob container `leases` with blob `mgmt-active` created
- [ ] Private endpoint reachable from both mgmt-we and mgmt-ne spokes
- [ ] `mgmt-we` and `mgmt-ne` UAMI granted `Storage Blob Data Contributor` on the lease container
- [ ] Terraform plan applies cleanly

#### US-005: Provision Azure Container Registry
**Description:** As a platform engineer, I want a shared ACR so that Jenkins can push images and workload clusters can pull them.

**Acceptance Criteria:**
- [ ] `acrplatformprod` created with Premium SKU (geo-replication to NE)
- [ ] Private endpoint in hub VNet
- [ ] AKS clusters configured with `acrpull` role assignment
- [ ] Jenkins UAMI granted `acrpush` role
- [ ] Terraform plan applies cleanly

---

### Phase 2: Control Plane Bootstrap

#### US-006: Bootstrap ArgoCD on mgmt-we
**Description:** As a platform engineer, I want ArgoCD installed on mgmt-we in HA mode so that it can manage all workload clusters as a hub.

**Acceptance Criteria:**
- [ ] ArgoCD installed via Helm with HA values: 3 application-controllers (sharded by cluster), 2 repo-servers, 3 redis-ha, 2 servers
- [ ] ArgoCD configured with SSH deploy key for `platform-gitops` Bitbucket repo
- [ ] All cluster Secrets registered with mandatory labels (`env`, `region`, `role`, `argocd.argoproj.io/secret-type: cluster`)
- [ ] Kyverno policy `K8sRequiredClusterSecretLabels` enforces label schema
- [ ] ArgoCD accessible only via private endpoint (no public Ingress)
- [ ] ArgoCD bootstrap ApplicationSet deployed that manages itself (App-of-Apps)

#### US-007: Install Crossplane with Azure Provider on mgmt-we
**Description:** As a platform engineer, I want Crossplane installed with the Azure provider so that cloud resources can be provisioned declaratively via XRDs.

**Acceptance Criteria:**
- [ ] Crossplane core installed via Helm
- [ ] `provider-azure` installed and healthy
- [ ] ProviderConfig configured with Workload Identity (UAMI on mgmt-we with Contributor on target subscriptions)
- [ ] Per-region ProviderConfig sharding configured for dual AKV writes
- [ ] XRDs deployed: SQLDatabase, CosmosAccount, DnsRecord, ServiceBus, NamespaceVaultBinding
- [ ] Compositions deployed with SLO-class-aware logic (bronze/silver/gold)
- [ ] `health.lua` custom health checks for Crossplane XR kinds registered in ArgoCD

#### US-008: Deploy Management-Plane Singleton Lock (mgmt-leader-lease)
**Description:** As a platform engineer, I want the `mgmt-leader-lease` controller deployed on both mgmt clusters so that exactly one management cluster is active, with automatic failover.

**Acceptance Criteria:**
- [ ] `mgmt-leader-lease` Go controller deployed as single-replica Deployment on both mgmt-we and mgmt-ne
- [ ] Controller acquires Azure Storage Blob Lease (TTL 15s, renewal every 5s)
- [ ] On lease acquisition: writes identity to ConfigMap `mgmt-leader-status` in `kube-system`
- [ ] On lease loss: updates ConfigMap to reflect non-leader status
- [ ] Prometheus metrics exposed: `mgmt_leader_lease_renewed_seconds`
- [ ] `mgmt-cli failback --to <cluster> --confirm` CLI tool functional

#### US-009: Deploy Controller-Scaler
**Description:** As a platform engineer, I want the `controller-scaler` deployed so that platform controllers scale to zero on the standby cluster and scale up on lease acquisition.

**Acceptance Criteria:**
- [ ] `controller-scaler` watches `mgmt-leader-status` ConfigMap
- [ ] On lease-acquisition: scales ArgoCD controllers → 3/2/2, Crossplane → 1, ESO → 1, Argo Rollouts → 1, jira-bridge → 1
- [ ] On lease-loss: scales all above → 0
- [ ] mgmt-ne confirmed at 0 replicas for all controllers in steady state
- [ ] Failover tested: kill mgmt-we lease → mgmt-ne scales up within 75s

#### US-010: Install External Secrets Operator
**Description:** As a platform engineer, I want ESO installed on all clusters so that secrets flow from AKV to Kubernetes Secrets automatically.

**Acceptance Criteria:**
- [ ] ESO controller installed on mgmt-we (orchestrator role, managed by controller-scaler)
- [ ] ESO agent installed on each workload cluster
- [ ] `--namespace-scoped-stores=true` flag set on all ESO instances
- [ ] No `ClusterSecretStore` deployed anywhere
- [ ] ESO healthy and reconciling on all clusters

#### US-011: Deploy Gatekeeper/Kyverno Policies
**Description:** As a platform engineer, I want admission policies enforced so that management clusters reject workloads and production clusters reject plain Deployments.

**Acceptance Criteria:**
- [ ] Gatekeeper/Kyverno installed on all clusters
- [ ] `tier=platform` label required on all mgmt cluster namespaces; pods in non-conformant namespaces rejected
- [ ] `K8sProdRequiresRollout` constraint rejects `Deployment` in prod cluster namespaces
- [ ] `K8sRequiredClusterSecretLabels` enforces ArgoCD cluster Secret labels
- [ ] Policies tested with both conformant and non-conformant resources

---

### Phase 3: CI Pipeline & GitOps Engine

#### US-012: Deploy Jenkins on mgmt-we
**Description:** As a platform engineer, I want Jenkins deployed as a single-replica StatefulSet on mgmt-we so that CI pipelines can build images and update GitOps state.

**Acceptance Criteria:**
- [ ] Jenkins deployed as StatefulSet with `replicas: 1`
- [ ] PVC: ReadWriteOnce Azure Disk Premium SSD for `$JENKINS_HOME`
- [ ] `terminationGracePeriodSeconds: 300` for in-flight job draining
- [ ] LivenessProbe on `/login`, readinessProbe on `/whoAmI/api/json`
- [ ] Jenkins scheduled on `cipool` node pool (tolerates `workload=ci:NoSchedule`)
- [ ] Kubernetes-plugin configured for ephemeral agent pods on `cipool`
- [ ] `bitbucket-workspace-token` credential sourced from AKV via ESO
- [ ] `jira-service-account-token` credential sourced from AKV via ESO
- [ ] Reloader configured to restart Jenkins on credential Secret change
- [ ] Velero backup schedule: every 6 hours, retention 30 days, GRS storage

#### US-013: Configure Jenkins Multibranch Pipeline
**Description:** As a platform engineer, I want the Jenkins pipeline configured with loop-prevention so that PlatformBot commits never trigger builds.

**Acceptance Criteria:**
- [ ] Bitbucket Branch Source trait configured with author filter: `^(?!PlatformBot).*$`
- [ ] Pipeline commits use `[ci skip]` marker as defensive layer
- [ ] GitOps update stage only touches `apps/<svc>/workload/overlays/dev/`
- [ ] Push retry logic (5 attempts with rebase) implemented
- [ ] Pipeline uses `git -c user.name=PlatformBot` for commits
- [ ] End-to-end test: commit to service repo → image built → image tag updated in platform-gitops

#### US-014: Configure Inbound Webhook Path
**Description:** As a platform engineer, I want Bitbucket webhooks to reach Jenkins securely so that pushes trigger CI immediately.

**Acceptance Criteria:**
- [ ] Azure Front Door configured with WAF rule pinning Atlassian webhook CIDRs
- [ ] DNAT rule on Azure Firewall routes webhook traffic to Jenkins controller
- [ ] TLS verified at WAF layer and at Jenkins
- [ ] Webhook delivery tested end-to-end (Bitbucket push → Jenkins build starts)

#### US-015: Configure Two-Tier ArgoCD ApplicationSets
**Description:** As a platform engineer, I want the `platform-infra-set` and `workloads-set` ApplicationSets deployed so that infrastructure and workload lifecycles are managed independently.

**Acceptance Criteria:**
- [ ] `platform-infra-set` ApplicationSet deployed with Matrix generator (env list × git directories under `apps/*/infra/overlays/`)
- [ ] Infra Applications have: `prune: false`, `selfHeal: true`, retry limit 10, max backoff 30m
- [ ] `workloads-set` ApplicationSet deployed with Matrix generator (clusters with `role: workload` × git directories under `apps/*/workload/overlays/`)
- [ ] Workload Applications have: `prune: true`, `selfHeal: true`, retry limit 5, max backoff 5m
- [ ] Application naming: `<svc>-infra-<env>` and `<svc>-app-<env>-<cluster>`
- [ ] Sync waves configured per blueprint (infra: -1 to 1; workload: -1 to 4)
- [ ] Workload Application blocks sync if corresponding infra Application is not Healthy
- [ ] `ServerSideApply=true` and `ApplyOutOfSyncOnly=true` on both tiers

#### US-016: Deploy argocd-jira-bridge
**Description:** As a platform engineer, I want the `argocd-jira-bridge` deployed so that drift events and rollback incidents are automatically tracked in Jira.

**Acceptance Criteria:**
- [ ] `argocd-jira-bridge` deployed on mgmt-we, managed by controller-scaler
- [ ] Failed syncs create Jira incidents in the IDP project
- [ ] Rollout analysis failures create SEV1 incidents
- [ ] Drift corrections are logged as informational tickets
- [ ] Bridge uses `jira-service-account-token` from AKV via ESO

---

### Phase 4: Security, Secrets & Progressive Delivery

#### US-017: Implement Per-Namespace Vault Binding (NamespaceVaultBinding XRD)
**Description:** As a platform engineer, I want the `NamespaceVaultBinding` Crossplane Composition functional so that each workload namespace gets isolated identity and AKV access.

**Acceptance Criteria:**
- [ ] XRD `xnamespacevaultbindings.platform.example.com` deployed
- [ ] Composition creates: UAMI (`uami-<namespace>-<cluster>`), FederatedIdentityCredential, RoleAssignment with ABAC condition (`secrets:Name LIKE '<prefix>-*'`)
- [ ] SecretStore and ServiceAccount auto-created per namespace
- [ ] ABAC condition confirmed: namespace `payments` cannot read secrets prefixed `billing-*`
- [ ] ApplicationSet `namespace-vault-bindings-set` generates one binding per namespace × cluster pair
- [ ] End-to-end test: create namespace → UAMI created → SecretStore functional → ExternalSecret pulls value

#### US-018: Implement Crossplane Dual-Write to Both Regional AKVs
**Description:** As a platform engineer, I want Crossplane Compositions to write connection strings to both regional AKVs so that each workload cluster reads from its local vault.

**Acceptance Criteria:**
- [ ] SQL Composition writes connection JSON to both `kv-platform-prod-we` and `kv-platform-prod-ne`
- [ ] Cosmos Composition writes to both vaults
- [ ] Service Bus Composition writes to both vaults
- [ ] Secret names are identical in both vaults (e.g., `payments-sql-conn`)
- [ ] Alert `PerRegionAKVDualWriteSkew` fires if vaults drift > 10 minutes apart
- [ ] Tested: provision SQL → both vaults contain identical secret

#### US-019: Deploy Argo Rollouts with SLO-Class Analysis Templates
**Description:** As a platform engineer, I want Argo Rollouts deployed on production clusters with auto-generated AnalysisTemplates so that canary deployments are validated against SLO metrics.

**Acceptance Criteria:**
- [ ] Argo Rollouts controller installed on `aks-prod-we` and `aks-prod-ne`
- [ ] Gold AnalysisTemplate: success-rate ≥ 99%, p99-latency ≤ 500ms, canary steps 5/25/50/100 with 5m pauses
- [ ] Silver AnalysisTemplate: success-rate ≥ 99%, canary steps 25/100
- [ ] Bronze: no analysis, direct cutover `[setWeight: 100]`
- [ ] Failed analysis aborts rollout (canary → 0, stable → 100%)
- [ ] ArgoCD marks Application Degraded on rollout failure
- [ ] `argocd-jira-bridge` opens Jira incident on rollout failure
- [ ] `RolloutAnalysisFailed` alert fires within 1 minute

#### US-020: Implement SaaS Token Rotation CronJob
**Description:** As a platform engineer, I want automated rotation of Bitbucket and Jira tokens so that static SaaS credentials have a quarterly lifecycle.

**Acceptance Criteria:**
- [ ] `bitbucket-token-rotator` CronJob deployed (lease-aware — only runs on active mgmt)
- [ ] Rotator mints new workspace token via Bitbucket API, writes to both regional AKVs
- [ ] Reloader detects new Secret hash; rolls Jenkins controller pod
- [ ] Previous token revoked after 24-hour grace period
- [ ] Prometheus metric `saas_token_age_days{token="bitbucket-workspace"}` exposed
- [ ] `SaaSTokenAgeExceeded` alert fires if token age > 100 days
- [ ] Same pattern for Jira service-account token

---

### Phase 5: Day-2 Operations, DR & Observability

#### US-021: Deploy Observability Stack with Platform Alerts
**Description:** As a platform engineer, I want the observability stack deployed with platform-specific alert rules so that operational issues are detected automatically.

**Acceptance Criteria:**
- [ ] Prometheus + Grafana deployed on mgmt-we (managed by controller-scaler)
- [ ] Alert: `MgmtLeaderLeaseLost` — fires if lease renewal > 30s stale for 1m
- [ ] Alert: `PerRegionAKVDualWriteSkew` — fires if vault pair out-of-sync > 10m
- [ ] Alert: `SaaSTokenAgeExceeded` — fires if token age > 100 days
- [ ] Alert: `RolloutAnalysisFailed` — fires if rollout phase == Degraded
- [ ] Platform-loop SLOs dashboarded: time-to-deploy, time-to-provision, drift-correction-MTTR, secret freshness, mgmt-failover RTO
- [ ] PagerDuty integration for critical alerts

#### US-022: Configure Velero Backup for Management Cluster
**Description:** As a platform engineer, I want Velero configured on mgmt-we so that the management cluster can be restored from backup.

**Acceptance Criteria:**
- [ ] Velero installed on mgmt-we
- [ ] Backup schedule: every 6 hours (etcd + PVCs including Jenkins home)
- [ ] Backup stored in GRS storage account
- [ ] Retention: 30 days
- [ ] Restore tested: Velero restore → Jenkins PVC restored → Jenkins starts with previous state
- [ ] mgmt-ne configured as Velero restore target

#### US-023: Validate DR Runbook — Management Plane Failover
**Description:** As a platform engineer, I want the automated DR failover validated end-to-end so that we have confidence in the 90-second RTO claim.

**Acceptance Criteria:**
- [ ] Simulate mgmt-we failure (cordon + drain + delete lease)
- [ ] mgmt-ne acquires lease within 20s of TTL expiry
- [ ] controller-scaler scales up mgmt-ne controllers within 25s
- [ ] ArgoCD on mgmt-ne re-establishes cluster connections within 75s
- [ ] All workload Applications return to Healthy/Synced state
- [ ] Crossplane re-adopts cloud resources (no duplicate provisioning)
- [ ] Total failover RTO measured ≤ 90s
- [ ] Failback via `mgmt-cli failback --to mgmt-we --confirm` validated
- [ ] No split-brain observed at any point during test

#### US-024: Implement "Create New IDP Service" Seed Job
**Description:** As a platform engineer, I want the Jenkins seed job functional so that a Jira ticket can trigger full service scaffolding end-to-end.

**Acceptance Criteria:**
- [ ] Jira webhook triggers Jenkins seed job on "IDP Service Request" issue type
- [ ] Seed job creates Bitbucket repository from Cookiecutter template
- [ ] Seed job creates two PRs to `platform-gitops`: one for `apps/<svc>/infra/`, one for `apps/<svc>/workload/`
- [ ] PRs use `bitbucket-workspace-token` credential
- [ ] Generated infra includes: xrc-sql.yaml, xrc-cosmos.yaml, xrc-sb.yaml, xrc-namespace-binding.yaml with overlays
- [ ] Generated workload includes: rollout.yaml, analysis-template.yaml, service.yaml, ingress.yaml, external-secrets with overlays
- [ ] Rollout.yaml uses `kind: Rollout` for prod overlays, `kind: Deployment` for dev/staging
- [ ] Analysis template auto-selected based on SLO class from Jira ticket
- [ ] End-to-end test: Jira ticket → repo created → PRs merged → infra provisioned → app deployed

#### US-025: Validate Seed Cluster DR (Catastrophic Recovery)
**Description:** As a platform engineer, I want the seed-wus cluster validated so that both management clusters can be rebuilt from scratch if needed.

**Acceptance Criteria:**
- [ ] `seed-wus` cluster operational with Crossplane installed
- [ ] `bootstrap/control-plane-claim.yaml` provisions a new management cluster when applied
- [ ] New management cluster acquires the (free) blob lease
- [ ] ArgoCD bootstraps itself from `platform-gitops`
- [ ] Full recovery RTO measured ≤ 45 minutes
- [ ] Workload clusters confirmed serving traffic throughout the entire DR scenario

---

## Functional Requirements

- FR-1: The system must provision and manage 7 AKS clusters across 3 Azure regions (WE, NE, WUS2) with private API servers
- FR-2: Exactly one management cluster must be active at any time, enforced by an Azure Storage Blob Lease singleton lock
- FR-3: Management-plane failover must complete automatically within 90 seconds of lease expiry
- FR-4: All platform controllers on the standby management cluster must be at zero replicas
- FR-5: Crossplane must be the sole cloud provisioning engine, with XRDs for SQL, Cosmos, Service Bus, DNS, and NamespaceVaultBinding
- FR-6: Every Crossplane Composition that emits secrets must write to both regional AKVs simultaneously
- FR-7: ESO must use per-namespace `SecretStore` (not `ClusterSecretStore`) with per-namespace UAMI and ABAC-scoped access
- FR-8: A compromised namespace must not be able to read secrets belonging to another namespace (enforced by AKV ABAC)
- FR-9: ArgoCD must manage two separate Applications per service: slow-lifecycle infra and fast-lifecycle workload
- FR-10: The workload Application must not sync if its corresponding infra Application is not Healthy
- FR-11: Argo Rollouts must be mandatory for all workloads on production clusters; Gatekeeper rejects `Deployment` resources
- FR-12: Canary analysis strategy must be auto-generated from the service's SLO class (bronze/silver/gold)
- FR-13: Jenkins must be the sole CI engine, running only on mgmt-we as a single-replica StatefulSet
- FR-14: The Jenkins pipeline must never trigger on PlatformBot commits (author filter is primary; `[ci skip]` is defensive)
- FR-15: The Jenkins pipeline must only modify `apps/<svc>/workload/overlays/dev/` — never the `infra/` subtree
- FR-16: Bitbucket webhooks must traverse: Front Door → WAF (Atlassian CIDR pin) → Azure Firewall DNAT → Jenkins
- FR-17: SaaS API tokens (Bitbucket workspace token, Jira service-account token) must be rotated quarterly via automated CronJob
- FR-18: Reloader must restart affected pods when mounted Secrets change hash
- FR-19: The "Create New IDP Service" workflow must be triggered by a Jira ticket and produce a fully-scaffolded service with all Crossplane claims and workload manifests
- FR-20: Velero must back up the management cluster (etcd + PVCs) every 6 hours to GRS storage with 30-day retention
- FR-21: Front Door must route user traffic Active-Active for reads across both prod regions
- FR-22: Cosmos must use Active-Passive writes with `multipleWriteLocationsEnabled: false` and automatic failover
- FR-23: All clusters must enforce naming convention `<role>-<env>-<region>` or `<role>-<region>` with label validation

---

## Non-Goals (Out of Scope)

- **Multi-master Jenkins**: Upstream does not support it; single-replica with Velero backup is the accepted pattern
- **Private endpoint to Bitbucket Cloud**: Atlassian doesn't offer Azure Private Link; egress via Firewall FQDN is accepted
- **Cosmos multi-region writes**: Intentionally disabled; Active-Passive writes simplifies consistency model
- **Automatic priority-based SLO class assignment**: SLO class is manually declared per service in the Jira ticket
- **Backstage integration**: While backstage/ exists in the repo, the blueprint does not include Backstage; it may be added later
- **Application code**: This PRD covers platform infrastructure only, not the workload application code
- **Cost optimization**: SKU choices follow the SLO class contract; FinOps tuning is a separate initiative
- **Compliance certifications**: The architecture supports compliance (audit logs, encryption, RBAC), but certification processes are out of scope
- **GitOps repo migration from GitHub to Bitbucket**: The blueprint assumes `platform-gitops` is on Bitbucket Cloud; if it's currently on GitHub, migration is a prerequisite but separate effort

---

## Design Considerations

### Architecture Diagrams
- Refer to `IDP-C4-Container-v2.drawio` for the full container diagram
- Network topology diagrams in §1.4 of the blueprint
- DR sequence in §7.3 of the blueprint

### Existing Repository Assets to Leverage
- `terraform/` — Base infrastructure modules (VNets, AKS, storage)
- `gitops/` — Existing GitOps patterns and manifests
- `backstage/` — Catalog info (not actively used in this implementation)

### UI/UX
- No UI component in this implementation
- Developer interaction is through: Jira (service requests), Bitbucket (code/config PRs), ArgoCD dashboard (deployment visibility)

### Key Design Decisions (from ADRs)
- ADR-001: Jenkins over Bamboo (failure-domain isolation)
- ADR-004: Active-Passive writes with Cosmos
- ADR-005: Per-namespace SecretStore with ABAC
- ADR-013: Two-tier ApplicationSet (infra vs workload)
- ADR-022: Azure Storage Blob Lease for mgmt singleton

---

## Technical Considerations

### Dependencies
- Azure subscription with sufficient quota for 7 AKS clusters, Premium SKUs, and multi-region resources
- Bitbucket Cloud workspace with admin access for workspace access tokens
- Jira Cloud project (`IDP`) with service account
- DNS zone delegation for service ingress
- Existing Terraform state backend (Azure Storage)

### Constraints
- AKS API servers must be private (no public endpoints)
- All Azure PaaS accessed via Private Endpoints (except Bitbucket Cloud SaaS)
- Jenkins cannot run in mgmt-ne (single-region only; acceptable per ADR-001)
- Azure ABAC for Key Vault requires RBAC authorization mode (`enableRbacAuthorization: true`)
- Crossplane provider-azure version must support all required resource kinds
- ArgoCD version must support ApplicationSet Matrix generators with goTemplate

### Performance Requirements
- Time-to-deploy (workload tier): p95 ≤ 5 minutes
- Time-to-provision (infra tier): p95 ≤ 30 minutes
- Drift-correction MTTR (workload): p95 ≤ 3 minutes
- Secret freshness: p95 ≤ 90 seconds
- Management-plane failover RTO: p99 ≤ 90 seconds

### Custom Components to Build
| Component | Language | Purpose |
|---|---|---|
| `mgmt-leader-lease` | Go | Acquire/renew Azure Blob Lease; write ConfigMap |
| `controller-scaler` | Go | Watch ConfigMap; scale controllers up/down |
| `argocd-jira-bridge` | Go/Python | Sync ArgoCD events to Jira tickets |
| `bitbucket-token-rotator` | Go/Python | Quarterly rotation of workspace tokens |
| `mgmt-cli` | Go | Operator CLI for failback and diagnostics |

### Integration Points
- Bitbucket Cloud → Jenkins (webhooks via Front Door + WAF)
- Jenkins → Bitbucket Cloud (API calls via Firewall FQDN allow-list)
- Jenkins → ACR (image push via private endpoint)
- ArgoCD → Bitbucket Cloud (Git polling via Firewall FQDN allow-list)
- ArgoCD → AKS clusters (via cluster Secrets with private API endpoints)
- Crossplane → Azure ARM (via Workload Identity)
- ESO → AKV (via Workload Identity + per-namespace UAMI)
- Argo Rollouts → Prometheus (analysis queries)
- argocd-jira-bridge → Jira Cloud (API via Firewall FQDN allow-list)
- mgmt-leader-lease → Azure Storage (Blob Lease API via private endpoint)

---

## Success Metrics

- **Platform availability**: Management-plane failover completes in ≤ 90s with zero workload impact (validated by DR drill)
- **Time-to-first-deploy**: New service from Jira ticket to running in dev ≤ 45 minutes (infra provision + workload sync)
- **Secret isolation**: Penetration test confirms no cross-namespace secret access possible
- **Progressive delivery confidence**: 100% of production deployments go through canary analysis; auto-rollback fires correctly on degraded metrics
- **Drift correction**: Manual kubectl changes reverted within 3 minutes (ArgoCD self-heal)
- **Zero split-brain events**: No period where both management clusters believe they are active simultaneously
- **Token rotation success rate**: 100% of quarterly rotations complete without manual intervention
- **Recovery validation**: Full catastrophic DR (seed-wus rebuild) demonstrated in ≤ 45 minutes

---

## Open Questions

1. **Crossplane provider version**: Which exact version of `provider-azure` (Upbound official vs community) should be used? Does it support all required resource types (ABAC conditions on RoleAssignment, FederatedIdentityCredential)?
2. **ArgoCD version**: Does the installed version support the `dependencies` field for inter-Application ordering, or do we need the custom admission webhook approach?
3. **Existing Terraform state**: What is the current state of `terraform/` modules? Do they already provision some of the required infrastructure (VNets, AKS)?
4. **Bitbucket workspace**: Is the workspace already created? Do we have admin access to create workspace access tokens?
5. **Cosmos consistency default**: The blueprint defaults to `session` consistency — is this confirmed as the correct default for the organization's workloads?
6. **Observability stack choice**: Prometheus + Grafana, or Azure Monitor + Managed Grafana? The blueprint implies self-hosted Prometheus.
7. **Kyverno vs Gatekeeper**: The blueprint mentions both — which should be the primary policy engine?
8. **DNS provider**: Which DNS zone and provider will be used for workload ingress (Azure DNS, external)?
9. **Seed cluster location**: West US 2 is specified — is this confirmed as an acceptable DR region given data residency requirements?
10. **Budget/quota**: Are Azure quotas sufficient for 7 AKS clusters with Premium SKUs across 3 regions?

---

## Implementation Phases Summary

| Phase | Focus | Key Deliverables |
|---|---|---|
| **Phase 1** | Foundation Infrastructure | Network topology, AKS clusters, AKVs, Storage, ACR |
| **Phase 2** | Control Plane Bootstrap | ArgoCD, Crossplane + XRDs, mgmt-leader-lease, controller-scaler, ESO, policies |
| **Phase 3** | CI Pipeline & GitOps Engine | Jenkins, pipeline config, webhooks, two-tier ApplicationSets, jira-bridge |
| **Phase 4** | Security, Secrets & Progressive Delivery | NamespaceVaultBinding, dual-write, Argo Rollouts, token rotation |
| **Phase 5** | Day-2 Operations & DR Validation | Observability, Velero, DR drills, seed job, catastrophic recovery test |

Each phase builds on the previous one. Phase 1 must complete before Phase 2 can start. Phases within a milestone can be parallelized where dependencies allow.
