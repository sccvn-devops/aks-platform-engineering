"""Tests for the service_seed cluster registry loader (US-V4-01, FR-V4-03).

Covers the public surface of tools/service_seed/cli.py against both:
  * the canonical committed registry at gitops/clusters/registry.yaml
  * tmpdir-written fixtures that exercise error paths

Note: this test deliberately does NOT pin pyyaml import — the loader's
fallback parser handles the registry shape in environments where pyyaml
isn't installed (e.g., minimal CI runners). Both code paths are exercised.
"""

from __future__ import annotations

import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from tools.service_seed.cli import (
    ClusterEntry,
    ClusterRegistryError,
    _parse_registry_fallback,
    _yaml_load,
    get_cluster,
    load_registry,
    registry_main as main,
    workload_keyvault_id,
)


REPO_ROOT = Path(__file__).resolve().parents[3]
CANONICAL_REGISTRY = REPO_ROOT / "gitops" / "clusters" / "registry.yaml"

EXPECTED_CLUSTERS = {
    "mgmt-we", "mgmt-ne", "aks-dev-we", "aks-staging-we",
    "aks-prod-we", "aks-prod-ne", "seed-wus",
}


class CanonicalRegistryTests(unittest.TestCase):
    def test_loads_seven_clusters(self) -> None:
        reg = load_registry(CANONICAL_REGISTRY)
        self.assertEqual(set(reg), EXPECTED_CLUSTERS)

    def test_every_entry_has_required_fields(self) -> None:
        reg = load_registry(CANONICAL_REGISTRY)
        for name, entry in reg.items():
            self.assertIsInstance(entry, ClusterEntry)
            self.assertEqual(entry.name, name)
            self.assertEqual(entry.aks_name, name)
            self.assertTrue(entry.subscription_id)
            self.assertTrue(entry.region)
            self.assertIn(entry.region_abbrev, {"we", "ne", "wus"})
            self.assertTrue(entry.resource_group)
            self.assertTrue(entry.acr_hostname.endswith(".azurecr.io"))
            self.assertIn(entry.mgmt_role, {"active", "standby", "workload", "seed"})
            self.assertIn(entry.sku_tier, {"Free", "Standard", "Premium"})
            self.assertIsInstance(entry.azs, tuple)
            self.assertIsInstance(entry.gitops_addons, dict)

    def test_mgmt_role_distribution(self) -> None:
        reg = load_registry(CANONICAL_REGISTRY)
        roles = {name: entry.mgmt_role for name, entry in reg.items()}
        self.assertEqual(roles["mgmt-we"], "active")
        self.assertEqual(roles["mgmt-ne"], "standby")
        self.assertEqual(roles["seed-wus"], "seed")
        for prod in ("aks-dev-we", "aks-staging-we", "aks-prod-we", "aks-prod-ne"):
            self.assertEqual(roles[prod], "workload")

    def test_regions_match_naming_convention(self) -> None:
        reg = load_registry(CANONICAL_REGISTRY)
        self.assertEqual(reg["aks-prod-we"].region, "westeurope")
        self.assertEqual(reg["aks-prod-ne"].region, "northeurope")
        self.assertEqual(reg["seed-wus"].region, "westus2")

    def test_get_cluster_resolves_known(self) -> None:
        entry = get_cluster("aks-prod-we")
        self.assertEqual(entry.region, "westeurope")
        self.assertEqual(entry.sku_tier, "Premium")

    def test_get_cluster_raises_on_unknown(self) -> None:
        with self.assertRaises(ClusterRegistryError):
            get_cluster("does-not-exist")

    def test_workload_keyvault_id_format(self) -> None:
        result = workload_keyvault_id("aks-prod-we", "kv-platform-prod-we")
        self.assertIn("/subscriptions/", result)
        self.assertIn("/resourceGroups/", result)
        self.assertIn("/providers/Microsoft.KeyVault/vaults/kv-platform-prod-we", result)


class LoaderErrorPathTests(unittest.TestCase):
    def test_missing_file(self) -> None:
        with self.assertRaises(ClusterRegistryError):
            load_registry("/tmp/this-path-does-not-exist-12345.yaml")

    def test_top_level_key_mismatch_fails(self) -> None:
        with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as f:
            f.write(_minimal_entry_yaml(key="foo", aks_name="bar"))
            path = f.name
        try:
            with self.assertRaisesRegex(ClusterRegistryError, "aks_name"):
                load_registry(path)
        finally:
            Path(path).unlink(missing_ok=True)

    def test_missing_required_field_fails(self) -> None:
        body = (
            "foo:\n"
            "  subscription_id: \"00000000-0000-0000-0000-000000000000\"\n"
            "  region: westeurope\n"
            "  aks_name: foo\n"
            "  mgmt_role: workload\n"
        )
        with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as f:
            f.write(body)
            path = f.name
        try:
            with self.assertRaisesRegex(ClusterRegistryError, "missing required"):
                load_registry(path)
        finally:
            Path(path).unlink(missing_ok=True)


class FallbackParserTests(unittest.TestCase):
    """Exercise the embedded YAML-subset parser directly (no pyyaml needed)."""

    def test_parses_canonical_registry(self) -> None:
        text = CANONICAL_REGISTRY.read_text(encoding="utf-8")
        parsed = _parse_registry_fallback(text)
        self.assertEqual(set(parsed), EXPECTED_CLUSTERS)
        self.assertEqual(parsed["aks-prod-we"]["region"], "westeurope")
        self.assertEqual(parsed["aks-prod-we"]["sku_tier"], "Premium")
        self.assertIsInstance(parsed["aks-prod-we"]["azs"], list)
        self.assertEqual(parsed["aks-prod-we"]["azs"], ["1", "2", "3"])

    def test_empty_list_inline(self) -> None:
        parsed = _parse_registry_fallback(_minimal_entry_yaml(key="foo", aks_name="foo"))
        self.assertEqual(parsed["foo"]["azs"], [])

    def test_yaml_load_dispatches(self) -> None:
        # _yaml_load returns equivalent shape regardless of which backend is used.
        text = CANONICAL_REGISTRY.read_text(encoding="utf-8")
        loaded = _yaml_load(text)
        self.assertEqual(set(loaded), EXPECTED_CLUSTERS)


class CLIEntryPointTests(unittest.TestCase):
    def test_show_dumps_json(self) -> None:
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = main(["show", "aks-prod-we", "--registry", str(CANONICAL_REGISTRY)])
        self.assertEqual(rc, 0)
        payload = json.loads(buf.getvalue())
        self.assertEqual(set(payload), {"aks-prod-we"})
        self.assertEqual(payload["aks-prod-we"]["region"], "westeurope")

    def test_paths_prints_registry_location(self) -> None:
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = main(["paths", "--registry", "/x/y.yaml"])
        self.assertEqual(rc, 0)
        self.assertEqual(buf.getvalue().strip(), "/x/y.yaml")


def _minimal_entry_yaml(*, key: str, aks_name: str) -> str:
    return (
        f"{key}:\n"
        f"  subscription_id: \"00000000-0000-0000-0000-000000000000\"\n"
        f"  region: westeurope\n"
        f"  region_abbrev: we\n"
        f"  resource_group: rg-test\n"
        f"  acr_hostname: acrtest.azurecr.io\n"
        f"  aks_name: {aks_name}\n"
        f"  mgmt_role: workload\n"
        f"  azs: []\n"
        f"  sku_tier: Standard\n"
        f"  gitops_addons:\n"
        f"    enable_argocd: \"false\"\n"
        f"    enable_kyverno: \"true\"\n"
        f"    enable_external_secrets: \"true\"\n"
    )


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
