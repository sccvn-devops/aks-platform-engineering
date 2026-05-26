"""Service template rendering for service_seed (US-V4-07, US-V4-08).

Single owner of the *render* concern.  Drives Jinja2 templates under
``tools/service_seed/templates/`` plus the SLO/rollout profile data under
``tools/service_seed/profiles/`` to materialise the GitOps manifest set for a
new service request, and shells out to ``cookiecutter`` (with a pure-Python
fallback) for the per-service repository scaffold.

US-V4-08 (FR-V4-32..35) constraints:
    * Every output file is template-driven — zero YAML literals live in this
      module.  The templates mirror the rendered output directory structure
      under ``templates/`` (e.g., ``templates/workload/overlays/rollout.yaml.j2``
      renders to ``apps/<service>/workload/overlays/<env>/rollout.yaml``).
    * Jinja2 runs with ``StrictUndefined`` so a missing render-context key
      fails loudly at render time rather than producing a silently-empty
      manifest.
    * SLO thresholds and Argo Rollouts step strategies live in
      ``profiles/slo.yaml`` and ``profiles/rollout.yaml`` keyed by
      gold/silver/bronze.  No SLO numeric (success-rate, p99 latency, pause
      duration) appears as a Python literal here.

FR-V4-43: cookiecutter subprocess is bounded by ``SUBPROCESS_TIMEOUT_S``.
FR-V4-44: the fallback renderer rejects path-traversal templates *before* any
filesystem side-effect is observable.
"""

from __future__ import annotations

import json
import subprocess
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader, StrictUndefined

from .cli import _yaml_load, get_cluster, load_registry, workload_keyvault_id
from .jira_intake import ServiceRequest

# FR-V4-43: cookiecutter rendering is CPU-only on local disk; 300s is the
# upper-bound budget so a malformed template never ties up a runner.
SUBPROCESS_TIMEOUT_S = 300

# FR-V4-03: workload-cluster keys consumed by build_infra_files for the prod
# tier.  Adding a region is a single PR to gitops/clusters/registry.yaml; no
# code change should be needed here.
PROD_PRIMARY_CLUSTER = "aks-prod-we"
PROD_SECONDARY_CLUSTER = "aks-prod-ne"
PROD_WORKLOAD_KEYVAULT_WE = "kv-platform-prod-we"
PROD_WORKLOAD_KEYVAULT_NE = "kv-platform-prod-ne"

# Environments rendered into ``apps/<svc>/{infra,workload}/overlays/<env>/``.
# Order is significant: dev → staging → prod (sync-wave 0 → 1 → 2 in ArgoCD).
ENVIRONMENTS: tuple[str, ...] = ("dev", "staging", "prod")
_NONPROD_ENVIRONMENTS: frozenset[str] = frozenset({"dev", "staging"})

# Kinds in the workload base that need a per-overlay namespace patch.  The
# overlay kustomization template iterates this list once instead of repeating
# the patch block per kind.
_WORKLOAD_NAMESPACED_KINDS: tuple[str, ...] = (
    "ServiceAccount",
    "Service",
    "Ingress",
    "ExternalSecret",
)

_PACKAGE_DIR = Path(__file__).resolve().parent
_TEMPLATES_DIR = _PACKAGE_DIR / "templates"
_PROFILES_DIR = _PACKAGE_DIR / "profiles"


class TemplatePathTraversalError(RuntimeError):
    """Raised by the cookiecutter fallback renderer when a rendered template
    path escapes the destination directory (FR-V4-44).
    """


class ServiceTemplateError(RuntimeError):
    """Raised when a profile is missing a required SLO class or key."""


def _load_profile(name: str) -> dict[str, dict[str, Any]]:
    path = _PROFILES_DIR / name
    if not path.exists():
        raise ServiceTemplateError(f"profile not found: {path}")
    parsed = _yaml_load(path.read_text())
    if not isinstance(parsed, dict):
        raise ServiceTemplateError(f"profile root must be a mapping: {path}")
    return parsed


# Profiles loaded once at module import so render() does not pay disk cost per
# call.  Both files MUST declare the same three SLO classes (gold/silver/bronze);
# the check below catches drift between them at import time rather than at the
# first miss.
_SLO_PROFILE: dict[str, dict[str, Any]] = _load_profile("slo.yaml")
_ROLLOUT_PROFILE: dict[str, dict[str, Any]] = _load_profile("rollout.yaml")

