---
title: How to use observability — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to use observability — IDPGitOps

> What the system emits and how to answer questions with it.

## Signals

| Signal | Where | Evidence |
| --- | --- | --- |
| Last-successful-renew timestamp | Metrics endpoint on the lease controller | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164] |
| Secret age after rotation | Metrics from the rotator | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157] |
| Dual-write skew between the vault pair | Exporter on the active cluster | [D: terraform/akv_sync_exporter.tf:1] |
| Leadership status | Cluster-local ConfigMap | [D: tools/mgmt-plane-lock/internal/kube/status.go:35] |
| `lease-status` label | ArgoCD cluster Secret | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:115] |
| Controller logs | Injectable logger per runner | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:57] |
| Drift tickets | Opened in the issue tracker on a degraded application | [D: tools/mgmt-plane-lock/internal/jirabridge/jirabridge.go:1] |
| Onboarding outcome | One JSON line in the job log | [D: tools/service_seed/cli.py:408] |
| Key Vault near-expiry alerts | Event Grid topic on the vaults | [D: terraform/akv_alerts.tf:47] |

## Required instrumentation

What the code establishes as the pattern: a controller exports a liveness-of-purpose
metric rather than only process liveness
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164]; it publishes a readable
local statement of its state [D: tools/mgmt-plane-lock/internal/kube/status.go:35];
and no error path may carry secret material
[D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208].

Gap: the Python tooling emits nothing but its result line
[D: tools/service_seed/cli.py:408] — no metric, no structured log, no timing.

## Dashboards and alerts

`docs/architect.md:636` lists four platform alerts and a SLO dashboard. Alert rules
exist as charts in the tree [D: gitops/platform/platform-alerts/Chart.yaml:1]
[D: gitops/platform/rollout-alerts/Chart.yaml:1].

- OPEN: The dashboard itself is not in the repository; where is it defined?
- OPEN: Alert routing and severity are not visible from the charts alone.

## Debug playbook

| Symptom | Read first | Then |
| --- | --- | --- |
| Nothing is reconciling | Leadership status in both clusters [D: tools/mgmt-plane-lock/internal/kube/status.go:35] | If neither is active, the arbiter is unreachable — that stand-down is by design |
| A standby looks busy | Governed controller replica counts [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41] | Non-zero means the scaling loop is not running there |
| A service never appeared | Whether both change sets merged [D: tools/service_seed/cli.py:384] | The platform opens, humans merge |
| A canary never finished | The tier's analysis definition [D: tools/service_seed/profiles/slo.yaml:13] | Gold gates on latency; bronze has no analysis |
| A rotation seems stale | The secret-age gauge [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157] | Rotation runs only on the active cluster |
| The vault pair disagrees | The skew metric [D: terraform/akv_sync_exporter.tf:1] | Nothing repairs it automatically |

Rule: read the cluster-local statement before the dashboard — during the failures
that matter, the dashboard may be on the cluster that just went away.
