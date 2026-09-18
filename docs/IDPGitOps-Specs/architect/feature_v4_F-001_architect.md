---
title: Feature Architecture v4 F-001 — Service onboarding pipeline
id: F-001
kind: architecture
feature: F-001
version: v4
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Feature Architecture v4 F-001 — Service onboarding pipeline

> Low-level design for one feature: contracts, schema, sequence, failure modes.

As built, read out of the code. Design intent that the code does not state is an
OPEN: question at the end, not a paragraph here.

## Design summary

I: the pipeline is organised as four separable concerns sharing exactly one type — basis: `jira_intake` performs the tracker fetch and builds a frozen dataclass and imports no local module [D: tools/service_seed/jira_intake.py:19]; `service_template` imports that dataclass and the registry loader and touches no network [D: tools/service_seed/service_template.py:36]; `gitops_pr` owns every git subprocess and every source-host call [D: tools/service_seed/gitops_pr.py:116]; and `cli` wires the three together [D: tools/service_seed/cli.py:340].

I: rendering is pure with respect to the outside world — basis: `render` takes the request and a registry mapping as arguments and returns a path→content dict, writing nothing [D: tools/service_seed/service_template.py:251]; the only writer is a separate function [D: tools/service_seed/service_template.py:208].

The shape rules out a module that both fetches and pushes, a second registry read
inside rendering, and any tier numeric living in Python: the profiles are loaded
once at import and indexed by class [D: tools/service_seed/service_template.py:95].

## API contracts

Surface, shapes and error model: [`../data/api-contract_v4_F-001.md`](../data/api-contract_v4_F-001.md).

Behaviour worth stating here:

- Two commands, not one flag: the local render and the full pipeline are separate
  subparsers [D: tools/service_seed/cli.py:431].
- Registry inspection has its own entry point that cannot reach the tracker
  [D: tools/service_seed/cli.py:257].
- Credentials are named, not passed: the command takes the variable name and reads
  the environment at run time [D: tools/service_seed/cli.py:441].
- Errors carrying a field abort with exit 2 [D: tools/service_seed/cli.py:415].

OPEN: Why two commands rather than one with a dry-run flag is not recorded
anywhere in the repository.

## Data model

Scope and fields: [`../data/data-erd_v4_F-001.md`](../data/data-erd_v4_F-001.md).

Owns `service_request`, `gitops_manifest_file`, `gitops_pull_request`,
`seed_result`; reads `slo_profile`, `rollout_profile`, `cluster_entry`. The request
is frozen on construction [D: tools/service_seed/jira_intake.py:47] and validated
in `__post_init__` [D: tools/service_seed/jira_intake.py:63].

## Sequence

Happy path, `seed-from-jira`:

1. Read both credentials from the environment by name; a missing one returns 2 before any network call [D: tools/service_seed/cli.py:350].
2. Fetch the tracker issue over a 30-second-bounded call [D: tools/service_seed/cli.py:356].
3. Parse into a frozen request, with command-line overrides applied [D: tools/service_seed/cli.py:358].
4. Create the repository named for the slug [D: tools/service_seed/cli.py:370].
5. Render the service template into a temporary tree, commit as `PlatformBot`, push as the first commit [D: tools/service_seed/gitops_pr.py:177].
6. Render the scaffold [D: tools/service_seed/cli.py:380].
7. Partition by an `infra/` or `workload/` path segment [D: tools/service_seed/cli.py:381].
8. Stage and open the infra change set [D: tools/service_seed/cli.py:384].
9. Stage and open the workload change set [D: tools/service_seed/cli.py:396].
10. Print the result line, return 0 [D: tools/service_seed/cli.py:408].

