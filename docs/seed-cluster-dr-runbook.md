# Seed Cluster DR Runbook

This runbook supports `US-025` in [prd.json](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/prd.json) and validates catastrophic recovery from the `seed-wus` bootstrap cluster.

## Preconditions

- The platform has been applied to Azure and `seed-wus` is reachable.
- `seed-wus` already has Crossplane installed and healthy.
- The placeholders in [bootstrap/control-plane-claim.yaml](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/bootstrap/control-plane-claim.yaml) have been replaced with live tenant, identity, storage, and repository metadata.
- `kubectl` contexts exist for `seed-wus` and every workload cluster you want to probe during the drill.
- `WORKLOAD_PROBES` is populated with one or more live service URLs if you want the validator to prove workloads kept serving traffic throughout the recovery window. Format: `name=https://service.example/healthz`.
- `az login` has been completed if you want to cross-check Azure-side resource creation timestamps during the exercise.

## Recovery Manifest

[bootstrap/control-plane-claim.yaml](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/bootstrap/control-plane-claim.yaml) is the concrete Crossplane claim applied to `seed-wus` during a catastrophic management-plane rebuild. It does three things:

- Provisions a replacement `mgmt-we` AKS cluster from the seed cluster.
- Installs Argo CD on the replacement cluster.
- Points the `cluster-bootstrap` Argo CD Application at `gitops/bootstrap/control-plane/addons` in this repository so the management-plane addons resync from Git.

The claim metadata is also copied onto the Argo CD cluster Secret that the control-plane addon ApplicationSets read, so the recovered cluster gets the same repo, identity, Velero, and lease annotations that Terraform normally publishes.

## Run The Drill

```bash
WORKLOAD_CONTEXTS="aks-dev-we-admin aks-staging-we-admin aks-prod-we-admin aks-prod-ne-admin" \
WORKLOAD_PROBES="payments=https://payments.example.internal/healthz catalog=https://catalog.example.internal/healthz" \
./scripts/dr-validation/validate-seed-cluster-dr.sh --apply-claim
```

The script uses `seed-wus` as the Crossplane control plane, waits for the recovery claim to become `Ready`, extracts the generated kubeconfig from the claim connection Secret, and then validates the recovered management cluster directly.

## What The Script Verifies

- `seed-wus` has Crossplane ready before the drill starts.
- The recovery claim in `bootstrap/control-plane-claim.yaml` becomes `Ready`.
- The generated kubeconfig Secret for the rebuilt management cluster is published by Crossplane.
- Argo CD on the rebuilt management cluster creates `cluster-bootstrap` and points it at the expected repo/path from the claim annotations.
- The rebuilt management cluster reports `leadershipStatus=active` and exposes the blob lease URL in `kube-system/mgmt-leader-status`.
- Argo CD Applications on the rebuilt management cluster converge to `Healthy` and `Synced`.
- The total recovery RTO stays within the 45 minute target.
- Each workload cluster context listed in `WORKLOAD_CONTEXTS` remains API-reachable during the drill.
- Each workload endpoint listed in `WORKLOAD_PROBES` keeps returning HTTP 2xx/3xx throughout the recovery drill.
- The script refuses to apply the recovery claim if [bootstrap/control-plane-claim.yaml](/home/tuanna47/workspaces/STE-CITYOS/aks-platform-engineering/bootstrap/control-plane-claim.yaml) still contains `REPLACE_WITH_*` placeholders.

## Manual Checks Still Required

The validator automates the repository and cluster-observable checks. Operators still need to capture:

- Azure evidence that the original management-plane resources were intentionally destroyed or isolated before reusing the `mgmt-we` identity.
- End-user traffic checks beyond the URLs covered by `WORKLOAD_PROBES`, especially if workloads need authenticated or synthetic transaction validation instead of a simple health endpoint.
- Recovery timestamps and screenshots or logs from Argo CD, Crossplane, and Azure for the DR evidence pack.
