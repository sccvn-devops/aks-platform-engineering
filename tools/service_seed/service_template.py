"""Service template rendering for service_seed (US-V4-07, FR-V4-27..31).

Single owner of the *render* concern: cookiecutter scaffold rendering for the
service repository plus the GitOps manifest emitters used by the seed pipeline.
Replaces the render-related slice of the legacy ``seed_job.py`` module.

Public surface:
    TemplatePathTraversalError      Raised by the fallback renderer on path-traversal.
    render_cookiecutter_template    Drive the ``cookiecutter`` CLI, fall back on missing binary.
    render_cookiecutter_fallback    Pure-Python fallback renderer (used by tests).
    build_gitops_files              Compose infra + workload file dictionaries.
    build_infra_files               Per-service infra manifests (Crossplane claims, etc.).
    build_workload_files            Per-service workload manifests (Argo Rollouts, ESO).

FR-V4-43: cookiecutter subprocess is bounded by ``SUBPROCESS_TIMEOUT_S``.
FR-V4-44: the fallback renderer rejects path-traversal templates *before* any
filesystem side-effect is observable.
"""

from __future__ import annotations

import json
import subprocess
from pathlib import Path

from .cli import get_cluster, load_registry, workload_keyvault_id
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


class TemplatePathTraversalError(RuntimeError):
    """Raised by the cookiecutter fallback renderer when a rendered template
    path escapes the destination directory (FR-V4-44).
    """


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


def build_gitops_files(req: ServiceRequest) -> dict[str, str]:
    return {
        **build_infra_files(req),
        **build_workload_files(req),
    }


