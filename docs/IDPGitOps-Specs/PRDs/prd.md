---
title: PRD — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# PRD — IDPGitOps

> Product scope, MVP definition and the index of all child PRDs.

**Reconstructed.** A product requirement is a statement of intent, and intent is
not in a repository. What follows is the observable product surface, the four
features the code splits into, and — for almost everything else — a question.

## Problem and outcome

I: the product turns platform operations that would otherwise be manual and privileged into reviewed changes to a repository — basis: the four capabilities in the tree are a request-to-pull-request pipeline [D: tools/service_seed/cli.py:340], an automatic control-plane arbiter [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:139], a set of repository gates [D: .pre-commit-config.yaml:48] and an automated secret lifecycle [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110].

- OPEN: What problem was this built to solve, in the organisation's own words? Not recoverable from code.
- OPEN: What did the previous state cost — in time, incidents or people — and what is the target?

## MVP definition

OPEN: No MVP boundary is recorded anywhere in the repository. Everything in the
tree ships together; no feature flag, staged rollout or scope marker distinguishes
a minimum from an extension [D: gitops/bootstrap/control-plane/addons/oss/addons-argo-cd-appset.yaml:1].

What can be said: the four features are mutually dependent in one direction only —
onboarding and secret rotation both read topology
[D: tools/service_seed/service_template.py:270], rotation depends on arbitration
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115], and the gates depend
on nothing [D: scripts/validate-action-pins.py:135].

## Personas and top user flows

Derived from who the code's inputs and outputs are addressed to:

| Persona | Flow | Evidence |
| --- | --- | --- |
| Requester | Raise a request, receive a repository and two change sets to review | [D: tools/service_seed/cli.py:384] |
| Platform engineer | Change committed data or infrastructure; gates run locally and on the pull request | [D: .pre-commit-config.yaml:48] |
| Operator / SRE | Observe which cluster is active; perform an explicit failback | [D: tools/mgmt-plane-lock/cmd/mgmt-cli/main.go:43] |
| Security reviewer | Read the secret catalogue and the suppression metadata | [D: terraform/locals.tf:41] |

OPEN: These are roles the code implies, not personas anyone has described. Team
names, sizes and responsibilities are not in the repository.

## Child PRD index

| Version | Feature | Title | Status | Link |
| --- | --- | --- | --- | --- |
| v4 | F-001 | Service onboarding pipeline | draft | [prd_v4_F-001-service-onboarding-pipeline.md](prd_v4_F-001-service-onboarding-pipeline.md) |
| v4 | F-002 | Management plane arbitration | draft | [prd_v4_F-002-management-plane-arbitration.md](prd_v4_F-002-management-plane-arbitration.md) |
| v4 | F-003 | Platform invariant gates | draft | [prd_v4_F-003-platform-invariant-gates.md](prd_v4_F-003-platform-invariant-gates.md) |
| v4 | F-004 | Secret and token lifecycle | draft | [prd_v4_F-004-secret-and-token-lifecycle.md](prd_v4_F-004-secret-and-token-lifecycle.md) |

## Non-functional requirements

Numbers that are **configured in the tree** — these are derived, and they are the
only non-functional statements this hub can make on its own authority:

| # | Requirement | Value | Evidence |
| --- | --- | --- | --- |
| NFR-01 | Outbound HTTP timeout, onboarding | 30 s | [D: tools/service_seed/jira_intake.py:27] |
| NFR-02 | Subprocess timeout, onboarding | 300 s | [D: tools/service_seed/gitops_pr.py:34] |
| NFR-03 | Lease term | 60 s default, 15–60 s permitted | [D: tools/mgmt-plane-lock/internal/config/config.go:16] |
| NFR-04 | Lease renewal interval | 15 s default, strictly shorter than the term | [D: tools/mgmt-plane-lock/internal/config/config.go:17] |
| NFR-05 | Controller tick and poll interval | 5 s default | [D: tools/mgmt-plane-lock/internal/config/config.go:18] |
| NFR-06 | Platform secret expiry window | 90 days | [D: terraform/keyvaults.tf:59] |
| NFR-07 | Gold tier release gate | success ≥ 0.99 and p99 ≤ 500 ms, evaluated every 60 s | [D: tools/service_seed/profiles/slo.yaml:13] |
| NFR-08 | Silver tier release gate | success ≥ 0.99, no latency gate | [D: tools/service_seed/profiles/slo.yaml:19] |
| NFR-09 | Bronze tier release gate | none | [D: tools/service_seed/profiles/slo.yaml:24] |
| NFR-10 | Governed controller replica counts on standby | 0 | [D: tools/mgmt-plane-lock/internal/scaling/scaling.go:41] |

Targets the repository **asserts in prose** but does not instrument, which makes
them claims rather than measurements: a 45-minute end-to-end onboarding target
[D: docs/architect.md:586], a 120-second failover objective
[D: docs/architect.md:651] and the dashboard table around them
[D: docs/architect.md:645].

- OPEN: Are those three numbers commitments, estimates or aspirations? Nothing measures any of them.
- OPEN: No availability, throughput, capacity or cost requirement is expressed anywhere.
- OPEN: No accessibility or compliance requirement appears in the repository.

## Traceability

| Feature | Domain | Architecture | Data | Tasks | Tests | Route |
| --- | --- | --- | --- | --- | --- | --- |
| F-001 | [DOM-001](../ddd/domain_DOM-001-service-onboarding-pipeline.md) | [arch](../architect/feature_v4_F-001_architect.md) | [erd](../data/data-erd_v4_F-001.md) | [tasks](../tasks/tasks_v4_F-001.md) | [tests](../tests/test_v4_F-001.md) | [route](../route/route_v4_F-001.md) |
| F-002 | [DOM-002](../ddd/domain_DOM-002-management-plane-arbitration.md) | [arch](../architect/feature_v4_F-002_architect.md) | [erd](../data/data-erd_v4_F-002.md) | [tasks](../tasks/tasks_v4_F-002.md) | [tests](../tests/test_v4_F-002.md) | [route](../route/route_v4_F-002.md) |
| F-003 | [DOM-003](../ddd/domain_DOM-003-platform-invariant-gates.md) | [arch](../architect/feature_v4_F-003_architect.md) | [erd](../data/data-erd_v4_F-003.md) | [tasks](../tasks/tasks_v4_F-003.md) | [tests](../tests/test_v4_F-003.md) | [route](../route/route_v4_F-003.md) |
| F-004 | [DOM-004](../ddd/domain_DOM-004-secret-and-token-lifecycle.md) | [arch](../architect/feature_v4_F-004_architect.md) | [erd](../data/data-erd_v4_F-004.md) | [tasks](../tasks/tasks_v4_F-004.md) | [tests](../tests/test_v4_F-004.md) | [route](../route/route_v4_F-004.md) |

## Open questions

- OPEN: Who is the customer of this platform, and how many teams use it today?
- OPEN: What is the release cadence, and who approves a release?
- OPEN: Which of the four features is most valuable, and on what evidence?
- OPEN: What is explicitly out of scope for the product as a whole? The repository's own PRD series states Non-Goals; this hub cannot tell which of them still hold.
