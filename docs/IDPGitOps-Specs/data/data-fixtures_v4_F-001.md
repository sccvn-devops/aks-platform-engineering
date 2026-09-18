---
title: Fixtures v4 F-001 — Service onboarding pipeline
id: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Fixtures v4 F-001 — Service onboarding pipeline

> The test data this feature is implemented and verified against, and what each fixture is for.

Derived, not invented: request keys and service names come from the repository's
own test data [D: tools/service_seed/tests/test_service_template.py:34]
[D: tools/service_seed/tests/test_service_template.py:150], cluster rows are copied from
the committed catalogue [D: gitops/clusters/registry.yaml:97], and profile records
are the committed values [D: tools/service_seed/profiles/slo.yaml:13].

## Fixture sets

| Set | Entities | Scenario | Used by |
| --- | --- | --- | --- |
| F-001-FX1 | `service_request` IDP-42 (silver) | The repository's own canonical request [D: tools/service_seed/tests/test_seed_job.py:34] | F-001-TC1, TC8, TC14 |
| F-001-FX2 | `service_request` IDP-43 (gold) | Strictest tier; the only one with a latency gate [D: tools/service_seed/tests/test_service_template.py:189] | F-001-TC15, TC18 |
| F-001-FX3 | `service_request` IDP-44 (bronze) | No analysis template at all [D: tools/service_seed/tests/test_seed_job.py:66] | F-001-TC16, TC17 |
| F-001-FX4 | `slo_profile` ×3, `rollout_profile` ×3 | The complete tier matrix as committed [D: tools/service_seed/profiles/rollout.yaml:12] | F-001-TC15, TC16, TC17 |
| F-001-FX5 | `cluster_entry` ×2 | The two production clusters the renderer resolves [D: tools/service_seed/service_template.py:46] | F-001-TC13, TC19 |
| F-001-FX6 | `gitops_manifest_file` ×6 | A representative slice of one rendered scaffold [D: tools/service_seed/tests/test_service_template.py:149] | F-001-TC10, TC11, TC12 |
| F-001-FX7 | `gitops_pull_request` ×2 | The two change sets for one request [D: tools/service_seed/tests/test_gitops_pr.py:151] | F-001-TC11, TC20 |
| F-001-FX8 | `seed_result` | The single success line a job parses [D: tools/service_seed/cli.py:408] | F-001-TC1 |

All sets share one file: [`fixtures/fixtures_v4_F-001.json`](fixtures/fixtures_v4_F-001.json).

## Fixture file

[`fixtures/fixtures_v4_F-001.json`](fixtures/fixtures_v4_F-001.json). `route.py`
validates every record against [`schema/schemas.json`](schema/schemas.json) and
fails the route on an unknown entity, a missing required field or an undeclared one.

## Encoded invariants

| Encoded in the data | Rule it exercises | Evidence |
| --- | --- | --- |
| Every request carries the one accepted issue type | DOM-001-R1 | [D: tools/service_seed/jira_intake.py:71] |
| `orders`, `payments`, `cart` are the hyphen-free lowercase forms of their names | DOM-001-R3 | [D: tools/service_seed/jira_intake.py:88] |
| Gold carries a latency gate; silver the same success rate without one; bronze no analysis | DOM-001-R8 | [D: tools/service_seed/profiles/slo.yaml:15] |
| Both profile sets declare exactly gold, silver, bronze | DOM-001-R8 | [D: tools/service_seed/service_template.py:98] |
| Every manifest path begins `apps/<slug>/`, and no path appears in both tiers | DOM-001-R6, DOM-001-R9 | [D: tools/service_seed/cli.py:381] |
| Overlay records carry `env`; base records omit it | DOM-001-R7 | [D: tools/service_seed/service_template.py:53] |
| Branch names follow `seed/<slug>-<tier>-<key lowercased>`, descriptions name key and tier | DOM-001-R6 | [D: tools/service_seed/cli.py:388] |
| Cluster rows carry the catalogue's own placeholder subscription | DOM-001-R10 | [D: gitops/clusters/registry.yaml:29] |

Rejections cannot be represented as records here — the schema makes an invalid
issue type or tier unrepresentable — so they are expressed as negative test cases
(F-001-TC4…TC7) that build the payload inline.

## Determinism rules

- Fixed request keys; no timestamps appear in this set at all.
- No randomness and no clock read; the pipeline itself performs none either
  [D: tools/service_seed/service_template.py:251].
- Subscription identifiers are the all-zero placeholders the committed catalogue
  carries [D: gitops/clusters/registry.yaml:36].
- No credential, token or connection string appears, and no field in these entities
  can hold one.

## Loading

There is no store. The repository's own tests build a request from a dict literal
and call the parser [D: tools/service_seed/tests/test_jira_intake.py:100], render
into a temporary directory [D: tools/service_seed/tests/test_service_template.py:112],
and replace the source host with a fake at the call site
[D: tools/service_seed/tests/test_gitops_pr.py:151]. Reset is per test: each case
creates its own temporary tree.

I: these fixtures are a documentation artefact rather than an input the suite already consumes — basis: no test in the tree reads a JSON fixture file; every case constructs its data inline [D: tools/service_seed/tests/test_seed_job.py:11].

## Traceability

| Set | Test cases |
| --- | --- |
| F-001-FX1 | F-001-TC1, TC8, TC14 |
| F-001-FX2 | F-001-TC15, TC18 |
| F-001-FX3 | F-001-TC16, TC17 |
| F-001-FX4 | F-001-TC15, TC16, TC17 |
| F-001-FX5 | F-001-TC13, TC19 |
| F-001-FX6 | F-001-TC10, TC11, TC12 |
| F-001-FX7 | F-001-TC11, TC20 |
| F-001-FX8 | F-001-TC1 |

## Open questions

- OPEN: Should these fixtures become the suite's actual input, replacing the
  inline dict literals? Today the same shapes exist twice — here and in each test
  module — and nothing keeps them in step.
- OPEN: No fixture exists for a service that already has a scaffold, which is the
  re-seed path [D: tools/service_seed/gitops_pr.py:152]; what should the expected
  post-state be?
