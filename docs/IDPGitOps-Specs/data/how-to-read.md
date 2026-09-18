---
title: How to read the data folder — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to read the data folder — IDPGitOps

> Orient a human or agent in the data folder: which file is authoritative for shape, and what may never be duplicated elsewhere.

## Naming

Readable documents at the top; machine artifacts under `schema/` and `fixtures/`.
One set per feature, plus the master registry.

## Single source of truth

| Concern | Authoritative file | Everyone else may only |
| --- | --- | --- |
| Entity fields and types | `schema/schemas.json` | name the entity |
| Which entities exist and who owns them | `data-master-erd.md` | link to the row |
| A feature's read/write scope | `data-erd_v4_F-00n.md` | cite the feature |
| Wire surfaces | `schema/openapi_*.json`, `schema/asyncapi_*.json` | `$ref` into `schemas.json` |
| Test data | `fixtures/fixtures_v4_F-00n.json` | load a set by its ID |

Every `$defs` entry carries the `path:line` it was read from, which is the property
that makes this folder checkable: a shape that no longer matches its citation is a
defect a reader can find in one step.

## Contract

- One entity: one `$defs` entry, one master-ERD box, one registry row.
- One fixture record: valid against `schemas.json` — no unknown entity, no missing required field, no undeclared field.
- No secret value in any record, ever. The value field on the vault projection exists in code and is deliberately absent from every fixture.
- A feature that adds an entity extends `schemas.json`; it never forks it.

## What belongs here / what does not

Shapes, contracts and test data. Business rules belong in `../ddd/`; rationale
belongs to the repository's ADR set.

## Consistency check

```bash
python docs/IDPGitOps-Specs/route/route.py --version v4 --id F-001
python .claude/skills/product-reverse-docs/scripts/reverse.py verify \
    --root . --evidence <survey>/evidence.json --docs docs
```

The first validates fixtures against the model; the second re-opens every citation
and reports any that no longer resolve.

## Change policy

Specification, not scratch space. These files change during a spec pass and are
read-only while a route is being executed. A shape that does not fit the code is a
spec bug to report — and in a reversed hub it is usually the code that moved, which
makes it a `product-docs-flow revise` job rather than an edit here.
