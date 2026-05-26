# Architecture Decision Records — IDP GitOps Blueprint — **v2**

**Companion to:** `IDP-GitOps-Blueprint-v2.md`
**Programme:** Internal Developer Platform on Azure AKS
**Owner:** Principal Enterprise Architect, Platform Engineering
**Document version:** 2.0 — 2026-05-19
**Originals (preserved):** `IDP-GitOps-ADRs.md` — kept in repository for traceability.

This v2 pack:

- **Amends** ADR-001, ADR-003, ADR-004, ADR-005, ADR-006, ADR-008, ADR-010, ADR-013 — the original ADRs remain Accepted at v1; the amendments below are **superseding versions** marked `Accepted (supersedes v1)`. The originals retain status `Superseded by ADR-<n>-v2`.
- **Adds** ADR-016 through ADR-022 — new decisions surfaced by the audit.
- **Leaves untouched** ADR-002, ADR-007, ADR-009, ADR-011, ADR-012, ADR-014, ADR-015 — those decisions remain Accepted as written in v1.

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
| ADR-008-v2 | Supply Chain — Cosign + AKV + Kyverno Verification (enforcement changed from Gatekeeper) | Accepted (supersedes v1) |
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
- ADR-031-v4 (PRD-v4) — codifies the data shape of the cluster topology described here.

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

# ADR-008-v2: Supply Chain — Cosign Image Signing with AKV-Backed Key + Kyverno Verification

**Status:** Accepted — supersedes ADR-008 (v1)
**Date:** 2026-05-20
**Deciders:** Principal Architect, Security Lead, Platform Lead

## Context

ADR-008 (v1) chose Cosign + AKV for image signing (correct) and OPA/Gatekeeper with a `K8sRequiredCosignSignature` constraint template for admission enforcement. The v2 audit standardized on **Kyverno** as the sole policy engine (ADR-003-v2, ADR-016, ADR-021). Running both Gatekeeper and Kyverno simultaneously increases operational surface, conflicts on admission control, and contradicts the platform's "one tool per concern" principle.

## Decision

The signing side is unchanged from v1:

- **Cosign** signs every image at CI time using an **AKV-backed key** (HSM if compliance demands).
- `cosign attest --type spdx` attaches an SBOM attestation alongside the signature.
- The signing key is stored in `kv-platform-prod-we:cosign-signing-key`.

The enforcement side changes from Gatekeeper to **Kyverno**:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-cosign-signature
spec:
  validationFailureAction: Enforce
  background: true
  rules:
  - name: verify-image-signature
    match:
      any:
      - resources:
          kinds:
          - Pod
        clusterSelector:
          matchLabels:
            role: workload
    exclude:
      any:
      - resources:
          namespaces:
          - kube-system
          - kyverno
          - external-secrets
          - argo-rollouts
    verifyImages:
    - imageReferences:
      - "acrplatformprod.azurecr.io/*"
      attestors:
      - entries:
        - keys:
            publicKeys: |-
              -----BEGIN PUBLIC KEY-----
              {{ fetched from AKV at deploy time via ExternalSecret }}
              -----END PUBLIC KEY-----
      mutateDigest: true
      verifyDigest: true
      required: true
