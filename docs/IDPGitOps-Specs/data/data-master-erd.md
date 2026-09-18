---
title: Master Data Model — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Master Data Model — IDPGitOps

> The system-wide entity registry and relationship map. Every entity is introduced here once and referenced everywhere else.

Reconstructed from code. The automated survey reported **zero entities** for this
repository, because its extractor looks for DDL, migrations and ORM models and
this platform has none of those. Every entity below was read out of a Go struct, a
Python frozen dataclass, a committed YAML catalogue or an in-repo JSON Schema, and
carries the `path:line` it came from.

## Entity registry

| Entity | Owning component | Introduced by | System of record | Defined at | Retention |
| --- | --- | --- | --- | --- | --- |
| `service_request` | Onboarding intake | F-001 | Issue tracker; this is the accepted projection | [D: tools/service_seed/jira_intake.py:48] | Run-scoped |
| `cluster_entry` | Cluster registry | F-001 (first consumer), F-003 (gate) | `gitops/clusters/registry.yaml` | [D: tools/service_seed/cli.py:43] | Git history |
| `slo_profile` | Release profiles | F-001 | `tools/service_seed/profiles/slo.yaml` | [D: tools/service_seed/profiles/slo.yaml:14] | Git history |
| `rollout_profile` | Release profiles | F-001 | `tools/service_seed/profiles/rollout.yaml` | [D: tools/service_seed/profiles/rollout.yaml:12] | Git history |
| `gitops_manifest_file` | Onboarding render | F-001 | Platform GitOps repository, after merge | [D: tools/service_seed/service_template.py:251] | Git history |
| `gitops_pull_request` | Onboarding git layer | F-001 | Source host | [D: tools/service_seed/gitops_pr.py:63] | Host policy |
| `seed_result` | Onboarding wiring | F-001 | Not persisted — one stdout line | [D: tools/service_seed/cli.py:408] | CI log |
| `mgmt_leader_status` | Lease controller | F-002 | ConfigMap in the cluster it describes | [D: tools/mgmt-plane-lock/internal/kube/status.go:35] | Current value only |
| `lease_controller_config` | Lease controller | F-002 | Process environment | [D: tools/mgmt-plane-lock/internal/config/config.go:22] | Pod lifetime |
| `controller_workload` | Scaling reconciler | F-002 | Compiled list in the reconciler | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34] | Release-scoped |
| `checkov_baseline_finding` | Invariant gates | F-003 | `.checkov.baseline` | [D: .checkov.baseline:6] | Git history |
| `tool_version_pin` | Invariant gates | F-003 | `.tool-versions` | [D: .tool-versions:11] | Git history |
| `akv_secret_entry` | Secret lifecycle | F-004 | `terraform/locals.tf` catalogues | [D: terraform/locals.tf:41] | Git history |
| `akv_secret_version` | Secret lifecycle | F-004 | Azure Key Vault | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65] | Vault version history |
| `secret_write` | Secret lifecycle | F-004 | Not persisted — one call through the write path | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76] | — |

Fifteen entities, fifteen `$defs` entries in [`schema/schemas.json`](schema/schemas.json),
fifteen boxes in [`schema/erd_master.puml`](schema/erd_master.puml).

## Master ERD

Diagram: [`schema/erd_master.puml`](schema/erd_master.puml).

What the boxes cannot show:

- **There is no database.** Every system-of-record column above is a file, a
  cluster object, a vault or a process boundary. No connection string to a
  platform-owned datastore appears anywhere in the surveyed tree.
- I: `service_request` is a projection of a tracker issue rather than a record the platform owns — basis: it is constructed only inside the parse function from a fetched payload and is frozen on construction, and no code path writes it back [D: tools/service_seed/jira_intake.py:162].
- I: the tier profiles are modelled as entities rather than configuration — basis: they are keyed data loaded once at import and cross-checked against a fixed class set, and the renderer indexes them by `slo_class` [D: tools/service_seed/service_template.py:95].
- **The blob lease is deliberately absent.** Its authoritative state lives in Azure
  Storage; the only local trace is `leaseID` and `lastRenewedAt` inside
  `mgmt_leader_status` [D: tools/mgmt-plane-lock/internal/kube/status.go:35].
  Modelling it locally would create a second answer to the one question the design
  keeps single.
