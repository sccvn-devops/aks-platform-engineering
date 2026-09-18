---
title: PRD v4 F-001 — Service onboarding pipeline
id: F-001
kind: prd
feature: F-001
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# PRD v4 F-001 — Service onboarding pipeline

> Feature-level requirements: what and why, never how.

**Reconstructed from behaviour.** A requirement is a statement about intent, and
intent is not in a repository. Every story below is an inference from the
endpoints, commands and tests that exist, phrased from the consumer's side; the
acceptance criteria are lifted from test names, which are evidence that the
behaviour is wanted rather than merely present. Business value, priority and
metrics are OPEN: — nothing in the tree states them. This document holds no
paths and no schemas.

## Summary

I: one tracker request produces a service's own repository and two reviewable change sets covering three environments, with the availability tier deciding release strictness and no platform engineer executing anything — basis: the pipeline's single entry command performs exactly those steps and nothing merges or provisions [D: tools/service_seed/cli.py:340].

## User stories

- **F-001-US1** — As a requester, I want my request read once into a single agreed
  description of my service. I: inferred from the frozen request every later step
  consumes [D: tools/service_seed/jira_intake.py:47].
- **F-001-US2** — As a requester, I want an unusable request refused immediately
  with the offending field named. I: inferred from the field-carrying error type and
  the exit-2 path [D: tools/service_seed/cli.py:415].
- **F-001-US3** — As a requester, I want my service's repository created and seeded
  from the standard template. I: inferred from repository creation followed by a
  templated first commit [D: tools/service_seed/cli.py:372].
- **F-001-US4** — As a reviewer, I want the infrastructure a service needs proposed
  as its own reviewable change. I: inferred from the infra-only partition and its
  dedicated change set [D: tools/service_seed/cli.py:384].
- **F-001-US5** — As a reviewer, I want the runtime declarations proposed
  separately and covering every environment. I: inferred from the second change set
  and the three-environment overlay loop [D: tools/service_seed/cli.py:396].
- **F-001-US6** — As a platform engineer, I want the availability tier alone to fix
  release strictness. I: inferred from analysis and canary values living only in the
  per-class profiles [D: tools/service_seed/service_template.py:95].
- **F-001-US7** — As a platform engineer, I want to see what a request would
  produce, and what cluster identity it would use, without touching anything. I:
  inferred from the local render and registry-inspection subcommands
  [D: tools/service_seed/cli.py:431].

## Acceptance criteria

Lifted from test names; each is already a Given/When/Then in everything but form.

**F-001-US1** — a canonical issue parses into the expected slug and class
[D: tools/service_seed/tests/test_jira_intake.py:100]; overrides take precedence
[D: tools/service_seed/tests/test_jira_intake.py:106]; the constructed request is
frozen [D: tools/service_seed/tests/test_jira_intake.py:93].

**F-001-US2** — an unexpected issue type raises
[D: tools/service_seed/tests/test_jira_intake.py:122]; a missing issue key raises
[D: tools/service_seed/tests/test_jira_intake.py:115]; an unknown class is rejected
[D: tools/service_seed/tests/test_jira_intake.py:83]; a malformed slug is rejected
[D: tools/service_seed/tests/test_jira_intake.py:88]; a request stating no class
becomes silver [D: tools/service_seed/tests/test_jira_intake.py:127].

**F-001-US3** — repository creation includes the project key
[D: tools/service_seed/tests/test_gitops_pr.py:49]; an existing repository is
treated as such [D: tools/service_seed/tests/test_gitops_pr.py:66]; the scaffold
render and push are driven end to end
[D: tools/service_seed/tests/test_gitops_pr.py:192]; a traversal slug writes nothing
[D: tools/service_seed/tests/test_robustness.py:186].

**F-001-US4** — the generated set includes the required scaffold
[D: tools/service_seed/tests/test_seed_job.py:39]; infra files carry registry-sourced
identity [D: tools/service_seed/tests/test_service_template.py:173]; the change-set
payload has the expected shape [D: tools/service_seed/tests/test_gitops_pr.py:96].

