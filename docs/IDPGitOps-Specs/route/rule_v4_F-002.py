#!/usr/bin/env python3
"""Rule config for v4 F-002.

Config only — the logic lives in route.py. Add paths to EXTRA as the feature's
document set grows, then re-run this file to regenerate the JSON.

    python rule_v4_F-002.py
"""
from route import build, emit

VERSION = "v4"
FEATURE_ID = "F-002"
FEATURE_TITLE = "Management plane arbitration"

EXTRA = {
    "domain": ["docs/IDPGitOps-Specs/ddd/domain_DOM-002-management-plane-arbitration.md"],
    "adrs": [
        "docs/IDPGitOps-Specs/ADRs/adrs_ADR-0001-record-architecture-decisions.md",
        # Binding text for ADR-004-v2, ADR-017 and ADR-022 — this hub cites, never restates.
        "_docs/IDP-GitOps-ADRs-v2.md",
    ],
    "architecture": ["docs/IDPGitOps-Specs/architect/architect.md"],
    "runbooks": [
        "docs/IDPGitOps-Specs/runbook/runbook_DEV_RB-001-local-setup.md",
        "docs/management-plane-failover-runbook.md",
    ],
    "tests": ["docs/IDPGitOps-Specs/tests/test_v4_F-002.md"],
}

if __name__ == "__main__":
    emit(build(VERSION, FEATURE_ID, FEATURE_TITLE, EXTRA))
