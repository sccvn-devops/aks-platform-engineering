---
title: ADR-0001 — Record architecture decisions
id: ADR-0001
kind: adr
feature: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# ADR-0001 — Record architecture decisions

> Bootstrap ADR: establish that decisions are recorded as ADRs in this repository.

## Status

Accepted — 2026-09-18.

## Context

The repository already keeps a decision record: two packs of architecture decision
records, the later one superseding entries in the earlier
[D: _docs/IDP-GitOps-ADRs-v2.md:1] [D: _docs/IDP-GitOps-ADRs.md:1], cited from the
engineering guide [D: docs/architect.md:1] and from the agent instructions
[D: docs/agents/domain.md:1].

This hub was reconstructed from code and therefore needs a rule about where a *new*
decision goes, and how a decision that is visible in the code but whose reasoning
was never written down should be recorded. Both gaps are real: the architecture
document lists behaviours in force with no ADR at all.

## Decision

1. Platform-wide decisions stay in `_docs/IDP-GitOps-ADRs-v2.md`, which is
   authoritative. This hub cites them by ID and never restates them.
2. Decisions about this documentation hub are recorded here as
   `adrs_ADR-nnnn-<slug>.md`, four digits.
3. A decision visible in code whose reasoning is lost may be recorded as a
   reconstructed ADR, in the fixed format in [`how-to-read.md`](how-to-read.md),
   with its context stated as cited facts and its alternatives left OPEN:.
4. An accepted ADR is never edited; a change is a new ADR that supersedes it.

## Alternatives considered

| Option | Why not |
| --- | --- |
| Copy the platform ADRs into this hub | Two copies of one decision; they diverge, and nothing marks which is stale |
| Reconstruct ADRs for every decision found in code | Would fabricate deliberation that did not happen; the reconstructed format exists precisely to avoid that |
| Keep no hub-level ADRs | Decisions about the hub's own contracts would have no home, and this document is the proof that they need one |

## Consequences

Positive: one binding text per decision; a stable ID to cite from a route file; a
sanctioned way to record a decision whose reasoning is gone without inventing it.

Negative: two ADR locations, which a reader must know about. Mitigated by the
numbering split and by this document.

Obligation: when a decision is reached while working in this repository and nothing
records it, write the ADR before the change merges.

## Links

- [`how-to-read.md`](how-to-read.md) — format, including the reconstructed variant
- `_docs/IDP-GitOps-ADRs-v2.md` — the platform decision record
- [`../architect/architect.md`](../architect/architect.md) § Decision index, which lists behaviours in force with no ADR

## Open questions

- OPEN: Which of the behaviours listed in [`../architect/architect.md`](../architect/architect.md) § Decision index — the preferred-cluster release path, the controller tick, the rotation grace period, single-replica controllers — should be written up as reconstructed ADRs first?
- OPEN: Who accepts an ADR in this repository? No approval convention is recorded anywhere.
