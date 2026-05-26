# service_seed

Seed service repositories and GitOps scaffolding from Jira intake requests.

## Architecture (US-V4-07 / FR-V4-27..31)

The package is split by concern:

| Module | Concern |
|---|---|
| `jira_intake.py` | Jira REST fetch + `ServiceRequest` parse + validation |
| `service_template.py` | Cookiecutter render + GitOps YAML emitters |
| `gitops_pr.py` | Bitbucket REST + git CLI + PR creation |
| `cli.py` | Cluster registry loader + orchestrator CLI |
| `seed_job.py` | Deprecation shim — re-exports for legacy callers |

## Install

From the repository root:

```bash
pip install -e tools/service_seed             # core
pip install -e tools/service_seed'[render]'   # core + cookiecutter
pip install -e tools/service_seed'[test]'     # adds pytest + pytest-cov
```

This exposes two console scripts:

* `service-seed` — orchestrator with subcommands `generate-gitops`,
  `seed-from-jira`, `registry-show`, `registry-paths`.
* `service-seed-registry` — registry inspector (`show` / `paths`).

## Usage

```bash
service-seed generate-gitops \
    --service-name "Payments API" \
    --slo-class gold \
    --output-dir ./out

service-seed seed-from-jira \
    --jira-base-url https://acme.atlassian.net \
    --jira-issue-key IDP-24 \
    --jira-issue-type "IDP Service Request" \
    --bitbucket-workspace acme \
    --cookiecutter-template ./cookiecutter-service \
    --platform-gitops-repo-url https://bitbucket.org/acme/platform-gitops.git
```

`ServiceRequest` is a frozen dataclass with `__post_init__` validation; a
malformed Jira payload raises `JiraIntakeError` (with `.field` pointing at the
offending input) and the CLI exits non-zero with a clear message.

## Testing

```bash
python3 -m unittest discover tools/service_seed/tests -v
```

The test suite runs without pytest — install the `[test]` extra to also obtain
`pytest-cov`.
