"""US-V4-08 (FR-V4-32..35): template + profile contract tests.

Asserts:
    * Every emitted file is template-driven — service_template.py contains no
      YAML literal (AC1).
    * Jinja2 runs with ``StrictUndefined`` so a missing context key raises
      ``jinja2.UndefinedError`` instead of producing an incomplete manifest
      (AC2).
    * Thresholds and step strategies come from ``profiles/slo.yaml`` and
      ``profiles/rollout.yaml``; mutating the profile changes the rendered
      output (AC3).
    * No SLO numeric (0.99, 500, 5m) appears as a Python literal in
      ``service_template.py``.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

import jinja2

from tools.service_seed import service_template
from tools.service_seed.jira_intake import parse_service_request
from tools.service_seed.service_template import (
    ENVIRONMENTS,
    _PROFILES_DIR,
    _TEMPLATES_DIR,
    render,
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


class TemplateDirectoryShapeTest(unittest.TestCase):
    """AC1: templates exist for every emitted output path."""

    def test_templates_directory_exists(self) -> None:
        self.assertTrue(_TEMPLATES_DIR.is_dir(), f"templates dir missing: {_TEMPLATES_DIR}")

    def test_every_expected_template_is_a_jinja_file(self) -> None:
        expected = {
            "infra/base/kustomization.yaml.j2",
            "infra/base/namespace.yaml.j2",
            "infra/base/xrc-sql.yaml.j2",
            "infra/base/xrc-cosmos.yaml.j2",
            "infra/base/xrc-sb.yaml.j2",
            "infra/base/xrc-namespace-binding.yaml.j2",
            "infra/base/namespace-rollout-policy.yaml.j2",
            "infra/overlays/kustomization.yaml.j2",
            "workload/base/kustomization.yaml.j2",
            "workload/base/namespace.yaml.j2",
            "workload/base/serviceaccount.yaml.j2",
            "workload/base/service.yaml.j2",
            "workload/base/ingress.yaml.j2",
            "workload/base/external-secrets.yaml.j2",
            "workload/base/analysis-template.yaml.j2",
            "workload/overlays/kustomization.yaml.j2",
            "workload/overlays/rollout.yaml.j2",
        }
        for rel in expected:
            with self.subTest(template=rel):
                self.assertTrue(
                    (_TEMPLATES_DIR / rel).is_file(),
                    f"expected template missing: {rel}",
                )

    def test_emitted_files_mirror_template_paths(self) -> None:
        req = parse_service_request(_issue(slo_class="gold", service_name="Orders"))
        files = render(req)
        # Spot-check the rendered map has one entry per (template, env) where
        # applicable.
        self.assertIn("apps/orders/infra/base/xrc-sql.yaml", files)
        for env in ENVIRONMENTS:
            self.assertIn(f"apps/orders/infra/overlays/{env}/kustomization.yaml", files)
            self.assertIn(f"apps/orders/workload/overlays/{env}/kustomization.yaml", files)
            self.assertIn(f"apps/orders/workload/overlays/{env}/rollout.yaml", files)


class StrictUndefinedTest(unittest.TestCase):
    """AC2: a missing context variable raises jinja2.UndefinedError."""

    def test_environment_uses_strict_undefined(self) -> None:
        self.assertIs(service_template._env.undefined, jinja2.StrictUndefined)

    def test_missing_variable_raises_at_render_time(self) -> None:
        # Pick the simplest template that references a context variable.
        with self.assertRaises(jinja2.UndefinedError) as cm:
            service_template._render_template(
                "infra/base/namespace.yaml.j2", {}  # 'service' deliberately absent
            )
        self.assertIn("service", str(cm.exception).lower())

    def test_missing_slo_subkey_raises(self) -> None:
        # The analysis-template references slo.has_analysis_template + nested
        # threshold keys.  Passing a partial dict must raise.
        with self.assertRaises(jinja2.UndefinedError):
            service_template._render_template(
                "workload/base/analysis-template.yaml.j2",
                {"service": "svc", "slo": {}},  # missing has_analysis_template
            )


class ProfileDataTest(unittest.TestCase):
    """AC3: thresholds and step strategies come from profile YAML."""

    def test_profile_files_exist(self) -> None:
        self.assertTrue((_PROFILES_DIR / "slo.yaml").is_file())
        self.assertTrue((_PROFILES_DIR / "rollout.yaml").is_file())

    def test_profiles_declare_all_three_classes(self) -> None:
        for cls in ("gold", "silver", "bronze"):
            self.assertIn(cls, service_template._SLO_PROFILE)
            self.assertIn(cls, service_template._ROLLOUT_PROFILE)

    def test_gold_threshold_appears_in_rendered_analysis_template(self) -> None:
        req = parse_service_request(_issue(slo_class="gold"))
        files = render(req)
        text = files["apps/orders/workload/base/analysis-template.yaml"]
        expected_success = str(service_template._SLO_PROFILE["gold"]["success_rate_threshold"])
        expected_p99 = str(service_template._SLO_PROFILE["gold"]["p99_latency_ms_threshold"])
        self.assertIn(f"result[0] >= {expected_success}", text)
        self.assertIn(f"result[0] <= {expected_p99}", text)

    def test_silver_has_success_rate_but_not_p99(self) -> None:
        req = parse_service_request(_issue(slo_class="silver"))
        files = render(req)
        text = files["apps/orders/workload/base/analysis-template.yaml"]
        self.assertIn("success-rate", text)
        self.assertNotIn("p99-latency-ms", text)

    def test_bronze_emits_only_comment_stub(self) -> None:
        req = parse_service_request(_issue(slo_class="bronze"))
        files = render(req)
        text = files["apps/orders/workload/base/analysis-template.yaml"]
        self.assertIn("direct cutover", text)
        self.assertNotIn("apiVersion", text)

    def test_rollout_steps_come_from_profile(self) -> None:
        req = parse_service_request(_issue(slo_class="gold"))
        files = render(req)
        prod_rollout = files["apps/orders/workload/overlays/prod/rollout.yaml"]
        for step in service_template._ROLLOUT_PROFILE["gold"]["steps"]:
            self.assertIn(step, prod_rollout)

    def test_render_accepts_slo_class_override(self) -> None:
        req = parse_service_request(_issue(slo_class="bronze"))
        files = render(req, slo_class="gold")
        # Override applied: gold thresholds present even though req.slo_class is bronze.
        text = files["apps/orders/workload/base/analysis-template.yaml"]
        self.assertIn("p99-latency-ms", text)

    def test_mutating_profile_changes_rendered_threshold(self) -> None:
        """A profile change is the *only* place to alter the threshold —
        no parallel literal lives in service_template.py."""
        original = service_template._SLO_PROFILE["gold"]["success_rate_threshold"]
        service_template._SLO_PROFILE["gold"]["success_rate_threshold"] = 0.999
        try:
            req = parse_service_request(_issue(slo_class="gold"))
            files = render(req)
            text = files["apps/orders/workload/base/analysis-template.yaml"]
            self.assertIn("result[0] >= 0.999", text)
        finally:
            service_template._SLO_PROFILE["gold"]["success_rate_threshold"] = original


class NoSLONumericsAsPythonLiteralsTest(unittest.TestCase):
    """AC3 corollary: zero SLO numerics (0.99, 500, 5m) as Python literals."""

    def test_no_slo_numerics_in_service_template_py(self) -> None:
        path = Path(service_template.__file__)
        source = path.read_text()
        # Strip docstrings + comments before scanning for numerics.  The
        # scanner only needs to inspect executable code lines.
        scan_lines = []
        for line in source.splitlines():
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            scan_lines.append(line)
        scan_text = "\n".join(scan_lines)

        forbidden_patterns = [
            r"\b0\.99\b",      # success-rate threshold
            r"\b500\b",         # p99 latency bound (ms)
            r'"5m"',            # pause duration string literal
            r"'5m'",
            r'duration:\s*5m',  # YAML-literal form
        ]
        for pat in forbidden_patterns:
            with self.subTest(pattern=pat):
                self.assertFalse(
                    re.search(pat, scan_text),
                    f"SLO numeric literal {pat!r} leaked into service_template.py",
                )


class YamlLiteralAbsenceTest(unittest.TestCase):
    """AC1: zero YAML literals remain inside service_template.py."""

    def test_no_apiversion_string_in_module_source(self) -> None:
        path = Path(service_template.__file__)
        source = path.read_text()
        # Excise docstrings so that the description block does not register as
        # a YAML literal.
        no_docstrings = re.sub(r'"""[\s\S]*?"""', "", source)
        self.assertNotIn("apiVersion", no_docstrings)
        self.assertNotIn("kind: Kustomization", no_docstrings)
        self.assertNotIn("kind: Rollout", no_docstrings)
        self.assertNotIn("kind: Deployment", no_docstrings)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
