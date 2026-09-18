---
title: How to test — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# How to test — IDPGitOps

> How to run and extend tests locally and in CI.

## Running tests

From the repository root:

```bash
# Python — onboarding (94 cases; run here, all passed)
python -m pytest tools/service_seed/tests -q

# Go — platform controllers (run here, every package ok)
cd tools/mgmt-plane-lock && go test ./...

# Python — the two tested validators
python -m pytest scripts/tests -q

# Terraform module guards
terraform -chdir=terraform/modules/workload_identity test

# Portal
cd backstage && npm test

# This hub's own contracts
python docs/IDPGitOps-Specs/route/route.py --version v4 --id F-001

# Everything the hook set runs
pre-commit run --all-files
```

## Adding a test

| Kind | Where | Naming |
| --- | --- | --- |
| Python unit | `tools/service_seed/tests/` or `scripts/tests/` | `test_<module>.py`, stdlib `unittest` |
| Go unit | beside the package | `<file>_test.go` |
| Infrastructure | `terraform/modules/<module>/tests/` | `<module>.tftest.hcl` |

Follow what the suites already do: fake at the narrow interface, use temporary
directories, assert the message on a negative case, and name the domain rule the
test proves so the roll-up can cite it.

## Interpreting failures

| Failure | First thing to check |
| --- | --- |
| `No module named pytest` | The `[test]` extra is not installed in the active environment |
| `ModuleNotFoundError: tools.service_seed` | Not running from the repository root |
| A render test fails on an undefined variable | Strict undefined is doing its job; the context is missing a key |
| A profile test fails at import | The two tier profiles disagree on which classes exist |
| A lease test hangs | The fake clock was not advanced |
| A scaling test scales nothing | The status fixture holds a value outside `active`/`standby` |
| `route.py` reports `MISMATCH` | A fixture and the model disagree |
| A citation in a hub document no longer resolves | The code moved; reconcile with `product-docs-flow revise` |

## Pointer

Levels, absent levels, gates and the coverage gap:
[`../IDPGitOps-Specs/tests/testing_strategy.md`](../IDPGitOps-Specs/tests/testing_strategy.md).
Per-feature cases live in `tests/test_v4_F-00n.md`, and each one ends with the holes
in that feature's suite.
