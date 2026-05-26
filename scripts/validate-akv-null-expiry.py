#!/usr/bin/env python3
"""
US-V3.1-01 / FR-V3.1-03 — CI null-expiry assertion.

Scans terraform/*.tf for every azurerm_key_vault_secret resource and verifies
that an `expiration_date` attribute is declared. The Jenkins username secret is
explicitly allowlisted (a static identifier that does not rotate).

Sprint-1 (advisory): exit 0 even on findings; emit GitHub Actions warning
annotations and post the list as a PR comment.
Sprint-2 onward (blocking): set NULL_EXPIRY_MODE=block to exit 1 on findings.

Usage:
    NULL_EXPIRY_MODE=advisory python3 scripts/validate-akv-null-expiry.py
    NULL_EXPIRY_MODE=block    python3 scripts/validate-akv-null-expiry.py
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

TERRAFORM_DIR = Path(__file__).resolve().parent.parent / "terraform"

# Resources that legitimately do not rotate.
ALLOWLIST = {
    # Static username — not a credential; rotates via jenkins-admin-password instead.
    "jenkins_admin_username",
    # Public material derived from cosign-signing-key; rotation is driven by the
    # backing key's rotation_policy in jenkins.tf, not the secret itself.
    "cosign_public_key",
}

SECRET_BLOCK_RE = re.compile(
    r'resource\s+"azurerm_key_vault_secret"\s+"([^"]+)"\s*\{([^}]*?(?:\{[^}]*\}[^}]*?)*)\}',
    re.DOTALL,
)
EXPIRATION_DATE_RE = re.compile(r"\bexpiration_date\s*=", re.MULTILINE)


def find_secrets() -> list[tuple[Path, str, str]]:
    """Return list of (file, resource_label, body) for every azurerm_key_vault_secret."""
    results: list[tuple[Path, str, str]] = []
    for tf in sorted(TERRAFORM_DIR.glob("*.tf")):
        text = tf.read_text(encoding="utf-8")
        # Strip line comments to avoid false-positives inside catalogue commentary.
        text_no_comments = re.sub(r"#[^\n]*", "", text)
        for match in SECRET_BLOCK_RE.finditer(text_no_comments):
            label = match.group(1)
            body = match.group(2)
            results.append((tf, label, body))
    return results


def main() -> int:
    mode = os.environ.get("NULL_EXPIRY_MODE", "advisory").lower()
    if mode not in {"advisory", "block"}:
        print(f"NULL_EXPIRY_MODE must be 'advisory' or 'block' (got: {mode!r})", file=sys.stderr)
        return 2

    findings: list[tuple[Path, str]] = []
    total = 0
    for tf, label, body in find_secrets():
        total += 1
        if label in ALLOWLIST:
            continue
        if not EXPIRATION_DATE_RE.search(body):
            findings.append((tf, label))

    print(f"Scanned {total} azurerm_key_vault_secret resources across {TERRAFORM_DIR}")
    if not findings:
        print("[PASS] Every non-allowlisted secret declares expiration_date.")
        return 0

    print(f"[FINDINGS] {len(findings)} secret(s) missing expiration_date:")
    for tf, label in findings:
        rel = tf.relative_to(TERRAFORM_DIR.parent)
        # GitHub Actions annotation (warning or error depending on mode).
        annotation = "error" if mode == "block" else "warning"
        print(
            f"::{annotation} file={rel}::"
            f"azurerm_key_vault_secret.{label} is missing expiration_date "
            f"(US-V3.1-01 FR-V3.1-02). Add expiration_date = local.platform_secret_expiry_rfc3339 "
            f"or add to ALLOWLIST in scripts/validate-akv-null-expiry.py with rationale."
        )

    if mode == "block":
        print(f"[FAIL] {len(findings)} findings — failing because NULL_EXPIRY_MODE=block.")
        return 1

    print(
        f"[WARN] {len(findings)} findings — advisory mode (NULL_EXPIRY_MODE=advisory); "
        f"flip to block at sprint-2 cutover per US-V3.1-01 AC3."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