```

- **Production and staging clusters:** `validationFailureAction: Enforce` — unsigned images are rejected.
- **Dev clusters:** `validationFailureAction: Audit` — warn but don't block during initial rollout.
- System namespaces (`kube-system`, `kyverno`, `external-secrets`, `argo-rollouts`) are excluded since their images are platform-managed.

## Options Reconsidered

| Option | Verdict |
|---|---|
| A — Kyverno `verifyImages` (chosen) | Native Cosign verification in Kyverno; no separate tool |
| B — Keep Gatekeeper alongside Kyverno | Two admission controllers; conflict risk; rejected by v2 standardization |
| C — Connaisseur (dedicated image-verification admission controller) | Extra operator; Kyverno does it natively |

## Consequences

**Easier:**
- Single policy engine (Kyverno) handles all admission: label enforcement, Rollout-only-in-prod, AND image verification.
- `verifyImages` is a first-class Kyverno feature with built-in Cosign support — no external webhook or constraint template.
- Policy management is uniform across all concerns.

**Harder:**
- Kyverno's `verifyImages` adds latency to pod admission (~200-300ms for signature verification).
- Public key distribution requires an ExternalSecret to pull the Cosign public key from AKV into a ConfigMap/Secret referenced by the policy.

**Will need to revisit if:**
- Kyverno drops `verifyImages` support (unlikely — it's a headline feature).
- Notary v2 becomes the standard and Kyverno doesn't support it natively.

## Action Items
1. [ ] Remove any Gatekeeper installation from the platform (no Gatekeeper CRDs, no constraint templates).
2. [ ] Author the Kyverno `verify-cosign-signature` ClusterPolicy.
3. [ ] Create an ExternalSecret that pulls the Cosign public key from AKV for the policy to reference.
4. [ ] Deploy in `Audit` mode on dev, `Enforce` on staging/prod.
5. [ ] Validate end-to-end: unsigned image → rejected on staging; signed image → admitted.

## Related ADRs
- ADR-003-v2 (cluster topology) — Kyverno enforces `tier=platform` on mgmt clusters.
- ADR-005-v2 (AKV) — stores the signing key.
- ADR-006-v2 (identity model) — Workload Identity used by the signing step in CI.
- ADR-021 (Argo Rollouts) — Kyverno also enforces `Rollout`-only-in-prod.

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
- ADR-031-v4 (PRD-v4) — codifies the data shape of the cluster topology described here.

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

- The original ADR pack remains at `IDP-GitOps-ADRs.md`. Superseded ADRs are marked `Superseded by ADR-<n>-v2` in the index — they record what was decided at v1 and why it changed.
- The eight new/amended ADRs (008-v2, 016–022) follow the same template as v1: Context → Decision → Options → Trade-off → Consequences → Action Items → Related ADRs.
- Each `-v2` ADR cross-references the original it supersedes; readers can trace the evolution.
- Future ADRs continue the sequence: ADR-023, ADR-024, etc. Number reuse is forbidden.
- The decision ledger from the audit (Q1–Q17 → resolutions) is preserved in the blueprint's changelog for traceability.

*End of v2 ADR pack.*

---

# PRD-v3 Addendum — Proposed ADRs (ADR-023-v3 .. ADR-030-v3 + ADR-016-v3-amendment)

The following ADRs are introduced by **PRD-v3** (`IDP-GitOps-Blueprint-PRD-v3.md`, dated 2026-05-22) and were promoted to **Accepted** status at the PRD-v3 readiness review on 2026-05-26. They follow the v2 ADR template and cross-reference the PRD-v3 functional requirements (`FR-V3-NN`) and design-grilling questions (`Q-NNN`) they resolve. Number reuse remains forbidden; v3 ADRs occupy the contiguous block ADR-023..ADR-030 plus a named amendment to ADR-016.

| # | Title | Status | Resolves |
|---|---|---|---|
| ADR-023-v3 | Terraform Remote State on Azure Storage with Per-Env Files | Accepted (PRD-v3 readiness review 2026-05-26) | Q-001, Q-002, Q-003, Q-004 |
| ADR-024-v3 | RBAC Scope-Down for Platform UAMIs (akspe, Velero) | Accepted (PRD-v3 readiness review 2026-05-26) | Q-008 |
| ADR-025-v3 | Pre-commit + GitHub Actions Quality Gates for Platform Repo | Accepted (PRD-v3 readiness review 2026-05-26) | Q-010..Q-014 |
| ADR-026-v3 | Variable Validation Strategy (Inline + tflint + custom rule) | Accepted (PRD-v3 readiness review 2026-05-26) | Q-015, Q-016 |
| ADR-027-v3 | Checkov Supply-Chain Scanning with Baseline | Accepted (PRD-v3 readiness review 2026-05-26) | Q-017, Q-018 |
| ADR-028-v3 | SonarQube Static Analysis (TS + Dockerfile scope, configurable hosting) | Accepted (PRD-v3 readiness review 2026-05-26) | Q-019, Q-020, Q-021, A1, A7 |
| ADR-029-v3 | Terraform Version Pin (`~> 1.5.0`) and `uuid()` Drift Fix | Accepted (PRD-v3 readiness review 2026-05-26) | Q-022 (corrected), Q-023 |
| ADR-030-v3 | Credential Sourcing via OIDC + AKV (no plaintext defaults) | Accepted (PRD-v3 readiness review 2026-05-26) | Q-005, Q-006, Q-007, Q-009 |
| ADR-016-v3-amendment | Permit DX Tools (Jenkins, Sonar) on `cipool` cluster | Accepted (PRD-v3 readiness review 2026-05-26) | A5, OQ-V3-05 |

---

# ADR-023-v3: Terraform Remote State on Azure Storage with Per-Env Files

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Principal Architect, Platform Lead, SRE Lead
**Resolves:** Q-001, Q-002, Q-003, Q-004
**Implements:** FR-V3-01, FR-V3-02, FR-V3-03, FR-V3-04

## Context

The v2 Terraform code base has no remote backend (audit finding F-01). State files live on operator laptops — a single laptop loss, a stale checkout, or a botched `terraform apply` from a stale state risks unbounded damage to the platform. Without remote state there is also no shared lock, so two operators can race the same apply. The audit ranked F-01 as P0 because both data-loss and split-brain failure modes are realistic.

Terraform cannot bootstrap its own state backend without a chicken-and-egg problem: the Storage Account that holds the state cannot itself be defined by code whose state it would host.

## Decision

1. Adopt the AzureRM remote backend for every environment.
2. Provision the state Storage Account **out of band** by an idempotent Azure CLI script (`/scripts/bootstrap-tfstate.sh`) into a dedicated resource group `rg-tfstate-bootstrap` with a `CanNotDelete` management lock.
3. Use **one state file per cluster** under the key pattern `tfstate/<env>/<cluster>.tfstate`. Day-1 keys are the seven listed in FR-V3-02. No shared/consolidated state.
4. Use the AzureRM backend's **native Azure Blob lease** as the lock — the same primitive ADR-022 uses for the mgmt-plane singleton. No external lock store.
5. Use **Microsoft-managed keys** for the state SA in v3. CMK is roadmap (OQ-V3-04); retrofitting in v3 without a follow-up ADR is forbidden.
6. The Storage Account has `allowSharedKeyAccess: false`, blob versioning ON, soft-delete 30d, and a private endpoint in the West Europe hub VNet.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Terraform Cloud / HCP | Vendor lock, additional egress trust boundary, no operational gain over native AzureRM backend |
| Self-managed S3-style backend on a foreign cloud | Cross-cloud dependency conflicts with ADR-009 (private posture) |
| Single shared state file across all envs | Defeats per-env blast-radius isolation; lease contention across unrelated applies |
| DynamoDB-style external lock store | Re-introduces a foreign-cloud dependency; native blob lease is sufficient |
| CMK at v3 | Operational cost (rotation, AKV dependency) not justified before MSE baseline is exercised; deferred via OQ-V3-04 |

## Consequences

**Easier:**
- State loss from laptop failure or stale checkouts is impossible.
- Concurrent applies against different env-states do not contend.
- Operational experience with blob leases (ADR-022) transfers directly.

**Harder:**
- Bootstrap-time chicken-and-egg requires an out-of-band script and a runbook.
- Migration from local-to-remote state must be rehearsed on `dev` first (per-env, with version snapshots).
- Storage Account is itself a dependency; SA outage freezes Terraform (but not workloads or ArgoCD).

**Will need to revisit if:**
- CMK becomes a compliance requirement (then ADR-NN for CMK migration).
- The platform grows beyond ~20 env-states (key namespace organization may need a second axis).

## Action Items
1. [ ] Author `/scripts/bootstrap-tfstate.sh` (idempotent).
2. [ ] Document migration runbook (local → remote) and rehearse on `dev`.
3. [ ] Add CI check that rejects PRs declaring `backend "local"`.
4. [ ] Author CMK migration ADR when OQ-V3-04 is committed.

## Related ADRs
- ADR-022 — Blob Lease as singleton lock; same primitive reused here.
- ADR-029-v3 — Terraform version pin (compatible with this backend).
- ADR-030-v3 — Credential sourcing for the backend itself (OIDC at TF-time).

---

# ADR-024-v3: RBAC Scope-Down for Platform UAMIs (akspe, Velero)

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Principal Architect, Security Lead, Platform Lead
**Resolves:** Q-008
**Implements:** FR-V3-10, FR-V3-11

## Context

Audit finding F-07: the `akspe` user-assigned managed identity was granted **Owner at subscription scope** during early v2 development. The Velero UAMI carried similarly over-broad scopes. Both were expedient — every Terraform apply just worked — but compromise of either UAMI would have authorized arbitrary mutation of every resource in the subscription, including other teams' production data. Every higher-layer control (Kyverno admission, ESO per-namespace UAMI, ADR-020 per-namespace SecretStore) becomes window-dressing when the foundational identity is subscription-Owner.

## Decision

1. The `akspe` UAMI holds **Contributor** and **User Access Administrator** scoped to **each AKS resource group** (`rg-aks-<env>-<region>`). Never at subscription scope. Never `Owner`.
2. The Velero UAMI holds **Contributor** on its backup resource group and **Storage Blob Data Contributor** on its backup Storage Account. Nothing else.
3. The `gha-platform-ci` UAMI (referenced by ADR-030-v3) holds **Key Vault Secrets User** scoped to each per-env AKV — read-only at TF-time. No write roles, no subscription scope.
4. A CI assertion runs `az role assignment list --assignee <UAMI>` on every PR and **fails the build** if any assignment is at subscription scope or carries `Owner`.
5. A tflint custom rule warns on any new `azurerm_role_assignment` whose `scope` resolves to `data.azurerm_subscription.current.id`.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Keep subscription-Owner ("convenience") | The audit finding; defeats every higher control |
| Split `akspe` into per-cluster UAMIs | Increases identity sprawl; RG scope is already a clean blast-radius boundary |
| Use Azure built-in `Reader` + bespoke custom role for the writes `akspe` actually needs | Custom role drift; v3 ships with built-ins and revisits only if the over-grant of `Contributor` proves problematic |

## Consequences

**Easier:**
- Compromise blast-radius is bounded to one AKS resource group.
- The principle of least privilege is enforceable, not aspirational.

**Harder:**
- Any new resource type `akspe` provisions outside the AKS RGs requires a new role assignment (and review).
- Velero needs its backup RG carved out distinctly from AKS RGs (already the case).

**Will need to revisit if:**
- The platform adopts a tighter custom role to replace `Contributor` (likely a follow-up; tracked outside v3).

## Action Items
1. [ ] Remove subscription-scoped `Owner` from `akspe` in Terraform.
2. [ ] Add CI assertion job for role-assignment scopes.
3. [ ] Add tflint custom rule for subscription-scope detection.
4. [ ] Quarterly review of `akspe` and Velero assignments.

## Related ADRs
- ADR-006-v2 — Workload Identity foundational decision.
- ADR-020 — Per-namespace UAMI isolation; this ADR fixes the foundational-layer over-grant that ADR-020 sat on top of.
- ADR-030-v3 — Credential sourcing; `gha-platform-ci` UAMI scopes also defined here.

## Amendment 2026-05-26 — Crossplane UAMI Carve-Out

**Status:** Accepted (PRD-v4 readiness verification 2026-05-26)
**Deciders:** Principal Architect, Security Lead, Platform Lead

### Finding

The PRD-v4 readiness verification (2026-05-26) discovered that
`crossplane_contributor` UAMI at `terraform/main.tf:249-250` retains
`scope = data.azurerm_subscription.current.id` with role
`Contributor`. The `akspe` and Velero scope-downs in this ADR's day-1
set landed correctly; Crossplane was implemented with a wider scope
because its compositions provision Azure resources across multiple
resource groups by design (per-environment RGs, shared platform RGs,
ESO secret RGs).

### Decision (amendment)

1. The Crossplane UAMI's subscription-Contributor scope is **accepted
   as an explicit exception** to `ADR-024-v3` §Decision rule 1 ("never
   at subscription scope").
2. The exception is bounded: Crossplane's UAMI **must not be granted
   `Owner`** at any scope, and must not gain additional sub-roles
   beyond `Contributor`. The CI assertion (rule 4 of `ADR-024-v3`)
   continues to enforce that no `Owner` assignment exists.
3. The exception is **time-bounded**: it expires at the next platform
   architecture review unless explicitly renewed. Renewal requires
   either (a) a documented justification that the per-RG composition
   pattern is still operationally infeasible, or (b) a follow-up ADR
   that designs a Crossplane composition pattern with per-RG UAMI
   delegation.
4. The exception is **cataloged**: a new entry in PRD-v4
   §Pre-existing v3 obligations lists Crossplane UAMI as the explicit
   carve-out so that future readers do not interpret the scope as
   accidental.

### Rationale

Crossplane's `azurerm` provider creates resources whose target RG is
declared by composition inputs at run-time. Pinning the UAMI to a
fixed RG set at provisioning time would require either (a) granting
the UAMI on every conceivable target RG in advance — which inflates
the RG count and re-introduces the inventory-drift class the original
ADR was meant to close, or (b) authoring a meta-controller that
delegates per-RG identities to each composition invocation, which is a
non-trivial composition redesign.

The exception is preferred to the half-fix of "scope to a subset of
RGs" because half-coverage delivers the operational complexity of
multi-RG management without delivering the security benefit of true
least-privilege.

### Consequences

**Easier:**
- Status quo of Crossplane operations is preserved; no v3.1 patch
  required for this item.
- The exception is explicit, named, and reviewed quarterly — not a
  hidden gap.

**Harder:**
- A Crossplane UAMI compromise still grants subscription-Contributor
  blast radius. The platform security review must continue to factor
  this in.
- Future Crossplane composition work that introduces new role types
  (e.g., `User Access Administrator`) requires an amendment to this
  amendment — they are NOT in scope.

**Will need to revisit if:**
- Crossplane introduces native per-composition identity delegation
  (upstream feature; not currently available).
- A regulated workload requires zero-subscription-scope identities
  platform-wide for compliance reasons.

### Action Items
1. [ ] Add Crossplane UAMI to PRD-v4 §Pre-existing v3 obligations as
       the named carve-out.
2. [ ] Add a quarterly-review reminder for this exception (calendar
       entry, not code).
3. [ ] Confirm the existing CI assertion job correctly flags Crossplane
       UAMI's subscription-scope assignment as "exception-tagged"
       rather than as a failure — update the assertion's allow-list
       to recognize the Crossplane UAMI by name.

---

# ADR-025-v3: Pre-commit + GitHub Actions Quality Gates for Platform Repo

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Platform Lead, SRE Lead
**Resolves:** Q-010, Q-011, Q-012, Q-013, Q-014
**Implements:** FR-V3-13, FR-V3-14, FR-V3-15, FR-V3-16, FR-V3-17

## Context

Audit finding F-02: the platform repo has no pre-commit hooks, no PR-time lint or validation. Style drift, broken Terraform, leaked secrets, and other defects land on `main` because nothing catches them before merge. v2 assumed industry-standard hygiene; the review proved otherwise.

App CI runs on Jenkins (ADR-001-v2) and is not in question. What is missing is **platform-repo meta-CI** — the workflow that gates Terraform and Backstage changes.

## Decision

1. Adopt the Python `pre-commit` framework as the canonical local hook runner. A single `.pre-commit-config.yaml` at the repo root is the source of truth.
2. The v3 minimal hook set is exactly: `terraform_fmt`, `terraform_validate`, `tflint`, `detect-private-key`, `end-of-file-fixer`, `trailing-whitespace`. (Checkov runs in pre-commit too — added under ADR-027-v3.)
3. Platform-repo meta-CI runs on **GitHub Actions** with three workflows: `terraform-plan.yml`, `terraform-apply.yml`, `backstage.yml`. Jenkins is untouched — app CI continues there per ADR-001-v2.
4. Blocking is phased:
   - **Day-1 blocking:** `terraform_fmt`, `terraform_validate`, `tflint`, `detect-private-key`.
   - **Sprint-1 advisory, sprint-2 blocking:** `checkov` (ADR-027-v3), Sonar (ADR-028-v3, scoped to `packages/backend`), custom tflint unvalidated-var rule (ADR-026-v3).
5. The Terraform workflow is a **matrix of one job per env-state**. `terraform plan` runs on every PR; `terraform apply` runs only on merges to `main`.
6. `terraform apply` is gated by **GitHub Environments + required reviewers** — one Environment per env-state, each listing at least two members of `@<org>/platform-team` as required reviewers, with deployment branches restricted to `main` and OIDC subject claims scoped per-Environment.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Husky / Lefthook | Node-centric; mismatch with HCL toolchain; pre-commit is the industry default for IaC repos |
| Run meta-CI on Jenkins too | Couples platform-repo CI to the cluster whose Terraform it changes — fails the "don't change the runway you're flying off of" test |
| Single apply job (no matrix) | Loses per-env-state visibility; one failure blocks the whole apply |
| Auto-apply on `main` | Removes the human in the loop for prod changes; rejected on safety grounds |

## Consequences

**Easier:**
- Local pre-commit and CI catch the same classes of defect; same hook set, same config.
- App CI on Jenkins is untouched — no migration risk.
- Apply gating produces an auditable record (who approved what env-state change when).

**Harder:**
- Contributors must `pre-commit install` (one-time); office-hours session at each phase cutover.
- Environment-and-reviewer plumbing for seven env-states adds GitHub admin overhead.

**Will need to revisit if:**
- Sprint-2 blocking causes excessive merge friction (then re-phase, not abandon).
- A new env-state is added (must spin up matching GitHub Environment).

## Action Items
1. [ ] Land `.pre-commit-config.yaml` with the v3 minimal hook set.
2. [ ] Author `terraform-plan.yml`, `terraform-apply.yml`, `backstage.yml`.
3. [ ] Create seven GitHub Environments with required reviewers and `main`-only branch protection.
4. [ ] Office-hours session before day-1 blocking and before sprint-2 blocking.

## Related ADRs
- ADR-001-v2 — App CI on Jenkins; unchanged.
- ADR-026-v3 — Variable validation; consumed by tflint here.
- ADR-027-v3 — Checkov; integrated in both pre-commit and CI.
- ADR-028-v3 — Sonar; in the Backstage workflow.
- ADR-030-v3 — OIDC credential sourcing; consumed by the apply workflow.

---

# ADR-026-v3: Variable Validation Strategy (Inline + tflint + Custom Rule)

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Platform Lead, Principal Architect
**Resolves:** Q-015, Q-016
**Implements:** FR-V3-18, FR-V3-19, FR-V3-20

## Context

Audit finding F-03: Terraform variables in the v2 codebase carry no `validation {}` blocks. A typo such as `env = "qa"` propagates through `plan` and surfaces as a confused Azure API error at `apply`. There is no policy preventing new unvalidated variables from landing.

## Decision

1. **Critical-path variables carry inline `validation {}` from day-1.** The critical-path set is: `region`, `cluster_name`, `sku_tier`, `cidr_block`, `environment` (regex `^(dev|staging|prod)$`).
2. **Cloud-resource-level rules** (naming, SKU constraints, required tags) live in `tflint-ruleset-azurerm` — not duplicated inside `validation {}`. The split is deliberate: inline = input shape, tflint = resource shape.
3. **A custom tflint rule** detects any new `variable` declaration lacking a `validation {}` block. The rule is **advisory** while the 100%-coverage ratchet is in progress and **blocking** once 100% is declared.
4. Coverage ratchets to 100% across two sprints following P1 closure. Exemptions require an allow-list entry with rationale.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| OPA / Conftest over plan output | Adds a second policy language alongside Kyverno + tflint; redundant signal |
| `validation {}` everywhere with no tflint | Duplicates rules across modules; drift over time |
| tflint everywhere with no `validation {}` | Loses the per-module shape contract that documents intent at the variable site |
| Hard cutover to 100% on day-1 | Excessive churn; phased ratchet is the safer rollout |

## Consequences

**Easier:**
- Misconfigurations fail at `plan`, not `apply`.
- Each variable's contract is documented at its declaration.

**Harder:**
- The custom tflint rule must be authored and maintained (~50 lines of Go).
- Application teams inherit new variable-validation expectations on next PR.

**Will need to revisit if:**
- The `tflint-ruleset-azurerm` upstream adds a built-in unvalidated-var detector (then drop the custom rule).

## Action Items
1. [ ] Add `validation {}` blocks for the day-1 critical-path five.
2. [ ] Author the custom tflint rule for unvalidated-var detection.
3. [ ] Schedule the sprint-1 and sprint-2 ratchet PRs.
4. [ ] Flip the custom rule to blocking at 100% coverage.

## Related ADRs
- ADR-025-v3 — Quality gates; this rule executes inside the same tflint hook.

---

# ADR-027-v3: Checkov Supply-Chain Scanning with Baseline

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Platform Lead, Security Lead
**Resolves:** Q-017, Q-018
**Implements:** FR-V3-21, FR-V3-22, FR-V3-23

## Context

Audit finding F-04: no IaC-side supply-chain scanning. Cosign + Kyverno (ADR-008-v2) covers the **image** supply chain at admission. The **Terraform** supply chain — misconfigured Storage Accounts, open NSGs, public AKS endpoints, missing encryption — has no equivalent gate. Checkov is the industry-standard scanner for this surface.

## Decision

1. Run **`checkov`** against all Terraform and Dockerfile content using a single `.checkov.yaml` at the repo root. The same config is consumed by **both** pre-commit and CI — local failures are reproducible.
2. Track accepted findings in `.checkov.baseline` at the repo root. The baseline is **CODEOWNERS-protected**: ownership is assigned to `@<org>/platform-team` as interim owner. (When a dedicated Platform Security guild exists, the entry is re-pointed via follow-up PR.)
3. **Inline suppressions** use the form `# checkov:skip=CKV_*:<jira-ticket>`. The referenced Jira ticket must exist and must carry an expiry date. A CI step queries Jira and **fails the build** if any suppression lacks a ticket, references a closed ticket, or references a ticket whose expiry has passed.
4. Checkov is **advisory in sprint-1** and **blocking from sprint-2** once the baseline has landed and the team has had a sprint to adjust.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| `tfsec` | Functionally similar; Checkov has broader framework coverage (including Dockerfiles) |
| Scan only in CI (no pre-commit) | Slower feedback loop; contributors discover failures post-push |
| No baseline (all findings must be fixed before adoption) | Unrealistic for a large existing codebase; baseline is the on-ramp |
| Ungated suppression syntax (`checkov:skip` with no ticket) | Suppressions silently inflate; a year later nobody knows why |

