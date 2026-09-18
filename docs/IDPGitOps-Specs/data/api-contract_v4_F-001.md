---
title: API Contract v4 F-001 — Service onboarding pipeline
id: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# API Contract v4 F-001 — Service onboarding pipeline

> The wire contract for this feature — the readable companion to its OpenAPI and AsyncAPI files.

## Surface summary

This feature serves nothing. The survey found one HTTP route in the entire
repository and it belongs to another feature [D: tools/mgmt-plane-lock/internal/bootstrap/bootstrap.go:54].
What F-001 exposes is a command with four subcommands [D: tools/service_seed/cli.py:420],
an exit-code contract [D: tools/service_seed/cli.py:310] and one machine-readable
result line [D: tools/service_seed/cli.py:408]. What it consumes is three outbound
HTTP calls.

I: there is no way to trigger onboarding except through the tracker — basis: the only issue-fetching call takes an issue key from the command line and no listener, webhook receiver or scheduler exists anywhere in the surveyed tree [D: tools/service_seed/cli.py:340].

## Endpoints

Outbound calls (client contract — the method and path are in separate columns
because this feature serves none of them):

| Method | Path | Auth | Request | Response | Evidence |
| --- | --- | --- | --- | --- | --- |
| GET | `rest/api/3/issue/{issueKey}` | Basic with email, Bearer without | — | tracker-native issue, projected into `$defs/service_request` | [D: tools/service_seed/jira_intake.py:212] |
| POST | `2.0/repositories/{workspace}/{repoSlug}` | Basic | `{scm, is_private, project?}` | repository | [D: tools/service_seed/gitops_pr.py:44] |
| POST | `2.0/repositories/{workspace}/{repoSlug}/pullrequests` | Basic | values modelled as `$defs/gitops_pull_request` | change set | [D: tools/service_seed/gitops_pr.py:63] |

Command surface:

| Command | Reads | Writes | Exit | Evidence |
| --- | --- | --- | --- | --- |
| `seed-from-jira` | tracker issue, catalogue, profiles | repository, two change sets, one `$defs/seed_result` line | 0, or 2 on rejected input or absent credential | [D: tools/service_seed/cli.py:438] |
| `generate-gitops` | profiles, catalogue | `$defs/gitops_manifest_file` set on local disk | 0, or 2 | [D: tools/service_seed/cli.py:431] |
| `registry-show [name]` | catalogue | `$defs/cluster_entry` records as JSON | 0, or 2 on unknown cluster | [D: tools/service_seed/cli.py:456] |
| `registry-paths` | — | the catalogue path | 0 | [D: tools/service_seed/cli.py:461] |

Credentials are passed by variable **name**, not value
[D: tools/service_seed/cli.py:441], and read from the environment at run time
[D: tools/service_seed/cli.py:350].

## Events

**None.** [`schema/asyncapi_v4_F-001.json`](schema/asyncapi_v4_F-001.json) has empty
channels and operations. No broker client, publish call or subscription appears in
the onboarding modules; the survey's single event literal belongs to a Key Vault
alert topic [D: terraform/akv_alerts.tf:47].

## Error model

One line on stdout, one exit code:

| Condition | Message shape | Exit | Evidence |
| --- | --- | --- | --- |
| Rejected intake | `error: invalid Jira intake (field=<field>): <message>` | 2 | [D: tools/service_seed/cli.py:415] |
| Credential variable absent | `error: environment variable <NAME> is not set` | 2 | [D: tools/service_seed/cli.py:353] |
| Unknown cluster | `error: cluster '<name>' not in registry; known: [...]` | 2 | [D: tools/service_seed/cli.py:474] |
| Source-host failure | `bitbucket api <METHOD> <path> failed: <code> <body>` | non-zero | [D: tools/service_seed/gitops_pr.py:99] |
| Template escapes destination | `rendered path escapes destination: …` | non-zero | [D: tools/service_seed/service_template.py:173] |
| Profile missing a class | `profiles/slo.yaml missing classes: [...]` | non-zero at import | [D: tools/service_seed/service_template.py:102] |

Two properties hold across the table. Credentials never appear: the transport error
carries method, path, status and body only [D: tools/service_seed/gitops_pr.py:99].
And intake rejection precedes every outbound creation call
[D: tools/service_seed/cli.py:358].

## Versioning and compatibility

| Stable by construction | Changes with care | Evidence |
| --- | --- | --- |
| The three result-line keys, read by whatever invokes the command | — | [D: tools/service_seed/cli.py:408] |
| Exit code 2 meaning rejected input | — | [D: tools/service_seed/cli.py:310] |
| — | Adding a required argument breaks existing invocations | [D: tools/service_seed/cli.py:438] |
| — | Generated path layout: already-merged services keep the old paths | [D: tools/service_seed/service_template.py:303] |
| — | Consumed vendor APIs are version-pinned in the path (`/rest/api/3/`, `/2.0/`) | [D: tools/service_seed/jira_intake.py:212] |

OPEN: No deprecation policy for a subcommand or an argument is expressed
anywhere in the repository; nothing in the code marks anything deprecated.

## Spec files

- [`schema/openapi_v4_F-001.json`](schema/openapi_v4_F-001.json) — the three outbound operations.
- [`schema/asyncapi_v4_F-001.json`](schema/asyncapi_v4_F-001.json) — empty.

Shapes the platform owns are `$ref`'d from [`schema/schemas.json`](schema/schemas.json).
Vendor payloads are named and not redefined. The repository contains no OpenAPI or
AsyncAPI file of its own, so there is nothing to reconcile these against — the
survey's in-repo spec list is empty.

## Traceability

| Surface | Satisfies |
| --- | --- |
| Issue fetch | F-001-US1 · DOM-001-R1 |
| Exit-2 field-named rejection | F-001-US2 · DOM-001-R2 |
| Repository creation, conflict tolerated | F-001-US3 |
| Two change-set calls | F-001-US4, F-001-US5 · DOM-001-R6 |
| Result line | F-001-US1 |
| `generate-gitops` | F-001-US6 |
| `registry-show` / `registry-paths` | F-001-US7 · DOM-001-R10 |

## Open questions

- OPEN: The tracker call has no retry and no pagination handling
  [D: tools/service_seed/jira_intake.py:220]; is a transient tracker failure meant
  to fail the whole seed run, or be retried by the job around it?
- OPEN: A run that creates the repository and then fails to open a change set
  leaves cross-system state behind [D: tools/service_seed/cli.py:370]. Is re-running
  the intended recovery, and is anything expected to clean up a half-seeded service?
- OPEN: No rate-limit handling exists for either vendor API; the observed limits
  are not documented anywhere in the repository.
