# Management Plane Failover Runbook

This runbook supports `US-023` in [prd.json](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/prd.json) and validates the management-plane lease failover between `mgmt-we` and `mgmt-ne`.

## Preconditions

- The platform has been applied to Azure and both management clusters are reachable.
- `kubectl` contexts for both clusters exist.
- `az login` has been completed.
- `mgmt-cli` is built from [tools/mgmt-plane-lock/cmd/mgmt-cli/main.go](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/tools/mgmt-plane-lock/cmd/mgmt-cli/main.go).

## Build The CLI

`go build ./tools/mgmt-plane-lock/cmd/mgmt-cli`

The binary reads `LEASE_BLOB_URL` from the environment. The validation script can now discover that URL from `kube-system/mgmt-leader-status`, so you do not need to export it manually after deployment.

## Run The Drill

```
MGMT_WE_CONTEXT=mgmt-we-admin \
MGMT_NE_CONTEXT=mgmt-ne-admin \
./scripts/dr-validation/validate-mgmt-failover.sh --failback
```

## What The Script Verifies

- `mgmt-we` starts as the only active leader.
- `mgmt-cli failback --to mgmt-ne --confirm` breaks the active lease and requests promotion.
- `mgmt-ne` becomes leader within the 65 second lease-acquisition window.
- `mgmt-we` transitions to standby and split-brain is not observed.
- `controller-scaler` effects are visible on `mgmt-ne`: Argo CD and Crossplane controllers become ready and Argo CD cluster Secrets are relabeled to `lease-status=active`.
- Argo CD Applications on `mgmt-ne` return to `Healthy` and `Synced` inside the 120 second failover target.
- Optional operator failback to `mgmt-we` also completes cleanly.

## Manual Checks Still Required

The script covers the cluster-observable parts of `US-023`. The following acceptance checks still need operator review during the drill:

- Confirm no duplicate Crossplane provisioning occurred from Azure control-plane evidence.
- Confirm workload-cluster Argo Rollouts continued without disruption for any in-flight canaries.
- Record the measured timestamps and archive them with the drill evidence for the release.
