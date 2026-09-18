---
title: Product Roadmap — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Product Roadmap — IDPGitOps

> High-level, outcome-oriented roadmap. Themes and horizons, not dated Gantt charts.

OPEN: **A roadmap is a statement of future intent and is not recoverable from a
repository.** No roadmap, milestone list or dated plan exists in the tree, and the
git history cannot substitute: all 94 commits carry one date and there are no merge
commits, so even ordering is lost.

## Now / Next / Later

What can honestly be said is what is *present* versus what is *named but unbuilt*:

| State | Item | Evidence |
| --- | --- | --- |
| Present | Topology as committed data | [D: gitops/clusters/registry.yaml:1] |
| Present | Service onboarding pipeline | [D: tools/service_seed/cli.py:340] |
| Present | Control-plane arbitration | [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139] |
| Present | Secret and token lifecycle | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110] |
| Present | Repository invariant gates | [D: .pre-commit-config.yaml:48] |
| Present | Drift-to-ticket bridge | [D: tools/mgmt-plane-lock/internal/jirabridge/runner.go:1] |
| Present | Developer portal | [D: backstage/package.json:1] |
| Named, not built here | Seed-cluster recovery beyond the catalogue entry and the scripts | [D: gitops/clusters/registry.yaml:133] |

- OPEN: What comes next, and who decides?
- OPEN: The repository's PRD series implies phases; are any of them still open?

## Release themes

OPEN: No theme is stated anywhere. Grouping the seven present items into themes
would be this hub inventing a narrative.

## Dependencies

Dependencies the code makes real, which any roadmap will inherit:

| Theme | Gated by | Evidence |
| --- | --- | --- |
| Onboarding | The issue tracker and the source host | [D: tools/service_seed/jira_intake.py:205] |
| Rotation | Arbitration — it runs only on the active cluster | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115] |
| Everything provisioned | One pinned Terraform version | [D: .tool-versions:16] |
| Onboarding and arbitration | The cluster catalogue | [D: tools/service_seed/cli.py:35] |
