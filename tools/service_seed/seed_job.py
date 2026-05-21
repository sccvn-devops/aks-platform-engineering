from __future__ import annotations

import argparse
import base64
import json
import os
import re
import shutil
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from string import Template
from typing import Any
from urllib import error, parse, request


VALID_SLO_CLASSES = {"bronze", "silver", "gold"}


@dataclass(frozen=True)
class ServiceRequest:
    issue_key: str
    issue_type: str
    service_name: str
    service_slug: str
    slo_class: str
    summary: str
    description: str


class BitbucketClient:
    def __init__(self, base_url: str, workspace: str, username: str, token: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.workspace = workspace
        self.username = username
        self.token = token

    def create_repository(self, slug: str, project_key: str | None = None) -> dict[str, Any]:
        payload: dict[str, Any] = {"scm": "git", "is_private": True}
        if project_key:
            payload["project"] = {"key": project_key}
        return self._request(
            "POST",
            f"/2.0/repositories/{self.workspace}/{slug}",
            payload,
            treat_conflict_as_success=True,
        )

    def create_pull_request(
        self,
        repo_slug: str,
        title: str,
        description: str,
        source_branch: str,
        destination_branch: str = "main",
    ) -> dict[str, Any]:
        payload = {
            "title": title,
            "description": description,
            "source": {"branch": {"name": source_branch}},
            "destination": {"branch": {"name": destination_branch}},
            "close_source_branch": True,
        }
        return self._request("POST", f"/2.0/repositories/{self.workspace}/{repo_slug}/pullrequests", payload)

    def _request(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        *,
        treat_conflict_as_success: bool = False,
    ) -> dict[str, Any]:
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        req = request.Request(f"{self.base_url}{path}", data=body, method=method)
        req.add_header("Accept", "application/json")
        if body is not None:
            req.add_header("Content-Type", "application/json")
        basic = base64.b64encode(f"{self.username}:{self.token}".encode("utf-8")).decode("ascii")
        req.add_header("Authorization", f"Basic {basic}")

        try:
            with request.urlopen(req) as resp:
                raw = resp.read().decode("utf-8")
                return {} if not raw else json.loads(raw)
        except error.HTTPError as exc:
            if treat_conflict_as_success and exc.code == 400:
                return {}
            detail = exc.read().decode("utf-8", "ignore")
            raise RuntimeError(f"bitbucket api {method} {path} failed: {exc.code} {detail}") from exc


def slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")
    if not slug:
        raise ValueError("unable to derive service slug from empty value")
    return slug


def adf_to_text(node: Any) -> str:
    if isinstance(node, str):
        return node
    if isinstance(node, list):
        return "\n".join(filter(None, (adf_to_text(item) for item in node)))
    if not isinstance(node, dict):
        return ""
    text = node.get("text", "")
    child_text = adf_to_text(node.get("content", []))
    parts = [part for part in [text, child_text] if part]
    if node.get("type") in {"paragraph", "heading", "bulletList", "orderedList", "listItem"}:
        return "\n".join(parts)
    return " ".join(parts).strip()


def parse_service_request(
    issue: dict[str, Any],
    *,
    issue_key_override: str | None = None,
    issue_type_override: str | None = None,
    service_name_override: str | None = None,
    slo_class_override: str | None = None,
) -> ServiceRequest:
    fields = issue.get("fields", {})
    issue_key = issue_key_override or issue.get("key") or ""
    issue_type = issue_type_override or fields.get("issuetype", {}).get("name") or ""
    summary = fields.get("summary") or issue_key
    description = adf_to_text(fields.get("description", ""))

    service_name = service_name_override or infer_service_name(fields, summary, description)
    service_slug = slugify(service_name)
    slo_class = (slo_class_override or infer_slo_class(fields, summary, description)).lower()

    if issue_type != "IDP Service Request":
        raise ValueError(f"unsupported Jira issue type {issue_type!r}; expected 'IDP Service Request'")
    if slo_class not in VALID_SLO_CLASSES:
        raise ValueError(f"unsupported slo class {slo_class!r}; expected one of {sorted(VALID_SLO_CLASSES)}")

    return ServiceRequest(
        issue_key=issue_key,
        issue_type=issue_type,
        service_name=service_name,
        service_slug=service_slug,
        slo_class=slo_class,
        summary=summary,
        description=description,
    )


def infer_service_name(fields: dict[str, Any], summary: str, description: str) -> str:
    explicit_candidates = [
        fields.get("serviceName"),
        fields.get("service_name"),
        fields.get("customfield_service_name"),
        fields.get("customfield_10000"),
    ]
    for candidate in explicit_candidates:
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip()

    for label in fields.get("labels", []):
        if isinstance(label, str) and label.startswith("service:"):
            return label.split(":", 1)[1].strip()

    for text in (summary, description):
        match = re.search(r"(?:service(?:\s+name)?|app(?:lication)?)\s*[:=-]\s*([A-Za-z0-9 _-]+)", text, re.IGNORECASE)
        if match:
            return match.group(1).strip()

    return summary.strip()


def infer_slo_class(fields: dict[str, Any], summary: str, description: str) -> str:
    explicit_candidates = [
        fields.get("sloClass"),
        fields.get("slo_class"),
        fields.get("customfield_slo_class"),
        fields.get("customfield_10001"),
    ]
    for candidate in explicit_candidates:
        if isinstance(candidate, dict):
            candidate = candidate.get("value")
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip().lower()

    for label in fields.get("labels", []):
        if isinstance(label, str) and label.startswith("slo:"):
            return label.split(":", 1)[1].strip().lower()

    for text in (summary, description):
        match = re.search(r"slo(?:\s+class)?\s*[:=-]\s*(bronze|silver|gold)", text, re.IGNORECASE)
        if match:
            return match.group(1).lower()

    return "silver"


def jira_get_issue(base_url: str, issue_key: str, email: str, token: str) -> dict[str, Any]:
    req = request.Request(f"{base_url.rstrip('/')}/rest/api/3/issue/{parse.quote(issue_key)}")
    req.add_header("Accept", "application/json")
    if email:
        basic = base64.b64encode(f"{email}:{token}".encode("utf-8")).decode("ascii")
        req.add_header("Authorization", f"Basic {basic}")
    else:
        req.add_header("Authorization", f"Bearer {token}")

    with request.urlopen(req) as resp:
        return json.loads(resp.read().decode("utf-8"))


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
        )
        return destination / context["service_slug"]
    except (FileNotFoundError, subprocess.CalledProcessError):
        return render_cookiecutter_fallback(template_dir, destination, context)


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
        target = destination / rendered_relative
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
    namespace_rollout_composition = f"namespaceroutoutpolicy-{slo}.platform.cityos.io"
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
                "    region: westeurope",
                "    resourceGroupName: rg-platform-prod",
                f"    serverName: sql-{service}-prod",
                f"    sloClass: {slo}",
                f"    secretName: {service}-sql-conn",
                "    keyVaultWeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-we",
                "    keyVaultNeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-ne",
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
                "    region: westeurope",
                "    resourceGroupName: rg-platform-prod",
                "    primaryLocation: westeurope",
                "    secondaryLocation: northeurope",
                f"    sloClass: {slo}",
                f"    secretName: {service}-cosmos-conn",
                "    keyVaultWeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-we",
                "    keyVaultNeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-ne",
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
                "    region: westeurope",
                "    location: westeurope",
                "    resourceGroupName: rg-platform-prod",
                f"    sloClass: {slo}",
                f"    secretName: {service}-sb-conn",
                "    keyVaultWeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-we",
                "    keyVaultNeId: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-platform-prod/providers/Microsoft.KeyVault/vaults/kv-platform-prod-ne",
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


