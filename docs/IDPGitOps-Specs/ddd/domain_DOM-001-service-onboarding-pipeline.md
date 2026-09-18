---
title: Domain — Service onboarding pipeline
id: DOM-001
kind: domain
feature: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Domain — Service onboarding pipeline

> Business rules, process and user flow for this domain, in the language of the business.

Reconstructed. Each rule below is enforced somewhere in the code and cites that
enforcement; a rule nobody enforces is not listed here, it is an open question.

## Ubiquitous language

| Term | Definition | Source |
| --- | --- | --- |
| Service request | A tracker issue of type *IDP Service Request* | [D: tools/service_seed/jira_intake.py:30] |
| Service slug | The service's identity in repository name, paths and namespaces; lowercase words joined by single hyphens | [D: tools/service_seed/jira_intake.py:32] |
| SLO class | One of `bronze`, `silver`, `gold` | [D: tools/service_seed/jira_intake.py:29] |
| Scaffold | The rendered set of declarations for a service, keyed by path | [D: tools/service_seed/service_template.py:251] |
| Infra tier | The claims and namespace binding, reviewed as one change set | [D: tools/service_seed/service_template.py:293] |
| Workload tier | Namespace, identity, service, ingress, secret projection, analysis, release | [D: tools/service_seed/service_template.py:313] |
| Environment | `dev`, `staging`, `prod`, in that order | [D: tools/service_seed/service_template.py:53] |
| Cluster registry | The committed catalogue of cluster identity | [D: tools/service_seed/cli.py:35] |
| Platform bot | The commit identity the pipeline authors as | [D: tools/service_seed/gitops_pr.py:157] |

Aliases the code does not use, and which should be avoided in writing about it:
"ticket" for a service request, "app" for a service, "tier" for SLO class — the
code already uses tier for the infra/workload split
[D: tools/service_seed/cli.py:381].

## Actors

| Actor | Role | Evidence |
| --- | --- | --- |
| Requester | Raises the tracker issue the run reads | [D: tools/service_seed/cli.py:356] |
| Platform bot | Creates the repository, authors commits, opens change sets | [D: tools/service_seed/gitops_pr.py:157] |
| Reviewer | I: the only actor who can merge — basis: the pipeline opens change sets and no merge call exists in the module [D: tools/service_seed/gitops_pr.py:164] |
| Issue tracker | System of record for the request | [D: tools/service_seed/jira_intake.py:205] |
| Source host | Holds the service repository and the platform repository | [D: tools/service_seed/gitops_pr.py:38] |

## Business rules

- **DOM-001-R1** — A request is accepted only when its issue type is exactly *IDP Service Request*; any other type is rejected naming the type found. [D: tools/service_seed/jira_intake.py:71]
- **DOM-001-R2** — Issue key, issue type, service name, slug and SLO class are all required and non-empty; a missing one is reported by field name. [D: tools/service_seed/jira_intake.py:64]
- **DOM-001-R3** — A slug is lowercase alphanumeric words joined by single hyphens, with no leading, trailing or repeated hyphen. [D: tools/service_seed/jira_intake.py:32]
- **DOM-001-R4** — Exactly one SLO class per request, from `bronze`, `silver`, `gold`; a request stating none is `silver`. [D: tools/service_seed/jira_intake.py:76] [D: tools/service_seed/jira_intake.py:159]
- **DOM-001-R5** — An accepted request cannot be altered afterwards. [D: tools/service_seed/jira_intake.py:47]
- **DOM-001-R6** — One run produces exactly two change sets, one per tier, and the platform opens but never merges them. [D: tools/service_seed/cli.py:384] [D: tools/service_seed/cli.py:396]
- **DOM-001-R7** — Every service is scaffolded for `dev`, `staging` and `prod`, in that order. [D: tools/service_seed/service_template.py:53]
- **DOM-001-R8** — The SLO class alone decides analysis and canary progression; both come from the profile files and neither exists as a literal in code. [D: tools/service_seed/service_template.py:217] [D: tools/service_seed/service_template.py:224]
- **DOM-001-R9** — Everything a scaffold writes lands under that service's own directory, and a template resolving outside it is rejected before any write. [D: tools/service_seed/service_template.py:192] [D: tools/service_seed/service_template.py:150]
- **DOM-001-R10** — Cluster identity comes from the registry; the pipeline holds no region, subscription, resource group or vault identifier of its own. [D: tools/service_seed/service_template.py:270] [D: tools/service_seed/cli.py:236]
- **DOM-001-R11** — Re-seeding a service replaces its scaffold rather than merging into it. [D: tools/service_seed/gitops_pr.py:152]
- **DOM-001-R12** — Every call leaving the process is time-bounded: 30 seconds for HTTP, 300 for a subprocess. [D: tools/service_seed/jira_intake.py:27] [D: tools/service_seed/gitops_pr.py:34]