_EXPECTED_SLO_CLASSES: frozenset[str] = frozenset({"gold", "silver", "bronze"})
_missing_slo = _EXPECTED_SLO_CLASSES - _SLO_PROFILE.keys()
_missing_rollout = _EXPECTED_SLO_CLASSES - _ROLLOUT_PROFILE.keys()
if _missing_slo:
    raise ServiceTemplateError(f"profiles/slo.yaml missing classes: {sorted(_missing_slo)}")
if _missing_rollout:
    raise ServiceTemplateError(f"profiles/rollout.yaml missing classes: {sorted(_missing_rollout)}")


# Jinja2 environment shared across renders.  StrictUndefined ensures a missing
# render-context key raises ``jinja2.UndefinedError`` instead of silently
# substituting an empty string into a CRD field (FR-V4-33).
_env: Environment = Environment(
    loader=FileSystemLoader(str(_TEMPLATES_DIR)),
    undefined=StrictUndefined,
    keep_trailing_newline=False,
    autoescape=False,
    trim_blocks=False,
    lstrip_blocks=False,
)


def _render_template(template_path: str, context: dict[str, Any]) -> str:
    template = _env.get_template(template_path)
    # Strip leading/trailing whitespace introduced by Jinja2 control tags so
    # the rendered output is byte-stable across template edits.  ``write_files``
    # adds a single trailing newline at write time.
    return template.render(**context).strip()


def render_cookiecutter_template(template_dir: Path, destination: Path, context: dict[str, str]) -> Path:
    try:
        subprocess.run(
            [
                "cookiecutter",
                "--no-input",
                str(template_dir),
                "--output-dir",
                str(destination),
                *[f"{key}={value}" for key, value in context.items()],
            ],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=SUBPROCESS_TIMEOUT_S,
        )
        return destination / context["service_slug"]
    except (FileNotFoundError, subprocess.CalledProcessError):
        return render_cookiecutter_fallback(template_dir, destination, context)


def _safe_join(destination: Path, relative: Path) -> Path:
    """Join ``relative`` onto ``destination`` and reject any path that escapes
    the destination directory (FR-V4-44).

    Rejects absolute paths, paths containing ``..`` components, and paths whose
    resolved location is outside ``destination``.  Resolution is symbolic
    (``Path.resolve(strict=False)``) so the check works *before* any directory
    is created on disk.
    """
    if relative.is_absolute():
        raise TemplatePathTraversalError(
            f"absolute path in template not allowed: {relative!s}"
        )
    if any(part == ".." for part in relative.parts):
        raise TemplatePathTraversalError(
            f"path traversal in template not allowed: {relative!s}"
        )
    candidate = (destination / relative).resolve(strict=False)
    dest_resolved = destination.resolve(strict=False)
    try:
        candidate.relative_to(dest_resolved)
    except ValueError as exc:
        raise TemplatePathTraversalError(
            f"rendered path escapes destination: {relative!s} -> {candidate!s}"
        ) from exc
    return candidate


def render_cookiecutter_fallback(template_dir: Path, destination: Path, context: dict[str, str]) -> Path:
    config = json.loads((template_dir / "cookiecutter.json").read_text())
    render_context = {**config, **context}
    root = destination / render_template_string("{{cookiecutter.service_slug}}", render_context)
    for source in template_dir.rglob("*"):
        if source.name == "cookiecutter.json":
            continue
        relative = source.relative_to(template_dir)
        rendered_relative = Path(
            *[render_template_string(part, render_context) for part in relative.parts]
        )
        # FR-V4-44: validate the rendered target BEFORE creating directories
        # or writing files — a path-traversal template produces zero filesystem
        # side-effect.
        target = _safe_join(destination, rendered_relative)
        if source.is_dir():
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(render_template_string(source.read_text(), render_context))
    return root


def render_template_string(content: str, context: dict[str, str]) -> str:
    rendered = content
    for key, value in context.items():
        rendered = rendered.replace(f"{{{{cookiecutter.{key}}}}}", value)
    return rendered


def write_files(base_dir: Path, files: dict[str, str]) -> None:
    for relative_path, content in files.items():
        target = base_dir / relative_path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content.rstrip() + "\n")