## Consequences

**Easier:**
- IaC-side defects are caught in the same loop as image-side defects.
- Baseline ratchets findings down over time.
- Suppression discipline is enforceable.

**Harder:**
- Sprint-1 advisory phase requires the team to triage warnings without merge-blocking.
- CODEOWNERS routing of baseline PRs adds a review hop.

**Will need to revisit if:**
- A Platform Security guild team is stood up (then re-point CODEOWNERS).
- Checkov rule coverage moves significantly faster than v3 cadence (then evaluate `checkov --check` allow-listing).

## Action Items
1. [ ] Land `.checkov.yaml` and an initial `.checkov.baseline`.
2. [ ] Add `.checkov.baseline` to `CODEOWNERS` pointing at `@<org>/platform-team`.
3. [ ] Wire the Jira-expiry suppression-validator CI step.
4. [ ] Flip Checkov from advisory to blocking at sprint-2.

## Related ADRs
- ADR-008-v2 — Cosign + Kyverno; covers the image supply chain. This ADR is the IaC complement.
- ADR-025-v3 — Quality gates; Checkov runs in both hooks.

---

# ADR-028-v3: SonarQube Static Analysis (TS + Dockerfile Scope, Configurable Hosting)

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Platform Lead, Principal Architect
**Resolves:** Q-019, Q-020, Q-021; clarifications A1 and A7
**Implements:** FR-V3-24, FR-V3-25, FR-V3-25a, FR-V3-26

