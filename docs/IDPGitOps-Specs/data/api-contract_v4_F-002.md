---
title: API Contract v4 F-002 — Management plane arbitration
id: F-002
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# API Contract v4 F-002 — Management plane arbitration

> The wire contract for this feature — the readable companion to its OpenAPI and AsyncAPI files.

## Surface summary

Three surfaces: one served, one outbound, one cluster-local.

- **Served:** `/metrics`, the only HTTP route in the surveyed repository
  [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:54].
- **Outbound:** six lease operations against Azure Storage, invoked through the SDK
  client rather than composed as requests
  [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43].
- **Cluster-local:** the leadership status ConfigMap, written by one loop and read
  by the other [D: tools/mgmt-plane-lock/internal/kube/status.go:25]
  [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:83], plus a label written
  onto the ArgoCD cluster Secret
  [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115].

I: there is no promotion endpoint — basis: no handler, flag or API call sets leadership; the only way a cluster becomes active is a successful acquire [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:184].

## Endpoints

Served:

| Method | Path | Auth | Response | Evidence |
| --- | --- | --- | --- | --- |
| GET | `metrics` | none in code | exposition text including the last-renew gauge | [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:54] |

Outbound lease operations (SDK calls, not composed requests):

| Operation | When | Evidence |
| --- | --- | --- |
| Acquire | Every tick while not the holder | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] |
| Renew | When the renewal interval has elapsed | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:59] |
| Release | On drain, and on a preference change | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:71] |
| Break | Operator failback only | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:86] |
| Read preferred cluster | Every reconcile, before anything else | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:140] |
| Set preferred cluster | Operator failback only | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:127] |

Operator command surface:

| Command | Effect | Evidence |
| --- | --- | --- |
| `mgmt-cli failback --to <cluster> --confirm` | Sets the preference, then breaks the active lease | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43] |
| `mgmt-cli break-lease` | Breaks the lease without setting a preference | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:72] |

## Events

**None.** Both controllers poll: the lease loop on its tick
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:78] and the scaling loop
from the status object [D: tools/mgmt-plane-lock/internal/scaling/runner.go:113].
The coupling between them is that ConfigMap, read on a poll, not a message.

## Error model

| Condition | Behaviour | Evidence |
| --- | --- | --- |
| Acquire conflicts (another holder) | Publishes standby; not an error to the caller | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:189] |
| Renew fails with a conflict or missing lease | Drops leadership and publishes standby | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:157] |
| Renew fails otherwise | Surfaces the error and drops leadership | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:158] |
| Release finds no lease | Treated as success | [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:71] |
| Status value unreadable or unrecognised | Rejected; nothing is scaled | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:91] |
| Required dependency nil at startup | Returns a sentinel before the loop starts | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:75] |
| Configuration invalid | Refuses to start, naming the setting | [D: tools/mgmt-plane-lock/internal/config/config.go:79] |
| Governed workload absent and not optional | Reconcile returns the error | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:107] |
| ArgoCD cluster Secret absent by name | Falls back to a label selector, then errors | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:118] |

## Versioning and compatibility

| Stable | Changes carefully | Evidence |
| --- | --- | --- |
| The two `leadershipStatus` values | — | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:14] |
| The eight status keys, read by operators and scripts outside this repository | Adding a key: writer first | [D: tools/mgmt-plane-lock/internal/kube/status.go:35] |
| The metrics address default | Changing it moves the scrape target | [D: tools/mgmt-plane-lock/internal/config/config.go:15] |
| — | Lease term and rhythm: rolled out one cluster at a time | [D: tools/mgmt-plane-lock/internal/config/config.go:85] |

## Spec files

- [`schema/openapi_v4_F-002.json`](schema/openapi_v4_F-002.json) — the served route, plus the outbound operations as an extension block.
- [`schema/asyncapi_v4_F-002.json`](schema/asyncapi_v4_F-002.json) — empty.

The repository contains no OpenAPI or AsyncAPI file of its own, so there is nothing
to reconcile these against.

## Traceability

| Surface | Satisfies |
| --- | --- |
| Acquire and renew with a bounded term | F-002-US1 · DOM-002-R1, R3 |
| Release on drain | F-002-US4 · DOM-002-R8 |
| Preference read each reconcile, release when it names another cluster | F-002-US5 · DOM-002-R10 |
| Status upsert | F-002-US3 · DOM-002-R6 |
| Status read decides replica counts | F-002-US2 · DOM-002-R5 |
| Metrics | F-002-US7 · DOM-002-R11 |
| Failback command | F-002-US5 · DOM-002-R10 |

## Open questions

- OPEN: `/metrics` is served with no authentication in code [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:53]; is it expected to be reachable only from inside the cluster, and is that enforced by a NetworkPolicy anywhere?
- OPEN: The ArgoCD cluster Secret is labelled with the lease status [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115]. Which consumer reads that label, and is it load-bearing or informational?
- OPEN: Break and set-preference exist only on the operator path [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:23]; who is allowed to run that command, and is its use audited?
