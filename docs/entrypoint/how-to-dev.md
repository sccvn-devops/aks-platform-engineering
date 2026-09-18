---
title: How to develop — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to develop — IDPGitOps

> The development loop for a human or agent, from picking a task to opening a PR.

## Before you start

1. Read the route for the feature you are touching, and let it check itself:

   ```bash
   python docs/IDPGitOps-Specs/route/route.py --version v4 --id F-001
   ```

2. Confirm your work matches a task row in
   `docs/IDPGitOps-Specs/tasks/tasks_v4_<feature>.md`. These rows are an as-built
   inventory, so a change that fits no row is either new work — which needs a row —
   or a change to something the hub has not documented yet.
3. Set up once: [`../IDPGitOps-Specs/runbook/runbook_DEV_RB-001-local-setup.md`](../IDPGitOps-Specs/runbook/runbook_DEV_RB-001-local-setup.md).

**These documents were reconstructed from code.** A claim carries `[D: path:line]`
when the code states it, `I: … — basis: …` when it is an inference, and OPEN: when
nobody knows. If you find a `[D:]` claim that no longer matches its line, the code
moved: fix the document through `product-docs-flow revise`, not by editing in place
while implementing.

## Loop

1. Pick one task, in the order the file gives.
2. Move it to `wip` with `docs_flow.py task`.
3. Read what it points at: its artifact, its done-when, the story above it, the domain rules that story cites.
4. Implement, following [`../IDPGitOps-Specs/architect/architect_common.md`](../IDPGitOps-Specs/architect/architect_common.md).
5. Run the done-when command — that one, not a similar one.
6. Self-check against the review checklist and the definition of done.
7. Move the task to `done` with the command output as evidence, or to `blocked` with the reason.
8. Open a pull request naming the task ID and the command that produced the evidence.

## Standards

[`../IDPGitOps-Specs/architect/architect_common.md`](../IDPGitOps-Specs/architect/architect_common.md)
is the authority, and it records which rules have a machine behind them and which
are only habits. Two facts worth knowing before the first commit: toolchain versions
live only in `.tool-versions`, and the hook set is the local half of CI.

## Definition of done

- [ ] One task row, its done-when command run here and passing.
- [ ] Shapes match `data/schema/schemas.json`.
- [ ] No SLO numeric outside the profiles; no cluster identity outside the registry.
- [ ] Every new outbound call has an explicit timeout; no error path can carry a credential.
- [ ] `pre-commit run --all-files` passes.
- [ ] No document under `docs/IDPGitOps-Specs/` was edited as part of implementing.
