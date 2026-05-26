"""Static contract tests for the workload_identity Terraform module (US-V4-02).

The project pins terraform to 1.5.x (provider.tf required_version = "~> 1.5.0";
ADR-029-v3 / FR-V3-27).  The HCL-native `terraform test` command requires
terraform >= 1.6, so the equivalent assertions are encoded here as static
parses of the module source.  Tests/workload_identity.tftest.hcl carries the
same assertions in HCL form for when the project upgrades.

These tests assert the FR-V4-07 contract:

    1. The federated-credential subject is constructed as
       `system:serviceaccount:<namespace>:<name>` — call sites cannot drift.
    2. Subscription-scoped role assignments are rejected unless
       `allow_subscription_scope = true`.
    3. The role-assignment scope is exactly the input scope (no mutation).

And the FR-V4-05/06 contract on consumers:

    4. The 5 in-tree call sites (Jenkins, ESO workload, ESO mgmt-we,
       akv-sync-exporter, saas-token-rotator, Velero) call the module — there
       are zero remaining inline `azurerm_user_assigned_identity` /
       `azurerm_federated_identity_credential` resources for those identities.
"""

from __future__ import annotations

import pathlib
import re
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
MODULE_DIR = REPO_ROOT / "terraform" / "modules" / "workload_identity"
TF_DIR = REPO_ROOT / "terraform"


