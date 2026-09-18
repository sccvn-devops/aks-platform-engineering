---
title: Tasks v4 F-001 — Service onboarding pipeline
id: F-001
kind: tasks
feature: F-001
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Tasks v4 F-001 — Service onboarding pipeline

> Ordered, dependency-aware implementation tasks — plan only, no code.

**As-built inventory, not a plan.** Each row names a unit of work that already
exists in the tree and points at the artifact that carries it. The done-when column
holds the check that would prove the row, which is what a `done` status then has to
cash.

## Task list

| Task | Description | Depends on | Artifact | Done-when | Status |
| --- | --- | --- | --- | --- | --- |
| F-001-T1 | Cluster catalogue loader with typed entries, key/name equality check and canonical vault identifier | — | [D: tools/service_seed/cli.py:164] | `python -m pytest tools/service_seed/tests/test_registry.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T2 | Intake: rich-text flattening, name and class inference, slug derivation, frozen self-validating request | — | [D: tools/service_seed/jira_intake.py:162] | `python -m pytest tools/service_seed/tests/test_jira_intake.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T3 | Tier profiles as data, loaded once, cross-checked against the three classes at import | — | [D: tools/service_seed/service_template.py:95] | `python -m pytest tools/service_seed/tests/test_template_profiles.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T4 | Jinja environment with strict undefined, and the template tree mirroring the output layout | F-001-T3 | [D: tools/service_seed/service_template.py:107] | Render for each class produces the expected file set | wip — unverified, run kubeconform over the rendered output; not available here |
| F-001-T5 | Infra rendering: namespace, three claims, vault binding, delivery policy, three overlays | F-001-T1, F-001-T4 | [D: tools/service_seed/service_template.py:293] | `python -m pytest tools/service_seed/tests/test_service_template.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T6 | Workload rendering: namespace, identity, service, ingress, secret projection, analysis, per-env release | F-001-T5 | [D: tools/service_seed/service_template.py:313] | Same suite; prod overlay emits a rollout | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T7 | Cookiecutter render with a pure-Python fallback and a path-safety check before any write | F-001-T2 | [D: tools/service_seed/service_template.py:128] | `python -m pytest tools/service_seed/tests/test_robustness.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T8 | Source-host client: repository creation treating conflict as success, change-set creation, redacted errors | — | [D: tools/service_seed/gitops_pr.py:38] | `python -m pytest tools/service_seed/tests/test_gitops_pr.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T9 | Change-set staging: shallow clone, branch, subtree replacement, bot identity, CI-skip commit, push, open | F-001-T6, F-001-T8 | [D: tools/service_seed/gitops_pr.py:134] | Same suite; the staging case asserts one host call | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T10 | CLI wiring: four subcommands, credentials by variable name, exit 2 on field errors, result line | F-001-T1, F-001-T2, F-001-T9 | [D: tools/service_seed/cli.py:420] | `python -m pytest tools/service_seed/tests/test_seed_job.py -q` passes | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 |
| F-001-T11 | Packaging: installable distribution exposing both console entry points, strict typing configured | F-001-T10 | [D: tools/service_seed/pyproject.toml:47] | `pip install -e "tools/service_seed[test]"` succeeds and both entry points resolve | wip — pip install -e "tools/service_seed[test]" succeeded here; console entry points not exercised |

## Execution order

Reconstructed from the import graph, not from history — the repository's commits
are squashed and same-day, so ordering cannot be recovered from git:

```
group A (no local imports)   T1 catalogue · T2 intake · T3 profiles · T8 host client
group B (needs A)            T4 jinja env (T3) · T7 cookiecutter+safety (T2)
group C (needs B)            T5 infra render (T1,T4) → T6 workload render (T5)
group D (needs C)            T9 staging (T6,T8)
group E (needs D)            T10 CLI wiring · T11 packaging
```

I: T1, T2, T3 and T8 are genuinely parallel — basis: `jira_intake` imports no local module [D: tools/service_seed/jira_intake.py:19], the registry loader lives beside the CLI and imports none of the others [D: tools/service_seed/cli.py:29], and `gitops_pr` imports only the request type and the writer [D: tools/service_seed/gitops_pr.py:31].

## Definition of done

- [ ] The done-when command ran **here** and passed; the command and result are the status note.
- [ ] Shapes match [`../data/schema/schemas.json`](../data/schema/schemas.json); `route.py` reports no fixture mismatch.
- [ ] No SLO numeric outside the profile files, and no cluster identity outside the registry.
- [ ] Every outbound call passes an explicit timeout, and no error string can carry a credential.
- [ ] No document under `docs/IDPGitOps-Specs/` was edited while implementing.

## Open questions

- OPEN: T11 declares a coverage dependency and calls it an acceptance gate [D: tools/service_seed/pyproject.toml:38], but no threshold is configured anywhere. What is the number?
- OPEN: The ordering above is inferred from imports; the actual delivery order is unrecoverable, since all 94 commits carry the same date and no merge commits exist.