- **`gitops_manifest_file` models the path, not the bytes.** The renderer returns a
  path→content mapping; content is template output with no identity or lifecycle
  [D: tools/service_seed/service_template.py:251].

## Relationships and cardinality

| From | To | Cardinality | Who may delete | Referential rule |
| --- | --- | --- | --- | --- |
| `service_request` | `gitops_manifest_file` | 1 → many | Re-seed removes the service's whole subtree first [D: tools/service_seed/gitops_pr.py:152] | Every path begins `apps/<service_slug>/` [D: tools/service_seed/service_template.py:303] |
| `service_request` | `gitops_pull_request` | 1 → exactly 2 | Source host, on merge or close | Two calls, one per tier [D: tools/service_seed/cli.py:384] |
| `service_request` | `seed_result` | 1 → 1 | Not persisted | `issueKey` is the request key [D: tools/service_seed/cli.py:408] |
| `gitops_pull_request` | `gitops_manifest_file` | 1 → many | — | Partitioned by an `infra/` or `workload/` path segment, so the sets are disjoint [D: tools/service_seed/cli.py:381] |
| `slo_profile` / `rollout_profile` | `service_request` | 1 → many | Profile edit | A class missing from either profile raises at import [D: tools/service_seed/service_template.py:99] |
| `cluster_entry` | `gitops_manifest_file` | 1 → many | Registry PR | Render resolves two named production clusters [D: tools/service_seed/service_template.py:270] |
| `lease_controller_config` | `mgmt_leader_status` | 1 → 1 | Pod lifecycle | `clusterName` is the configured cluster name [D: tools/mgmt-plane-lock/internal/config/config.go:63] |
| `mgmt_leader_status` | `controller_workload` | 1 → many | — | One status decides every governed workload in that cluster [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:100] |
| `cluster_entry` | `mgmt_leader_status` | 1 → 0..1 | — | I: only management clusters have one — basis: the catalogue marks exactly two clusters `active`/`standby` and the controllers are deployed from the management-plane chart [D: gitops/clusters/registry.yaml:40] |
| `akv_secret_entry` | `akv_secret_version` | 1 → many | Vault policy | Superseded versions are disabled, not deleted [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308] |
| `secret_write` | `akv_secret_version` | 1 → 0..1 | — | A write against a soft-deleted secret fails unless the recovering strategy is chosen [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50] |

## Identity and keys

Every key here is natural; nothing in the surveyed tree mints a surrogate id.

| Entity | Key | Evidence |
| --- | --- | --- |
| `service_request` | `issue_key`, with `service_slug` as a second unique key | [D: tools/service_seed/jira_intake.py:64] |
| `cluster_entry` | `name`, which must equal `aks_name` | [D: tools/service_seed/cli.py:201] |
| `slo_profile`, `rollout_profile` | `slo_class` | [D: tools/service_seed/service_template.py:98] |
| `gitops_manifest_file` | `path` — it encodes service, tier, scope and environment | [D: tools/service_seed/service_template.py:303] |
| `gitops_pull_request` | `source_branch` | [D: tools/service_seed/cli.py:388] |
| `seed_result` | `issueKey` | [D: tools/service_seed/cli.py:408] |
| `mgmt_leader_status` | `clusterName`, one object per cluster, upserted | [D: tools/mgmt-plane-lock/internal/kube/status.go:25] |
| `lease_controller_config` | `cluster_name` | [D: tools/mgmt-plane-lock/internal/config/config.go:63] |
| `controller_workload` | (`namespace`, `name`) | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:25] |
| `tool_version_pin` | `tool` | [D: .tool-versions:17] |
| `akv_secret_entry` | `name` | [D: terraform/locals.tf:42] |
| `akv_secret_version` | `name` plus the vault's own version identifier | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:65] |

Cross-entity: I: one `service_slug` maps to one repository, one scaffold subtree
and one namespace prefix per environment — basis: the same slug is used as the
repository name, the path root and the namespace prefix at three separate call
sites [D: tools/service_seed/cli.py:371]. And at most one `mgmt_leader_status` in
the platform may read `active`; that is upheld by the arbiter, not by validation
[D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43].

