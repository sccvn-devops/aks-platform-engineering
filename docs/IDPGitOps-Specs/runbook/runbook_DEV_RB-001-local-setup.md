---
title: RB-001 DEV — Local setup
id: RB-001
kind: runbook
feature: F-001
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# RB-001 DEV — Local setup

> Get a working local environment from a clean checkout.

Every command below is marked **run here** or `I:` **unrun here**. Nothing in this
runbook provisions anything in Azure.

## Preconditions

| Requirement | Version | Evidence |
| --- | --- | --- |
| A toolchain manager (`mise` or `asdf`) | — | [D: .tool-versions:5] |
| Terraform | 1.5.7 | [D: .tool-versions:16] |
| Python | ≥ 3.10 | [D: tools/service_seed/pyproject.toml:16] |
| Go | 1.24.0 | [D: tools/mgmt-plane-lock/go.mod:3] |
| `pre-commit` | — | [D: .pre-commit-config.yaml:1] |
| Node and yarn, for the portal only | — | [D: backstage/package.json:1] |

No credential is needed for anything here.

## Steps

1. Install the pinned toolchain. I: unrun here.

   ```bash
   mise install      # or: asdf install
   ```

2. Install the hook set. I: unrun here.

   ```bash
   pre-commit install
   ```

3. Install the onboarding package with its test extra. **Run here, succeeded.**

   ```bash
   python -m venv .venv && source .venv/bin/activate
   pip install -e "tools/service_seed[test]"
   ```

4. Run the Python suite from the repository root — the tests import
   `tools.service_seed.*`, so the root must be the working directory.
   **Run here: 94 passed in 1.01s.**

   ```bash
   python -m pytest tools/service_seed/tests -q
   ```

5. Run the Go suite. **Run here: every package `ok`, no failures.**

   ```bash
   cd tools/mgmt-plane-lock && go test ./... && cd -
   ```

6. Run the repository validators. I: unrun here.

   ```bash
   python3 scripts/validate-cluster-registry.py
   python3 scripts/validate-tool-version-singleton.py
   python3 scripts/validate-action-pins.py
   bash scripts/validate-mgmt-plane-lock-runners.sh
   ```

7. Check the Terraform tree without a backend. I: unrun here.

   ```bash
   terraform -chdir=terraform init -backend=false
   terraform -chdir=terraform fmt -check -recursive
   terraform -chdir=terraform validate
   ```

8. Render a scaffold locally; nothing external is contacted
   [D: tools/service_seed/cli.py:315]. I: unrun here.

   ```bash
   service-seed generate-gitops --service-name "Orders" --slo-class gold --output-dir /tmp/seed-demo
   find /tmp/seed-demo -type f | sort
   ```

9. Verify this hub. **Run here** for each feature.

   ```bash
   python docs/IDPGitOps-Specs/route/route.py --version v4 --id F-001
   ```

## Verification

| Step | Proof | Status here |
| --- | --- | --- |
| 4 | `94 passed` | observed |
| 5 | `ok` for each package, no `FAIL` | observed |
| 6 | Each validator exits 0 | not observed |
| 7 | `fmt -check` silent, `validate` reports success | not observed |
| 8 | The find lists infra base, three infra overlays, workload base and three workload overlays under `apps/orders/` | not observed |
| 9 | "all N referenced documents present" and a fixture line with no mismatch | observed |

## Rollback / cleanup

```bash
deactivate 2>/dev/null; rm -rf .venv
pre-commit uninstall
rm -rf /tmp/seed-demo terraform/.terraform
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
```

Confirm with `git status --short`: only intended changes should remain.

## Common failures

| Symptom | Cause | Fix |
| --- | --- | --- |
| `No module named pytest` | The test extra is not installed in the active environment — observed here before step 3 | Re-run step 3 inside the virtualenv |
| `ModuleNotFoundError: tools.service_seed` | Step 4 run from inside the package directory | Run from the repository root |
| `ImportError: Start directory is not importable` from `unittest discover` | The test directory is not a package — observed here | Use pytest, as in step 4 |
| Registry loads without PyYAML | Not a failure: the loader falls back to an embedded parser [D: tools/service_seed/cli.py:57] | None |
| `cookiecutter: command not found` | The render extra is absent | Install `[render]`, or rely on the pure-Python fallback [D: tools/service_seed/service_template.py:146] |
| `route.py` reports `MISSING` | A document moved | Fix the path in the feature's rule stub and regenerate |
| `route.py` reports `MISMATCH` | A fixture and the model disagree | Fix one before anything else |

## Open questions

- OPEN: Steps 1, 2, 6, 7 and 8 have not been executed here; the claim that they succeed rests on the files, not on a run.
- OPEN: No documented way exists to run the platform locally — there is no compose file or local cluster definition in the tree.
