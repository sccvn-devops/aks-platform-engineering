---
title: How to deploy — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to deploy — IDPGitOps

> The deployment procedure and its guardrails.

**None of the commands below were run here.** They are read from the repository's
own guide and its workflows; treat each as `I:` until someone watches it succeed.

## Preconditions

- The pull request is merged and every gate was green [D: .github/workflows/terraform-ci.yml:1].
- Remote state exists for the target cluster and the account still carries its delete lock [D: scripts/bootstrap-tfstate.sh:59].
- The toolchain matches `.tool-versions` [D: .tool-versions:16].
- For a management-plane change: you know which cluster currently holds the lease [D: tools/mgmt-plane-lock/internal/kube/status.go:35].

## Procedure

I: the platform is provisioned per cluster with its own backend file, then reconciled by ArgoCD — basis: one backend file per cluster [D: terraform/backends/mgmt-we.tfbackend:1] and an App-of-Apps ApplicationSet applied from Terraform [D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1].

```bash
./scripts/bootstrap-tfstate.sh                                   # once per environment
cd terraform && terraform init -backend-config=backends/mgmt-we.tfbackend -upgrade
terraform apply -var gitops_addons_org=https://github.com/<org> --auto-approve
```

A service deploys by merging its two change sets; nothing is applied by hand
[D: tools/service_seed/cli.py:384].

## Verification

| Check | Expected |
| --- | --- |
| Leadership status in both management clusters | Exactly one reads `active`, with a fresh `lastRenewedAt` [D: tools/mgmt-plane-lock/internal/kube/status.go:35] |
| Governed controllers on the standby | Zero replicas [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41] |
| Registry versus reality | The clusters that exist match the catalogue [D: gitops/clusters/registry.yaml:1] |
| A seeded service | Both change sets merged, the workload's rollout reaching stable [D: tools/service_seed/profiles/rollout.yaml:12] |

## Rollback

| Trigger | Action | Evidence |
| --- | --- | --- |
| A bad merged declaration | Revert the commit; the engine converges | [D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1] |
| A bad canary | The tier's analysis aborts it | [D: tools/service_seed/profiles/slo.yaml:13] |
| A bad apply | Re-apply the previous configuration against the same state file | [D: scripts/validate-state-partitioning.sh:1] |
| Loss of the active management cluster | No rollback: the lease expires and the other cluster acquires | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:184] |

- OPEN: No rollback runbook exists in the repository; the table above is inferred from the mechanisms, not from a procedure anyone wrote.
- OPEN: Who approves an apply, and against which subscription, is not in the tree.
