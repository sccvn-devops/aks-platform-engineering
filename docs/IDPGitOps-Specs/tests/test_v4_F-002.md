---
title: Test Plan v4 F-002 — Management plane arbitration
id: F-002
kind: test
feature: F-002
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Test Plan v4 F-002 — Management plane arbitration

> Concrete cases traced back to acceptance criteria.

**As-built inventory.** One `F-002-TC<k>` per real Go test function, cited. The
packages that carry arbitration hold 48 test functions; the cases below cover each
distinct behaviour, and the holes are listed at the end.

## Traceability matrix

| Story | Test cases | Level |
| --- | --- | --- |
| F-002-US1 | F-002-TC1, TC2, TC3, TC12 | unit |
| F-002-US2 | F-002-TC4, TC5, TC6, TC8 | unit |
| F-002-US3 | F-002-TC7, TC9 | unit |
| F-002-US4 | F-002-TC10, TC14 | unit |
| F-002-US5 | F-002-TC11, TC13 | unit |
| F-002-US6 | F-002-TC15, TC16 | unit |
| F-002-US7 | F-002-TC17, TC18 | unit |

## Test cases

| Case | What it proves | Cited test |
| --- | --- | --- |
| F-002-TC1 | An acquire returns a lease id and the runner publishes active | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:65] |
| F-002-TC2 | A renew happens once the interval has elapsed | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:17] |
| F-002-TC3 | A conflicting acquire publishes standby instead of erroring | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:58] |
| F-002-TC4 | Active status scales the governed workloads up | [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:33] |
| F-002-TC5 | Standby status scales them to zero | [D: tools/mgmt-plane-lock/internal/scaling/scaling_test.go:101] |
| F-002-TC6 | A workload already at the desired count is not patched | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:83] |
| F-002-TC7 | The status object is created when absent | [D: tools/mgmt-plane-lock/internal/kube/status_test.go:13] |
| F-002-TC8 | An unknown status value is rejected | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:113] |
| F-002-TC9 | The status object is updated in place when present | [D: tools/mgmt-plane-lock/internal/kube/status_test.go:46] |
| F-002-TC10 | Run releases the lease on shutdown | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:171] |
| F-002-TC11 | Leadership is released when the preference changes | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:121] |
| F-002-TC12 | A renew conflict results in standby | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:143] |
| F-002-TC13 | Failback sets the target and breaks the lease | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main_test.go:25] |
| F-002-TC14 | Drain does nothing when this cluster is not the holder | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:84] |
| F-002-TC15 | Controller configuration defaults load | [D: tools/mgmt-plane-lock/internal/config/config_test.go:8] |
| F-002-TC16 | An invalid lease window is rejected | [D: tools/mgmt-plane-lock/internal/config/config_test.go:31] |
| F-002-TC17 | The metrics server shuts down on context cancel | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap_test.go:29] |
| F-002-TC18 | The metrics server surfaces a bind error | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap_test.go:76] |
| F-002-TC19 | A nil dependency fails before the loop starts | [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:213] |
| F-002-TC20 | The cluster Secret label is updated, and skipped when already correct | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:69] |

## Implementation status

| Case | Status | Evidence |
| --- | --- | --- |
| F-002-TC1 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC2 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC5 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC6 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC7 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC8 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC9 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC10 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC11 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC12 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC13 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC14 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC15 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC16 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC17 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC18 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC19 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |
| F-002-TC20 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — |

## Edge and negative cases

| Case | Covered by |
| --- | --- |
| Release treats a missing lease as success | [D: tools/mgmt-plane-lock/internal/bloblease/manager_test.go:121] |
| Break treats a missing lease as success | [D: tools/mgmt-plane-lock/internal/bloblease/manager_test.go:152] |
| A non-conflict renew error surfaces | [D: tools/mgmt-plane-lock/internal/bloblease/manager_test.go:137] |
| A non-conflict acquire error surfaces | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:73] |
| An empty status value is rejected | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:123] |
| An unsupported workload kind is rejected | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:95] |
| A nil replica pointer compares safely | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:104] |
| The cluster Secret is found by label selector when the name lookup misses | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:39] |
| The tick interval defaults when unset | [D: tools/mgmt-plane-lock/internal/bloblease/coverage_test.go:127] |
| The scaling loop recovers from a watch setup failure | [D: tools/mgmt-plane-lock/internal/scaling/coverage_test.go:147] |

## Out of scope

Holes in the suite, each real:

- **No test drives both clusters at once.** Every case runs one runner against a
  fake; the mutual-exclusion property is inherited from the arbiter rather than
  asserted [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:65].
- **No test measures handover time**, so the 120-second objective is unevidenced.
- **No test covers the both-clusters-partitioned case**, where neither can reach the
  arbiter and both must stand down.
- **No test exercises the real Azure Storage transport**; the manager tests inject a
  fake transport [D: tools/mgmt-plane-lock/internal/bloblease/manager_test.go:26].
- **The DR scripts are not part of any suite**
  [D: scripts/dr-validation/validate-mgmt-failover.sh:1]; nothing runs them
  automatically.

## Open questions

- OPEN: Is `scripts/validate-blob-lease-locking.sh` run anywhere on a schedule, or only by hand during a drill?
- OPEN: What is the coverage threshold for these packages? None is configured in the module.