## Context

The Backstage portal (`backstage/`) is TypeScript and ships several Dockerfiles. Static analysis for code quality + coverage gating is industry standard; SonarQube/SonarCloud is the chosen tool. Two open questions: scope (does Sonar also cover HCL?) and hosting (SaaS vs self-hosted?).

The HCL question is resolved by other v3 ADRs — `tflint` (ADR-026-v3) and `checkov` (ADR-027-v3) cover that surface; double-coverage with Sonar adds CI time without adding signal.

The hosting question is operator-dependent: future data-residency or cost constraints may make SaaS untenable. A toggle is required.

## Decision

1. **Scope:** Sonar analyzes Backstage TypeScript (`backstage/packages/`) and Dockerfiles (`backstage/**/Dockerfile*`) only. HCL is **excluded**.
2. **Onboarding is greenfield** (clarification A7): no prior Sonar project, profile, or finding history exists. The Platform team delivers `backstage/sonar-project.properties`, the GHA scan step, and the initial quality-gate configuration.
3. **Hosting is configurable** via the Terraform variable `var.sonar_hosting`:
   - Type: `string`, default `"saas"`, inline `validation {}` restricting values to `"saas" | "cipool"`.
   - `"saas"` → SonarCloud (`sonarcloud.io`). No in-cluster footprint.
   - `"cipool"` → Self-hosted Sonar Helm release on the `cipool` node pool of `mgmt-we` (`nodeSelector: { nodepool: cipool }`, `tolerations: [{ key: workload, value: ci, effect: NoSchedule }]`). Never on `systempool`; never on `mgmt-ne` or any workload cluster.