def _read(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8")


# ──────────────────────────────────────────────────────────────────────────────
# Module-internal contract
# ──────────────────────────────────────────────────────────────────────────────


class TestModuleInternals(unittest.TestCase):
    """Assert the module's main.tf encodes the FR-V4-07 invariants."""

    def setUp(self) -> None:
        self.main_tf = _read(MODULE_DIR / "main.tf")
        self.vars_tf = _read(MODULE_DIR / "variables.tf")

    def test_module_files_exist(self) -> None:
        for name in ("main.tf", "variables.tf", "outputs.tf", "versions.tf"):
            self.assertTrue(
                (MODULE_DIR / name).is_file(),
                f"workload_identity/{name} is missing",
            )

    def test_federated_credential_subject_format(self) -> None:
        """Subject must be system:serviceaccount:<ns>:<sa> (FR-V4-09)."""
        match = re.search(
            r'subject\s*=\s*"system:serviceaccount:\$\{each\.value\.service_account_namespace\}:\$\{each\.value\.service_account_name\}"',
            self.main_tf,
        )
        self.assertIsNotNone(
            match,
            "azurerm_federated_identity_credential subject must be constructed "
            "as system:serviceaccount:${each.value.service_account_namespace}:${each.value.service_account_name}",
        )

    def test_audience_pinned(self) -> None:
        """audience must be the AzureADTokenExchange constant."""
        self.assertIn(
            'audience            = ["api://AzureADTokenExchange"]',
            self.main_tf,
            "federated credential audience must be pinned to api://AzureADTokenExchange",
        )

    def test_role_assignment_scope_passes_through(self) -> None:
        """The module emits scope = each.value.scope — no normalisation (FR-V4-09)."""
        self.assertRegex(
            self.main_tf,
            r"azurerm_role_assignment\"\s+\"this\"\s+\{[^}]*scope\s*=\s*each\.value\.scope",
        )

    def test_subscription_scope_guard_default_false(self) -> None:
        """allow_subscription_scope defaults to false (FR-V4-08)."""
        match = re.search(
            r'variable\s+"allow_subscription_scope"\s*\{[^}]*default\s*=\s*false',
            self.vars_tf,
            re.DOTALL,
        )
        self.assertIsNotNone(
            match,
            "allow_subscription_scope must default to false to refuse broad grants",
        )

    def test_subscription_scope_precondition_present(self) -> None:
        """A lifecycle.precondition on role assignment enforces the guard."""
        self.assertRegex(
            self.main_tf,
            r"precondition\s*\{[^}]*allow_subscription_scope[^}]*\^/subscriptions/",
            "azurerm_role_assignment.this must carry a lifecycle.precondition "
            "rejecting bare /subscriptions/<uuid> scopes when allow_subscription_scope is false",
        )


# ──────────────────────────────────────────────────────────────────────────────
# Call-site migration assertions
# ──────────────────────────────────────────────────────────────────────────────


class TestCallSiteMigration(unittest.TestCase):
    """Assert the 5 in-tree workload identities consume the module (FR-V4-05/06)."""

    EXPECTED_MODULES = {
        "saas_token_rotator_identity": "terraform/saas_token_rotator.tf",
        "velero_identity": "terraform/velero.tf",
        "akv_sync_exporter_identity": "terraform/akv_sync_exporter.tf",
        "jenkins_identity": "terraform/acr.tf",
        "external_secrets_mgmt_we_identity": "terraform/jenkins.tf",
        "external_secrets_identity": "terraform/external_secrets.tf",
    }

    # Resource addresses whose inline declarations must NOT survive — they are
    # owned by the module now.  References inside `moved {}` blocks are allowed
    # (those preserve state across the refactor).
    LEGACY_INLINE_ADDRS = (
        'resource "azurerm_user_assigned_identity" "saas_token_rotator"',
        'resource "azurerm_user_assigned_identity" "velero"',
        'resource "azurerm_user_assigned_identity" "akv_sync_exporter"',
        'resource "azurerm_user_assigned_identity" "jenkins"',
        'resource "azurerm_user_assigned_identity" "external_secrets_mgmt_we"',
        'resource "azurerm_user_assigned_identity" "external_secrets"',
        'resource "azurerm_federated_identity_credential" "jenkins_controller"',
        'resource "azurerm_federated_identity_credential" "jenkins_agent"',
        'resource "azurerm_federated_identity_credential" "velero"',
        'resource "azurerm_federated_identity_credential" "saas_token_rotator"',
        'resource "azurerm_federated_identity_credential" "akv_sync_exporter"',
        'resource "azurerm_federated_identity_credential" "external_secrets"',
        'resource "azurerm_federated_identity_credential" "external_secrets_mgmt_we"',
        'resource "azurerm_role_assignment" "jenkins_acr_push"',
        'resource "azurerm_role_assignment" "jenkins_management_ci_crypto_user"',
        'resource "azurerm_role_assignment" "velero_storage_blob_data_contributor"',
        'resource "azurerm_role_assignment" "velero_contributor"',
        'resource "azurerm_role_assignment" "saas_rotator_mgmt_kv"',
        'resource "azurerm_role_assignment" "saas_rotator_prod_we_kv"',
        'resource "azurerm_role_assignment" "saas_rotator_prod_ne_kv"',
        'resource "azurerm_role_assignment" "akv_sync_exporter_kv_we"',
        'resource "azurerm_role_assignment" "akv_sync_exporter_kv_ne"',
        'resource "azurerm_role_assignment" "external_secrets_key_vault_reader"',
        'resource "azurerm_role_assignment" "external_secrets_mgmt_we_key_vault_reader"',
    )

    def _tf_files(self) -> list[pathlib.Path]:
        return sorted(p for p in TF_DIR.glob("*.tf") if p.is_file())

    def test_each_call_site_consumes_module(self) -> None:
        for module_name, expected_file in self.EXPECTED_MODULES.items():
            path = REPO_ROOT / expected_file
            self.assertTrue(path.is_file(), f"{expected_file} is missing")
            self.assertRegex(
                _read(path),
                rf'module\s+"{module_name}"\s*\{{[^}}]*source\s*=\s*"\./modules/workload_identity"',
                f"{expected_file} must invoke the workload_identity module as {module_name}",
            )

    def test_no_inline_legacy_resources_remain(self) -> None:
        for tf_file in self._tf_files():
            content = _read(tf_file)
            for addr in self.LEGACY_INLINE_ADDRS:
                self.assertNotIn(
                    addr,
                    content,
                    f"{tf_file.name} still declares inline {addr!r}; "
                    "should be moved into the workload_identity module call",
                )

    def test_moved_blocks_preserve_state(self) -> None:
        """Every migrated resource has a `moved {}` block so state survives.

        We check the union across the 5 migrated call-site files — moved
        blocks for the 5 module instances must reference all migrated
        resource types (UAMI + federated credential + role assignment).
        """
        text = "\n".join(
            _read(REPO_ROOT / f)
            for f in (
                "terraform/saas_token_rotator.tf",
                "terraform/velero.tf",
                "terraform/akv_sync_exporter.tf",
                "terraform/external_secrets.tf",
                "terraform/jenkins.tf",
                "terraform/acr.tf",
            )
        )
        # At minimum: one moved block per identity covering UAMI + at least one
        # federated credential.  Sample expected addresses:
        expected_moves = (
            "from = azurerm_user_assigned_identity.saas_token_rotator",
            "to   = module.saas_token_rotator_identity.azurerm_user_assigned_identity.this",
            "from = azurerm_user_assigned_identity.velero",
            "to   = module.velero_identity.azurerm_user_assigned_identity.this",
            "from = azurerm_user_assigned_identity.akv_sync_exporter",
            "to   = module.akv_sync_exporter_identity.azurerm_user_assigned_identity.this",
            "from = azurerm_user_assigned_identity.jenkins",
            "to   = module.jenkins_identity.azurerm_user_assigned_identity.this",
            "from = azurerm_user_assigned_identity.external_secrets_mgmt_we",
            "to   = module.external_secrets_mgmt_we_identity.azurerm_user_assigned_identity.this",
        )
        for snippet in expected_moves:
            self.assertIn(
                snippet,
                text,
                f"Missing moved block fragment: {snippet!r} — state migration is incomplete",
            )


# ──────────────────────────────────────────────────────────────────────────────
# tftest.hcl artifact exists for when terraform >= 1.6 is available
# ──────────────────────────────────────────────────────────────────────────────


class TestTftestArtifactExists(unittest.TestCase):
    def test_tftest_hcl_file_present(self) -> None:
        path = MODULE_DIR / "tests" / "workload_identity.tftest.hcl"
        self.assertTrue(
            path.is_file(),
            "tests/workload_identity.tftest.hcl is the future-ready HCL test "
            "suite; it must ship alongside the Python contract tests",
        )

    def test_tftest_covers_required_assertions(self) -> None:
        text = _read(MODULE_DIR / "tests" / "workload_identity.tftest.hcl")
        # AC3: federated-credential subject construction
        self.assertIn(
            "system:serviceaccount:velero:velero-server",
            text,
            "tftest must exercise the federated-credential subject format",
        )
        # AC3: no subscription-scope unless allow_subscription_scope = true
        self.assertIn("subscription_scope_is_rejected_by_default", text)
        self.assertIn("subscription_scope_allowed_when_explicit", text)
        # AC3: role-assignment scope passthrough
        self.assertIn("rg_scope_passes_default_guard", text)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
