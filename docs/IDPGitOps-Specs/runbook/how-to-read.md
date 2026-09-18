---
title: How to read the runbook folder — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to read the runbook folder — IDPGitOps

> Explain runbook types and when an engineer or agent should reach for one.

## Naming

`runbook_<TYPE>_RB-nnn-<slug>.md`, `TYPE ∈ DEV | TEST | TROUBLESHOOT | DEPLOY`.

| Runbook | Type | Covers |
| --- | --- | --- |
| `runbook_DEV_RB-001-local-setup.md` | DEV | Clean checkout to a green test run |

Operational runbooks already exist outside this hub and are **not** copied into it:

| Existing | Covers |
| --- | --- |
| `docs/management-plane-failover-runbook.md` | Failover and failback |
| `docs/seed-cluster-dr-runbook.md` | Rebuild from the seed cluster |
| `docs/akv-near-expiry-runbook.md` | Secret material approaching expiry |
| `docs/tls-history-purge-2026-05-runbook.md` | The 2026-05 history purge |

A route file may reference those paths directly; they resolve from the repository
root exactly as hub documents do.

## Contract

Preconditions, numbered steps, verification, rollback, and a common-failures table.
In a reversed hub one extra rule applies: **a command nobody here has run is marked
`I:`**, and the verification section says which command proves the step and whether
it was executed.

## When to write a new one

Any procedure performed twice by hand. A runbook that only points at an existing one
is legitimate when it adds verification; it must not restate the other's steps.