4. The `"cipool"` value depends on **ADR-016-v3-amendment** being `Accepted`. Until then, `var.sonar_hosting` must remain at its default `"saas"`.
5. **Quality gate:** Sonar Way profile with one override — **new-code coverage greater than or equal to 80%**. Advisory for one sprint, blocking from sprint-2 onward, scoped to `packages/backend`.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Sonar also covering HCL | Duplicates tflint + checkov; no incremental signal |
| Hard-coded SaaS (no toggle) | Forecloses future data-residency / cost-control needs |
| Hard-coded self-hosted on `mgmt-we` | Violates ADR-016's spirit even with the amendment; SaaS-by-default is the lower-friction baseline |
| Custom quality profile fork | Maintenance burden; Sonar Way + one override is sufficient |
| Block immediately (no advisory phase) | Greenfield onboarding will surface findings the team has not yet triaged |

## Consequences

**Easier:**
- Coverage gating drives test discipline on the Backstage codebase.
- Hosting can pivot from SaaS → self-hosted with a variable flip, not a re-engineering.

**Harder:**
- The "cipool" path requires the ADR-016 amendment to be `Accepted` (cross-coupling).
- Greenfield onboarding produces a burst of initial findings that need triage in sprint-1.
- Quality-gate failures on `packages/backend` may surface latent coverage gaps that pre-date v3.

**Will need to revisit if:**
- SonarCloud line-of-code pricing crosses a budget threshold (then flip to `"cipool"`).
- The Sonar Way profile changes upstream in ways that disagree with the 80% override (re-evaluate the override).

## Action Items
1. [ ] Author `backstage/sonar-project.properties` (Platform team).
2. [ ] Add the `sonar-scanner` step to `backstage.yml` workflow.
3. [ ] Configure the initial quality gate (Sonar Way + 80% new-code coverage).
4. [ ] Land `var.sonar_hosting` with `validation {}` (default `"saas"`).
5. [ ] Conditional Helm release for the `"cipool"` path — gated on ADR-016-v3-amendment Accepted.
6. [ ] Flip to blocking at sprint-2.

## Related ADRs
- ADR-016 — Original terminology decision.
- ADR-016-v3-amendment — Permits Sonar on `cipool`.
- ADR-025-v3 — Quality gates; Sonar runs as part of the Backstage workflow.
- ADR-026-v3 — `var.sonar_hosting` `validation {}` block lives in the same regime.

---

# ADR-029-v3: Terraform Version Pin (`~> 1.5.0`) and `uuid()` Drift Fix

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Platform Lead
**Resolves:** Q-022 (corrected from `~> 1.15.0` to `~> 1.5.0`), Q-023
**Implements:** FR-V3-27, FR-V3-28

## Context

Audit findings F-08 and F-09:

- **F-08:** The Terraform version floor is too permissive. Without a pin, contributors and CI runners can drift across minor versions, producing inconsistent plan output and surfacing bugs that are specific to one version.
- **F-09:** Several modules use `uuid()` interpolations directly. `uuid()` regenerates on every plan, surfacing spurious diffs and forcing recreation of dependent resources on every apply.

The initial grilling-session pin proposal was `~> 1.15.0`. That number was an error — the intended pin is the LTS-ish **1.5.x** line, the last MPL-licensed Terraform release series, broadly supported by `tflint`, `checkov`, and the current `azurerm` provider. Clarification A2 corrects the pin to `~> 1.5.0`.