def _slo_data(slo_class: str) -> dict[str, Any]:
    try:
        return _SLO_PROFILE[slo_class]
    except KeyError as exc:
        raise ServiceTemplateError(f"unknown slo_class: {slo_class!r}") from exc


def _rollout_data(slo_class: str) -> dict[str, Any]:
    try:
        return _ROLLOUT_PROFILE[slo_class]
    except KeyError as exc:
        raise ServiceTemplateError(f"unknown slo_class: {slo_class!r}") from exc


def _infra_patch_targets(service: str, env: str) -> list[dict[str, Any]]:
    """Patch entries consumed by templates/infra/overlays/kustomization.yaml.j2.

    Each entry is a dict (instead of a tuple) so the template can reference
    fields by name and StrictUndefined catches mis-spelled keys.
    """
    target = f"{service}-infra-{env}"
    return [
        {"kind": "Namespace", "name": "placeholder-infra", "value": target, "field": "name", "add_env_label": True},
        {"kind": "SQLDatabase", "name": f"{service}-sql", "value": target, "field": "namespace", "add_env_label": False},
        {"kind": "CosmosAccount", "name": f"{service}-cosmos", "value": target, "field": "namespace", "add_env_label": False},
        {"kind": "ServiceBus", "name": f"{service}-sb", "value": target, "field": "namespace", "add_env_label": False},
        {
            "kind": "ConfigMap",
            "name": f"{service}-namespace-vault-binding-reference",
            "value": target,
            "field": "namespace",
            "add_env_label": False,
        },
    ]


def render(
    req: ServiceRequest,
    *,
    slo_class: str | None = None,
    registry: dict[str, Any] | None = None,
) -> dict[str, str]:
    """Render the full GitOps manifest set for *req* (FR-V4-32, FR-V4-34).

    ``slo_class`` defaults to ``req.slo_class``; callers can override it to
    re-render the same request under a different SLO class (kubeconform
    fixture generation depends on this).  ``registry`` defaults to the
    canonical cluster registry loaded by :func:`tools.service_seed.cli.load_registry`.
    """
    effective_slo = slo_class or req.slo_class
    slo = _slo_data(effective_slo)
    rollout = _rollout_data(effective_slo)
    if registry is None:
        registry = load_registry()

    prod_primary = get_cluster(PROD_PRIMARY_CLUSTER, registry=registry)
    prod_secondary = get_cluster(PROD_SECONDARY_CLUSTER, registry=registry)
    common_ctx: dict[str, Any] = {
        "service": req.service_slug,
        "slo_class": effective_slo,
        "slo": slo,
        "rollout": rollout,
        "primary_region": prod_primary.region,
        "primary_resource_group": prod_primary.resource_group,
        "secondary_region": prod_secondary.region,
        "key_vault_we_id": workload_keyvault_id(
            PROD_PRIMARY_CLUSTER, PROD_WORKLOAD_KEYVAULT_WE, registry=registry
        ),
        "key_vault_ne_id": workload_keyvault_id(
            PROD_SECONDARY_CLUSTER, PROD_WORKLOAD_KEYVAULT_NE, registry=registry
        ),
        "nonprod_envs": _NONPROD_ENVIRONMENTS,
        "workload_namespaced_kinds": _WORKLOAD_NAMESPACED_KINDS,
    }
    service = req.service_slug
    files: dict[str, str] = {}

    # ---- Infra base -------------------------------------------------------
    infra_base = [
        ("kustomization.yaml", "infra/base/kustomization.yaml.j2"),
        ("namespace.yaml", "infra/base/namespace.yaml.j2"),
        ("xrc-sql.yaml", "infra/base/xrc-sql.yaml.j2"),
        ("xrc-cosmos.yaml", "infra/base/xrc-cosmos.yaml.j2"),
        ("xrc-sb.yaml", "infra/base/xrc-sb.yaml.j2"),
        ("xrc-namespace-binding.yaml", "infra/base/xrc-namespace-binding.yaml.j2"),
        ("namespace-rollout-policy.yaml", "infra/base/namespace-rollout-policy.yaml.j2"),
    ]
    for out_name, tpl in infra_base:
        files[f"apps/{service}/infra/base/{out_name}"] = _render_template(tpl, common_ctx)

    # ---- Infra overlays ---------------------------------------------------
    for env in ENVIRONMENTS:
        ctx = {**common_ctx, "env": env, "patch_targets": _infra_patch_targets(service, env)}
        files[f"apps/{service}/infra/overlays/{env}/kustomization.yaml"] = _render_template(
            "infra/overlays/kustomization.yaml.j2", ctx
        )

    # ---- Workload base ----------------------------------------------------
    workload_base = [
        ("kustomization.yaml", "workload/base/kustomization.yaml.j2"),
        ("namespace.yaml", "workload/base/namespace.yaml.j2"),
        ("serviceaccount.yaml", "workload/base/serviceaccount.yaml.j2"),
        ("service.yaml", "workload/base/service.yaml.j2"),
        ("ingress.yaml", "workload/base/ingress.yaml.j2"),
        ("external-secrets.yaml", "workload/base/external-secrets.yaml.j2"),
        ("analysis-template.yaml", "workload/base/analysis-template.yaml.j2"),
    ]
    for out_name, tpl in workload_base:
        files[f"apps/{service}/workload/base/{out_name}"] = _render_template(tpl, common_ctx)

    # ---- Workload overlays ------------------------------------------------
    for env in ENVIRONMENTS:
        namespace = f"{service}-{env}"
        ctx = {**common_ctx, "env": env, "namespace": namespace, "image": f"acrplatformprod.azurecr.io/{service}:latest"}
        files[f"apps/{service}/workload/overlays/{env}/kustomization.yaml"] = _render_template(
            "workload/overlays/kustomization.yaml.j2", ctx
        )
        files[f"apps/{service}/workload/overlays/{env}/rollout.yaml"] = _render_template(
            "workload/overlays/rollout.yaml.j2", ctx
        )

    return files


