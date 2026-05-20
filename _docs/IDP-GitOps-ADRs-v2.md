# Architecture Decision Records — IDP GitOps Blueprint — **v2**

**Companion to:** `IDP-GitOps-Blueprint-v2.md`
**Programme:** Internal Developer Platform on Azure AKS
**Owner:** Principal Enterprise Architect, Platform Engineering
**Document version:** 2.0 — 2026-05-19
**Originals (preserved):** `IDP-GitOps-Blueprint.md`, `IDP-GitOps-ADRs.md` — kept in repository for traceability.

This v2 pack:

- **Amends** ADR-001, ADR-003, ADR-004, ADR-005, ADR-006, ADR-010, ADR-013 — the original ADRs remain Accepted at v1; the amendments below are **superseding versions** marked `Accepted (supersedes v1)`. The originals retain status `Superseded by ADR-<n>-v2`.
- **Adds** ADR-016 through ADR-022 — new decisions surfaced by the audit.
- **Leaves untouched** ADR-002, ADR-007, ADR-008, ADR-009, ADR-011, ADR-012, ADR-014, ADR-015 — those decisions remain Accepted as written in v1.

## Index (v2)

| # | Title | Status |
|---|---|---|
| ADR-001-v2 | CI Engine — Jenkins (single-replica StatefulSet, mgmt-we only, RTO-tolerant) | Accepted (supersedes v1) |
| ADR-002 | Cloud Control Plane — Crossplane over Terraform/CAPZ | Accepted (unchanged) |
| ADR-003-v2 | Cluster Topology — Mgmt + Per-Env Workload + Seed; with Formal "Platform Component" Definition | Accepted (supersedes v1) |
| ADR-004-v2 | Multi-Region — Active-Passive Mgmt; Active-Active **Reads** + Active-Passive **Writes** for Data Plane | Accepted (supersedes v1) |
| ADR-005-v2 | Secret Management — ESO + Per-Region AKV Pair + Per-Namespace SecretStore | Accepted (supersedes v1) |
| ADR-006-v2 | Identity Model — Workload Identity for Azure; Vaulted Rotating Tokens for SaaS (Bitbucket, Jira) | Accepted (supersedes v1) |
| ADR-007 | GitOps Engine — ArgoCD with ApplicationSet | Accepted (unchanged) |
| ADR-008 | Supply Chain — Cosign + AKV + Kyverno | Accepted (unchanged) |
| ADR-009 | Network Posture — Private AKS + Private Endpoints + Hub-Spoke (SaaS via Firewall + WAF) | Accepted (unchanged in spirit; §1.4 of blueprint clarifies SaaS path) |
| ADR-010-v2 | Race-Free GitOps Update — Author Filter Primary; `[ci skip]` Defensive | Accepted (supersedes v1) |
| ADR-011 | Source of Truth — Monorepo `platform-gitops` | Accepted (unchanged) |
| ADR-012 | Drift Visibility — `argocd-jira-bridge` | Accepted (unchanged) |
| ADR-013-v2 | GitOps Layout — Two-Tier Applications (Infra + Workload); Sync Waves Within Each | Accepted (supersedes v1) |
| ADR-014 | CI Agents — Ephemeral Pods in `cipool` | Accepted (unchanged) |
| ADR-015 | Container Build Tool — Buildah Rootless | Accepted (unchanged) |
| **ADR-016** | **Platform Terminology — Canonical Glossary; "Workload" vs "Platform Component"** | **Accepted (new)** |
| **ADR-017** | **Management-Plane Active-Passive — Controllers Scaled to Zero in Standby Cluster** | **Accepted (new)** |
| **ADR-018** | **SLO Class Contract — Bronze/Silver/Gold as Availability Tiers; Cosmos Consistency Decoupled** | **Accepted (new)** |
| **ADR-019** | **Key Vault Topology — Per-Region Pair with Crossplane Dual-Write** | **Accepted (new)** |
| **ADR-020** | **Multi-Tenant Secret Isolation — Per-Namespace UAMI with Prefix-Scoped AKV RBAC** | **Accepted (new)** |
| **ADR-021** | **Progressive Delivery — Argo Rollouts Mandatory in Production** | **Accepted (new)** |
| **ADR-022** | **Management-Plane Singleton Lock — Azure Storage Blob Lease as Source of Truth** | **Accepted (new)** |

---

# ADR-001-v2: CI Engine — Jenkins (Single-Replica StatefulSet, mgmt-we Only, RTO-Tolerant)

**Status:** Accepted — supersedes ADR-001 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, Security Lead, SRE Lead

## Context

ADR-001 (v1) accepted Jenkins over Bamboo for failure-domain isolation from Atlassian SaaS, and described HA as "2 replicas + leader election via the kubernetes plugin." That HA pattern is incorrect — upstream Jenkins is a single-master architecture, the kubernetes plugin handles agent scheduling not controller election, and running two controllers against the same `$JENKINS_HOME` PVC causes split-brain.

ADR-001 (v1) also did not pin which mgmt cluster Jenkins runs in. Given Q11/Q16 resolutions (mgmt-ne is scale-to-zero standby; mgmt-leader-lease decides who is active), Jenkins's location and DR behavior need to be explicit.

## Decision

1. The choice of Jenkins over Bamboo stands (failure-domain isolation from Atlassian).
2. **Jenkins runs as a single-replica StatefulSet** in `mgmt-we` only.
3. PVC is ReadWriteOnce Azure Disk Premium SSD; Velero snapshots `$JENKINS_HOME` every 6h.
4. `cipool` node pool (taint `workload=ci:NoSchedule`) exists only in `mgmt-we`.
5. On `mgmt-we` loss, **CI is paused** until `mgmt-we` recovers (~30 min RTO via the DR runbook). Drift correction continues via `mgmt-ne` promotion.
6. RTO for a Jenkins controller pod restart (no cluster loss): ~60-90s.