def build_infra_files(req: ServiceRequest) -> dict[str, str]:
    service = req.service_slug
    slo = req.slo_class
    namespace_rollout_composition = f"namespacerolloutpolicy-{slo}.platform.cityos.io"

    # FR-V4-03: source per-cluster identity from the committed registry, never
    # from hardcoded literals.  Adding a region = single PR to
    # gitops/clusters/registry.yaml.
    registry = load_registry()
    prod_primary = get_cluster(PROD_PRIMARY_CLUSTER, registry=registry)
    prod_secondary = get_cluster(PROD_SECONDARY_CLUSTER, registry=registry)
    primary_region = prod_primary.region
    primary_rg = prod_primary.resource_group
    secondary_region = prod_secondary.region
    kv_we_id = workload_keyvault_id(PROD_PRIMARY_CLUSTER, PROD_WORKLOAD_KEYVAULT_WE, registry=registry)
    kv_ne_id = workload_keyvault_id(PROD_SECONDARY_CLUSTER, PROD_WORKLOAD_KEYVAULT_NE, registry=registry)

    files = {
        f"apps/{service}/infra/base/kustomization.yaml": "\n".join(
            [
                "apiVersion: kustomize.config.k8s.io/v1beta1",
                "kind: Kustomization",
                "resources:",
                "  - namespace.yaml",
                "  - xrc-sql.yaml",
                "  - xrc-cosmos.yaml",
                "  - xrc-sb.yaml",
                "  - namespace-rollout-policy.yaml",
            ]
        ),
        f"apps/{service}/infra/base/namespace.yaml": "\n".join(
            [
                "apiVersion: v1",
                "kind: Namespace",
                "metadata:",
                "  name: placeholder-infra",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "0"',
                "  labels:",
                f"    app.kubernetes.io/part-of: {service}",
                "    platform.ste.io/tier: infra",
            ]
        ),
        f"apps/{service}/infra/base/xrc-sql.yaml": "\n".join(
            [
                "apiVersion: platform.cityos.io/v1alpha1",
                "kind: SQLDatabase",
                "metadata:",
                f"  name: {service}-sql",
                "  namespace: placeholder-infra",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "1"',
                "spec:",
                "  parameters:",
                f"    region: {primary_region}",
                f"    resourceGroupName: {primary_rg}",
                f"    serverName: sql-{service}-prod",
                f"    sloClass: {slo}",
                f"    secretName: {service}-sql-conn",
                f"    keyVaultWeId: {kv_we_id}",
                f"    keyVaultNeId: {kv_ne_id}",
            ]
        ),
        f"apps/{service}/infra/base/xrc-cosmos.yaml": "\n".join(
            [
                "apiVersion: platform.cityos.io/v1alpha1",
                "kind: CosmosAccount",
                "metadata:",
                f"  name: {service}-cosmos",
                "  namespace: placeholder-infra",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "1"',
                "spec:",
                "  parameters:",
                f"    region: {primary_region}",
                f"    resourceGroupName: {primary_rg}",
                f"    primaryLocation: {primary_region}",
                f"    secondaryLocation: {secondary_region}",
                f"    sloClass: {slo}",
                f"    secretName: {service}-cosmos-conn",
                f"    keyVaultWeId: {kv_we_id}",
                f"    keyVaultNeId: {kv_ne_id}",
            ]
        ),
        f"apps/{service}/infra/base/xrc-sb.yaml": "\n".join(
            [
                "apiVersion: platform.cityos.io/v1alpha1",
                "kind: ServiceBus",
                "metadata:",
                f"  name: {service}-sb",
                "  namespace: placeholder-infra",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "1"',
                "spec:",
                "  parameters:",
                f"    region: {primary_region}",
                f"    location: {primary_region}",
                f"    resourceGroupName: {primary_rg}",
                f"    sloClass: {slo}",
                f"    secretName: {service}-sb-conn",
                f"    keyVaultWeId: {kv_we_id}",
                f"    keyVaultNeId: {kv_ne_id}",
            ]
        ),
        f"apps/{service}/infra/base/xrc-namespace-binding.yaml": "\n".join(
            [
                "# NamespaceVaultBinding claims are rendered automatically by namespace-vault-bindings-set",
                "# from workload overlays. This stub keeps the expected scaffold file in the service repo",
                "# without creating duplicate Crossplane claims after the GitOps PR is merged.",
                "apiVersion: v1",
                "kind: ConfigMap",
                "metadata:",
                f"  name: {service}-namespace-vault-binding-reference",
                "  namespace: placeholder-infra",
                "data:",
                f"  serviceName: {service}",
                f"  namespacePrefix: {service}",
            ]
        ),
        f"apps/{service}/infra/base/namespace-rollout-policy.yaml": "\n".join(
            [
                "apiVersion: platform.cityos.io/v1alpha1",
                "kind: NamespaceRolloutPolicy",
                "metadata:",
                f"  name: {service}-rollout-policy",
                f"  namespace: {service}",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "5"',
                "spec:",
                "  compositionRef:",
                f"    name: {namespace_rollout_composition}",
                "  parameters:",
                f"    namespace: {service}-prod",
                f"    sloClass: {slo}",
                "    prometheusAddress: http://prometheus-operated.monitoring.svc.cluster.local:9090",
            ]
        ),
    }

    for env in ("dev", "staging", "prod"):
        files[f"apps/{service}/infra/overlays/{env}/kustomization.yaml"] = infra_overlay_kustomization(service, env)
    return files


