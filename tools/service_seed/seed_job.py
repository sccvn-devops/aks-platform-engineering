"""Deprecation shim re-exporting the v4 service_seed module split.

US-V4-07 (FR-V4-27..31) split the 981-line ``seed_job.py`` into three
single-concern modules:

    tools/service_seed/jira_intake.py        Jira REST + ServiceRequest parse.
    tools/service_seed/service_template.py   Cookiecutter render + YAML emitters.
    tools/service_seed/gitops_pr.py          Bitbucket REST + git CLI + PR.

The orchestrator CLI lives at :func:`tools.service_seed.cli.main`.

This module is retained as a thin re-export for callers that have not yet
migrated their imports.  New code MUST import from the per-concern modules
above — the symbols here will be removed once the in-tree callers (Jenkins
pipeline scripts, existing tests) are migrated.
"""

from __future__ import annotations

from .cli import main as _orchestrator_main
from .gitops_pr import (
    HTTP_TIMEOUT_S,
    SUBPROCESS_TIMEOUT_S,
    BitbucketClient,
    authenticated_remote,
    clone_repo,
    create_service_repository,
    git,
    stage_gitops_pr,
)
from .jira_intake import (
    EXPECTED_ISSUE_TYPE,
    VALID_SLO_CLASSES,
    JiraIntakeError,
    ServiceRequest,
    adf_to_text,
    infer_service_name,
    infer_slo_class,
    jira_get_issue,
    parse_service_request,
    slugify,
)
from .service_template import (
    PROD_PRIMARY_CLUSTER,
    PROD_SECONDARY_CLUSTER,
    PROD_WORKLOAD_KEYVAULT_NE,
    PROD_WORKLOAD_KEYVAULT_WE,
    TemplatePathTraversalError,
    _safe_join,
    analysis_template_yaml,
    build_gitops_files,
    build_infra_files,
    build_workload_files,
    infra_overlay_kustomization,
    render_cookiecutter_fallback,
    render_cookiecutter_template,
    render_template_string,
    rollout_yaml,
    workload_base_kustomization,
    workload_overlay_kustomization,
    write_files,
)

__all__ = [
    "BitbucketClient",
    "EXPECTED_ISSUE_TYPE",
    "HTTP_TIMEOUT_S",
    "JiraIntakeError",
    "PROD_PRIMARY_CLUSTER",
    "PROD_SECONDARY_CLUSTER",
    "PROD_WORKLOAD_KEYVAULT_NE",
    "PROD_WORKLOAD_KEYVAULT_WE",
    "SUBPROCESS_TIMEOUT_S",
    "ServiceRequest",
    "TemplatePathTraversalError",
    "VALID_SLO_CLASSES",
    "_safe_join",
    "adf_to_text",
    "analysis_template_yaml",
    "authenticated_remote",
    "build_gitops_files",
    "build_infra_files",
    "build_workload_files",
    "clone_repo",
    "create_service_repository",
    "git",
    "infer_service_name",
    "infer_slo_class",
    "infra_overlay_kustomization",
    "jira_get_issue",
    "main",
    "parse_service_request",
    "render_cookiecutter_fallback",
    "render_cookiecutter_template",
    "render_template_string",
    "rollout_yaml",
    "slugify",
    "stage_gitops_pr",
    "workload_base_kustomization",
    "workload_overlay_kustomization",
    "write_files",
]


def main() -> None:
    """Backwards-compatible entry point — delegates to :func:`cli.main`."""
    raise SystemExit(_orchestrator_main())


if __name__ == "__main__":  # pragma: no cover
    main()
