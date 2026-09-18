---
title: Tasks v4 F-002 — Management plane arbitration
id: F-002
kind: tasks
feature: F-002
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Tasks v4 F-002 — Management plane arbitration

> Ordered, dependency-aware implementation tasks — plan only, no code.

**As-built inventory.** Each row names work that exists and points at its artifact;
the done-when column is the check a `done` status would have to cash.

## Task list

| Task | Description | Depends on | Artifact | Done-when | Status |
| --- | --- | --- | --- | --- | --- |
| F-002-T1 | Environment configuration for all four binaries, with defaults and startup refusals | — | [D: tools/mgmt-plane-lock/internal/config/config.go:61] | `go test ./internal/config/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T2 | Lease manager: acquire, renew, release, break, and the preferred-cluster metadata read and write | — | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] | `go test ./internal/bloblease/... -run Manager` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T3 | Typed conflict and missing-lease helpers so callers branch on cause, not on text | — | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:152] | `go test ./internal/bloblease/... -run IsLease` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T4 | Leadership status upsert, creating the object with its labels when absent | F-002-T1 | [D: tools/mgmt-plane-lock/internal/kube/status.go:25] | `go test ./internal/kube/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T5 | Lease loop: preference check, renew cadence, acquire, status publication, metric export | F-002-T2, F-002-T3, F-002-T4 | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139] | `go test ./internal/bloblease/... -run Runner` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T6 | Shutdown drain on a fresh context, skipped when not the holder | F-002-T5 | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:120] | `go test ./internal/bloblease/... -run Drain` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T7 | Governed workload list and patching, tolerating optional absences and no-op patches | — | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34] | `go test ./internal/scaling/... -run Reconcile` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T8 | Scaling loop: read status, reconcile counts, label the ArgoCD cluster Secret | F-002-T4, F-002-T7 | [D: tools/mgmt-plane-lock/internal/scaling/runner.go:112] | `go test ./internal/scaling/... -run Runner` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T9 | Operator CLI: failback sets the preference and breaks the lease; break-lease does only the latter | F-002-T2 | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43] | `go test ./cmd/mgmt-cli/...` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T10 | Lifecycle: signal-aware context and a metrics server shared by every binary | F-002-T5, F-002-T8 | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:33] | `go test ./internal/bootstrap/...` passes and `bash scripts/validate-mgmt-plane-lock-runners.sh` passes | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 |
| F-002-T11 | Failover drill scripts covering promotion and lease locking | F-002-T5, F-002-T8 | [D: scripts/dr-validation/validate-mgmt-failover.sh:1] | The drill runs and reports an observed handover time | wip — unverified, run scripts/dr-validation/validate-mgmt-failover.sh against a live pair |

## Execution order

```
group A   T1 config · T2 lease manager · T3 typed errors · T7 workload list
group B   T4 status upsert (T1) · T9 operator CLI (T2)
group C   T5 lease loop (T2,T3,T4) · T8 scaling loop (T4,T7)
group D   T6 drain (T5) · T10 lifecycle (T5,T8)
group E   T11 drill (T5,T8)
```

I: the two loops are genuinely independent — basis: neither package imports the other, and they communicate only through the status object [D: tools/mgmt-plane-lock/internal/scaling/runner.go:113].

## Definition of done

- [ ] The done-when command ran here and passed; its output is the status note.
- [ ] No new way exists for a cluster to act as active without holding the lease.
- [ ] Bounds stay refusals, not silent corrections.
- [ ] Tests use the narrow fake interfaces; none needs Azure or a live cluster.
- [ ] Every `cmd/*/main.go` stays wiring-only.

## Open questions

- OPEN: T11's drill is not wired into any automation the survey could find; is it run on a schedule, and by whom?
- OPEN: No task covers a both-clusters-partitioned rehearsal, which is the one failure the design most depends on.
