---
title: PRD v4 F-002 — Management plane arbitration
id: F-002
kind: prd
feature: F-002
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# PRD v4 F-002 — Management plane arbitration

> Feature-level requirements: what and why, never how.

**Reconstructed from behaviour.** Stories are inferences from the loops and tests
that exist; acceptance criteria are lifted from test names. Value, priority and
targets are OPEN:.

## Summary

I: exactly one management cluster reconciles at a time, and which one is decided by a renewable external lease rather than by configuration or by an operator's belief — basis: leader state is set only from a successful acquire and dropped on any renew failure [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:192], and every governed controller's replica count follows the published status [D: tools/mgmt-plane-lock/internal/scaling/runner.go:118].

## User stories

- **F-002-US1** — As an operator, I want activeness to expire unless it is renewed. I: inferred from the acquire/renew state machine [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:154].
- **F-002-US2** — As an operator, I want a non-active cluster to run no platform controllers. I: inferred from the standby replica count of every governed entry [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41].
- **F-002-US3** — As an operator, I want to read from inside a cluster what it currently believes it is. I: inferred from the published status object [D: tools/mgmt-plane-lock/internal/kube/status.go:35].
- **F-002-US4** — As an operator, I want a planned restart to hand activeness back at once rather than after the term. I: inferred from the shutdown drain [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:120].
- **F-002-US5** — As an operator, I want to move activeness to a named cluster deliberately. I: inferred from the failback command and the preference-driven release [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43].
- **F-002-US6** — As a platform engineer, I want an unsafe renewal configuration to stop the controller at startup. I: inferred from the validation branches [D: tools/mgmt-plane-lock/internal/config/config.go:79].
- **F-002-US7** — As an on-call engineer, I want a signal that distinguishes "up" from "still renewing". I: inferred from the last-renew gauge [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164].

## Acceptance criteria

Lifted from test names.

**F-002-US1** — a lease is acquired and active status written [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:65]; renewal happens after the interval [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:17]; a conflicting acquire yields standby [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:58]; a renew conflict yields standby [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:143].

**F-002-US2** — active scales up [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:33]; standby scales to zero [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:101]; an unknown status is rejected [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:113].

**F-002-US3** — the object is created when absent [D: tools/mgmt-plane-lock/internal/kube/status_test.go:13] and updated in place when present [D: tools/mgmt-plane-lock/internal/kube/status_test.go:46].

**F-002-US4** — shutdown releases the lease [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:171]; a non-holder releases nothing [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:84].

**F-002-US5** — failback sets the target and breaks the lease [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main_test.go:25]; a preference change releases leadership [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:121].

**F-002-US6** — defaults load [D: tools/mgmt-plane-lock/internal/config/config_test.go:8]; an invalid lease window is rejected [D: tools/mgmt-plane-lock/internal/config/config_test.go:31]; a missing cluster name is rejected [D: tools/mgmt-plane-lock/internal/config/config_test.go:63].

**F-002-US7** — the metrics server starts and stops cleanly [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap_test.go:29].

## Implementation status

| Story | Status | Carried by | Evidence |
| --- | --- | --- | --- |
| F-002-US1 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC1, TC2, TC3, TC12 · F-002-T5 | — |
| F-002-US2 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC4, TC5, TC6, TC8 · F-002-T7, T8 | — |
| F-002-US3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC7, TC9 · F-002-T4 | — |
| F-002-US4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC10, TC14 · F-002-T6 | — |
| F-002-US5 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC11, TC13 · F-002-T9 | — |
| F-002-US6 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC15, TC16 · F-002-T1 | — |
| F-002-US7 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | F-002-TC17, TC18 · F-002-T10 | — |

## Scope boundaries

Derived from what the code does not do:

- No effect on tenant workloads: only the declared platform controllers are patched [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34].
- No data replication or write placement: nothing in these packages touches a datastore.
- No provisioning of the arbiter: the blob URL is configuration [D: tools/mgmt-plane-lock/internal/config/config.go:64].
- No three-cluster arbitration: the design is one blob, one holder [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43].
- No drift reporting: that is a separate binary [D: tools/mgmt-plane-lock/cmd/argocd-jira-bridge/main.go:14].

## Dependencies

| Dependency | Nature | Evidence |
| --- | --- | --- |
| Azure Storage blob | Hard — it is the arbiter | [D: tools/mgmt-plane-lock/internal/config/config.go:64] |
| Kubernetes API in each management cluster | Hard — status object and replica patches | [D: tools/mgmt-plane-lock/internal/kube/status.go:25] |
| ArgoCD cluster Secret | Soft — labelled with the lease status | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115] |
| Prometheus client | Runtime — the renewal gauge | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:48] |

## Metrics

OPEN: No success metric for arbitration exists in the repository. The renewal
gauge is a liveness signal, not a measure of handover time, and nothing records how
often leadership moved or how long a promotion took
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164].
`docs/architect.md:651` states a 120-second objective; nothing measures it.

## Open questions

- OPEN: What business consequence follows from a split brain, and what would count as an acceptable rate of accidental handovers?
- OPEN: Who owns the decision to fail back, and what is the expected time-to-decision?
- OPEN: Is a frozen control plane (neither cluster active) acceptable indefinitely, and after how long should it page?
- OPEN: The 120-second objective — commitment or estimate?