def clone_repo(remote: str, destination: Path, branch: str = "main") -> None:
    subprocess.run(
        ["git", "clone", "--depth", "1", "--branch", branch, remote, str(destination)],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def git(*args: str, cwd: Path) -> None:
    subprocess.run(["git", *args], cwd=str(cwd), check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)


def authenticated_remote(url: str, username: str, token: str) -> str:
    return re.sub(r"^https://", f"https://{parse.quote(username)}:{parse.quote(token)}@", url, count=1)


def stage_gitops_pr(
    *,
    repo_url: str,
    repo_username: str,
    repo_token: str,
    branch_name: str,
    pr_title: str,
    pr_description: str,
    path_root: str,
    files: dict[str, str],
    service_slug: str,
    bitbucket: BitbucketClient,
) -> None:
    with tempfile.TemporaryDirectory(prefix="gitops-seed-") as tmp:
        worktree = Path(tmp) / "gitops"
        clone_repo(authenticated_remote(repo_url, repo_username, repo_token), worktree)
        git("checkout", "-b", branch_name, cwd=worktree)
        root = worktree / path_root / service_slug
        if root.exists():
            shutil.rmtree(root)
        write_files(worktree, files)
        git("add", path_root, cwd=worktree)
        git("config", "user.email", "ci@platform", cwd=worktree)
        git("config", "user.name", "PlatformBot", cwd=worktree)
        git("commit", "-m", f"[ci skip] seed {service_slug} {path_root}", cwd=worktree)
        git("push", "origin", branch_name, cwd=worktree)

    repo_slug = repo_url.rstrip("/").split("/")[-1]
    if repo_slug.endswith(".git"):
        repo_slug = repo_slug[:-4]
    bitbucket.create_pull_request(repo_slug, pr_title, pr_description, branch_name)


def create_service_repository(
    *,
    template_dir: Path,
    req: ServiceRequest,
    repo_url: str,
    username: str,
    token: str,
) -> None:
    with tempfile.TemporaryDirectory(prefix="service-seed-") as tmp:
        root = Path(tmp)
        service_tree = render_cookiecutter_template(
            template_dir,
            root,
            {
                "service_name": req.service_name,
                "service_slug": req.service_slug,
                "service_description": req.summary,
            },
        )
        git("init", "-b", "main", cwd=service_tree)
        git("config", "user.email", "ci@platform", cwd=service_tree)
        git("config", "user.name", "PlatformBot", cwd=service_tree)
        git("add", ".", cwd=service_tree)
        git("commit", "-m", "Initial scaffold from Cookiecutter", cwd=service_tree)
        git("remote", "add", "origin", authenticated_remote(repo_url, username, token), cwd=service_tree)
        git("push", "-u", "origin", "main", cwd=service_tree)


def cmd_generate_gitops(args: argparse.Namespace) -> None:
    issue = {
        "key": args.issue_key or "SEED-0",
        "fields": {
            "issuetype": {"name": "IDP Service Request"},
            "summary": f"Seed {args.service_name}",
            "description": f"Service Name: {args.service_name}\nSLO Class: {args.slo_class}",
        },
    }
    req = parse_service_request(issue)
    files = build_gitops_files(req)
    output = Path(args.output_dir)
    write_files(output, files)


def cmd_seed_from_jira(args: argparse.Namespace) -> None:
    jira_token = os.environ[args.jira_token_env]
    bitbucket_token = os.environ[args.bitbucket_token_env]
    issue = jira_get_issue(args.jira_base_url, args.jira_issue_key, args.jira_email, jira_token)
    req = parse_service_request(
        issue,
        issue_key_override=args.jira_issue_key,
        issue_type_override=args.jira_issue_type,
        service_name_override=args.service_name,
        slo_class_override=args.slo_class,
    )

    bitbucket = BitbucketClient(args.bitbucket_api_url, args.bitbucket_workspace, args.bitbucket_username, bitbucket_token)
    bitbucket.create_repository(req.service_slug, project_key=args.bitbucket_project_key)
    service_repo_url = f"{args.bitbucket_git_base_url.rstrip('/')}/{args.bitbucket_workspace}/{req.service_slug}.git"
    create_service_repository(
        template_dir=Path(args.cookiecutter_template),
        req=req,
        repo_url=service_repo_url,
        username=args.bitbucket_username,
        token=bitbucket_token,
    )

    generated = build_gitops_files(req)
    infra_files = {path: content for path, content in generated.items() if f"apps/{req.service_slug}/infra/" in path}
    workload_files = {path: content for path, content in generated.items() if f"apps/{req.service_slug}/workload/" in path}
    stage_gitops_pr(
        repo_url=args.platform_gitops_repo_url,
        repo_username=args.bitbucket_username,
        repo_token=bitbucket_token,
        branch_name=f"seed/{req.service_slug}-infra-{req.issue_key.lower()}",
        pr_title=f"[Seed] Add {req.service_slug} infra scaffolding",
        pr_description=f"Seeded from Jira {req.issue_key} ({req.slo_class} SLO).",
        path_root="apps",
        files=infra_files,
        service_slug=req.service_slug,
        bitbucket=bitbucket,
    )
    stage_gitops_pr(
        repo_url=args.platform_gitops_repo_url,
        repo_username=args.bitbucket_username,
        repo_token=bitbucket_token,
        branch_name=f"seed/{req.service_slug}-workload-{req.issue_key.lower()}",
        pr_title=f"[Seed] Add {req.service_slug} workload scaffolding",
        pr_description=f"Seeded from Jira {req.issue_key} ({req.slo_class} SLO).",
        path_root="apps",
        files=workload_files,
        service_slug=req.service_slug,
        bitbucket=bitbucket,
    )
    print(json.dumps({"service": req.service_slug, "issueKey": req.issue_key, "sloClass": req.slo_class}))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Seed service repositories and GitOps scaffolding from Jira requests.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    generate = subparsers.add_parser("generate-gitops")
    generate.add_argument("--service-name", required=True)
    generate.add_argument("--slo-class", required=True, choices=sorted(VALID_SLO_CLASSES))
    generate.add_argument("--output-dir", required=True)
    generate.add_argument("--issue-key")
    generate.set_defaults(func=cmd_generate_gitops)

    seed = subparsers.add_parser("seed-from-jira")
    seed.add_argument("--jira-base-url", required=True)
    seed.add_argument("--jira-email", default="")
    seed.add_argument("--jira-token-env", default="JIRA_TOKEN")
    seed.add_argument("--jira-issue-key", required=True)
    seed.add_argument("--jira-issue-type", required=True)
    seed.add_argument("--service-name")
    seed.add_argument("--slo-class")
    seed.add_argument("--bitbucket-api-url", default="https://api.bitbucket.org")
    seed.add_argument("--bitbucket-git-base-url", default="https://bitbucket.org")
    seed.add_argument("--bitbucket-workspace", required=True)
    seed.add_argument("--bitbucket-project-key")
    seed.add_argument("--bitbucket-username", default="x-token-auth")
    seed.add_argument("--bitbucket-token-env", default="BITBUCKET_TOKEN")
    seed.add_argument("--cookiecutter-template", required=True)
    seed.add_argument("--platform-gitops-repo-url", required=True)
    seed.set_defaults(func=cmd_seed_from_jira)

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