Staging, step 8 in detail: clone shallow [D: tools/service_seed/gitops_pr.py:147], branch [D: tools/service_seed/gitops_pr.py:150], remove any existing subtree for the slug [D: tools/service_seed/gitops_pr.py:152], write [D: tools/service_seed/gitops_pr.py:154], set the bot identity [D: tools/service_seed/gitops_pr.py:156], commit with a CI-skip marker [D: tools/service_seed/gitops_pr.py:158], push [D: tools/service_seed/gitops_pr.py:159], open the change set [D: tools/service_seed/gitops_pr.py:164].

Failure paths: intake rejection returns 2 at step 3 [D: tools/service_seed/cli.py:365]; a repository conflict at step 4 is treated as success [D: tools/service_seed/gitops_pr.py:93]; a traversal template aborts before any write at step 5 [D: tools/service_seed/service_template.py:192]; an unknown cluster aborts at step 6 [D: tools/service_seed/cli.py:230].

I: the run is not atomic across systems — basis: repository creation and the first push happen at steps 4–5, and no compensating delete exists anywhere in the module if a later step fails [D: tools/service_seed/gitops_pr.py:167].

## Failure modes

| Failure | Detection | Behaviour | Evidence |
| --- | --- | --- | --- |
| Wrong issue type, missing name, bad tier, unusable slug | `__post_init__` | exit 2 naming the field, nothing created | [D: tools/service_seed/jira_intake.py:63] |
| Credential variable absent | KeyError on lookup | exit 2 naming the variable | [D: tools/service_seed/cli.py:352] |
| Tracker slow or down | 30 s urlopen bound | raises; no partial state | [D: tools/service_seed/jira_intake.py:220] |
| Source host 4xx other than 400 | HTTPError branch | RuntimeError with method, path, status, body — no credential | [D: tools/service_seed/gitops_pr.py:99] |
| Repository already exists | 400 with conflict flag | treated as success | [D: tools/service_seed/gitops_pr.py:93] |
| Template path escapes destination | `_safe_join` before any mkdir | raises with zero filesystem effect | [D: tools/service_seed/service_template.py:150] |
| Missing template variable | `StrictUndefined` | raises at render | [D: tools/service_seed/service_template.py:112] |
| Profile missing a class | import-time cross-check | module refuses to import | [D: tools/service_seed/service_template.py:101] |
| Unknown cluster | registry lookup | raises listing the known names | [D: tools/service_seed/cli.py:230] |
| Git hangs | 300 s subprocess bound | subprocess timeout raises | [D: tools/service_seed/gitops_pr.py:112] |
| Cookiecutter binary absent | FileNotFoundError branch | falls back to the pure-Python renderer | [D: tools/service_seed/service_template.py:146] |

## Observability

What the code emits: the result line [D: tools/service_seed/cli.py:408],
field-named error lines [D: tools/service_seed/cli.py:415], a credential-absent
line [D: tools/service_seed/cli.py:353], and transport errors carrying method,
path, status and body [D: tools/service_seed/gitops_pr.py:99].

What it does not emit: no metric, no structured log, no span. The surveyed telemetry
libraries belong to other features, and no logging import appears in these modules
[D: tools/service_seed/cli.py:1].

- OPEN: Is the run's duration measured anywhere? No timing is emitted, so any
  end-to-end onboarding target cannot be observed from the tool's own output.
- OPEN: Is there an intended correlation identifier between the request key, the
  two change sets and the eventual deployment?

## Traceability

| Design element | Satisfies |
| --- | --- |
| Frozen self-validating request | F-001-US1, F-001-US2 · DOM-001-R1…R5 |
| Validation before any creation call | F-001-US2 · DOM-001-R2 |
| Template-seeded repository, conflict tolerated | F-001-US3 |
| Path-safety check before first write | F-001-US3 · DOM-001-R9 |
| Two disjoint change sets | F-001-US4, F-001-US5 · DOM-001-R6 |
| Three environment overlays | F-001-US5 · DOM-001-R7 |
| Tier profiles as the only home for numerics | F-001-US6 · DOM-001-R8 |
| Registry as the only identity source | F-001-US7 · DOM-001-R10 |