## Process flow

1. A requester raises the issue. [D: tools/service_seed/cli.py:356]
2. Intake projects it into an accepted request. [D: tools/service_seed/cli.py:358]
3. The service repository is created and seeded from the template. [D: tools/service_seed/cli.py:372]
4. The scaffold is rendered for all three environments. [D: tools/service_seed/cli.py:380]
5. The infra change set is opened. [D: tools/service_seed/cli.py:384]
6. The workload change set is opened. [D: tools/service_seed/cli.py:396]
7. The run reports slug, request key and class. [D: tools/service_seed/cli.py:408]

Alternates: an existing repository is accepted as-is [D: tools/service_seed/gitops_pr.py:93];
an existing scaffold is removed before writing [D: tools/service_seed/gitops_pr.py:152];
a request stating no class becomes silver [D: tools/service_seed/jira_intake.py:159];
a local render writes to a directory and contacts nothing [D: tools/service_seed/cli.py:315].

Errors: rejection at step 2 returns 2 before step 3 runs [D: tools/service_seed/cli.py:365];
an absent credential stops the run before step 1 [D: tools/service_seed/cli.py:352];
a traversal template aborts with nothing written [D: tools/service_seed/service_template.py:192];
an unknown cluster aborts listing the known ones [D: tools/service_seed/cli.py:230].

## Invariants

- A partially-formed accepted request cannot exist. [D: tools/service_seed/jira_intake.py:63]
- The slug is one string across repository name, paths and namespaces. [D: tools/service_seed/cli.py:371]
- The two change sets are disjoint. [D: tools/service_seed/cli.py:381]
- No credential reaches an error message; the transport error carries method, path, status and body. [D: tools/service_seed/gitops_pr.py:99]
- No SLO numeric exists outside the profile files. [D: tools/service_seed/profiles/slo.yaml:13]

## Implementation status

| Rule | Status | Evidence | Covering test in tree |
| --- | --- | --- | --- |
| DOM-001-R1 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_jira_intake.py:78] |
| DOM-001-R2 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_jira_intake.py:73] |
| DOM-001-R3 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_jira_intake.py:88] |
| DOM-001-R4 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_jira_intake.py:83] |
| DOM-001-R5 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_jira_intake.py:93] |
| DOM-001-R6 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_gitops_pr.py:151] |
| DOM-001-R7 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_seed_job.py:57] |
| DOM-001-R8 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_template_profiles.py:126] |
| DOM-001-R9 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_robustness.py:186] |
| DOM-001-R10 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_service_template.py:173] |
| DOM-001-R11 | wip — implemented, no test names this rule | — | — |
| DOM-001-R12 | done — pytest tools/service_seed/tests -q, 94 passed here 2026-09-18 | — | [D: tools/service_seed/tests/test_robustness.py:41] |

DOM-001-R11 has no test naming it: the re-seed replacement path is exercised
nowhere in the suite. That gap is the row's content, not a formatting accident.

## Open questions

- OPEN: `docs/architect.md:600` states the request validates an owner team; no such field is validated [D: tools/service_seed/jira_intake.py:63]. Which is wrong, the document or the code?
- OPEN: Why is `silver` the default for a request that states no class [D: tools/service_seed/jira_intake.py:159]? The choice is in the code with no recorded reason.
- OPEN: Is a service ever legitimately re-seeded after a human has edited its scaffold? DOM-001-R11 destroys such edits and nothing warns.
- OPEN: Service-name inference walks explicit fields, then labels, then free text [D: tools/service_seed/jira_intake.py:110]. Which of those paths is the supported contract with requesters, and which is a tolerance?
- OPEN: No rule governs slug collision: two services whose names slugify identically would target the same repository and paths, and nothing checks for that.
