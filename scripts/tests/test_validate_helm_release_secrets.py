"""Tests for scripts/validate-helm-release-secrets.py (US-V4-09, FR-V4-37)."""

from __future__ import annotations

import importlib.util
import io
import shutil
import sys
import tempfile
import textwrap
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
SCRIPT_PATH = REPO_ROOT / "scripts" / "validate-helm-release-secrets.py"


def _load_module():
    spec = importlib.util.spec_from_file_location("vhrs", SCRIPT_PATH)
    mod = importlib.util.module_from_spec(spec)  # type: ignore[arg-type]
    sys.modules["vhrs"] = mod
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


class HelmReleaseSecretsValidatorTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.vhrs = _load_module()

    def setUp(self):
        self.tmpdir = Path(tempfile.mkdtemp(prefix="vhrs-test-"))
        self.variables_tf = self.tmpdir / "variables.tf"
        # A small sensitive variables surface used by every fixture.
        self.variables_tf.write_text(
            textwrap.dedent(
                """
                variable "github_token" {
                  type      = string
                  sensitive = true
                }
                variable "postgres_password" {
                  type      = string
                  sensitive = true
                }
                variable "innocuous_id" {
                  type    = string
                  default = "abc"
                }
                """
            ).strip()
            + "\n"
        )

    def tearDown(self):
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def _patch_paths(self):
        # Re-point the module globals at the tmp tree for the duration of one
        # test (each test calls find_violations(tmpdir) with its own scope).
        self.vhrs.VARIABLES_FILE = self.variables_tf
        self.vhrs.TERRAFORM_DIR = self.tmpdir

    def _write(self, name: str, body: str) -> Path:
        p = self.tmpdir / name
        p.write_text(body)
        return p

    # ----- direct var.<sensitive> in helm_release.set ------------------------
    def test_detects_direct_sensitive_var_reference(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.GITHUB_TOKEN"
                    value = var.github_token
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 1)
        _, _, label, name, reason = findings[0]
        self.assertEqual(label, "demo")
        self.assertEqual(name, "env.GITHUB_TOKEN")
        self.assertIn("var.github_token", reason)

    # ----- non-sensitive var is allowed --------------------------------------
    def test_allows_non_sensitive_var(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "config.id"
                    value = var.innocuous_id
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(findings, [])

    # ----- one-hop local alias to a sensitive var ----------------------------
    def test_detects_one_hop_local_alias_to_sensitive_var(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                locals {
                  github_token = var.github_token
                }
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.GITHUB_TOKEN"
                    value = local.github_token
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 1)
        _, _, _, _, reason = findings[0]
        self.assertIn("local.github_token", reason)
        self.assertIn("var.github_token", reason)

    # ----- known-sensitive resource attribute --------------------------------
    def test_detects_known_sensitive_attr_postgres_admin(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.POSTGRES_PASSWORD"
                    value = azurerm_postgresql_flexible_server.pg[0].administrator_password
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 1)
        _, _, _, _, reason = findings[0]
        self.assertIn("administrator_password", reason)

    def test_detects_known_sensitive_attr_sp_password_value(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.AZURE_CLIENT_SECRET"
                    value = azuread_service_principal_password.sp[0].value
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 1)
        _, _, _, name, reason = findings[0]
        self.assertEqual(name, "env.AZURE_CLIENT_SECRET")
        self.assertIn("azuread_service_principal_password", reason)

    def test_detects_known_sensitive_attr_k8s_sa_token(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.K8S_SERVICE_ACCOUNT_TOKEN"
                    value = kubernetes_secret.backstage_service_account_secret[0].data.token
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 1)
        _, _, _, _, reason = findings[0]
        self.assertIn("kubernetes_secret", reason)

    # ----- envFrom secretRef.name is allowed (migration target) --------------
    def test_allows_envfrom_secret_ref_name(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "envFrom[0].secretRef.name"
                    value = "demo-secrets"
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(findings, [])

    # ----- multiple findings reported, not just the first --------------------
    def test_reports_all_findings(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.GITHUB_TOKEN"
                    value = var.github_token
                  }
                  set {
                    name  = "env.POSTGRES_PASSWORD"
                    value = var.postgres_password
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(len(findings), 2)
        names = {f[3] for f in findings}
        self.assertEqual(names, {"env.GITHUB_TOKEN", "env.POSTGRES_PASSWORD"})

    # ----- catalogue/comment lines are ignored -------------------------------
    def test_ignores_comment_lines_describing_forbidden_pattern(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                # Forbidden example (FR-V4-37) — do NOT do this:
                # set {
                #   name  = "env.GITHUB_TOKEN"
                #   value = var.github_token
                # }
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(findings, [])

    # ----- variable without sensitive=true is NOT flagged --------------------
    def test_does_not_flag_non_sensitive_named_variable(self):
        self._patch_paths()
        # innocuous_id has no sensitive=true.
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "id"
                    value = var.innocuous_id
                  }
                }
                """
            ),
        )
        findings = self.vhrs.find_violations(self.tmpdir)
        self.assertEqual(findings, [])

    # ----- canonical repo tree is clean --------------------------------------
    def test_canonical_repo_tree_is_clean(self):
        """The real terraform/ directory under repo root must have zero findings.

        Regression guard for AC3 (in-tree call-site count = 0).
        """
        self.vhrs.VARIABLES_FILE = REPO_ROOT / "terraform" / "variables.tf"
        self.vhrs.TERRAFORM_DIR = REPO_ROOT / "terraform"
        findings = self.vhrs.find_violations(REPO_ROOT / "terraform")
        if findings:
            details = "\n".join(
                f"  {tf}:{line}  helm_release.{label}  name={name}  -- {reason}"
                for tf, line, label, name, reason in findings
            )
            self.fail(
                "Expected zero helm_release.set sensitive-value findings in "
                f"terraform/, got {len(findings)}:\n{details}"
            )

    # ----- main() exit codes -------------------------------------------------
    def test_main_returns_zero_on_clean_tree(self):
        self.vhrs.VARIABLES_FILE = REPO_ROOT / "terraform" / "variables.tf"
        self.vhrs.TERRAFORM_DIR = REPO_ROOT / "terraform"
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = self.vhrs.main()
        self.assertEqual(rc, 0)
        self.assertIn("PASS", buf.getvalue())

    def test_main_returns_one_on_violation(self):
        self._patch_paths()
        self._write(
            "main.tf",
            textwrap.dedent(
                """
                resource "helm_release" "demo" {
                  name  = "demo"
                  chart = "demo-chart"

                  set {
                    name  = "env.GITHUB_TOKEN"
                    value = var.github_token
                  }
                }
                """
            ),
        )
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            rc = self.vhrs.main()
        self.assertEqual(rc, 1)
        combined = out.getvalue() + err.getvalue()
        self.assertIn("FR-V4-37", combined)
        self.assertIn("::error", combined)


if __name__ == "__main__":
    unittest.main()