## Why this works

Jenkins is the most write-side and most pause-tolerant of all platform components. A CI freeze during a regional disaster does not stop running workloads or ArgoCD reconciliation — exactly the failure-isolation property the v1 ADR established.

## Options Reconsidered

### Option A: Single-replica StatefulSet, mgmt-we only (chosen)
- Simple, well-trodden Kubernetes pattern; no false claims of HA.
- ~$0 standby cost (no idle Jenkins in mgmt-ne).
- CI pauses during DR; acceptable.

### Option B: Jenkins Operator (`jenkinsci/kubernetes-operator`)
- Faster pod restart (~30s vs 60-90s).
- Adds an operator to maintain.
- Same single-master model.

### Option C: Mirror Jenkins in mgmt-ne (scale-to-zero standby)
- DR RTO for CI drops from ~30min to ~5min.
- ~$50-100/month idle cost.
- Adds Velero restore step to the auto-promotion runbook.

### Option D: Two replicas with shared PVC (the v1 claim)
- Not supported by upstream Jenkins.
- Causes split-brain on the same `$JENKINS_HOME`.
- **Rejected.**

## Consequences

**Easier:**
- Operational model matches what Jenkins actually supports.
- No phantom HA story to debug during an incident.

**Harder:**
- CI is unavailable for ~30 minutes during a regional disaster — the team must accept and rehearse this.
- Annual DR game day must include a Jenkins Velero restore drill.

**Will need to revisit if:**
- Compliance requires sub-5-minute CI recovery (then move to Option C).
- Jenkins releases native multi-master support (unlikely).

## Action Items
1. [ ] Recreate Jenkins manifests as a single-replica StatefulSet.
2. [ ] Migrate `$JENKINS_HOME` from Azure Files RWX to Azure Disk RWO.
3. [ ] Schedule Velero `$JENKINS_HOME` snapshot every 6h.
4. [ ] Document the "CI paused during DR" expectation in the team handbook.
5. [ ] Add the Jenkins Velero restore step to the §7.3 DR runbook.

## Related ADRs
- ADR-014 (ephemeral pod agents) — unchanged.
- ADR-022 (singleton lock) — defines which cluster Jenkins (and everything else) considers "active."

---

# ADR-003-v2: Cluster Topology — Mgmt + Per-Env Workload + Seed; with Formal "Platform Component" Definition

**Status:** Accepted — supersedes ADR-003 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, SRE Lead

## Context

ADR-003 (v1) established the three-tier cluster topology and stated "the management cluster must not host any workload pods." The audit surfaced that "workload" was never formally defined, creating an ambiguity: Jenkins agents in `cipool` build customer code — are they workloads? The contradiction needs to be resolved at the definition layer.

## Decision

Adopt the cluster topology of v1 unchanged. Add a **formal definition** of "workload" vs "platform component" (see ADR-016) and **enforce it by Kyverno**:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: mgmt-only-platform
spec:
  validationFailureAction: Enforce
  background: false
  rules:
  - name: require-tier-platform
    match:
      any:
      - resources:
          kinds:
          - Pod
          namespaceSelector:
            matchExpressions:
            - key: kubernetes.io/metadata.name
              operator: NotIn
              values: [kube-system, kyverno]
        clusterSelector:
          matchLabels:
            role: management
    validate:
      message: "Pods on management clusters require namespace label tier=platform"
      deny:
        conditions:
          any:
          - key: "{{ request.object.metadata.namespace }}"
            operator: AnyNotIn
            value: "{{ namespaces | filter(@, 'tier', 'platform') }}"
