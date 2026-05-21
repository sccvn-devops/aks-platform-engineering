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

By default the script performs the `US-023` outage drill literally: it cordons and drains all schedulable nodes on `mgmt-we`, deletes the `mgmt-leader-lease` pod so lease renewal stops, waits for passive takeover on `mgmt-ne`, then uncordons `mgmt-we` after the failover measurement completes. Use `--planned-failover` only for a non-disruptive smoke test that asks `mgmt-cli` to transfer leadership without simulating cluster loss.

## What The Script Verifies

- `mgmt-we` starts as the only active leader.
- The drill can simulate `mgmt-we` failure by cordoning and draining the active cluster, then deleting the `mgmt-leader-lease` pod so the Azure blob lease expires naturally.
- `mgmt-ne` becomes leader within the 65 second lease-acquisition window.
- `mgmt-we` transitions to standby and split-brain is not observed.
- `controller-scaler` effects are visible on `mgmt-ne`: Argo CD and Crossplane controllers become ready and Argo CD cluster Secrets are relabeled to `lease-status=active`.
- Argo CD Applications on `mgmt-ne` return to `Healthy` and `Synced` inside the 120 second failover target.
- Optional operator failback to `mgmt-we` uses `mgmt-cli failback --to mgmt-we --confirm` and completes cleanly.

## Emergency Commands

- `mgmt-cli failback --to mgmt-we --confirm` requests an operator-driven failback after `mgmt-we` has recovered.
- `mgmt-cli break-lease --confirm` force-breaks the Azure blob lease without changing the preferred target. This is for emergency recovery and should not be used for the timed RTO drill because it bypasses the natural 60-second lease expiry window.

## Manual Checks Still Required

The script covers the cluster-observable parts of `US-023`. The following acceptance checks still need operator review during the drill:

- Confirm no duplicate Crossplane provisioning occurred from Azure control-plane evidence.
- Confirm workload-cluster Argo Rollouts continued without disruption for any in-flight canaries.
- Record the measured timestamps and archive them with the drill evidence for the release.
