---
title: Feature Architecture v4 F-002 — Management plane arbitration
id: F-002
kind: architecture
feature: F-002
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Feature Architecture v4 F-002 — Management plane arbitration

> Low-level design for one feature: contracts, schema, sequence, failure modes.

## Design summary

I: activeness is a renewable lease rather than a stored flag — basis: a cluster sets its own leader state only after a successful acquire [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192], and drops it whenever a renew fails [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:156].

I: the two controllers are decoupled through a cluster-local object rather than a shared process — basis: the lease loop writes the status [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:168] and the scaling loop reads it and never touches the lease [D: tools/mgmt-plane-lock/internal/scaling/runner.go:113].

Neither controller is highly available in the tree: each runs its own single loop
with a 5-second default tick
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:80]. OPEN: whether that is
a deliberate self-healing choice or simply what was built is not recorded anywhere.

## API contracts

Surfaces and error model: [`../data/api-contract_v4_F-002.md`](../data/api-contract_v4_F-002.md).

Behaviour that matters and is easy to misread:

- The preferred-cluster metadata is **not advisory**. A holder that reads a
  preference naming another cluster releases the lease and publishes standby
  [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146].
- A non-holder that reads a preference naming another cluster does not even attempt
  to acquire [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:181].
- The scaling loop also writes a `lease-status` label onto the ArgoCD cluster Secret
  [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115].

## Data model

Scope and fields: [`../data/data-erd_v4_F-002.md`](../data/data-erd_v4_F-002.md).
Owns `lease_controller_config`, `mgmt_leader_status`, `controller_workload`; reads
the catalogued cluster role. The lease itself is not modelled — its state lives in
Azure Storage [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43].

## Sequence

Lease loop, per tick [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:89]:

1. Read the preferred-cluster metadata [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:140].
2. If this cluster holds the lease and the preference names another, release, clear state, publish standby [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146].
3. If it holds the lease and the renewal interval has elapsed, renew [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:154].
4. On a successful renew, record the moment and export it [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:162].
5. Publish active status [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:168].
6. If it does not hold the lease and no other cluster is preferred, acquire [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:184].
7. On acquire, set state, record the moment, export it, publish active [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192].

Scaling loop, per cycle [D: tools/mgmt-plane-lock/internal/scaling/runner.go:112]:

1. Read the leadership status [D: tools/mgmt-plane-lock/internal/scaling/runner.go:113].
2. Reconcile every governed workload to the count that status implies [D: tools/mgmt-plane-lock/internal/scaling/runner.go:118].
3. Label the ArgoCD cluster Secret with the same status [D: tools/mgmt-plane-lock/internal/scaling/runner.go:122].

Shutdown: the loop exits on context cancellation and drains
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:95]; the drain releases only
when this cluster is the holder
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:121] and uses a fresh context
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:130].

## Failure modes

| Failure | Detection | Behaviour | Evidence |
| --- | --- | --- | --- |
| Another cluster holds the lease | Conflict on acquire | Publish standby, no error | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:189] |
| Renew conflicts or the lease is gone | Typed conflict check | Drop leadership, publish standby | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:157] |
| Renew fails otherwise | Same branch, other side | Drop leadership and surface the error | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:158] |
| Preference names another cluster | Metadata read each tick | Release and stand down | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146] |
| Status unreadable or unrecognised | Read rejects the value | Nothing scales up | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91] |
| Governed Deployment absent | NotFound from the API | Optional entries return nil; required ones error | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:164] |
| Already at the desired count | Replica comparison | No patch issued | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:171] |
| Missing dependency at construction | Nil check before the loop | Sentinel error | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:75] |
| Unsafe configuration | Startup validation | Process refuses to start | [D: tools/mgmt-plane-lock/internal/config/config.go:79] |
| Shutdown while holding the lease | Context cancellation | Release plus standby publish on a fresh context | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:130] |

## Observability

| Signal | Where | Evidence |
| --- | --- | --- |
| Last-successful-renew timestamp | Gauge on the metrics endpoint | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164] |
| Leadership status object | Cluster-local, readable during an arbiter outage | [D: tools/mgmt-plane-lock/internal/kube/status.go:35] |
| Lease transitions and scaling decisions | Controller logs through an injectable logger | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:57] |
| `lease-status` label on the ArgoCD cluster Secret | Cluster state | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115] |

- OPEN: No metric records how long a handover took, so the recovery objective quoted in `docs/architect.md:651` cannot be observed from this feature's own signals.
- OPEN: Nothing exports whether the two clusters disagree; a split would be visible only by reading both status objects.

## Traceability

| Design element | Satisfies |
| --- | --- |
| Acquire-then-set-state | F-002-US1 · DOM-002-R1, R2 |
| Renew cadence and expiry | F-002-US1 · DOM-002-R3 |
| Startup validation of term and rhythm | F-002-US6 · DOM-002-R4 |
| Status-driven replica reconciliation | F-002-US2 · DOM-002-R5, R7 |
| Optional entries tolerated | F-002-US2 · DOM-002-R9 |
| Status publication with both timestamps | F-002-US3 · DOM-002-R6 |
| Drain on shutdown | F-002-US4 · DOM-002-R8 |
| Preference-driven release and failback command | F-002-US5 · DOM-002-R10 |
| Renewal gauge | F-002-US7 · DOM-002-R11 |
