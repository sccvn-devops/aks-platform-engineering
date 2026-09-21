---
title: Tasks Index — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Tasks Index — IDPGitOps

> Index of task files with status roll-up. Not a substitute for the tracker.

## Task files

| Version | Feature | File | Tasks | Blocked by |
| --- | --- | --- | --- | --- |
| v4 | F-001 Service onboarding pipeline | [tasks_v4_F-001.md](tasks_v4_F-001.md) | 11 | — |
| v4 | F-002 Management plane arbitration | [tasks_v4_F-002.md](tasks_v4_F-002.md) | 11 | — |
| v4 | F-003 Platform invariant gates | [tasks_v4_F-003.md](tasks_v4_F-003.md) | 11 | — |
| v4 | F-004 Secret and token lifecycle | [tasks_v4_F-004.md](tasks_v4_F-004.md) | 10 | — |

Roll-up is printed, not transcribed:

```bash
for f in F-001 F-002 F-003 F-004; do
  python docs/IDPGitOps-Specs/route/route.py --version v4 --id $f | tail -3
done
```

## Conventions

- **ID format** `F-nnn-T<k>`, minted in the feature's own file.
- **Done-when is a command**, and in these files it is the command that would prove
  work which already exists.
- **Status vocabulary** is [`../status-model.md`](../status-model.md)'s, moved by
  `docs_flow.py task` one legal transition at a time.

These task files are an **as-built inventory**. The repository already contains an
implementation of every row, so the first transition is normally `todo → wip` and
the evidence for `done` is the done-when command run here.