**F-001-US5** — prod overlays emit rollouts
[D: tools/service_seed/tests/test_service_template.py:167]; rollout kind varies by
environment [D: tools/service_seed/tests/test_seed_job.py:57].

**F-001-US6** — the gold threshold appears in the rendered analysis template
[D: tools/service_seed/tests/test_template_profiles.py:126]; silver has a success
rate and no p99 [D: tools/service_seed/tests/test_template_profiles.py:135]; bronze
omits the analysis template [D: tools/service_seed/tests/test_seed_job.py:66]; both
profiles declare all three classes
[D: tools/service_seed/tests/test_template_profiles.py:121].

**F-001-US7** — the registry dumps as JSON
[D: tools/service_seed/tests/test_registry.py:152]; the registry path prints
[D: tools/service_seed/tests/test_registry.py:161]; an unknown cluster raises
[D: tools/service_seed/tests/test_registry.py:84].

## Implementation status

| Story | Status | Carried by | Evidence |
| --- | --- | --- | --- |
| F-001-US1 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC1, TC2, TC3 · F-001-T2 | — |
| F-001-US2 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC4, TC5, TC6, TC7 · F-001-T2, T10 | — |
| F-001-US3 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC8, TC9, TC22 · F-001-T7, T8 | — |
| F-001-US4 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC10, TC11, TC12, TC13 · F-001-T5, T9 | — |
| F-001-US5 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC14, TC20 · F-001-T6 | — |
| F-001-US6 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC15, TC16, TC17, TC18 · F-001-T3, T4 | — |
| F-001-US7 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | F-001-TC19, TC21 · F-001-T1, T10 | — |

## Scope boundaries

Derived from what the code does **not** do, which is the only scope statement a
repository can make:

- No merge: no merge call exists in the source-host client [D: tools/service_seed/gitops_pr.py:55].
- No provisioning: nothing in the pipeline calls a cloud API; the claims it writes are reconciled elsewhere [D: tools/service_seed/service_template.py:293].
- No access grants: no identity or role assignment is created [D: tools/service_seed/cli.py:340].
- No promotion between environments: all three overlays are written at once and nothing sequences them [D: tools/service_seed/service_template.py:53].
- No intake surface of its own: the run takes an issue key on the command line [D: tools/service_seed/cli.py:440].

## Dependencies

| Dependency | Nature | Evidence |
| --- | --- | --- |
| F-003 cluster registry | Hard — the renderer resolves clusters from it | [D: tools/service_seed/service_template.py:270] |
| Issue tracker | External — the run cannot start without it | [D: tools/service_seed/jira_intake.py:205] |
| Source host | External — repository and change sets | [D: tools/service_seed/gitops_pr.py:38] |
| Jinja2, PyYAML, cookiecutter | Runtime — PyYAML optional with a fallback, cookiecutter optional with a fallback | [D: tools/service_seed/pyproject.toml:20] |

## Metrics

OPEN: No metric for this feature exists in the repository. Nothing measures
onboarding duration, request rejection rate, or how many services were seeded; the
pipeline emits no timing and no counter [D: tools/service_seed/cli.py:408].
`docs/architect.md:586` states a 45-minute end-to-end target, but that document is
a claim about intent rather than an observation, and no instrumentation exists that
could confirm or refute it.

## Open questions

- OPEN: What business outcome does this feature serve, and who signed off on it? Not recoverable from code.
- OPEN: What is the priority of this feature relative to the other three, and why? Not recoverable.
- OPEN: Is the 45-minute target in `docs/architect.md:586` a commitment, an estimate or an aspiration?
- OPEN: Who are the requesters in practice — application teams, a platform team on their behalf, or both?
- OPEN: What happens to a seeded service that is later retired? Nothing in the tree removes a scaffold.