def infra_overlay_kustomization(service: str, env: str) -> str:
    patch_targets = [
        ("Namespace", "placeholder-infra", f"{service}-infra-{env}", None),
        ("SQLDatabase", f"{service}-sql", f"{service}-infra-{env}", "namespace"),
        ("CosmosAccount", f"{service}-cosmos", f"{service}-infra-{env}", "namespace"),
        ("ServiceBus", f"{service}-sb", f"{service}-infra-{env}", "namespace"),
        ("ConfigMap", f"{service}-namespace-vault-binding-reference", f"{service}-infra-{env}", "namespace"),
    ]
    patches = [
        "apiVersion: kustomize.config.k8s.io/v1beta1",
        "kind: Kustomization",
        "resources:",
        "  - ../../base",
        "patches:",
    ]
    patches.extend(
        [
            "  - target:",
            f"      kind: {kind}",
            f"      name: {name}",
            "    patch: |-",
            "      - op: replace",
            f"        path: /metadata/{'name' if namespace_field is None else 'namespace'}",
            f"        value: {target}",
        ]
        + (
            []
            if namespace_field is not None or kind != "Namespace"
            else [
                "      - op: add",
                "        path: /metadata/labels/environment",
                f"        value: {env}",
            ]
        )
        for kind, name, target, namespace_field in patch_targets
    )
    flat = [patches[0], patches[1], patches[2], patches[3], patches[4]]
    for block in patches[5:]:
        if isinstance(block, list):
            flat.extend(block)
        else:
            flat.append(block)
    flat.extend(
        [
            "  - target:",
            "      kind: NamespaceRolloutPolicy",
            f"      name: {service}-rollout-policy",
            "    patch: |-",
            "      - op: replace",
            f"        path: /metadata/namespace",
            f"        value: {service}-infra-{env}",
            "      - op: replace",
            "        path: /spec/parameters/namespace",
            f"        value: {service}-{env}",
        ]
    )
    return "\n".join(flat)


def build_workload_files(req: ServiceRequest) -> dict[str, str]:
    service = req.service_slug
    files = {
        f"apps/{service}/workload/base/kustomization.yaml": workload_base_kustomization(req.slo_class),
        f"apps/{service}/workload/base/namespace.yaml": "\n".join(
            [
                "apiVersion: v1",
                "kind: Namespace",
                "metadata:",
                "  name: placeholder-workload",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "-1"',
                "  labels:",
                f"    app.kubernetes.io/part-of: {service}",
                "    platform.ste.io/tier: workload",
            ]
        ),
        f"apps/{service}/workload/base/serviceaccount.yaml": "\n".join(
            [
                "apiVersion: v1",
                "kind: ServiceAccount",
                "metadata:",
                f"  name: {service}",
                "  namespace: placeholder-workload",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "0"',
                "  labels:",
                f"    app.kubernetes.io/part-of: {service}",
            ]
        ),
        f"apps/{service}/workload/base/service.yaml": "\n".join(
            [
                "apiVersion: v1",
                "kind: Service",
                "metadata:",
                f"  name: {service}",
                "  namespace: placeholder-workload",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "2"',
                "spec:",
                "  selector:",
                f"    app.kubernetes.io/name: {service}",
                "  ports:",
                "    - name: http",
                "      port: 80",
                "      targetPort: http",
            ]
        ),
        f"apps/{service}/workload/base/ingress.yaml": "\n".join(
            [
                "apiVersion: networking.k8s.io/v1",
                "kind: Ingress",
                "metadata:",
                f"  name: {service}",
                "  namespace: placeholder-workload",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "3"',
                "spec:",
                "  ingressClassName: nginx",
                "  rules:",
                "    - host: placeholder.example.internal",
                "      http:",
                "        paths:",
                "          - path: /",
                "            pathType: Prefix",
                "            backend:",
                "              service:",
                f"                name: {service}",
                "                port:",
                "                  number: 80",
            ]
        ),
        f"apps/{service}/workload/base/external-secrets.yaml": "\n".join(
            [
                "apiVersion: external-secrets.io/v1beta1",
                "kind: ExternalSecret",
                "metadata:",
                f"  name: {service}-app-secrets",
                "  namespace: placeholder-workload",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "1"',
                "spec:",
                "  refreshInterval: 1h",
                "  secretStoreRef:",
                "    kind: SecretStore",
                "    name: azure-keyvault",
                "  target:",
                f"    name: {service}-app-secrets",
                "  data:",
                "    - secretKey: DATABASE_URL",
                "      remoteRef:",
                f"        key: {service}-sql-conn",
                "    - secretKey: COSMOS_CONNECTION_STRING",
                "      remoteRef:",
                f"        key: {service}-cosmos-conn",
                "    - secretKey: SERVICEBUS_CONNECTION_STRING",
                "      remoteRef:",
                f"        key: {service}-sb-conn",
            ]
        ),
        f"apps/{service}/workload/base/analysis-template.yaml": analysis_template_yaml(req),
    }

    for env in ("dev", "staging", "prod"):
        files[f"apps/{service}/workload/overlays/{env}/kustomization.yaml"] = workload_overlay_kustomization(service, req.slo_class, env)
        files[f"apps/{service}/workload/overlays/{env}/rollout.yaml"] = rollout_yaml(req, env)
    return files