## Decision

1. Pin `required_version = "~> 1.5.0"` in every module. This allows patch upgrades within 1.5.x while blocking unscheduled minor jumps.
2. CI rejects any module declaring a `required_version` other than `~> 1.5.0`.
3. Replace every occurrence of `uuid()` with a `random_uuid` resource keyed by a stable input via `keepers`:

   ```hcl
   resource "random_uuid" "example" {
     keepers = {
       trigger = var.cluster_name
     }
   }
   ```

   The UUID is stable across plans, regenerates only when `var.cluster_name` changes, and produces empty diffs on consecutive applies.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| `~> 1.15.0` (original grilling-session pin) | Numerical error; intended pin was the LTS-ish 1.5.x line |
| `>= 1.5.0` (floor only) | Allows unbounded drift; defeats the purpose of pinning |
| Stay on `uuid()` and accept the drift | The drift forces resource recreation on every apply — operationally untenable |
| Use `random_id` instead of `random_uuid` | `random_uuid` is the semantically correct primitive when downstream APIs require a UUID format |

## Consequences

**Easier:**
- Plans are deterministic; consecutive `terraform plan` runs produce empty diffs.
- All contributors and CI run the same version.

**Harder:**
- Migration from `uuid()` to `random_uuid` requires `terraform state mv` or targeted replacement; rehearse on `dev` first.
- Patch-version updates require explicit re-pinning when 1.5.x reaches end-of-life.

**Will need to revisit if:**
- The 1.5.x line is no longer maintained or a required provider drops support (then re-evaluate the next stable pin, with an ADR).

## Action Items
1. [ ] Set `required_version = "~> 1.5.0"` in every `versions.tf`.
2. [ ] CI assertion rejecting other `required_version` values.
3. [ ] Migrate `uuid()` to `random_uuid` with `keepers`; rehearse on `dev`.
4. [ ] Add `grep -R 'uuid()' terraform/` as a CI guard.

## Related ADRs
- ADR-023-v3 — Remote state backend; same versioning regime applies.
- ADR-025-v3 — Quality gates; the version assertion runs in this CI.

---

# ADR-030-v3: Credential Sourcing via OIDC + AKV (No Plaintext Defaults)

**Status:** Accepted (PRD-v3 readiness review 2026-05-26)
**Date:** 2026-05-22
**Deciders:** Principal Architect, Security Lead, Platform Lead
**Resolves:** Q-005, Q-006, Q-007, Q-009
**Implements:** FR-V3-08, FR-V3-09, FR-V3-12

## Context

Audit finding F-05: sensitive Terraform inputs (subscription IDs, client secrets, SAS tokens, SaaS API tokens, connection strings) were carried as `default = "..."` on variable declarations, then materialized into Terraform state on every apply. Once in state, they are visible to anyone who can read the state file.

Clarification A3: the OIDC federated credential for GitHub Actions → Azure is **already provisioned**. v3 must reference and consume it, not re-provision it. The federated credential maps the GitHub Actions OIDC issuer (`token.actions.githubusercontent.com`, audience `api://AzureADTokenExchange`, subject pinned per repo + protected `main` GitHub Environment) to the `gha-platform-ci` user-assigned managed identity.

## Decision

1. **No sensitive variable carries a `default = "..."` value.** Sensitive inputs are sourced via `data "azurerm_key_vault_secret"` blocks or via `TF_VAR_*` populated by CI.
2. **No plaintext `*.tfvars` containing sensitive material may be committed.** A pre-commit hook scans for variable names matching `password|secret|token|key|conn` in any `*.tfvars`.
3. **CI fetches sensitive values from AKV** using the already-provisioned `gha-platform-ci` UAMI via the existing OIDC federated credential. v3 references this credential; v3 does not re-provision it.
4. The `gha-platform-ci` UAMI holds only `Key Vault Secrets User` scoped to each per-env AKV — read-only at TF-time. No write roles, no subscription scope (cross-referenced with ADR-024-v3).
5. **Rotation model:**
   - AKV keys and certificates rotate quarterly via AKV-native rotation policies. No code, no CronJob.
   - Workload Identity federation has no rotating secret — nothing to rotate.
   - SaaS tokens (Bitbucket, Jira) continue to rotate via the existing `saas-token-rotator` CronJob (unchanged by v3).

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Long-lived service-principal client secrets in CI | Defeats the purpose of OIDC federation; rotation toil |
| Personal Access Tokens for GitHub Actions → Azure | Per-user trust boundary; not auditable; not in the federated-trust regime |
| Sensitive values in repo-encrypted GitHub Secrets | Less auditable than AKV (no per-secret RBAC, no rotation policies, no soft-delete) |
| Provision a new federated credential in v3 | Duplicates the existing wiring (clarification A3); risk of misconfiguration |

## Consequences

**Easier:**
- Sensitive material never lands in `*.tfvars`, in `default = "..."`, or in repo-encrypted secrets.
- Rotation is AKV-native; v3 introduces zero new manual rotation toil.
- Credential audit is "list role assignments on `gha-platform-ci`" — one query.

**Harder:**
- CI cold-starts include an AKV round trip per fetched secret (sub-second; acceptable).
- The OIDC subject claim must be kept aligned with the GitHub Environment names — drift breaks the federation.

**Will need to revisit if:**
- A second CI system (beyond GitHub Actions) needs to invoke Terraform (then add a parallel federated credential, do not share `gha-platform-ci`).
- AKV regional outage frequency makes the AKV-fetch step a reliability hotspot.

## Action Items
1. [ ] Sweep variables for `default = "..."` on sensitive inputs; replace with AKV data sources or `TF_VAR_*`.
2. [ ] Pre-commit hook scanning `*.tfvars` for sensitive variable names.
3. [ ] Document the existing OIDC federated-credential subject pinning per env-state.
4. [ ] In-CI assertion: `terraform state pull | grep -iE 'password|secret|token|key|conn'` returns only baseline-known entries.

## Related ADRs
- ADR-005-v2 — ESO + per-region AKV; underlies the AKV-as-truth posture.
- ADR-006-v2 — Workload Identity foundational decision.
- ADR-024-v3 — `gha-platform-ci` UAMI scope-down (referenced here).
- ADR-025-v3 — The CI workflow that consumes this credential.

---

# ADR-016-v3-amendment: Permit DX Tools (Jenkins, Sonar) on `cipool` Cluster

**Status:** Accepted (PRD-v3 readiness review 2026-05-26) — amends ADR-016
**Date:** 2026-05-22
**Deciders:** Principal Architect, Platform Lead
**Resolves:** Clarification A5, PRD-v3 OQ-V3-05

## Context

