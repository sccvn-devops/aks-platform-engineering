---
title: How to read the domain folder — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to read the domain folder — IDPGitOps

> Orient a human or agent in the business-domain folder.

## Naming

`domain_DOM-nnn-<slug>.md`, one context per file.

| File | Covers | Feature |
| --- | --- | --- |
| `domain_DOM-001-service-onboarding-pipeline.md` | Turning a request into reviewable scaffolding | F-001 |
| `domain_DOM-002-management-plane-arbitration.md` | Deciding which management cluster may reconcile | F-002 |
| `domain_DOM-003-platform-invariant-gates.md` | Properties of the repository that must hold | F-003 |
| `domain_DOM-004-secret-and-token-lifecycle.md` | How secret material is declared, written and aged out | F-004 |

## Reading order

Ubiquitous language, then actors, then process flow, then the numbered rules, then
invariants, then the status table, then the open questions. Read the open questions
before designing anything that depends on the section above them.

## What belongs here / what does not

These documents were reconstructed from code, so the provenance markers matter more
than usual:

| Marker | Means |
| --- | --- |
| `[D: path:line]` | Derived: that location states this, and a machine could have produced the claim |
| `I: … — basis: …` | Inferred: the leap is written out |
| OPEN: | Not recoverable from code |

A rule here is a rule **the code enforces**, and it cites the enforcement. A rule
the business follows that the code does not enforce is an OPEN: question, not a
numbered rule — that distinction is the one thing a reversed domain document can
offer that a written one cannot.

Business rules belong here. Field names, payloads and schemas belong in
[`../data/`](../data/how-to-read.md). Rationale belongs to the repository's own ADR
set, cited but never restated [D: _docs/IDP-GitOps-ADRs-v2.md:1].

## Open questions

- OPEN: A rule the business follows that the code does not enforce has no home in these documents today — it becomes an open question rather than a numbered rule. Is that the right treatment, or should such rules be listed and marked unenforced?
- OPEN: Four domain documents exist, one per feature. Are these four the real bounded contexts, or an artefact of how this pass split the code?
