#!/usr/bin/env python3
"""
US-V3.1-01 / FR-V3.1-05 — Catalogue completeness lint.

The top-of-file catalogue in terraform/keyvaults.tf MUST list every platform
azurerm_key_vault_secret and azurerm_key_vault_certificate name declared
anywhere under terraform/*.tf. This script enforces the round-trip:

  - Every Terraform-declared name must appear in the catalogue header.
  - Every catalogue name must map to a real Terraform resource.

Exit codes:
  0 — catalogue is in sync.
  1 — catalogue is out of sync; emits GitHub Actions error annotations.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

TERRAFORM_DIR = Path(__file__).resolve().parent.parent / "terraform"
CATALOGUE_FILE = TERRAFORM_DIR / "keyvaults.tf"

NAME_ATTR_RE = re.compile(r'^\s*name\s*=\s*"([^"]+)"', re.MULTILINE)
SECRET_OR_CERT_RE = re.compile(
    r'resource\s+"azurerm_key_vault_(?:secret|certificate)"\s+"[^"]+"\s*\{(.*?)\n\}',
    re.DOTALL,
)
CATALOGUE_START_RE = re.compile(r"^# ── Certificates", re.MULTILINE)
CATALOGUE_END_RE = re.compile(r"^# CI assertions:", re.MULTILINE)


def extract_terraform_names() -> set[str]:
    names: set[str] = set()
    for tf in sorted(TERRAFORM_DIR.glob("*.tf")):
        text = tf.read_text(encoding="utf-8")
        # Strip comments so we don't pick up catalogue entries as resources.
        text_no_comments = re.sub(r"#[^\n]*", "", text)
        for block in SECRET_OR_CERT_RE.finditer(text_no_comments):
            body = block.group(1)
            m = NAME_ATTR_RE.search(body)
            if m:
                names.add(m.group(1))
    return names


def extract_catalogue_names() -> set[str]:
    text = CATALOGUE_FILE.read_text(encoding="utf-8")
    start = CATALOGUE_START_RE.search(text)
    end = CATALOGUE_END_RE.search(text)
    if not start or not end:
        print("::error::Catalogue header markers not found in terraform/keyvaults.tf "
              "(expected '# ── Certificates' and '# CI assertions:')")
        sys.exit(1)
    block = text[start.start():end.start()]
    names: set[str] = set()
    for line in block.splitlines():
        stripped = line.strip()
        if not stripped.startswith("#"):
            continue
        body = stripped.lstrip("#").strip()
        if not body or body.startswith("──") or body.startswith("Certificates") or body.startswith("Platform-generated"):
            continue
        token = body.split()[0] if body.split() else ""
        # Catalogue entries start with the AKV object name (kebab-case).
        if re.match(r"^[a-z][a-z0-9-]+$", token):
            names.add(token)
    return names


def main() -> int:
    tf_names = extract_terraform_names()
    cat_names = extract_catalogue_names()

    missing_from_catalogue = sorted(tf_names - cat_names)
    extra_in_catalogue = sorted(cat_names - tf_names)

    if not missing_from_catalogue and not extra_in_catalogue:
        print(f"[PASS] Catalogue is in sync with Terraform ({len(tf_names)} entries).")
        return 0

    rel = CATALOGUE_FILE.relative_to(TERRAFORM_DIR.parent)

    for name in missing_from_catalogue:
        print(
            f"::error file={rel}::AKV resource '{name}' is declared in Terraform "
            f"but missing from the catalogue header (US-V3.1-01 FR-V3.1-05). "
            f"Add a row under the appropriate section of terraform/keyvaults.tf."
        )
    for name in extra_in_catalogue:
        print(
            f"::error file={rel}::Catalogue lists '{name}' but no matching "
            f"azurerm_key_vault_secret/certificate resource exists. Remove the "
            f"catalogue row or restore the resource."
        )

    print(
        f"[FAIL] Catalogue drift: {len(missing_from_catalogue)} missing, "
        f"{len(extra_in_catalogue)} stale."
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
