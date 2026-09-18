---
title: Data Model v4 F-001 — Service onboarding pipeline
id: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Data Model v4 F-001 — Service onboarding pipeline

> The slice of the data model this feature reads and writes, at field precision.

As built. Every field below was read from the code that defines it; the survey's
own entity extractor found none, so nothing here came from a generator.

## Scope

| Entity | Access | Evidence |
| --- | --- | --- |
| `service_request` | own (construct) | [D: tools/service_seed/jira_intake.py:162] |
| `slo_profile` | read | [D: tools/service_seed/service_template.py:217] |
| `rollout_profile` | read | [D: tools/service_seed/service_template.py:224] |
| `cluster_entry` | read | [D: tools/service_seed/service_template.py:270] |
| `gitops_manifest_file` | own (create) | [D: tools/service_seed/service_template.py:303] |
| `gitops_pull_request` | own (create) | [D: tools/service_seed/cli.py:384] |
| `seed_result` | own (emit) | [D: tools/service_seed/cli.py:408] |

Out of bounds: every arbitration, gate and secret entity. Nothing in this feature
reads or writes them.

## Feature ERD

[`schema/erd_v4_F-001.puml`](schema/erd_v4_F-001.puml).

## Field definitions

`service_request` [D: tools/service_seed/jira_intake.py:48]

| Field | Type | Constraint | Default | Evidence |
| --- | --- | --- | --- | --- |
| `issue_key` | string | non-empty | — | [D: tools/service_seed/jira_intake.py:64] |
| `issue_type` | const | `IDP Service Request` | — | [D: tools/service_seed/jira_intake.py:71] |
| `service_name` | string | non-empty | — | [D: tools/service_seed/jira_intake.py:64] |
| `service_slug` | string | `^[a-z0-9]+(?:-[a-z0-9]+)*$` | derived from `service_name` | [D: tools/service_seed/jira_intake.py:81] |
| `slo_class` | enum | `bronze`, `silver`, `gold` | `silver` | [D: tools/service_seed/jira_intake.py:159] |
| `summary` | string | — | the issue key | [D: tools/service_seed/jira_intake.py:181] |
| `description` | string | plain text | empty | [D: tools/service_seed/jira_intake.py:95] |

`slo_profile` [D: tools/service_seed/profiles/slo.yaml:14] · `rollout_profile` [D: tools/service_seed/profiles/rollout.yaml:12]

| Entity | Field | Type | Value set in tree |
| --- | --- | --- | --- |
| `slo_profile` | `has_analysis_template` | boolean | true for gold and silver, false for bronze [D: tools/service_seed/profiles/slo.yaml:25] |
| `slo_profile` | `include_p99_metric` | boolean | true for gold only [D: tools/service_seed/profiles/slo.yaml:15] |
| `slo_profile` | `analysis_interval` | string | `60s` for gold and silver [D: tools/service_seed/profiles/slo.yaml:16] |
| `slo_profile` | `success_rate_threshold` | number | `0.99` for gold and silver [D: tools/service_seed/profiles/slo.yaml:17] |
| `slo_profile` | `p99_latency_ms_threshold` | integer | `500`, gold only [D: tools/service_seed/profiles/slo.yaml:18] |
| `rollout_profile` | `steps` | string[] | gold 5→25→50→100 with `pause: {duration: 5m}` between [D: tools/service_seed/profiles/rollout.yaml:13] |

`cluster_entry` — read only; full field table in [`data-erd_v4_F-003.md`](data-erd_v4_F-003.md),
which owns the catalogue. This feature reads `region`, `resource_group`,
`subscription_id` and resolves two named production clusters
[D: tools/service_seed/service_template.py:46].

`gitops_manifest_file` [D: tools/service_seed/service_template.py:303]

| Field | Type | Constraint | Evidence |
| --- | --- | --- | --- |
| `service_slug` | string | slug pattern | [D: tools/service_seed/service_template.py:303] |
| `path` | string | starts `apps/<service_slug>/` | [D: tools/service_seed/service_template.py:303] |
| `tier` | enum | `infra`, `workload` | [D: tools/service_seed/cli.py:381] |
| `scope` | enum | `base`, `overlay` | [D: tools/service_seed/service_template.py:308] |
| `env` | enum | `dev`, `staging`, `prod`; overlay only | [D: tools/service_seed/service_template.py:53] |

