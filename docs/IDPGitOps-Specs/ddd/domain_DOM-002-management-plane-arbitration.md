---
title: Domain — Management plane arbitration
id: DOM-002
kind: domain
feature: F-002
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Domain — Management plane arbitration

> Business rules, process and user flow for this domain, in the language of the business.

## Ubiquitous language

| Term | Definition | Source |
| --- | --- | --- |
| Active | The state a cluster publishes while it holds the lease | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:14] |
| Standby | The state a cluster publishes otherwise | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:15] |
| Lease | Time-bounded possession of one blob, acquired and renewed | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] |
| Lease term | How long possession survives without renewal, 15–60 s | [D: tools/mgmt-plane-lock/internal/config/config.go:79] |
| Renewal interval | How often the holder re-asserts, strictly shorter than the term | [D: tools/mgmt-plane-lock/internal/config/config.go:85] |
| Leadership status | The cluster-local object stating what this cluster believes it is | [D: tools/mgmt-plane-lock/internal/kube/status.go:35] |
| Preferred cluster | Operator-recorded blob metadata naming who should hold the lease | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:119] |
| Governed workload | A platform controller whose replica count arbitration decides | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:25] |
| Drain | Releasing the lease and publishing standby before exit | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:120] |
| Failback | Operator-initiated transfer: set the preference, break the lease | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43] |

## Actors

| Actor | Role | Evidence |
| --- | --- | --- |
| Lease holder | The cluster whose acquire succeeded | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192] |
| Contender | A cluster whose acquire conflicts | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:189] |
| Arbiter | The blob whose possession decides activeness | [D: tools/mgmt-plane-lock/internal/config/config.go:64] |
| Scaling reconciler | Reads the status, sets replica counts, labels the cluster Secret | [D: tools/mgmt-plane-lock/internal/scaling/runner.go:112] |
| Operator | Runs failback or break-lease | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:23] |
| Governed controllers | Affected party; scaled, never consulted | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34] |

## Business rules

- **DOM-002-R1** — At most one cluster is active: possession is exclusive, and a conflicting acquire yields standby rather than a second holder. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:189]
- **DOM-002-R2** — A cluster is active only while it holds the lease; leader state is set from a successful acquire and cleared on any renew failure. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192] [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:156]
- **DOM-002-R3** — Possession is time-bounded and must be renewed within the interval. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:154]
- **DOM-002-R4** — The renewal interval is strictly shorter than the term, and the term lies within 15–60 seconds; a cluster configured otherwise refuses to start. [D: tools/mgmt-plane-lock/internal/config/config.go:85] [D: tools/mgmt-plane-lock/internal/config/config.go:79]
- **DOM-002-R5** — Governed workloads run at their active counts only where the published status is active, and at zero everywhere else. [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:100]
- **DOM-002-R6** — Every cluster publishes its own state, with the moments of the last observation and the last successful renewal. [D: tools/mgmt-plane-lock/internal/kube/status.go:35]
- **DOM-002-R7** — A status value that is neither active nor standby is rejected, and nothing is scaled up on it. [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91]
- **DOM-002-R8** — A holder that is shutting down releases the lease and publishes standby first, using a context that its own cancellation cannot block. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:130]
- **DOM-002-R9** — A governed workload marked optional may be absent; the cycle continues. [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:164]
- **DOM-002-R10** — An operator's recorded preference moves activeness: a holder that is not the preferred cluster releases, and a non-holder does not contend while another cluster is preferred. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146] [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:181]
- **DOM-002-R11** — Every successful renewal is exported as a timestamp. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164]
- **DOM-002-R12** — The ArgoCD cluster Secret carries the same lease status as a label, updated only when it differs. [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115]

## Process flow

Steady state: read the preference, renew or acquire, publish status, and on the
other loop read that status and reconcile replica counts
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139]
[D: tools/mgmt-plane-lock/internal/scaling/runner.go:112].

Alternates: preference names another cluster, so the holder releases
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146]; shutdown, so the holder
drains [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:95]; operator
failback, which sets the preference and breaks the lease
[D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43].

Errors: acquire conflict yields standby
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:189]; renew conflict drops
leadership [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:157]; an
unreadable status scales nothing
[D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91]; an absent required
workload fails the cycle
[D: tools/mgmt-plane-lock/internal/scaling/scaling.go:167].

## Invariants

- Leader state is never set without an acquire that returned a lease id. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192]
- A cluster that cannot renew stops calling itself active within one interval. [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:156]
- The published status always names the cluster that wrote it. [D: tools/mgmt-plane-lock/internal/kube/status.go:37]
- A replica patch is issued only when the current count differs. [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:171]

## Implementation status

| Rule | Status | Evidence | Covering test in tree |
| --- | --- | --- | --- |
| DOM-002-R1 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:58] |
| DOM-002-R2 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:65] |
| DOM-002-R3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:17] |
| DOM-002-R4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/config/config_test.go:31] |
| DOM-002-R5 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:101] |
| DOM-002-R6 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/kube/status_test.go:13] |
| DOM-002-R7 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:113] |
| DOM-002-R8 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:171] |
| DOM-002-R9 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:83] |
| DOM-002-R10 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:121] |
| DOM-002-R11 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:17] |
| DOM-002-R12 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:127] |

## Open questions

- OPEN: The recovery objective quoted in `docs/architect.md:651` is 120 seconds. Nothing in the code or the suite measures a handover, so the rule set above cannot say whether it holds.
- OPEN: The preferred-cluster metadata is undocumented outside the code. `docs/architect.md:344` shows failback as a command that breaks the lease, and no document mentions that a healthy holder releases voluntarily when the recorded preference names another cluster [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146]. Is that voluntary release the intended mechanism, or a second path nobody has written down?
- OPEN: Neither controller is replicated and neither uses a Kubernetes lease for its own process. Is single-replica intentional, given the blob lease already arbitrates?
- OPEN: No rule covers clock skew: expiry is the arbiter's, but the renewal decision compares local time [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:154].
- OPEN: Who may run failback, and is its use recorded anywhere? The command takes a confirmation flag [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:46] and nothing else.
