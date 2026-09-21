#!/usr/bin/env python3
"""Rule config for v4 F-004.

Config only — the logic lives in route.py. Add paths to EXTRA as the feature's
document set grows, then re-run this file to regenerate the JSON.

    python rule_v4_F-004.py
"""
from route import build, emit

VERSION = "v4"
FEATURE_ID = "F-004"
FEATURE_TITLE = "Secret and token lifecycle"

EXTRA = {
    "domain": ["docs/IDPGitOps-Specs/ddd/domain_DOM-004-secret-and-token-lifecycle.md"],
    "adrs": [
        "docs/IDPGitOps-Specs/ADRs/adrs_ADR-0001-record-architecture-decisions.md",
        # Binding text for ADR-005-v2, ADR-006-v2, ADR-019 and ADR-020 — this hub cites, never restates.
        "_docs/IDP-GitOps-ADRs-v2.md",
    ],
    "architecture": ["docs/IDPGitOps-Specs/architect/architect.md"],
    "runbooks": [
        "docs/IDPGitOps-Specs/runbook/runbook_DEV_RB-001-local-setup.md",
        "docs/akv-near-expiry-runbook.md",
    ],
    "tests": ["docs/IDPGitOps-Specs/tests/test_v4_F-004.md"],
}

# The write surface this feature owns, confirmed against the tree by
# `product-docs-flow revise` on 2026-09-21. It is set explicitly rather than left
# to `drift --apply`: this hub's masters (data-master-erd.md, schemas.json,
# architect_common.md, status-model.md, testing_strategy.md) cite every feature's
# code, so a mechanical sweep over F-004's route claims F-001, F-002 and F-003
# files as well — and `writes` is what bounds `implement` and scopes `data`.
WRITES = {
    "code": [
        "tools/mgmt-plane-lock/cmd/saas-token-rotator/main.go",
        "tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go",
        "tools/mgmt-plane-lock/cmd/saas-token-rotator/wiring.go",
        "tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go",
        "tools/mgmt-plane-lock/internal/rotation/rotation.go",
        "tools/mgmt-plane-lock/internal/httpx/httpx.go",
        "scripts/validate-akv-catalogue.py",
        "scripts/validate-akv-null-expiry.py",
        "terraform/keyvaults.tf",
        "terraform/locals.tf",
        "terraform/akv_alerts.tf",
        "terraform/akv_sync_exporter.tf",
        "gitops/platform/saas-token-rotator/",
        "gitops/platform/akv-sync-exporter/",
    ],
    "tests": [
        "tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go",
        "tools/mgmt-plane-lock/internal/akvwriter/fake_test.go",
        "tools/mgmt-plane-lock/internal/httpx/httpx_test.go",
        "tools/mgmt-plane-lock/internal/rotation/rotation_test.go",
    ],
}

if __name__ == "__main__":
    rule = build(VERSION, FEATURE_ID, FEATURE_TITLE, EXTRA)
    rule["writes"] = WRITES
    emit(rule)