`gitops_pull_request` [D: tools/service_seed/gitops_pr.py:63]

| Field | Type | Value in tree | Evidence |
| --- | --- | --- | --- |
| `repo_slug` | string | last URL segment, `.git` stripped | [D: tools/service_seed/gitops_pr.py:161] |
| `title` | string | `[Seed] Add <slug> <tier> scaffolding` | [D: tools/service_seed/cli.py:389] |
| `description` | string | `Seeded from Jira <key> (<class> SLO).` | [D: tools/service_seed/cli.py:390] |
| `source_branch` | string | `seed/<slug>-<tier>-<key lowercased>` | [D: tools/service_seed/cli.py:388] |
| `destination_branch` | string | `main` | [D: tools/service_seed/gitops_pr.py:61] |
| `close_source_branch` | boolean | `true` | [D: tools/service_seed/gitops_pr.py:68] |
| `path_root` | string | `apps` | [D: tools/service_seed/cli.py:391] |

`seed_result` [D: tools/service_seed/cli.py:408] — `service`, `issueKey`, `sloClass`,
camelCase as emitted.

Casing is the system's own: snake_case where Python and the registry define it,
camelCase where a JSON line or a ConfigMap key defines it. A style-normalised copy
would not match what a consumer parses.

## New and changed entities

F-001 introduces six of the model's fifteen entities — `service_request`,
`slo_profile`, `rollout_profile`, `gitops_manifest_file`, `gitops_pull_request`,
`seed_result` — and is the first consumer of `cluster_entry`, which F-003 owns.
Each has a registry row in [`data-master-erd.md`](data-master-erd.md).

## Migrations

No datastore and no migration directory in the surveyed tree. Three format changes
carry risk:

| # | Change | Forward | Reverse | Proof |
| --- | --- | --- | --- | --- |
| M1 | Add or change a tier numeric | Edit the profile file | Revert it | Re-render and diff the analysis and rollout manifests [D: tools/service_seed/service_template.py:217] |
| M2 | Add an SLO class | Both profile files in one change | Revert both | Import no longer raises on the class cross-check [D: tools/service_seed/service_template.py:99] |
| M3 | Change the generated path layout | Template tree and this document together | Revert both | A re-seed shows only the intended moves; already-merged services keep the old paths until re-rendered |

I: M3 is the only change with a compatibility hazard — basis: the renderer removes and rewrites a service's subtree wholesale, so two layouts can coexist in the repository only across services, never within one [D: tools/service_seed/gitops_pr.py:152].

## Traceability

| Field or constraint | Satisfies |
| --- | --- |
| `issue_type` const | DOM-001-R1 · F-001-US2 |
| `service_name` non-empty | DOM-001-R2 · F-001-US2 |
| `service_slug` pattern | DOM-001-R3 · F-001-US1 |
| `slo_class` enum with silver default | DOM-001-R4 · F-001-US1 |
| frozen record | DOM-001-R5 · F-001-US1 |
| `tier` partition and path prefix | DOM-001-R6, DOM-001-R9 · F-001-US4, F-001-US5 |
| three `env` overlays | DOM-001-R7 · F-001-US5 |
| profiles keyed by class | DOM-001-R8 · F-001-US6 |
| `cluster_entry` as the only identity source | DOM-001-R10 · F-001-US7 |

## Open questions

- OPEN: `docs/architect.md:600` says the request validates an owner team; no such
  field exists [D: tools/service_seed/jira_intake.py:48]. Dropped requirement or
  unimplemented one?
- OPEN: `silver` and `gold` carry the same `success_rate_threshold`
  [D: tools/service_seed/profiles/slo.yaml:23]; silver differs only by the absent
  latency gate. Is that the intended tier contract?
- OPEN: The renderer hardcodes two production cluster keys and two vault names
  [D: tools/service_seed/service_template.py:46]. Is a third production region a
  registry change, a code change, or both?
- OPEN: No retention or archival rule exists for a seeded service's scaffold when
  the service is retired; nothing in the tree removes one.
