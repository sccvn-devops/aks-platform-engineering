---
title: Test Plan v4 F-001 — Service onboarding pipeline
id: F-001
kind: test
feature: F-001
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Test Plan v4 F-001 — Service onboarding pipeline

> Concrete cases traced back to acceptance criteria.

**As-built inventory.** These cases are the tests that exist, read out of the suite
and cited; they are not a plan for tests someone should write. The suite holds 94
cases across seven modules [D: tools/service_seed/tests/test_jira_intake.py:53];
one `F-001-TC<k>` below stands for one real test function, chosen to cover each
distinct behaviour. Coverage holes are listed at the end and are the most useful
part of this document.

## Traceability matrix

| Story | Test cases | Level |
| --- | --- | --- |
| F-001-US1 | F-001-TC1, TC2, TC3 | unit |
| F-001-US2 | F-001-TC4, TC5, TC6, TC7 | unit |
| F-001-US3 | F-001-TC8, TC9, TC22 | unit |
| F-001-US4 | F-001-TC10, TC11, TC12, TC13 | unit |
| F-001-US5 | F-001-TC14, TC20 | unit |
| F-001-US6 | F-001-TC15, TC16, TC17, TC18 | unit |
| F-001-US7 | F-001-TC19, TC21 | unit |
| all | F-001-TC23, TC24 | invariant |

## Test cases

| Case | What it proves | Cited test |
| --- | --- | --- |
| F-001-TC1 | A canonical issue parses into the expected slug and class | [D: tools/service_seed/tests/test_jira_intake.py:100] |
| F-001-TC2 | Command-line overrides beat inferred values | [D: tools/service_seed/tests/test_jira_intake.py:106] |
| F-001-TC3 | A valid request constructs and is frozen | [D: tools/service_seed/tests/test_jira_intake.py:69] |
| F-001-TC4 | An unexpected issue type is rejected | [D: tools/service_seed/tests/test_jira_intake.py:78] |
| F-001-TC5 | An empty issue key is rejected | [D: tools/service_seed/tests/test_jira_intake.py:73] |
| F-001-TC6 | An unknown SLO class is rejected | [D: tools/service_seed/tests/test_jira_intake.py:83] |
| F-001-TC7 | A malformed slug is rejected | [D: tools/service_seed/tests/test_jira_intake.py:88] |
| F-001-TC8 | Repository creation sends the project key | [D: tools/service_seed/tests/test_gitops_pr.py:49] |
| F-001-TC9 | A 400 from repository creation is treated as existing | [D: tools/service_seed/tests/test_gitops_pr.py:66] |
| F-001-TC10 | The generated scaffold contains the required files | [D: tools/service_seed/tests/test_seed_job.py:39] |
| F-001-TC11 | The change-set payload has the expected shape | [D: tools/service_seed/tests/test_gitops_pr.py:96] |
| F-001-TC12 | Staging derives the repository slug and calls the host once | [D: tools/service_seed/tests/test_gitops_pr.py:151] |
| F-001-TC13 | Infra files carry identity sourced from the registry | [D: tools/service_seed/tests/test_service_template.py:173] |
| F-001-TC14 | Rollout kind varies by environment | [D: tools/service_seed/tests/test_seed_job.py:57] |
| F-001-TC15 | The gold analysis template contains the p99 threshold | [D: tools/service_seed/tests/test_service_template.py:189] |
| F-001-TC16 | Silver has a success rate and no p99 | [D: tools/service_seed/tests/test_template_profiles.py:135] |
| F-001-TC17 | Bronze omits the analysis template | [D: tools/service_seed/tests/test_seed_job.py:66] |
| F-001-TC18 | The gold threshold reaches the rendered analysis template | [D: tools/service_seed/tests/test_template_profiles.py:126] |
| F-001-TC19 | The committed registry loads with every required field | [D: tools/service_seed/tests/test_registry.py:48] |
| F-001-TC20 | Prod overlays emit rollouts | [D: tools/service_seed/tests/test_service_template.py:167] |
| F-001-TC21 | An unknown cluster raises | [D: tools/service_seed/tests/test_registry.py:84] |
| F-001-TC22 | The fallback renderer refuses a traversal slug before any write | [D: tools/service_seed/tests/test_robustness.py:186] |
| F-001-TC23 | Every outbound HTTP call passes the declared timeout | [D: tools/service_seed/tests/test_robustness.py:47] |
| F-001-TC24 | Every subprocess passes the declared timeout | [D: tools/service_seed/tests/test_robustness.py:95] |

## Implementation status

| Case | Status | Evidence |
| --- | --- | --- |
| F-001-TC1 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC2 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC3 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC4 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC5 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC6 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC7 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC8 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC9 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC10 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC11 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC12 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC13 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC14 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC15 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC16 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC17 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC18 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC19 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC20 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC21 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC22 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC23 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |
| F-001-TC24 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — |

## Edge and negative cases

| Case | Covered by |
| --- | --- |
| Slugify rejects a name that yields an empty slug | [D: tools/service_seed/tests/test_jira_intake.py:145] |
| Rich text with nested paragraphs flattens | [D: tools/service_seed/tests/test_jira_intake.py:149] |
| Service name taken from a label | [D: tools/service_seed/tests/test_jira_intake.py:157] |
| SLO class taken from a label, upper-cased in the source | [D: tools/service_seed/tests/test_jira_intake.py:161] |
| Bearer auth when no email is configured | [D: tools/service_seed/tests/test_jira_intake.py:203] |
| Non-400 4xx raises rather than being swallowed | [D: tools/service_seed/tests/test_gitops_pr.py:75] |
| Registry file missing | [D: tools/service_seed/tests/test_registry.py:96] |
| Registry top-level key and `aks_name` disagree | [D: tools/service_seed/tests/test_registry.py:100] |
| Registry entry missing a required field | [D: tools/service_seed/tests/test_registry.py:110] |
| Fallback YAML parser handles the committed registry | [D: tools/service_seed/tests/test_registry.py:131] |
| A missing template variable raises at render | [D: tools/service_seed/tests/test_template_profiles.py:96] |
| A missing SLO sub-key raises | [D: tools/service_seed/tests/test_template_profiles.py:104] |

## Out of scope

Coverage holes in the suite as it stands — each is a real gap, not a deferral:

- **No test covers the re-seed replacement path** (DOM-001-R11): nothing exercises
  a platform repository that already contains a scaffold for the slug
  [D: tools/service_seed/gitops_pr.py:152].
- **No test covers the full `seed-from-jira` wiring end to end**; the modules are
  tested separately and the orchestrator's own error paths are exercised only
  through them [D: tools/service_seed/cli.py:340].
- **No test asserts that an absent credential stops the run before the first
  network call** [D: tools/service_seed/cli.py:352].
- **No test asserts the two change sets are disjoint**; each is asserted alone
  [D: tools/service_seed/cli.py:381].
- **Nothing validates rendered manifests against Kubernetes schemas** inside the
  suite; the survey found no CI configuration at all, and the workflow files that
  exist are not detected by it — see the report.

## Open questions

- OPEN: Is `kubeconform` run against rendered output anywhere? `docs/architect.md:622` says CI does it; the survey found no CI configuration and no reference to `kubeconform` in the suite.
- OPEN: What coverage threshold applies to this package? No coverage gate is configured in the project's own manifest [D: tools/service_seed/pyproject.toml:38].
