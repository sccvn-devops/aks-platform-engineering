#!/usr/bin/env python3
"""Rule config for v4 F-003.

Config only — the logic lives in route.py. Add paths to EXTRA as the feature's
document set grows, then re-run this file to regenerate the JSON.

    python rule_v4_F-003.py
"""
from route import build, emit

VERSION = "v4"
FEATURE_ID = "F-003"
FEATURE_TITLE = "Platform invariant gates"

EXTRA = {
    "domain": ["docs/IDPGitOps-Specs/ddd/domain_DOM-003-platform-invariant-gates.md"],
    "adrs": [
        "docs/IDPGitOps-Specs/ADRs/adrs_ADR-0001-record-architecture-decisions.md",
        # Binding text for ADR-023-v3, ADR-029-v3 and ADR-031-v4 — this hub cites, never restates.
        "_docs/IDP-GitOps-ADRs-v2.md",
    ],
    "architecture": ["docs/IDPGitOps-Specs/architect/architect.md"],
    "runbooks": [
        "docs/IDPGitOps-Specs/runbook/runbook_DEV_RB-001-local-setup.md",
    ],
    "tests": ["docs/IDPGitOps-Specs/tests/test_v4_F-003.md"],
}

if __name__ == "__main__":
    emit(build(VERSION, FEATURE_ID, FEATURE_TITLE, EXTRA))