```

Every namespace in a management cluster must carry `tier=platform`. Workload namespaces (which lack this label) cannot schedule pods on mgmt clusters.

## Options Reconsidered

(Same three options as v1; the only change is adding the precise definition + enforcement.)

## Consequences

**Easier:**
- Eliminates the lingering "is Jenkins a workload?" debate.
- Cross-team conversations use canonical terms (see ADR-016).

**Harder:**
- Adding a new platform component requires labeling its namespace correctly; a missing label is a surprising deployment failure during initial rollout.
- The Kyverno policy must be in the bootstrap manifests so mgmt rebuilds reapply it.

## Action Items
1. [ ] Apply the Kyverno policy to both mgmt clusters as a bootstrap manifest.
2. [ ] Update onboarding docs for adding new platform components.
3. [ ] Cross-reference with ADR-016 (canonical glossary).

## Related ADRs
- ADR-016 (canonical glossary) — defines the terms enforced here.
- ADR-022 (singleton lock) — uses the same "mgmt cluster" definition.

---

# ADR-004-v2: Multi-Region — Active-Passive Mgmt; Active-Active **Reads** + Active-Passive **Writes** for Data Plane

**Status:** Accepted — supersedes ADR-004 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, SRE Lead, Business Continuity Lead

## Context

ADR-004 (v1) promised an "Active-Active data plane." The audit found that §4.3's Cosmos composition sets `multipleWriteLocationsEnabled: false`, meaning writes are single-region. The v1 description was misleading.

## Decision

State precisely what is actually built:

- **Data plane reads:** Active-Active. Front Door routes user traffic to the geographically nearer region. Both `aks-prod-we` and `aks-prod-ne` serve read traffic continuously.
- **Data plane writes:** Active-Passive with auto-failover. Cosmos has a single write region (WE primary, NE failover priority 1). All writes traverse to the active write region. On WE-region loss, Cosmos auto-promotes NE to primary (RTO ~30-60s for forced failover).
- **Management plane:** Active-Passive with Azure Storage Blob Lease arbitration (ADR-022). RTO ~120s for automatic promotion.

`multipleWriteLocationsEnabled` remains `false` for Cosmos because:
- Most workloads do not require sub-100ms writes from the secondary region.
- Multi-master Cosmos requires conflict-aware app design (~weeks of dev effort per service).
- Cost uplift is ~25% RU pricing.

If a future workload genuinely needs multi-master Cosmos, it gets its own dedicated XRC variant — not a flag flipped globally.

## Options Reconsidered

### Option A: Active-Active reads + Active-Passive writes (chosen) — honest naming, no app changes
### Option B: Multi-master Cosmos — requires app-side conflict awareness; out of scope for v2
### Option C: Single-region — defeats second-region investment

## Consequences

**Easier:**
- Architecture descriptions are accurate; design reviewers don't get surprised.
- No app-team education burden around conflict resolution.
- DR runbook is shorter — write-region failover is automatic.

**Harder:**
- App designers in NE-region pods must tolerate ~30-60ms cross-region write latency during normal operation. This is documented in the SLO contract (ADR-018).
- "Active-Active" is no longer a marketing-grade term; technical reviewers expect the qualification.

**Will need to revisit if:**
- A specific workload needs sub-100ms writes from NE — then add a new SLO tier "Gold-Plus" with multi-master Cosmos.

## Action Items
1. [ ] Update all blueprint references to "Active-Active data plane" with the explicit reads/writes qualification.
2. [ ] Add the cross-region write latency note to the SLO contract.
3. [ ] DR runbook: document Cosmos auto-failover step.

## Related ADRs
- ADR-018 (SLO contract) — `consistency` is now a separate claim parameter.
- ADR-019 (per-region AKVs) — symmetric pattern for secret storage.
- ADR-022 (lease lock) — mgmt-plane active-passive mechanism.

---

# ADR-005-v2: Secret Management — ESO + Per-Region AKV Pair + Per-Namespace SecretStore

**Status:** Accepted — supersedes ADR-005 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Platform Lead

## Context

ADR-005 (v1) chose ESO + Azure Key Vault. The audit surfaced two issues:

1. **AKV scope:** v1 implied a single AKV (`kv-platform-prod.vault.azure.net`). A single regional resource is a single failure domain — contradicts ADR-004's multi-region promise.
2. **Multi-tenant isolation:** v1 used `ClusterSecretStore`, giving every namespace cluster-wide access to any AKV secret name, gated only by cluster-wide UAMI RBAC.

## Decision

Two changes to v1:

1. **Per-region AKV pair** (one per region per env): `kv-platform-<env>-we` and `kv-platform-<env>-ne`. Crossplane Compositions dual-write to both. ESO `SecretStore` in each workload cluster reads from the local regional vault. AKV is in RBAC mode (`enableRbacAuthorization: true`) to support ABAC conditions.
2. **Per-namespace `SecretStore`** (not `ClusterSecretStore`). Each namespace has its own UAMI, federated to its own `eso-sa` ServiceAccount, granted `Key Vault Secrets User` on the local vault **conditioned** on secret name matching `<namespace>-*`.

This pattern is materialized by a new Composition `XNamespaceVaultBinding` (see blueprint §4.6), invoked once per (namespace, cluster) pair.

## Options Reconsidered

Same options as v1 (ESO+AKV vs CSI Driver vs Vault vs SealedSecrets). ESO+AKV remains the chosen tool. The new dimensions:

| Dimension | v1 (single vault, ClusterSecretStore) | v2 (per-region pair, per-namespace SecretStore) |
|---|---|---|
| Regional failure isolation | Poor | Strong |
| Multi-tenant isolation | Cluster-wide trust | IAM-level scoping per namespace |
| Operational overhead | Low | Medium (automated by Composition) |
| AKV cost | $1× | $2× (both vaults) |
| Compliance posture | Adequate | Strong (per-namespace audit trail in AKV logs) |

## Consequences

**Easier:**
- Audit logs in AKV attribute reads to specific namespace UAMIs.
- Regional failures don't take down secret access.
- "Secret leakage" is now an IAM problem, not a controller-trust problem.

**Harder:**
- Two AKVs to back up, monitor, RBAC.
- Composition complexity (every secret-producing XR has two writers).
- AKV ABAC requires careful condition expressions; testing is important.

## Action Items
1. [ ] Stand up per-region AKV pairs per environment.
2. [ ] Migrate any in-flight `ClusterSecretStore` to per-namespace `SecretStore`.
3. [ ] Author and apply the `XNamespaceVaultBinding` XRD/Composition (ADR-020).
4. [ ] Update §5 of the blueprint and all ExternalSecret examples.

## Related ADRs
- ADR-019 (Key Vault topology) — formalizes the per-region pair as its own ADR.
- ADR-020 (per-namespace isolation) — formalizes the per-namespace SecretStore as its own ADR.
- ADR-006-v2 (identity model) — provides the UAMIs used here.

---

# ADR-006-v2: Identity Model — Workload Identity for Azure; Vaulted Rotating Tokens for SaaS

**Status:** Accepted — supersedes ADR-006 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, Identity Lead

## Context

ADR-006 (v1) declared "no client secrets exist anywhere." The audit found counter-examples: PlatformBot's Bitbucket app password and the Jira service-account API token. These are unavoidable because Bitbucket Cloud and Jira Cloud don't accept federated tokens inbound. The aspirational claim invited bypass.

## Decision

Narrow ADR-006 to:

> **Workload Identity Federation is mandatory for every cluster ↔ Azure authentication path.** No service-principal client secrets, no static service-principal certificates, no client-credentials-in-Secret patterns exist for any Azure API call.
>
> **Residual static credentials** exist only for cluster ↔ SaaS APIs (Bitbucket Cloud, Jira Cloud). These are:
>
> - **Minimized** — scoped to the least permissions possible (workspace-level access tokens, not user app passwords; service-account API tokens scoped to a single Jira project).
> - **Vaulted** — stored exclusively in AKV; never in Jenkins credentials store, never in environment files, never in Git.
> - **Operator-delivered** — consumed by Jenkins (and any other client) via ESO + Reloader, the same pipeline as application connection strings.
> - **Rotated quarterly** — automated by a `saas-token-rotator` CronJob that mints a new token via the SaaS API, writes to AKV, and revokes the old after 24h.

## Options Reconsidered

| Option | Verdict |
|---|---|
| A — Narrow scope; vault-and-rotate SaaS tokens (chosen) | Pragmatic; honest |
| B — Use OAuth 2.0 client_credentials | Still has client_secret; marginal improvement |
| C — Keep the aspirational claim; document violations | Worst — invites bypass |

## Consequences

**Easier:**
- Honest. Architecture document says what is actually built.
- SaaS tokens get the same lifecycle treatment as cloud connection strings.
- Audit trail is unified (AKV access logs).

**Harder:**
- A new component (`saas-token-rotator` with per-token CronJob configs) to operate.
- Annual review of "should this token still exist? still have these scopes?"

## Action Items
1. [ ] Replace `bitbucket-app-password` with a workspace access token; store in AKV.
2. [ ] Replace user Jira token with service-account token; store in AKV.
3. [ ] Implement `saas-token-rotator` CronJob (one binary, two configs).
4. [ ] Add Prometheus metric `saas_token_age_days` and alert at >100d.

## Related ADRs
- ADR-005-v2 (per-namespace AKV access) — same pipeline used here.
- ADR-021 (Argo Rollouts) — unrelated, but cross-references for "platform-managed tokens."

---

# ADR-010-v2: Race-Free GitOps Update — Author Filter Primary; `[ci skip]` Defensive

**Status:** Accepted — supersedes ADR-010 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

ADR-010 (v1) layered two mechanisms — `[ci skip]` marker and author filter — but didn't say which was authoritative. Removing either could re-introduce the loop. The contract must be explicit.

## Decision

> **Loop-prevention contract:**
>
> - **Primary mechanism:** the author filter on the Jenkins multibranch Bitbucket Branch Source trait (`^(?!PlatformBot).*$`). Commits authored by `PlatformBot` (the Bitbucket workspace service account) never trigger a Jenkins build. This is the authoritative gate.
> - **Defensive mechanism:** the `[ci skip]` marker in the commit message. Provided for forward-compatibility with any future CI tool addition (a second Jenkins instance, a Bitbucket Pipelines lint job, a GitHub Actions mirror). Not relied on by the current Jenkins setup.
>
> **Removing either layer requires a new ADR.**

## Consequences

**Easier:**
- Future tool additions inherit skip behavior automatically.
- Operators reading a CI commit know it won't trigger a build, regardless of which CI runs.

**Harder:**
- Pipeline code must remember to include the marker (linted in CI).

## Action Items
1. [ ] Add a lint check that fails the Jenkinsfile if the GitOps commit message lacks `[ci skip]`.
2. [ ] Document the contract in `CONTRIBUTING.md` of the `platform-gitops` repo.

## Related ADRs
- ADR-001-v2 (Jenkins) — the runtime that hosts the author filter.
- ADR-011 (monorepo) — the GitOps repo this protocol writes to.

---

# ADR-013-v2: GitOps Layout — Two-Tier Applications (Infra + Workload); Sync Waves Within Each

**Status:** Accepted — supersedes ADR-013 (v1)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

ADR-013 (v1) put Crossplane XRCs and workload manifests under the same ArgoCD Application with sync waves 0 → 5. The retry budget (`maxDuration: 5m`) was shorter than Azure provisioning times (5–25 min). More fundamentally, infrastructure and workload have different lifecycles: SQL is provisioned once per env and lives for years; the workload image rolls forward dozens of times per week.

## Decision

Split each service into **two ApplicationSet-generated Applications**:

| Tier | Path in repo | Application name | Sync retry | Lifecycle |
|---|---|---|---|---|
| Infra | `apps/<svc>/infra/overlays/<env>/` | `<svc>-infra-<env>` | `maxDuration: 30m, limit: 10` | Slow; rare changes |
| Workload | `apps/<svc>/workload/overlays/<env>/` | `<svc>-app-<env>-<cluster>` | `maxDuration: 5m, limit: 5` | Fast; per-image-bump |

The workload Application is gated on the infra Application's health (via an admission-webhook-injected annotation or ArgoCD's native dependencies field where available).

Sync waves continue to operate **within** each Application:
- Infra waves: 0 (NamespaceVaultBinding) → 1 (SQL/Cosmos/SB/DNS XRCs).
- Workload waves: -1 (NetworkPolicy/RBAC) → 0 (ExternalSecret) → 1 (CM/SA) → 2 (Rollout/Service) → 3 (HPA/PDB) → 4 (Ingress).

Custom `health.lua` for Crossplane XR kinds remains the gating mechanism within the infra Application.

## Options Reconsidered

### Option A: Extend timeouts; keep one Application
- Cheapest fix; doesn't address the lifecycle mismatch.

### Option B: Two-tier ApplicationSet (chosen)
- Decouples lifecycles cleanly.
- After steady state, 99% of deploys touch only the workload tier and complete in <60s.
- Infra changes are rare and take their time without blocking app deploys.

### Option C: Apply everything in parallel; let pods CrashLoopBackOff
- Ambiguous health signal; noisy alerts during first sync.

## Consequences

**Easier:**
- Image bumps deploy in seconds.
- Cloud-resource changes get the time they need without timing out.
- Production change-management can require an extra approval on `infra/` PRs while leaving `workload/` to the team's normal review.

**Harder:**
- Two Applications per service in ArgoCD UI; operators must understand the relationship.
- Cross-tier dependency wiring (workload waits for infra) needs implementing.
- Repo layout has more depth; templates need updating.

**Will need to revisit if:**
- ArgoCD adds a native multi-source Application with cross-source ordering primitives.

## Action Items
1. [ ] Restructure repo layout: introduce `apps/<svc>/{infra,workload}/{base,overlays/<env>}/`.
2. [ ] Author `platform-infra-set` and `workloads-set` ApplicationSets.
3. [ ] Implement the cross-tier dependency mechanism.
4. [ ] Migrate `payments` service first as the reference; document migration steps.

## Related ADRs
- ADR-007 (ArgoCD) — provides ApplicationSet primitives.
- ADR-021 (Argo Rollouts) — sits inside the workload tier.

---

# ADR-016: Platform Terminology — Canonical Glossary; "Workload" vs "Platform Component"

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead

## Context

V1 used several terms (workload, platform component, management cluster, control plane, data plane) loosely. This caused real ambiguity: §1.3 forbade "workloads" on the management cluster but Jenkins agents (which build customer code) ran there. Future readers will continue to hit this confusion unless the terms are pinned.

## Decision

Adopt a canonical glossary (full text in §0.1 of the v2 blueprint). Key definitions:

- **Workload** — any application serving customer/business traffic. Lives in workload clusters only.
- **Platform component** — any service supporting the developer or operator experience (ArgoCD, Crossplane, ESO, Jenkins controller and agents, Velero, observability stack, lease/scaler controllers, jira-bridge). Lives in management clusters only.
- **Workload-cluster operator** — a small set of controllers that run locally in workload clusters to support workloads (ESO agent, Reloader, Argo Rollouts controller, Kyverno). These are NOT "workloads" but are NOT "platform components" either — they are local operators.
- **Management cluster** — `mgmt-<region>`. `tier=platform` required on every namespace, enforced by Kyverno.
- **Workload cluster** — `aks-<env>-<region>`.
- **Active management cluster** — the one currently holding the Azure Storage Blob Lease (exactly one at a time).

## Options Considered

| Option | Why |
|---|---|
| A — Formal glossary + Kyverno enforcement (chosen) | Definitions become enforceable, not just documentation |
| B — Documentation only | Words drift; ambiguities re-emerge |
| C — Per-service glossary | Inconsistency across services |

## Consequences

**Easier:**
- All future ADRs, runbooks, and diagrams use the same words.
- Onboarding new platform engineers is faster.

**Harder:**
- New platform components must be labeled correctly at creation; misses cause Kyverno denials.

## Action Items
1. [ ] Publish §0 of the v2 blueprint as the canonical glossary; reference from every other doc.
2. [ ] Add a Kyverno policy that warns on documents using "workload" outside the canonical meaning (best-effort).

## Related ADRs
- ADR-003-v2 (cluster topology) — operationalizes these terms.

---

# ADR-017: Management-Plane Active-Passive — Controllers Scaled to Zero in Standby Cluster

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, SRE Lead

## Context

V1 referred to mgmt-ne as having "Crossplane (read-only mode)" — not a real Crossplane mode. Two patterns actually exist: `managementPolicies: ["Observe"]` (per-resource, granular but invasive) and **scale to zero** of provider controllers (cluster-wide, simple). The standby mechanism must be precisely defined.

## Decision

In the standby management cluster (`mgmt-ne` during normal operation):

- All Crossplane providers are scaled to 0 replicas.
- All ArgoCD controllers (application-controller, server, repo-server) are scaled to 0 replicas.
- ESO controller is scaled to 0 replicas.
- `argocd-jira-bridge` is scaled to 0 replicas.

Note: **Argo Rollouts controllers run in workload clusters, not management clusters** (see ADR-021). They are not managed by `controller-scaler`.

Scaling is driven by `controller-scaler` (see ADR-022), which reacts to the lease state. This is fully automated.

The `controller-scaler` also updates the `lease-status` label on the cluster's ArgoCD Secret (`lease-status: active` or `lease-status: standby`). A platform-components ApplicationSet uses this label to gate deployment of platform-level resources (XRCs, ProviderConfigs) to only the active management cluster.

## Options Considered

### Option A: Scale controllers to 0 in standby (chosen) — simple, atomic, fast to flip
### Option B: `managementPolicies: ["Observe"]` cluster-wide — invasive; doubles ARM read load
### Option C: Don't install in standby — requires helm install during DR; adds ~5min RTO

## Consequences

**Easier:**
- DR promotion is a controller-scale operation, taking ~120s total.
- No state cleanup needed during failback.
- Standby cost is rounding error (idle CRDs + 0-replica deployments).

**Harder:**
- Operators must remember not to manually scale controllers in the standby cluster.
- Lease loss → scale to 0 is automatic; a bug in `controller-scaler` could cause split-brain. Hence ADR-022's lease being the *authoritative* source.

## Action Items
1. [ ] Author `controller-scaler` Go binary; deploy in both mgmt clusters.
2. [ ] Configure standby state for all platform components.
3. [ ] DR rehearsal: validate scale-up RTO ≤ 120s.

## Related ADRs
- ADR-022 (lease lock) — provides the trigger for scale-up/down.
- ADR-004-v2 (multi-region) — the broader strategy this implements.

---

# ADR-018: SLO Class Contract — Bronze/Silver/Gold as Availability Tiers; Cosmos Consistency Decoupled

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, SRE Lead

## Context

V1's `sloClass` parameter (bronze/silver/gold) meant different things in different XRDs — SKU tier for SQL/Service Bus, *consistency level* for Cosmos. Consumers didn't have a clear mental model. Worse, Cosmos consistency is an app-correctness choice, not a reliability one — coupling them was a category error.

## Decision

Define a single platform-wide SLO contract. Bronze/silver/gold are **availability tiers** with concrete RTO/RPO targets:

| Class | Availability | RTO | RPO | Use case | Cost |
|---|---|---|---|---|---|
| Bronze | 99.5% | ≤ 4h | ≤ 1h | Dev, sandbox | 1× |
| Silver | 99.9% | ≤ 15m | ≤ 5m | Staging, internal prod | 3× |
| Gold | 99.95%+ | ≤ 1m | ≤ 30s | Revenue-critical prod | 8-10× |

Each XRD's Composition translates the class to SKU/posture choices independently. The translation table is published as `docs/slo-contract.md` so claim authors can reason about what they'll get.

**Cosmos `consistency` becomes its own claim parameter** (`eventual | session | bounded | strong`), defaulting to `session`. SLO class controls geo-replica + failover + backup posture; consistency controls correctness semantics.

**Progressive delivery is also driven by SLO class:**
- Bronze: no canary (direct cutover).
- Silver: 25% / 100% canary with success-rate analysis.
- Gold: 5% / 25% / 50% / 100% canary with success-rate + p99-latency analysis.

(See ADR-021.)

## Options Considered

| Option | Verdict |
|---|---|
| A — Availability tier + decoupled correctness params (chosen) | Honest; matches user mental model |
| B — Per-axis params (`availability:`, `replication:`, `consistency:`) | Too low-level; loses the menu UX |
| C — Per-XRD SLO classes | Loses the cross-service consistency |

## Consequences

**Easier:**
- Service teams pick one SLO class and understand the implications.
- Platform engineers can audit "all gold services" with a simple label query.
- SLO class is the natural input to incident-response prioritization.

**Harder:**
- Composition mapping tables must be kept in sync with the published contract.
- Apps that want unusual combinations (gold availability + eventual consistency) must learn the separate parameters.

## Action Items
1. [ ] Publish `docs/slo-contract.md` as the authoritative reference.
2. [ ] Update all XRD schemas and Compositions to honor the new contract.
3. [ ] Decouple Cosmos `consistency` from `sloClass`.
4. [ ] Tag every existing service in Jira with an SLO class (catch-up).

## Related ADRs
- ADR-021 (Argo Rollouts) — uses SLO class to choose canary strategy.

---

# ADR-019: Key Vault Topology — Per-Region Pair with Crossplane Dual-Write

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead, SRE Lead

## Context

A single regional AKV is a single failure domain. ADR-004-v2's multi-region promise is broken if NE-region workloads must reach a WE-only AKV for every secret refresh.

## Decision

Two AKVs per environment: `kv-platform-<env>-we` and `kv-platform-<env>-ne`. Each has a private endpoint in the corresponding hub. Each is in **RBAC mode** (`enableRbacAuthorization: true`) so per-namespace ABAC conditions work (ADR-020).

Crossplane Compositions emit **two** `keyvault.azure.upbound.io Secret` resources per managed secret — one targeting the WE vault, one targeting the NE vault. The two resources are selected by label `region: westeurope` vs `region: northeurope`. Writes are idempotent; ARM 429s are retried with exponential backoff; if one vault is unreachable, the other still receives the update and the failed write is retried on next reconcile.

ESO in each workload cluster's `SecretStore` points to the local regional vault. Cross-region calls happen only during DR or unusual configurations.

## Options Considered

| Option | Verdict |
|---|---|
| A — Per-region pair with dual-write (chosen) | True regional independence |
| B — Single vault + Azure Backup | Hours-long RTO; fails multi-region promise |
| C — Single vault, accept regional dependency | Defeats ADR-004-v2 |

## Consequences

**Easier:**
- Multi-region failover (Cosmos + mgmt + AKV) is symmetric.
- Regional data-residency commitments can be honored at the vault layer.
- ESO does no cross-region calls in normal operation.

**Harder:**
- Two vaults per env to back up and monitor.
- Brief skew possible during transient ARM issues; the `PerRegionAKVDualWriteSkew` alert (§7.1) catches sustained skew >10min.
- Cost ~2× AKV (negligible at platform scale).

## Action Items
1. [ ] Provision per-env AKV pairs via Crossplane.
2. [ ] Update every Composition that writes secrets to emit dual resources.
3. [ ] Add `PerRegionAKVDualWriteSkew` Prometheus alert.

## Related ADRs
- ADR-004-v2 (multi-region) — broader symmetry.
- ADR-005-v2 (secret management) — consumer-side pattern.
- ADR-020 (per-namespace isolation) — RBAC mode is a prerequisite.

---

# ADR-020: Multi-Tenant Secret Isolation — Per-Namespace UAMI with Prefix-Scoped AKV RBAC

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Security Lead

## Context

V1's `ClusterSecretStore` gave every namespace in a workload cluster access to every AKV secret name, gated only by the cluster's single UAMI. A compromised namespace with permission to create `ExternalSecret` resources could read any team's secrets.

## Decision

Three structural changes:

1. **Per-namespace `SecretStore`** (not `ClusterSecretStore`). Lives in the namespace; reference is local.
2. **Per-namespace UAMI** (`uami-<namespace>-<cluster>`), federated to the namespace's `eso-sa` ServiceAccount.
3. **Prefix-scoped AKV RBAC** using ABAC conditions on the `Key Vault Secrets User` role. Secret reads succeed only when the secret name matches `<namespace>-*`.

All three are materialized atomically by a `NamespaceVaultBinding` XRC (see blueprint §4.6), generated by the `namespace-vault-bindings-set` ApplicationSet from a curated list in `platform/namespaces.yaml`.

## ABAC Condition Example

```
(
  !(ActionMatches{'Microsoft.KeyVault/vaults/secrets/getSecret/action'})
  OR
  @Resource[Microsoft.KeyVault/vaults/secrets:Name] LIKE 'payments-*'
)
```

A compromised `payments` namespace cannot read `billing-sql-conn` — the ABAC condition denies the read at the AKV side, regardless of what `ExternalSecret` the namespace creates.

## Options Considered

| Option | Verdict |
|---|---|
| A — Per-namespace SecretStore + UAMI + ABAC (chosen) | Defense in depth at IAM layer |
| B — ClusterSecretStore + Kyverno admission policy | Single-layer defense; controller failures leak |
| C — Status quo (ClusterSecretStore + broad RBAC) | Fails zero-trust |

## Consequences

**Easier:**
- AKV audit logs attribute reads to specific namespaces.
- A compromised namespace's blast radius is its own secrets only.
- Compliance reviews pass cleanly.

**Harder:**
- More identities (one UAMI per namespace per cluster).
- Onboarding a new namespace requires the Composition to run before secrets become accessible (handled by ApplicationSet automation).
- ABAC syntax has a learning curve; needs unit tests.

## Action Items
1. [ ] Author `XNamespaceVaultBinding` XRD + Composition.
2. [ ] Create `namespace-vault-bindings-set` ApplicationSet driven by `platform/namespaces.yaml`.
3. [ ] Migrate existing services from ClusterSecretStore → per-namespace SecretStore.
4. [ ] Document the ABAC condition pattern with examples.

## Related ADRs
- ADR-005-v2 (secret management) — consumer-side ESO pattern.
- ADR-019 (per-region AKVs) — provides the RBAC-mode AKVs this depends on.

---

# ADR-021: Progressive Delivery — Argo Rollouts Mandatory in Production

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, Platform Lead, SRE Lead

## Context

V1 referenced Argo Rollouts in §6.4 but did not formally adopt it. Without progressive delivery, a bad image deployed to production exposes 100% of traffic within ~30 seconds. The gold SLO class (99.95% availability) cannot be honored under this rollout model — a single bad deploy per quarter exhausts the entire error budget.

## Decision

Adopt **Argo Rollouts** as the platform default for production workloads.

- All workloads in `aks-prod-we` and `aks-prod-ne` must deploy as `Rollout` (not `Deployment`).
- A Kyverno policy `KyvernoProdRequiresRollout` rejects `Deployment` resources in any namespace residing on a prod cluster.
- Dev and staging may use either `Deployment` or `Rollout`.
- Canary strategy defaults to platform-issued templates per SLO class:
  - Bronze: single step (effectively direct cutover, but using the Rollout primitive for uniformity).
  - Silver: 25% / 100% with success-rate analysis.
  - Gold: 5% / 25% / 50% / 100% with success-rate + p99-latency analysis.
- AnalysisTemplate is **synthesized** by the `NamespaceRolloutPolicy` Composition (separate from `NamespaceVaultBinding`) based on the service's SLO class — service teams don't author their own analysis logic.
- Failed analysis aborts the rollout (canary ReplicaSet scaled to 0); the stable ReplicaSet stays at 100%.

Traffic shifting uses NGINX Ingress Controller — no service mesh requirement.

## Options Considered

| Option | Verdict |
|---|---|
| A — Argo Rollouts mandatory for prod (chosen) | Honors SLOs; same ecosystem as ArgoCD |
| B — Manual rollback via revisionHistoryLimit | Doesn't meet 99.95% SLO under realistic failure rates |
| C — Flagger + service mesh | Requires adopting a mesh — out of scope |

## Consequences

**Easier:**
- Gold SLOs become achievable.
- Service teams don't write canary logic; the platform provides it.
- Rollback is automatic on analysis failure.

**Harder:**
- Rollout pod-template syntax differs slightly from Deployment (TemplateRefs, etc.) — onboarding doc needed.
- AnalysisTemplate Prometheus queries must match the actual metric labels apps emit; the platform's scaffolding template emits them automatically.
- Mid-rollout incidents (analysis flapping) require operator triage; runbook included.

**Will need to revisit if:**
- A service mesh becomes platform-standard (then consider Flagger).

## Action Items
1. [ ] Install Argo Rollouts controller in every workload cluster (via bootstrap ApplicationSet). Note: Rollouts runs locally in workload clusters, NOT in the management cluster.
2. [ ] Apply Kyverno policy `KyvernoProdRequiresRollout`.
3. [ ] Update Cookiecutter templates: prod overlay generates `Rollout`; dev/staging optional.
4. [ ] Author AnalysisTemplate generators in the `NamespaceRolloutPolicy` Composition (separate from NamespaceVaultBinding).
5. [ ] Migrate existing prod workloads one service at a time.

## Related ADRs
- ADR-018 (SLO contract) — drives canary strategy choice.
- ADR-007 (ArgoCD) — Rollouts is a complement, not a replacement.

---

# ADR-022: Management-Plane Singleton Lock — Azure Storage Blob Lease as Source of Truth

**Status:** Accepted (new)
**Date:** 2026-05-19
**Deciders:** Principal Architect, SRE Lead, Security Lead

## Context

ADR-017 establishes that the standby mgmt cluster has all controllers scaled to 0. ADR-004-v2 promises ~120s mgmt-plane failover. But there's a split-brain risk: under partition, both clusters could believe they're active, and operators could mis-flip labels under incident stress. Two active Crossplane controllers cause ARM thrash; two active ArgoCD instances cause sync races; two active ESOs cause AKV write contention.

Operator discipline alone is not enough.

## Decision

Adopt a **singleton lock** mechanism:

- A small in-house Go controller, **`mgmt-leader-lease`**, runs in every mgmt cluster.
- It acquires and continuously renews an **Azure Storage Blob Lease** (60s TTL, renewed every 15s) on a dedicated, geo-redundant storage account `stplatleaseplat<random>/leases/mgmt-active`.
- The lease holder writes its identity to ConfigMap `mgmt-leader-status` in `kube-system`.
- A second controller, **`controller-scaler`**, watches that ConfigMap and reconciles all platform controllers' replica counts: scale up when the local cluster holds the lease; scale to 0 otherwise.
- Failback (planned operator-initiated promotion) is via a CLI `mgmt-cli failback --to <cluster> --confirm` that breaks the lease.

The lease blob is the **authoritative** source of "who is active." Cluster labels (`lease-status=active|standby`) remain as operational aids but are not authoritative — the lease is.

## Behavior Across Failure Modes

| Scenario | Outcome |
|---|---|
| mgmt-we hard crash | Lease expires in ≤65s; mgmt-ne acquires; controllers scale up; total RTO ~120s |
| Network partition between mgmt-we and Azure Storage | mgmt-we cannot renew; lease expires; mgmt-ne acquires; mgmt-we's controllers scale to 0 |
| Both mgmt clusters can't reach Storage | Neither renews; both scale to 0; control plane frozen; workloads keep running; operator restores connectivity |
| Operator failback | `mgmt-cli failback` breaks the lease; target cluster acquires; previous holder's controllers scale to 0 |
| Storage account itself fails | GRS replica becomes primary; lease may briefly be unavailable; controllers may flap; alert fires; operator intervenes |

## Options Considered

| Option | Verdict |
|---|---|
| A — Azure Storage Blob Lease singleton (chosen) | Battle-tested pattern; simple |
| B — Label-based; operator discipline | Fragile under stress |
| C — Etcd-style quorum (3-cluster) | Overkill for 2 voters |

## Implementation Notes

- The lease blob storage account uses a private endpoint reachable from both regions via VNet peering.
- The Storage Account has soft-delete on for the lease blob (recovery if accidentally deleted).
- `mgmt-leader-lease` has a hardcoded 1-replica Deployment (no HA) — if it crashes, the lease expires in 60s and another cluster takes over. Self-healing by design.
- `controller-scaler` similarly runs as a 1-replica Deployment.
- Both controllers' code is under platform-engineering ownership; ~150 lines of Go each.

## Consequences

**Easier:**
- DR auto-promotion is fully automated, with a documented RTO of ~120s.
- Split-brain is impossible by construction.
- Operator failback is one CLI call.
- Failback procedure tested on every release.

**Harder:**
- Two new in-house controllers to maintain.
- The lease blob is itself a dependency; a Storage Account outage freezes the control plane (but not workloads).
- Quarterly DR drill must exercise both paths (auto-promotion and operator failback).

**Will need to revisit if:**
- A cloud-managed singleton primitive becomes available (e.g., Azure Frontdoor with active-active control planes).
- The platform grows to 3+ mgmt regions (then quorum, not lease).

## Action Items
1. [ ] Implement `mgmt-leader-lease` (single Go binary; takes Storage Account FQDN as arg).
2. [ ] Implement `controller-scaler` (Go binary; uses Kubernetes informers).
3. [ ] Provision the lease Storage Account via Crossplane (with private endpoint).
4. [ ] Author the `mgmt-cli failback` CLI binary.
5. [ ] Quarterly DR drill schedule: rotate between simulated WE crash, simulated NE crash, planned failback.
6. [ ] Prometheus alerts: `MgmtLeaderLeaseLost`, `ControllerScalerLagging`.

## Related ADRs
- ADR-004-v2 (multi-region) — operationalizes the active-passive promise.
- ADR-017 (controllers scaled to 0 in standby) — the lever this lock controls.

---

## Appendix — Maintenance Notes for the v2 Pack

- Originals remain at `IDP-GitOps-ADRs.md` and `IDP-GitOps-Blueprint.md`. They are marked `Superseded by v2` in the index but are not deleted — they record what was decided at v1 and why it changed.
- The seven new ADRs (016–022) follow the same template as v1: Context → Decision → Options → Trade-off → Consequences → Action Items → Related ADRs.
- Each `-v2` ADR cross-references the original it supersedes; readers can trace the evolution.
- Future ADRs continue the sequence: ADR-023, ADR-024, etc. Number reuse is forbidden.
- The decision ledger from the audit (Q1–Q17 → resolutions) is preserved in the blueprint's changelog for traceability.

*End of v2 ADR pack.*