# ---------------------------------------------------------------------------
# Legacy entry points — preserved as thin wrappers around :func:`render` so the
# v3-era callers and existing test suite continue to work without changes.
# New code SHOULD call :func:`render` directly.
# ---------------------------------------------------------------------------


def build_gitops_files(req: ServiceRequest) -> dict[str, str]:
    return render(req)


def build_infra_files(req: ServiceRequest) -> dict[str, str]:
    files = render(req)
    return {path: content for path, content in files.items() if "/infra/" in path}


def build_workload_files(req: ServiceRequest) -> dict[str, str]:
    files = render(req)
    return {path: content for path, content in files.items() if "/workload/" in path}


def infra_overlay_kustomization(service: str, env: str) -> str:
    """Legacy helper — returns the rendered overlay kustomization for *service*
    in environment *env*.  Used by callers that drove the v3-era YAML emitters
    directly.  Routes through :func:`render` to keep the template the single
    source of truth.
    """
    ctx = {
        "service": service,
        "env": env,
        "patch_targets": _infra_patch_targets(service, env),
    }
    return _render_template("infra/overlays/kustomization.yaml.j2", ctx)


def workload_base_kustomization(slo_class: str) -> str:
    ctx = {"slo": _slo_data(slo_class)}
    return _render_template("workload/base/kustomization.yaml.j2", ctx)


def workload_overlay_kustomization(service: str, slo_class: str, env: str) -> str:
    namespace = f"{service}-{env}"
    ctx = {
        "service": service,
        "env": env,
        "namespace": namespace,
        "slo": _slo_data(slo_class),
        "workload_namespaced_kinds": _WORKLOAD_NAMESPACED_KINDS,
    }
    return _render_template("workload/overlays/kustomization.yaml.j2", ctx)


def analysis_template_yaml(req: ServiceRequest) -> str:
    ctx = {"service": req.service_slug, "slo": _slo_data(req.slo_class)}
    return _render_template("workload/base/analysis-template.yaml.j2", ctx)


def rollout_yaml(req: ServiceRequest, env: str) -> str:
    namespace = f"{req.service_slug}-{env}"
    ctx = {
        "service": req.service_slug,
        "env": env,
        "namespace": namespace,
        "image": f"acrplatformprod.azurecr.io/{req.service_slug}:latest",
        "slo": _slo_data(req.slo_class),
        "rollout": _rollout_data(req.slo_class),
        "nonprod_envs": _NONPROD_ENVIRONMENTS,
    }
    return _render_template("workload/overlays/rollout.yaml.j2", ctx)