def workload_base_kustomization(slo_class: str) -> str:
    resources = [
        "apiVersion: kustomize.config.k8s.io/v1beta1",
        "kind: Kustomization",
        "resources:",
        "  - namespace.yaml",
        "  - serviceaccount.yaml",
        "  - service.yaml",
        "  - ingress.yaml",
        "  - external-secrets.yaml",
    ]
    if slo_class in {"gold", "silver"}:
        resources.append("  - analysis-template.yaml")
    return "\n".join(resources)


def analysis_template_yaml(req: ServiceRequest) -> str:
    service = req.service_slug
    if req.slo_class == "gold":
        metrics = [
            "  metrics:",
            "    - name: success-rate",
            "      interval: 60s",
            '      successCondition: "result[0] >= 0.99"',
            "      provider:",
            "        prometheus:",
            "          address: http://prometheus-operated.monitoring.svc.cluster.local:9090",
            f'          query: sum(rate(http_requests_total{{namespace="{service}-prod",status=~"2.."}}[5m])) / sum(rate(http_requests_total{{namespace="{service}-prod"}}[5m]))',
            "    - name: p99-latency-ms",
            "      interval: 60s",
            '      successCondition: "result[0] <= 500"',
            "      provider:",
            "        prometheus:",
            "          address: http://prometheus-operated.monitoring.svc.cluster.local:9090",
            f'          query: histogram_quantile(0.99, sum(rate(http_request_duration_milliseconds_bucket{{namespace="{service}-prod"}}[5m])) by (le)) * 1000',
        ]
    elif req.slo_class == "silver":
        metrics = [
            "  metrics:",
            "    - name: success-rate",
            "      interval: 60s",
            '      successCondition: "result[0] >= 0.99"',
            "      provider:",
            "        prometheus:",
            "          address: http://prometheus-operated.monitoring.svc.cluster.local:9090",
            f'          query: sum(rate(http_requests_total{{namespace="{service}-prod",status=~"2.."}}[5m])) / sum(rate(http_requests_total{{namespace="{service}-prod"}}[5m]))',
        ]
    else:
        return "\n".join(
            [
                "# Bronze services use direct cutover and do not reference an AnalysisTemplate.",
                "# This file is intentionally left out of the overlay resource graph.",
            ]
        )

    return "\n".join(
        [
            "apiVersion: argoproj.io/v1alpha1",
            "kind: AnalysisTemplate",
            "metadata:",
            f"  name: {service}-analysis",
            "  namespace: placeholder-workload",
            "spec:",
            *metrics,
        ]
    )


