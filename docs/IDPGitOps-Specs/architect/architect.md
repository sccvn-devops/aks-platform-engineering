---
title: System Architecture — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# System Architecture — IDPGitOps

> System-level architecture: components, data, integration points and the ADRs behind them.

Reconstructed from the module layout, the import graph and the manifests. What the
code cannot show — why a boundary sits where it does, what the platform is for — is
an OPEN: question or a pointer to the repository's own prose, never a paragraph
invented here.

## System context

| External party | Direction | What crosses | Evidence |
| --- | --- | --- | --- |
| Issue tracker | in / out | Service requests read in; drift tickets written out | [D: tools/service_seed/jira_intake.py:205] [D: tools/mgmt-plane-lock/internal/jirabridge/jirabridge.go:1] |
| Source host | out | Repositories created, branches pushed, change sets opened | [D: tools/service_seed/gitops_pr.py:38] |
| Azure Resource Manager | out | Everything Terraform provisions | [D: terraform/clusters.tf:1] |
| Azure Key Vault | in / out | Secret material read and written | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:81] |
| Azure Storage | in / out | The arbitration lease | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] |
| Container registry | out | Image host per cluster | [D: gitops/clusters/registry.yaml:39] |
| Metrics scraper | in | The one served route | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:54] |

## Component view

| Component | Responsibility | Owns | Evidence |
| --- | --- | --- | --- |
| Cluster registry | Committed catalogue of cluster identity | `cluster_entry` | [D: gitops/clusters/registry.yaml:1] |
| Terraform root | Clusters, network, vaults, registry, state, CI, ArgoCD bootstrap | Azure resources | [D: terraform/main.tf:1] |
| Workload-identity module | One identity plus federation and role assignments | — | [D: terraform/modules/workload_identity/main.tf:1] |
| Onboarding pipeline | Request to repository plus two change sets | `service_request`, manifests, change sets, result | [D: tools/service_seed/cli.py:340] |
| Release profiles | Per-tier analysis and canary values | `slo_profile`, `rollout_profile` | [D: tools/service_seed/profiles/slo.yaml:13] |
| `mgmt-leader-lease` | Holds and renews the arbitration lease; publishes status | `mgmt_leader_status` | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139] |
| `controller-scaler` | Brings governed controllers to the implied replica count | `controller_workload` | [D: tools/mgmt-plane-lock/internal/scaling/runner.go:112] |
| `saas-token-rotator` | Mints and dual-writes SaaS credentials | rotation state | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] |
| `argocd-jira-bridge` | Opens a ticket on drift or a failed rollout | bridge state | [D: tools/mgmt-plane-lock/internal/jirabridge/runner.go:1] |
| `mgmt-cli` | Operator failback and break-lease | — | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:23] |
| AKV writer | The single vault write path | `secret_write` | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76] |
| Transport seam | Bounded retries and redacted errors for every outbound call | — | [D: tools/mgmt-plane-lock/internal/httpx/httpx.go:1] |
| Invariant gates | Repository-wide checks in pre-commit and CI | baseline, pins | [D: .pre-commit-config.yaml:48] |
| Platform charts | Delivery of the controllers themselves | — | [D: gitops/platform/mgmt-plane-lock/Chart.yaml:1] |
| Backstage | Developer portal | catalogue entities | [D: backstage/package.json:1] |

## Data architecture

The entity registry is [`../data/data-master-erd.md`](../data/data-master-erd.md);
shapes live in `../data/schema/schemas.json`. Nothing is restated here.

At system level: **the platform owns no database**. Durable state is a committed
file [D: gitops/clusters/registry.yaml:1], a vault
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:81], a cluster object
[D: tools/mgmt-plane-lock/internal/kube/status.go:35] or an external SaaS record
[D: tools/service_seed/jira_intake.py:205]. Terraform state is the one
platform-owned store, partitioned per environment and cluster
[D: scripts/validate-state-partitioning.sh:1].

## API surface

| Surface | Purpose | Evidence |
| --- | --- | --- |
| `/metrics` on the controllers | The only served route in the repository | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:54] |
| `service-seed` and `service-seed-registry` | Onboarding and catalogue inspection | [D: tools/service_seed/pyproject.toml:47] |
| `mgmt-cli` | Operator arbitration control | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:25] |
| The validators | Repository gates, exit-code contract | [D: scripts/validate-action-pins.py:169] |
| Outbound: tracker, source host, vault, storage | Everything the platform calls | [D: tools/service_seed/jira_intake.py:212] |

Per-feature contracts: `../data/api-contract_v4_F-00{1,2,3,4}.md`.

## Cross-cutting concerns

Rules and enforcement: [`architect_common.md`](architect_common.md). System-level
shape:

- **Configuration** is environment variables with defaults and startup refusals [D: tools/mgmt-plane-lock/internal/config/config.go:61], plus committed data for anything per-cluster [D: gitops/clusters/registry.yaml:1].
- **Transport** is one seam with bounded retries [D: tools/mgmt-plane-lock/internal/httpx/httpx.go:1].
- **Errors** are typed at each boundary [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58] [D: tools/service_seed/jira_intake.py:35].
- **Redaction** is default-on in the transport seam [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208].
- **Observability** is Prometheus metrics from the Go controllers [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:48]; the Python tooling emits none [D: tools/service_seed/cli.py:408].
- OPEN: no distributed tracing appears anywhere in the tree.

## Decision index

The repository carries its own decision record; this hub cites it and does not
restate it. See [ADR-0001](../ADRs/adrs_ADR-0001-record-architecture-decisions.md).

| Decision visible in code | Where the reasoning lives | Evidence in code |
| --- | --- | --- |
| Blob lease arbitrates the active management cluster | `_docs/IDP-GitOps-ADRs-v2.md` (ADR-022) | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] |
| Standby runs platform controllers at zero | ADR-017 | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41] |
| Cluster topology is committed data | ADR-031-v4 | [D: gitops/clusters/registry.yaml:1] |
| Two-tier GitOps split | ADR-013-v2 | [D: tools/service_seed/cli.py:381] |
| Tiered progressive delivery | ADR-018, ADR-021 | [D: tools/service_seed/profiles/rollout.yaml:12] |
| ESO with a per-region vault pair | ADR-005-v2, ADR-019 | [D: terraform/locals.tf:38] |
| Per-namespace identity | ADR-020 | [D: terraform/modules/workload_identity/main.tf:1] |
| Terraform pinned at 1.5.x | ADR-029-v3 | [D: .tool-versions:16] |
| Per-environment state with a delete lock | ADR-023-v3 | [D: scripts/bootstrap-tfstate.sh:59] |

OPEN: Several behaviours in the code have no ADR at all — the preferred-cluster
release path, the 5-second controller tick, the grace period before disabling
superseded secret versions, and the choice of single-replica controllers. Each is a
decision in force with no recorded reasoning.

## Feature architecture index

| Version | Feature | Link |
| --- | --- | --- |
| v4 | F-001 Service onboarding pipeline | [feature_v4_F-001_architect.md](feature_v4_F-001_architect.md) |
| v4 | F-002 Management plane arbitration | [feature_v4_F-002_architect.md](feature_v4_F-002_architect.md) |
| v4 | F-003 Platform invariant gates | [feature_v4_F-003_architect.md](feature_v4_F-003_architect.md) |
| v4 | F-004 Secret and token lifecycle | [feature_v4_F-004_architect.md](feature_v4_F-004_architect.md) |

Components with no feature document: the drift bridge, the portal, the GitOps
bootstrap itself and the seed-cluster path. They appear in the component view so
references resolve, and they are listed as coverage holes in the reverse-docs report.