## Consistency rules

| # | Rule | Enforced at | Traced to |
| --- | --- | --- | --- |
| C1 | A `service_request` exists only fully valid | [D: tools/service_seed/jira_intake.py:64] | DOM-001-R1…R5 |
| C2 | Every rendered path starts `apps/<service_slug>/` | [D: tools/service_seed/service_template.py:303] | DOM-001-R9 |
| C3 | The two change sets are disjoint | [D: tools/service_seed/cli.py:381] | DOM-001-R6 |
| C4 | Three overlay environments, in `dev` → `staging` → `prod` order | [D: tools/service_seed/service_template.py:53] | DOM-001-R7 |
| C5 | Both profile files declare the same three classes, checked at import | [D: tools/service_seed/service_template.py:99] | DOM-001-R8 |
| C6 | `cluster_entry.name` equals `aks_name` | [D: tools/service_seed/cli.py:201] | DOM-003-R4 |
| C7 | `region_abbrev` matches the canonical region map | [D: terraform/invariants.tf:95] | DOM-003-R5 |
| C8 | At most one cluster reads `active` | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] | DOM-002-R1 |
| C9 | `renew_interval_seconds` < `lease_duration_seconds`, term within 15…60 s | [D: tools/mgmt-plane-lock/internal/config/config.go:79] | DOM-002-R4 |
| C10 | A standby cluster's governed workloads all sit at zero | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:100] | DOM-002-R5 |
| C11 | Every suppressed finding carries its three required metadata fields | [D: scripts/validate-checkov-baseline.py:91] | DOM-003-R1 |
| C12 | Exactly one file declares a toolchain version | [D: scripts/validate-tool-version-singleton.py:1] | DOM-003-R2 |
| C13 | A `terraform_managed` secret also exists as a Terraform resource | [D: terraform/locals.tf:38] | DOM-004-R2 |

## Change policy

No datastore, so "migration" means changing a file format, a cluster object or an
emitted line:

| Change | Order | Why that order |
| --- | --- | --- |
| Add a required field to `cluster_entry` | Schema, loader required-set and every entry, in one change | The loader rejects an entry missing a required field, so a partial change breaks every consumer at once [D: tools/service_seed/cli.py:198] |
| Add a key to `mgmt_leader_status` | Writer first, readers second | The object is replaced wholesale on every upsert, so a reader deployed first looks for a key nobody writes [D: tools/mgmt-plane-lock/internal/kube/status.go:64] |
| Add an SLO class | Both profile files in one change | The import-time cross-check refuses to start otherwise [D: tools/service_seed/service_template.py:99] |
| Change a tier numeric | Profile file only | Already-merged services keep their manifests until re-rendered [D: tools/service_seed/service_template.py:251] |
| Add a suppressed finding | Baseline entry with all three metadata fields | CI fails the PR without them [D: scripts/validate-checkov-baseline.py:91] |
| Bump a tool version | `.tool-versions` only | A second declaration fails the singleton check [D: .tool-versions:11] |
| Rename any key on `mgmt_leader_status` or `seed_result` | Add, dual-write, migrate readers, remove | Both are read outside this repository |

## Open questions

- OPEN: `docs/architect.md:600` states that the accepted request validates an
  **owner team**. No such field exists in the dataclass or its validation
  [D: tools/service_seed/jira_intake.py:48]. Is owner team a dropped requirement or
  an unimplemented one? Until that is answered, `owner_team` is deliberately absent
  from `schemas.json`.
- OPEN: `controller_workload` is compiled into the binary rather than committed
  as data like `cluster_entry` [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34].
  Should "which components are platform components" be reviewable in a pull request?
- OPEN: `cluster_entry.subscription_id` holds placeholder values by design
  [D: gitops/clusters/registry.yaml:29]. Where does the live value come from at
  apply time, and should the catalogue carry a pointer to it?
- OPEN: Retention is unstated for every entity whose store is external — the
  vault's version retention, the tracker's issue retention and the source host's
  branch policy are not expressed anywhere in this repository.
- OPEN: `secret_write.outcome` enumerates the typed sentinels a caller can
  observe [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58]; whether a
  failed write is retried, and by whom, is not visible in the surveyed tree.
