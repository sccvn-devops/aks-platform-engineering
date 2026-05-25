# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Layout

**Single-context repo.** There is one domain context for the entire codebase.

```
/
├── docs/architect.md          ← authoritative architecture reference (start here)
├── _docs/IDP-GitOps-ADRs-v2.md  ← all ADRs (authoritative)
├── _docs/IDP-GitOps-Blueprint-PRD.md
└── docs/agents/               ← this directory
```

## Before exploring, read these

1. **`docs/architect.md`** — comprehensive platform guide covering cluster topology, GitOps patterns, secrets management, progressive delivery, DR runbooks, and supply chain security. Read this first for any architecture question.
2. **`_docs/IDP-GitOps-ADRs-v2.md`** — all Architecture Decision Records. ADRs are the authoritative source of *why* decisions were made. Read ADRs that touch the area you're about to work in before proposing changes.
3. **`_docs/IDP-GitOps-Blueprint-PRD.md`** — Functional Requirements and Non-Goals. Use to verify whether a proposed feature is in scope.

There is no `CONTEXT.md` or `docs/adr/` at the repo root. Use the paths above instead. Proceed silently if they are missing — do not flag absence.

## Glossary

Domain terms are defined in `_docs/IDP-GitOps-Blueprint-v2.md §0.1`. Key terms:

| Term | Meaning |
|---|---|
| IDP | Internal Developer Platform — the full system this repo provisions |
| mgmt cluster | Management cluster running ArgoCD hub + Crossplane (`mgmt-we` active, `mgmt-ne` standby) |
| workload cluster | AKS cluster running tenant application workloads (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`) |
| App-of-Apps | ArgoCD pattern where one ApplicationSet manages child ApplicationSets/Applications |
| ESO | External Secrets Operator — syncs secrets from AKV into Kubernetes |
| UAMI | User-Assigned Managed Identity — per-namespace Azure identity for AKV access |
| AKV | Azure Key Vault |
| SLO class | bronze / silver / gold — tier of progressive delivery strictness for a workload namespace |
| XRD | Crossplane Composite Resource Definition — platform API type |
| Claim | Crossplane Composite Resource Claim — developer-facing CR that triggers provisioning |
| mgmt-leader-lease | Go controller that holds the Azure Storage Blob lease for the active management cluster |
| controller-scaler | Go controller that scales Crossplane/ArgoCD to zero on the standby mgmt cluster |
| seed cluster | `seed-wus` — West US 2 single-node DR bootstrap cluster |
| CAPZ | Cluster API Provider Azure — alternative infra provider (default is Crossplane) |

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined above. Don't drift to synonyms.

## Flag ADR conflicts

If your output contradicts an existing ADR in `_docs/IDP-GitOps-ADRs-v2.md`, surface it explicitly rather than silently overriding:

> _Contradicts ADR-020 (per-namespace UAMI isolation) — but worth reopening because…_

## Settled decisions — do not re-open without strong justification

- **ABAC on AKV data-plane**: Azure does not support attribute-based conditions on Key Vault data-plane RBAC. Per-namespace UAMI is the maximum isolation available. (ADR-020)
- **Cosmos Active-Passive writes**: `multipleWriteLocationsEnabled: false` for ALL SLO classes. `automaticFailoverEnabled: true` for all.
- **Argo Rollouts scope**: workload clusters only. Management clusters must NOT have Argo Rollouts installed.
- **User-traffic Front Door**: Non-Goal — application team responsibility, not platform layer.
