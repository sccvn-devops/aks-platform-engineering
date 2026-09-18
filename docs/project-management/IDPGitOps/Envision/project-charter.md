---
title: Project Charter — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Project Charter — IDPGitOps

> Define product vision, business value, target users and explicit project boundaries.

**This document is mostly questions, and that is the finding.** A charter states
intent: who the product is for, what it is worth, and what success looks like. None
of that is recoverable from a repository. What follows separates the little the code
can support from the much it cannot — the list of OPEN: items here is the most
useful part of this hub for anyone who owns the product.

## Vision statement

OPEN: No vision statement exists in the repository. The README describes what the
system is [D: README.md:28], never who it is for or what it replaces.

What the code supports: I: the product's purpose is to make platform operations
happen through reviewed repository changes rather than privileged manual action —
basis: the four capabilities in the tree are all pipelines from a committed or
requested input to a reviewed change [D: tools/service_seed/cli.py:340].

## Business value

OPEN: No value statement, business case or cost figure appears anywhere.

The only value-adjacent facts in the tree are the three prose targets in the
engineering guide — a 45-minute onboarding target [D: docs/architect.md:586], a
120-second failover objective [D: docs/architect.md:651] and a SLO dashboard table
[D: docs/architect.md:645] — and none of them is instrumented.

## Target users

Roles the code addresses, not personas anyone described:

| Role | Evidence it exists |
| --- | --- |
| Requester of a new service | [D: tools/service_seed/jira_intake.py:30] |
| Platform engineer changing committed data | [D: gitops/clusters/registry.yaml:1] |
| Operator performing failback | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43] |
| Security reviewer of secrets and suppressions | [D: terraform/locals.tf:41] |

- OPEN: How many teams use this platform, and how many services run on it?
- OPEN: Who are the secondary users — auditors, finance, application operators?

## In scope / Out of scope

In scope, evidenced: cluster provisioning [D: terraform/clusters.tf:1], topology as
data [D: gitops/clusters/registry.yaml:1], GitOps bootstrap
[D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1], service
onboarding [D: tools/service_seed/cli.py:340], control-plane arbitration
[D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139], secret lifecycle
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110], repository gates
[D: .pre-commit-config.yaml:48] and a developer portal [D: backstage/package.json:1].

OPEN: Out of scope is stated in the repository's own PRD series as Non-Goals. This
hub does not restate them, and cannot tell which still hold.

## Success metrics

OPEN: No success metric is defined or instrumented. Two gauges exist — lease
renewal [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:164] and secret age
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157] — and neither has a
target.

## Constraints and assumptions

Constraints visible in the tree: Azure-only [D: terraform/provider.tf:1]; Terraform
pinned at one version [D: .tool-versions:16]; exactly two management clusters
[D: gitops/clusters/registry.yaml:40]; subscription identifiers in the catalogue are
placeholders [D: gitops/clusters/registry.yaml:29].

- OPEN: Budget, timeline, team size and compliance regime are unstated.
- OPEN: The assumption behind two management regions rather than three is in the ADR set; whether it still holds is not.

## Top risks

Risks the code and history make visible, with no mitigation recorded anywhere:

| Risk | Evidence |
| --- | --- |
| Single-owner knowledge: 127 of 131 git-attributed files have one owner | survey `git_health` |
| History gives no ordering: 94 commits, one date, no merge commits | survey `history` |
| Documentation drift: the engineering guide describes a validated field the code does not have [D: docs/architect.md:600] | this hub |
| Nine of eleven repository gates are untested; a broken gate fails open | [D: scripts/validate-action-pins.py:1] |
| A mid-rotation failure leaves the vault pair inconsistent with no automatic repair [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:131] | this hub |

OPEN: Impact, likelihood and owner for each of these are for the team to state.
