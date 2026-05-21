from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from tools.service_seed.seed_job import build_gitops_files, parse_service_request


class SeedJobTests(unittest.TestCase):
    def request(self, *, service_name: str = "Payments API", slo_class: str = "gold") -> dict:
        return {
            "key": "IDP-24",
            "fields": {
                "issuetype": {"name": "IDP Service Request"},
                "summary": service_name,
                "description": {
                    "type": "doc",
                    "content": [
                        {
                            "type": "paragraph",
                            "content": [{"type": "text", "text": f"Service Name: {service_name}"}],
                        },
                        {
                            "type": "paragraph",
                            "content": [{"type": "text", "text": f"SLO Class: {slo_class}"}],
                        },
                    ],
                },
            },
        }

    def test_parse_service_request_extracts_slug_and_slo(self) -> None:
        req = parse_service_request(self.request())
        self.assertEqual(req.service_slug, "payments-api")
        self.assertEqual(req.slo_class, "gold")

    def test_generated_files_include_required_scaffold(self) -> None:
        req = parse_service_request(self.request(service_name="Orders", slo_class="silver"))
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

    def test_rollout_kind_varies_by_environment(self) -> None:
        req = parse_service_request(self.request(service_name="Inventory", slo_class="gold"))
        files = build_gitops_files(req)
        self.assertIn("kind: Deployment", files["apps/inventory/workload/overlays/dev/rollout.yaml"])
        self.assertIn("kind: Deployment", files["apps/inventory/workload/overlays/staging/rollout.yaml"])
        self.assertIn("kind: Rollout", files["apps/inventory/workload/overlays/prod/rollout.yaml"])
        self.assertIn("value: inventory-dev", files["apps/inventory/workload/overlays/dev/kustomization.yaml"])
        self.assertIn("value: inventory-prod.apps.internal", files["apps/inventory/workload/overlays/prod/kustomization.yaml"])

    def test_bronze_omits_analysis_template_from_kustomization(self) -> None:
        req = parse_service_request(self.request(service_name="Catalog", slo_class="bronze"))
        files = build_gitops_files(req)
        self.assertNotIn("analysis-template.yaml", files["apps/catalog/workload/base/kustomization.yaml"])
        self.assertIn("direct cutover", files["apps/catalog/workload/base/analysis-template.yaml"])


if __name__ == "__main__":
    unittest.main()
