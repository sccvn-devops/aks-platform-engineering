---
title: Fixtures v4 F-002 — Management plane arbitration
id: F-002
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Fixtures v4 F-002 — Management plane arbitration

> The test data this feature is implemented and verified against, and what each fixture is for.

Derived from the defaults and declared values in the code, not invented: the
configuration defaults [D: tools/mgmt-plane-lock/internal/config/config.go:13], the
eight status keys [D: tools/mgmt-plane-lock/internal/kube/status.go:35] and the
governed workload list [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:34].

## Fixture sets

| Set | Entities | Scenario | Used by |
| --- | --- | --- | --- |
| F-002-FX1 | `lease_controller_config` mgmt-we | Fully specified, safe: 60 s term renewed every 15 s | F-002-TC1, TC15 |
| F-002-FX2 | `lease_controller_config` mgmt-ne | Only the two required values; the rest take their documented defaults [D: tools/mgmt-plane-lock/internal/config/config.go:65] | F-002-TC15 |
| F-002-FX3 | `lease_controller_config` mgmt-we-unsafe | Schema-valid, domain-invalid: a 30 s rhythm against a 20 s term | F-002-TC16 |
| F-002-FX4 | `mgmt_leader_status` active, standby | The steady state: one holder, one contender | F-002-TC2, TC4, TC5, TC7 |
| F-002-FX5 | `mgmt_leader_status` mgmt-we after a preference change | A former holder that released and published standby with the preference recorded [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146] | F-002-TC10, TC11 |
| F-002-FX6 | `controller_workload` ×6 | The complete governed set, including the two optional entries | F-002-TC4, TC5, TC6 |
| F-002-FX7 | `cluster_entry` mgmt-we, mgmt-ne | The catalogued management pair | F-002-TC13 |

All sets live in [`fixtures/fixtures_v4_F-002.json`](fixtures/fixtures_v4_F-002.json).

## Fixture file

[`fixtures/fixtures_v4_F-002.json`](fixtures/fixtures_v4_F-002.json), validated by
`route.py` against [`schema/schemas.json`](schema/schemas.json).

## Encoded invariants

| Encoded | Rule | Evidence |
| --- | --- | --- |
| Exactly one status reads `active` | DOM-002-R1 | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:184] |
| The active record's renewal is 4 s before its observation, inside a 60 s term | DOM-002-R3 | [D: tools/mgmt-plane-lock/internal/config/config.go:16] |
| The standby carries the zero-value renewal timestamp and an empty lease id | DOM-002-R6 | [D: tools/mgmt-plane-lock/internal/kube/status.go:43] |
| Every governed workload has a standby count of zero, two are optional | DOM-002-R5, DOM-002-R9 | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41] |
| The third status record shows the released-on-preference outcome | DOM-002-R10 | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146] |
| F-002-FX3 validates structurally and is still unsafe: JSON Schema cannot express "rhythm < term" | DOM-002-R4 | [D: tools/mgmt-plane-lock/internal/config/config.go:85] |

F-002-FX3 is the deliberate trap. `route.py` checks fields, not cross-field rules,
so a record can pass validation and still describe a controller that refuses to
start. Reading "the fixture validates" as "the configuration is safe" is the
mistake this row exists to prevent.

## Determinism rules

- All timestamps are fixed literals on 2026-01-01; only the intervals between them matter.
- The standby's renewal timestamp is the Go zero value, which is what a never-renewed controller records [D: tools/mgmt-plane-lock/internal/kube/status.go:43].
- The lease identifier is an obviously synthetic UUID; the storage account name resolves to nothing.
- No credential appears, and no field in these entities can hold one.

## Loading

The repository's own tests build a fake Kubernetes client and a fake lease manager
implementing the two narrow interfaces
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:18]
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:28], then drive the loop with
a short tick [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:49]. Nothing
touches Azure Storage or a real cluster.

I: these fixtures duplicate values the Go tests already hold inline — basis: no test in the package reads a JSON fixture; each builds its own config struct [D: tools/mgmt-plane-lock/internal/bloblease/runner_test.go:65].

## Traceability

| Set | Test cases |
| --- | --- |
| F-002-FX1 | F-002-TC1, TC15 |
| F-002-FX2 | F-002-TC15 |
| F-002-FX3 | F-002-TC16 |
| F-002-FX4 | F-002-TC2, TC4, TC5, TC7 |
| F-002-FX5 | F-002-TC10, TC11 |
| F-002-FX6 | F-002-TC4, TC5, TC6 |
| F-002-FX7 | F-002-TC13 |

## Open questions

- OPEN: No fixture represents the both-clusters-partitioned state, and no test constructs it; the frozen-control-plane behaviour is asserted nowhere in the suite.
- OPEN: Should these fixtures replace the inline structs in the Go tests, or stay documentation-only? Today the same defaults exist in three places.