ADR-016 (Platform Terminology) defines "workload" vs "platform component" and is widely read as "no DX-plane tooling on management clusters." That reading was always inexact — Jenkins has run on `mgmt-we`'s `cipool` node pool since v2 GA per ADR-001-v2. PRD-v3 makes the inexactness explicit: when `var.sonar_hosting = "cipool"` (ADR-028-v3), a self-hosted Sonar Helm release lands on the same `cipool` node pool.

Without an explicit amendment, two readings of ADR-016 contradict the v3 design: a strict reading forbids Sonar on `cipool`, while ADR-001-v2's continued operation of Jenkins on `cipool` demonstrates the strict reading was never the intent.

## Decision

Amend ADR-016 to scope the "no DX tooling on management clusters" rule **strictly to management `systempool`s**:

1. **Management cluster `systempool`** (both `mgmt-we` and `mgmt-ne`): **platform-only**. Kyverno's `tier=platform` requirement is unchanged.
2. **Management cluster `cipool`** (which exists only on `mgmt-we`): **allowed host for DX-plane tooling**. Currently hosts Jenkins (ADR-001-v2); optionally hosts self-hosted Sonar when `var.sonar_hosting = "cipool"` (ADR-028-v3).
3. **Workload clusters** (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`): no DX tooling. No Jenkins, no Sonar.
4. **Seed cluster** (`seed-wus`): no DX tooling. Catastrophic bootstrap only.
5. New DX tools landing on `cipool` require an ADR that explicitly invokes this amendment.

The amendment does **not** weaken the cardinal rule that management clusters host no customer/business workloads.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Keep ADR-016 strict; move Jenkins off `cipool` | Would re-open ADR-001-v2 (settled); no operational gain |
| Permit DX tools anywhere on management clusters | Defeats the failure-domain isolation that ADR-016 protects |
| Allow Sonar but reject any future DX tool until per-tool ADR | Status-quo, but creates ambiguity for the next DX tool; this amendment establishes the rule |

## Consequences

**Easier:**
- The Jenkins-on-`cipool` precedent (ADR-001-v2) becomes consistent with documented terminology.
- The Sonar self-hosted path is unblocked once this amendment is `Accepted`.
- Future DX tools have a clear pattern: `cipool` is the allowed host.

**Harder:**
- Each new DX tool must explicitly invoke this amendment in its own ADR.
- Kyverno policy for `tier=platform` on management clusters must distinguish `systempool` from `cipool` (already the case in practice).

**Will need to revisit if:**
- A second `cipool`-equivalent node pool is added to `mgmt-ne` (then re-scope this amendment).
- A new DX tool's resource footprint risks crowding Jenkins on `cipool` (capacity, not policy, conversation).

## Action Items
1. [ ] Cross-reference this amendment from ADR-016 (one-line "Amended by" pointer).
2. [ ] Verify Kyverno policy permits non-`tier=platform` pods on `cipool` (and only on `cipool`).
3. [ ] Update §22 of `docs/architect.md` if any operational detail changes when this amendment moves to `Accepted`.

## Related ADRs
- ADR-016 — The amended decision.
- ADR-001-v2 — Establishes Jenkins on `cipool`; this amendment ratifies that placement explicitly.
- ADR-028-v3 — Sonar configurable hosting; depends on this amendment for the `"cipool"` value.

---

# PRD-v4 Addendum — Proposed ADRs (ADR-031-v4)

The following ADR is introduced by **PRD-v4** (`IDP-GitOps-Blueprint-PRD-v4.md`, dated 2026-05-25) and was promoted to **Accepted** status alongside PRD-v4 approval on 2026-05-26. It follows the v2 ADR template and cross-references the PRD-v4 functional requirements (`FR-V4-NN`) and design-grilling questions (`Q-V4-NN`) it resolves. Number reuse remains forbidden; v4 ADRs continue the sequence from ADR-031.

| # | Title | Status | Resolves |
|---|---|---|---|
| ADR-031-v4 | Cluster Topology Lives in a Single Committed Registry | Accepted (PRD-v4 approved 2026-05-26) | Q-V4-03 (grilling); informs FR-V4-01..04 |

---

# ADR-031-v4: Cluster Topology Lives in a Single Committed Registry

**Status:** Accepted (PRD-v4 approved 2026-05-26)
**Date:** 2026-05-25
**Deciders:** Principal Architect, Platform Lead, SRE Lead, DX Lead
**Resolves:** Q-V4-03 (PRD-v4 grilling)
**Implements:** FR-V4-01, FR-V4-02, FR-V4-03, FR-V4-04

## Context

The v3 codebase encodes the platform's cluster topology in **three independent places**:

1. `terraform/clusters.tf` — CAPZ/Crossplane resource definitions per cluster.
2. `terraform/argocd_bootstrap.tf:2–100` — `local.argocd_registered_clusters` map used by the ArgoCD ApplicationSet to determine which clusters receive which addons.
3. `tools/service_seed/seed_job.py:314–361` — hardcoded subscription IDs, regions, resource-group names, and ACR hostnames consumed when seeding new services.

Each location encodes the same facts (subscription ID, region, RG, ACR hostname, AKS name, mgmt role) in a different language with no shared schema. Adding a cluster requires three coordinated edits across HCL, HCL, and Python; missing any one of them produces silent drift that surfaces during a future apply or during the next service-seed run.

The PRD-v4 architecture review (item #7) flagged this as the highest-leverage deepening opportunity: the underlying data is identical, callers are heterogeneous, and the deletion test passes — removing any single encoding concentrates complexity in the remaining ones rather than dispersing it.

Three candidate sources of truth were considered during PRD-v4 grilling (Q-V4-03):

- **A — Committed YAML registry** read by all consumers via `yamldecode` / `yaml.safe_load`.
- **B — Terraform as source of truth** emitting a JSON artifact that CI commits back to git; non-TF consumers read the artifact.
- **C — In-cluster `ClusterRegistration` CRD** on `mgmt-we`; tools query Kubernetes.

Option C contradicts ADR-013-v2's two-tier infra-then-workload posture (the registry would have to live in the cluster it bootstraps, creating a chicken-and-egg) and is also incompatible with Backstage's offline planning needs. Option B introduces a CI write-back loop, requires `seed_job.py` to wait for a TF apply before knowing about new clusters, and inverts the natural direction (registry should *inform* TF, not be *generated by* it). Option A is the only candidate where the registry exists independently of any running system and is reviewable as YAML in a PR.

## Decision

1. **Authoritative cluster topology lives in a single committed YAML file at `gitops/clusters/registry.yaml`.** No other file in the repository may serve as a competing source of truth for cluster identity facts.

2. **Schema is enforced** by `gitops/clusters/registry.schema.json` (JSON Schema draft-2020-12). Pre-commit and CI validate every change.

3. **Required keys per cluster entry:**
   - `name` (matches the cluster's canonical `<role>-<env>-<region_abbrev>` identifier)
   - `subscription_id`
   - `region` (full Azure region: `westeurope`, `northeurope`, `westus2`)
   - `region_abbrev` (matching short form: `we`, `ne`, `wus`)
   - `resource_group`
   - `acr_hostname`
   - `aks_name`
   - `mgmt_role` (enum: `active | standby | workload | seed`)
   - `azs` (list of availability zones)
   - `sku_tier` (enum: `Free | Standard | Premium`)
   - `gitops_addons` (map of `enable_*` boolean flags consumed by the ArgoCD ApplicationSet)

4. **Consumers read the registry directly:**
   - Terraform reads via `yamldecode(file("${path.module}/../gitops/clusters/registry.yaml"))` and exposes the parsed map as `local.cluster_registry`.
   - ArgoCD ApplicationSet locals derive `local.argocd_registered_clusters` from `local.cluster_registry` rather than maintaining an independent map.
   - `tools/service_seed/cli.py` loads the registry at startup and passes the parsed object as data into `service_template.render(...)` and `gitops_pr.compose(...)`.

5. **The registry is human-curated.** No tool auto-generates entries from cloud discovery; cluster onboarding is a deliberate PR with reviewer approval. (Auto-generation is excluded by PRD-v4 Non-Goals Bucket 3.)

6. **The registry is Azure-only at v4 scope.** Multi-cloud variants are out of scope; if a future PRD extends to other CSPs, this ADR is amended (not replaced).

7. **Day-1 cluster set:** `mgmt-we`, `mgmt-ne`, `aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`, `seed-wus`. The set mirrors the v2/v3 cluster topology unchanged.

## Why this works

The registry is **inert reviewable data**. A PR adding a cluster is a YAML diff that any reviewer can read in any language without checkout. The schema validation in pre-commit and CI prevents the most common drift mode (missing field, wrong region/abbrev pairing). The single-source property eliminates the failure mode where TF was updated but `seed_job.py` was not — that class of bug becomes structurally impossible after FR-V4-03.

The decision **does not couple** the registry to any runtime. Backstage planning, future cost-allocation tooling, and operator scripts can all consume the same file with no dependency on Terraform state, ArgoCD readiness, or Kubernetes API availability.

The decision **respects ADR-013-v2** (two-tier infra-then-workload): the registry is a *git artifact* loaded by the infra tier, not a *cluster artifact* that requires the cluster to exist first.

## Options Reconsidered

### Option A: Committed YAML registry consumed by all (chosen)

- Single source of truth, language-agnostic, PR-reviewable.
- Pre-commit schema validation catches drift early.
- Works for tools that have no Terraform state access (Backstage, future planners).
- **Chosen.**

### Option B: Terraform-emitted registry artifact committed by CI

- Inverts the natural direction (registry *informs* TF, not the other way around).
- Requires `seed_job.py` to wait for a TF apply before knowing about new clusters.
- Adds a CI write-back loop with its own failure modes (race with concurrent PRs, signing concerns).
- **Rejected.**

### Option C: In-cluster `ClusterRegistration` CRD on `mgmt-we`

- Chicken-and-egg: the registry would live in the cluster it bootstraps.
- Contradicts ADR-013-v2's two-tier ordering.
- Makes Backstage offline planning impossible.
- Operationally heavier (controllers, RBAC, reconcile loops) for inert data.
- **Rejected.**

### Option D: Leave the triple-encoding in place

- The deletion test fails: removing any one encoding concentrates complexity in the others.
- Drift continues to surface late.
- **Rejected.**

## Consequences

**Easier:**

- Adding a cluster is one PR to one file; reviewers see the change in a single diff.
- Backstage / cost tooling / future planners can read cluster identity without TF state access.
- Tests use the same registry shape; fixture lives at `tools/service_seed/tests/fixtures/registry.yaml`.
- Schema-driven validation in pre-commit catches the most common onboarding mistakes.

**Harder:**

- A new file becomes a high-traffic merge surface; conflicts are likely during periods of rapid cluster onboarding. Mitigation: keep entries alphabetically sorted; CI fails on out-of-order entries.
- HCL `yamldecode` returns untyped values; TF code must defensively coerce types where the schema declares them (numbers, booleans). Mitigation: a thin `local.cluster_registry_typed = { for k, v in ... : k => { ... } }` re-projection in `locals.tf`.
- Backwards compatibility during migration: TF locals must read either the old `local.argocd_registered_clusters` or the new registry until all call sites are converted. PRD-v4 P1 lands the migration in a single release-train (see PRD-v4 Risks R-V4-1).

**Will need to revisit if:**

- Multi-cloud is introduced — the registry schema needs a `csp` discriminator and per-CSP key blocks. (Future ADR-NNN, not an amendment to this one.)
- A cluster-registry consumer requires authoritative ground-truth from the running cluster (e.g., actual subnet allocations) rather than declared topology — at which point a separate "discovered facts" companion file is added, not a replacement for the declared registry.
- Per-environment overrides become necessary (see PRD-v4 OQ-V4-01) — the schema is extended; the single-file property may relax.

## Action Items

1. [ ] Author `gitops/clusters/registry.yaml` with the seven day-1 cluster entries (mirrors current v3 state).
2. [ ] Author `gitops/clusters/registry.schema.json` (draft-2020-12) and wire it into pre-commit + CI.
3. [ ] Migrate `terraform/argocd_bootstrap.tf:2–100` locals to derive from `local.cluster_registry`.
4. [ ] Migrate `terraform/clusters.tf` cluster-identity references to read from `local.cluster_registry`.
5. [ ] Land `tools/service_seed/cli.py` registry loader once US-V4-07 (three-module split) lands.
6. [ ] Add a CI step that fails any PR adding a hardcoded `subscription_id`, `region`, or `acr_hostname` outside the registry (regex-based linter scoped to `terraform/*.tf` and `tools/service_seed/**/*.py`).
7. [ ] Update `docs/agents/domain.md` to name the registry as the canonical topology source.

## Related ADRs

- **ADR-003-v2** — Cluster topology three-tier model; this ADR codifies the data shape.
- **ADR-013-v2** — Two-tier GitOps layout; this ADR sits at the infra tier and is read by the workload tier without modification.
- **ADR-017** — Management-plane active-passive; `mgmt_role` field carries the active/standby distinction.
- **ADR-022** — Management-plane singleton lock; the lease is *authoritative for runtime active-ness*, while this registry is *authoritative for declared topology*. The two are intentionally separate (runtime fact vs declared fact).
- **ADR-024-v3** — RBAC scope-down for platform UAMIs; per-cluster identity scopes derived from this registry's RG entries.
