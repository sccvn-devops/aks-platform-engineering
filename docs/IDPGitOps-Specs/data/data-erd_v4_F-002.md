---
title: Data Model v4 F-002 — Management plane arbitration
id: F-002
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Data Model v4 F-002 — Management plane arbitration

> The slice of the data model this feature reads and writes, at field precision.

## Scope

| Entity | Access | Evidence |
| --- | --- | --- |
| `lease_controller_config` | own (read at startup) | [D: tools/mgmt-plane-lock/internal/config/config.go:61] |
| `mgmt_leader_status` | own (written by the lease loop, read by the scaling loop) | [D: tools/mgmt-plane-lock/internal/kube/status.go:25] [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:83] |
| `controller_workload` | own (read) | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34] |
| `cluster_entry` | read (role only) | [D: gitops/clusters/registry.yaml:40] |

Out of bounds: every onboarding, gate and secret entity.

## Feature ERD

[`schema/erd_v4_F-002.puml`](schema/erd_v4_F-002.puml).

## Field definitions

`lease_controller_config` [D: tools/mgmt-plane-lock/internal/config/config.go:22]

| Field | Type | Constraint | Default | Evidence |
| --- | --- | --- | --- | --- |
| `cluster_name` | string | required; empty refuses startup | — | [D: tools/mgmt-plane-lock/internal/config/config.go:74] |
| `lease_blob_url` | string | required; empty refuses startup | — | [D: tools/mgmt-plane-lock/internal/config/config.go:77] |
| `status_namespace` | string | — | `kube-system` | [D: tools/mgmt-plane-lock/internal/config/config.go:13] |
| `status_configmap` | string | — | `mgmt-leader-status` | [D: tools/mgmt-plane-lock/internal/config/config.go:14] |
| `metrics_addr` | string | — | `:8080` | [D: tools/mgmt-plane-lock/internal/config/config.go:15] |
| `lease_duration_seconds` | integer | 15…60 inclusive; outside refuses startup | 60 | [D: tools/mgmt-plane-lock/internal/config/config.go:79] |
| `renew_interval_seconds` | integer | positive, and strictly shorter than the term | 15 | [D: tools/mgmt-plane-lock/internal/config/config.go:85] |
| `preferred_metadata_key` | string | — | `preferred-cluster` | [D: tools/mgmt-plane-lock/internal/config/config.go:70] |

A non-numeric or non-positive value for either duration silently falls back to the
default rather than failing [D: tools/mgmt-plane-lock/internal/config/config.go:172].

`mgmt_leader_status` [D: tools/mgmt-plane-lock/internal/kube/status.go:35] — the
eight ConfigMap keys, written together on every upsert. `holderIdentity` is the
publishing cluster's own name [D: tools/mgmt-plane-lock/internal/kube/status.go:37];
`lastObservedAt` is written at upsert time
[D: tools/mgmt-plane-lock/internal/kube/status.go:42]; the reader accepts only
`active` and `standby` and rejects anything else, empty included
[D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91].

`controller_workload` [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:25] —
the declared set is six entries: the ArgoCD application controller (StatefulSet, 3
active), repo server and server (2 each), Crossplane (1), and two marked optional,
External Secrets and the Jira bridge
[D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34]. Every entry's standby
count is zero [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:40].

## New and changed entities

F-002 introduces `lease_controller_config`, `mgmt_leader_status` and
`controller_workload`; it changes nothing another feature introduced. Registry rows
are in [`data-master-erd.md`](data-master-erd.md).

## Migrations

| # | Change | Order | Proof | Evidence |
| --- | --- | --- | --- | --- |
| M1 | Add a key to the status object | Writer first, readers second | Both clusters publish and the scaling loop still reconciles | [D: tools/mgmt-plane-lock/internal/kube/status.go:64] |
| M2 | Add or remove a governed workload | Ship both controllers together | Active cluster reaches the declared count; standby reads zero | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34] |
| M3 | Change the lease term or renewal rhythm | One cluster at a time | The controller starts; an unsafe pair exits at startup instead | [D: tools/mgmt-plane-lock/internal/config/config.go:85] |

I: M1's ordering is the reverse of the usual advice — basis: the status object is replaced wholesale on every upsert, so a reader deployed first would look for a key no writer produces yet [D: tools/mgmt-plane-lock/internal/kube/status.go:64].

## Traceability

| Field or constraint | Satisfies |
| --- | --- |
| Term bounded 15…60, rhythm strictly shorter | DOM-002-R4 · F-002-US6 |
| `cluster_name` and `lease_blob_url` required | DOM-002-R4 · F-002-US6 |
| `leadershipStatus` two-valued, anything else rejected | DOM-002-R2, DOM-002-R5 · F-002-US2 |
| `lastRenewedAt` | DOM-002-R3, DOM-002-R11 · F-002-US3, F-002-US7 |
| `standby_replicas` fixed at 0 | DOM-002-R5 · F-002-US2 |
| `optional` | DOM-002-R9 · F-002-US2 |

## Open questions

- OPEN: A malformed duration falls back to the default rather than refusing startup [D: tools/mgmt-plane-lock/internal/config/config.go:172], while an out-of-range one refuses [D: tools/mgmt-plane-lock/internal/config/config.go:79]. Is that split intentional?
- OPEN: The governed workload list is compiled in [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34]; adding a platform component means shipping a binary. Should it be data?
- OPEN: `controller_workload.active_replicas` records 3 for the ArgoCD application controller [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:40]; nothing states where those counts come from or what they should be per cluster size.
