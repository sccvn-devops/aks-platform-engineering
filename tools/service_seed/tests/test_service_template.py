"""US-V4-07: service_template.py contract tests.

Covers the *render* concern: cookiecutter subprocess wiring, fallback renderer
path-traversal rejection (FR-V4-44), and the GitOps YAML emitters that
materialise infra + workload scaffolds for the prod tier.
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools.service_seed.jira_intake import parse_service_request
from tools.service_seed.service_template import (
    SUBPROCESS_TIMEOUT_S,
    TemplatePathTraversalError,
    _safe_join,
    analysis_template_yaml,
    build_gitops_files,
    build_infra_files,
    build_workload_files,
    render_cookiecutter_fallback,
    render_cookiecutter_template,
    render_template_string,
    write_files,
)


def _issue(*, slo_class: str = "gold", service_name: str = "Orders") -> dict:
    return {
        "key": "IDP-42",
        "fields": {
            "issuetype": {"name": "IDP Service Request"},
            "summary": service_name,
            "description": f"Service Name: {service_name}\nSLO Class: {slo_class}",
        },
    }


class CookiecutterSubprocessTest(unittest.TestCase):
    def test_cookiecutter_subprocess_passes_timeout(self) -> None:
        captured: dict = {}

        def _fake_run(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with tempfile.TemporaryDirectory() as tmp:
            template_dir = Path(tmp) / "tpl"
            template_dir.mkdir()
            (template_dir / "cookiecutter.json").write_text(json.dumps({"service_slug": "svc"}))
            destination = Path(tmp) / "out"
            destination.mkdir()
            with mock.patch("tools.service_seed.service_template.subprocess.run", side_effect=_fake_run):
                render_cookiecutter_template(template_dir, destination, {"service_slug": "svc"})

        self.assertEqual(captured["timeout"], SUBPROCESS_TIMEOUT_S)
        self.assertTrue(captured["check"])

    def test_cookiecutter_falls_back_when_binary_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            template_dir = Path(tmp) / "tpl"
            template_dir.mkdir()
            (template_dir / "cookiecutter.json").write_text(json.dumps({"service_slug": "default"}))
            (template_dir / "{{cookiecutter.service_slug}}").mkdir()
            (template_dir / "{{cookiecutter.service_slug}}" / "README.md").write_text("hi {{cookiecutter.service_slug}}")
            destination = Path(tmp) / "out"
            destination.mkdir()

            with mock.patch(
                "tools.service_seed.service_template.subprocess.run",
                side_effect=FileNotFoundError("cookiecutter not installed"),
            ):
                root = render_cookiecutter_template(template_dir, destination, {"service_slug": "svc"})

            self.assertTrue((root / "README.md").is_file())
            self.assertEqual((root / "README.md").read_text().strip(), "hi svc")


class SafeJoinTest(unittest.TestCase):
    """FR-V4-44: path-traversal rejection."""

    def test_accepts_simple_relative_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            j = _safe_join(Path(tmp), Path("a/b.txt"))
            self.assertTrue(str(j).startswith(str(Path(tmp).resolve())))

    def test_rejects_dotdot(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(TemplatePathTraversalError):
                _safe_join(Path(tmp), Path("../escape"))

    def test_rejects_absolute(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(TemplatePathTraversalError):
                _safe_join(Path(tmp), Path("/etc/passwd"))


class FallbackRendererTest(unittest.TestCase):
    def _template(self, root: Path) -> Path:
        tpl = root / "tpl"
        tpl.mkdir()
        (tpl / "cookiecutter.json").write_text(json.dumps({"service_slug": "default"}))
        slug_dir = tpl / "{{cookiecutter.service_slug}}"
        slug_dir.mkdir()
        (slug_dir / "README.md").write_text("hi {{cookiecutter.service_slug}}")
        return tpl

    def test_fallback_renders_safe_slug(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tpl = self._template(root)
            dest = root / "out"
            dest.mkdir()
            render_cookiecutter_fallback(tpl, dest, {"service_slug": "good"})
            self.assertTrue((dest / "good" / "README.md").is_file())

    def test_fallback_rejects_dotdot_slug_before_any_write(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tpl = self._template(root)
            dest = root / "out"
            dest.mkdir()
            before = list(dest.iterdir())
            with self.assertRaises(TemplatePathTraversalError):
                render_cookiecutter_fallback(tpl, dest, {"service_slug": "../escape"})
            self.assertEqual(before, list(dest.iterdir()))

    def test_render_template_string_substitutes(self) -> None:
        rendered = render_template_string(
            "hello {{cookiecutter.service_slug}}",
            {"service_slug": "foo"},
        )
        self.assertEqual(rendered, "hello foo")


class WriteFilesTest(unittest.TestCase):
    def test_writes_nested_paths(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            write_files(base, {"a/b/c.txt": "hello"})
            self.assertEqual((base / "a" / "b" / "c.txt").read_text(), "hello\n")


class GitOpsEmitterTest(unittest.TestCase):
    def test_build_gitops_files_emits_expected_scaffold(self) -> None:
        req = parse_service_request(_issue(slo_class="silver", service_name="Orders"))
        files = build_gitops_files(req)
        required = {
            "apps/orders/infra/base/xrc-sql.yaml",
            "apps/orders/infra/base/xrc-cosmos.yaml",
            "apps/orders/infra/base/xrc-sb.yaml",
            "apps/orders/infra/base/xrc-namespace-binding.yaml",
            "apps/orders/workload/base/service.yaml",
            "apps/orders/workload/base/ingress.yaml",
            "apps/orders/workload/base/external-secrets.yaml",
            "apps/orders/workload/base/analysis-template.yaml",
            "apps/orders/workload/overlays/dev/rollout.yaml",
            "apps/orders/workload/overlays/staging/rollout.yaml",
            "apps/orders/workload/overlays/prod/rollout.yaml",
        }
        self.assertTrue(required.issubset(files.keys()))

    def test_prod_overlays_emit_rollouts(self) -> None:
        req = parse_service_request(_issue(slo_class="gold", service_name="Inventory"))
        files = build_workload_files(req)
        self.assertIn("kind: Deployment", files["apps/inventory/workload/overlays/dev/rollout.yaml"])
        self.assertIn("kind: Rollout", files["apps/inventory/workload/overlays/prod/rollout.yaml"])

    def test_infra_files_source_registry_identity(self) -> None:
        req = parse_service_request(_issue(slo_class="bronze", service_name="Cart"))
        files = build_infra_files(req)
        sql = files["apps/cart/infra/base/xrc-sql.yaml"]
        # Region + RG come from gitops/clusters/registry.yaml.
        self.assertIn("region: westeurope", sql)
        self.assertIn("resourceGroupName: rg-platform-prod", sql)
        # AKV ARM IDs come from the registry's subscription_id + RG.
        self.assertIn("/providers/Microsoft.KeyVault/vaults/kv-platform-prod-we", sql)
        self.assertIn("/providers/Microsoft.KeyVault/vaults/kv-platform-prod-ne", sql)

    def test_bronze_analysis_template_is_commented(self) -> None:
        req = parse_service_request(_issue(slo_class="bronze", service_name="Catalog"))
        text = analysis_template_yaml(req)
        self.assertIn("direct cutover", text)

    def test_gold_analysis_template_contains_p99(self) -> None:
        req = parse_service_request(_issue(slo_class="gold", service_name="Payments"))
        text = analysis_template_yaml(req)
        self.assertIn("p99-latency-ms", text)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