def workload_overlay_kustomization(service: str, slo_class: str, env: str) -> str:
    namespace = f"{service}-{env}"
    lines = [
        "apiVersion: kustomize.config.k8s.io/v1beta1",
        "kind: Kustomization",
        "resources:",
        "  - ../../base",
        "  - rollout.yaml",
        "patches:",
        "  - target:",
        "      kind: Namespace",
        "      name: placeholder-workload",
        "    patch: |-",
        "      - op: replace",
        "        path: /metadata/name",
        f"        value: {namespace}",
        "      - op: add",
        "        path: /metadata/labels/environment",
        f"        value: {env}",
    ]
    for kind in ("ServiceAccount", "Service", "Ingress", "ExternalSecret"):
        lines.extend(
            [
                "  - target:",
                f"      kind: {kind}",
                "    patch: |-",
                "      - op: replace",
                "        path: /metadata/namespace",
                f"        value: {namespace}",
            ]
        )
    if slo_class in {"gold", "silver"}:
        lines.extend(
            [
                "  - target:",
                "      kind: AnalysisTemplate",
                "    patch: |-",
                "      - op: replace",
                "        path: /metadata/namespace",
                f"        value: {namespace}",
            ]
        )
    lines.extend(
        [
            "  - target:",
            "      kind: Ingress",
            "    patch: |-",
            "      - op: replace",
            "        path: /spec/rules/0/host",
            f"        value: {namespace}.apps.internal",
        ]
    )
    return "\n".join(lines)


def rollout_yaml(req: ServiceRequest, env: str) -> str:
    service = req.service_slug
    namespace = f"{service}-{env}"
    image = f"acrplatformprod.azurecr.io/{service}:latest"
    if env in {"dev", "staging"}:
        return "\n".join(
            [
                "apiVersion: apps/v1",
                "kind: Deployment",
                "metadata:",
                f"  name: {service}",
                f"  namespace: {namespace}",
                "  annotations:",
                '    argocd.argoproj.io/sync-wave: "4"',
                "spec:",
                "  replicas: 2",
                "  selector:",
                "    matchLabels:",
                f"      app.kubernetes.io/name: {service}",
                "  template:",
                "    metadata:",
                "      labels:",
                f"        app.kubernetes.io/name: {service}",
                "    spec:",
                "      serviceAccountName: azure-keyvault-reader",
                "      containers:",
                f"        - name: {service}",
                f"          image: {image}",
                "          ports:",
                "            - name: http",
                "              containerPort: 8080",
            ]
        )

    steps = {
        "gold": ["setWeight: 5", "pause: {duration: 5m}", "setWeight: 25", "pause: {duration: 5m}", "setWeight: 50", "pause: {duration: 5m}", "setWeight: 100"],
        "silver": ["setWeight: 25", "pause: {duration: 5m}", "setWeight: 100"],
        "bronze": ["setWeight: 100"],
    }[req.slo_class]
    lines = [
        "apiVersion: argoproj.io/v1alpha1",
        "kind: Rollout",
        "metadata:",
        f"  name: {service}",
        f"  namespace: {namespace}",
        "  annotations:",
        '    argocd.argoproj.io/sync-wave: "4"',
        "spec:",
        "  replicas: 3",
        "  selector:",
        "    matchLabels:",
        f"      app.kubernetes.io/name: {service}",
        "  workloadRef:",
        "    apiVersion: apps/v1",
        "    kind: Deployment",
        f"    name: {service}-template",
        "  strategy:",
        "    canary:",
    ]
    if req.slo_class in {"gold", "silver"}:
        lines.extend(
            [
                "      analysis:",
                "        templates:",
                f"          - templateName: {service}-analysis",
            ]
        )
    lines.append("      steps:")
    for step in steps:
        lines.append(f"        - {step}")
    lines.extend(
        [
            "---",
            "apiVersion: apps/v1",
            "kind: Deployment",
            "metadata:",
            f"  name: {service}-template",
            f"  namespace: {namespace}",
            "spec:",
            "  replicas: 3",
            "  selector:",
            "    matchLabels:",
            f"      app.kubernetes.io/name: {service}",
            "  template:",
            "    metadata:",
            "      labels:",
            f"        app.kubernetes.io/name: {service}",
            "    spec:",
            "      serviceAccountName: azure-keyvault-reader",
            "      containers:",
            f"        - name: {service}",
            f"          image: {image}",
            "          ports:",
            "            - name: http",
            "              containerPort: 8080",
        ]
    )
    return "\n".join(lines)
