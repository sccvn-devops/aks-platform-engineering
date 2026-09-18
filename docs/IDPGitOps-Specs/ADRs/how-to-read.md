---
title: How to read the ADR folder — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to read the ADR folder — IDPGitOps

> Explain the ADR format and status lifecycle.

## Naming

`adrs_ADR-nnnn-<slug>.md`, four digits, immutable once accepted. The repository's
own decision record uses three digits with an optional revision suffix
[D: _docs/IDP-GitOps-ADRs-v2.md:1], so the two numbering schemes cannot collide.

## Status lifecycle

`Proposed` → `Accepted` → `Superseded`. A changed decision is a new ADR; the old one
stays. The repository already follows this — its v1 pack remains alongside the v2
one [D: _docs/IDP-GitOps-ADRs.md:1].

## Format

Status → Context → Decision → Alternatives considered → Consequences → Links.

For a decision **reconstructed from code**, where the reasoning was never written
down, the format is fixed and deliberately incomplete:

```
Status: accepted (reconstructed from code, <date>) — rationale not recovered
Context: <what the code does, cited. Facts, no motive.>
Decision: <the behaviour now in force, cited.>
Alternatives considered: OPEN: not recoverable — code retains no record of what was rejected.
Consequences: <what the shape now obliges, cited where visible.>
```

Inventing an alternatives table for a reconstructed ADR is worse than leaving it
open: it forecloses the discussion the ADR should start.

## When an ADR is required

When a decision constrains other work and is costly to reverse. The architecture
document lists behaviours currently in force with no ADR at all — the
preferred-cluster release path, the controller tick, the rotation grace period and
single-replica controllers. Each is a candidate for a reconstructed ADR, and each
would carry an open rationale.

## Open questions

- OPEN: The reconstructed-ADR format above is this documentation pass's own convention, assembled from what ADR practice insists on. Does the team accept it, or should a decision with lost reasoning simply not be recorded as an ADR?
- OPEN: Should reconstructed ADRs use the three-digit platform sequence, so that all platform decisions stay in one numbering, or the four-digit hub sequence as this document assumes?
