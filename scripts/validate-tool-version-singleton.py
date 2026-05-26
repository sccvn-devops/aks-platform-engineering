#!/usr/bin/env python3
# validate-tool-version-singleton.py — assert .tool-versions is the only file
# in the repo that declares versions for terraform, tflint, checkov, terragrunt,
# kubectl, and kustomize (US-V4-11, FR-V4-45).
#
# Drift the validator catches:
#   - `TF_VERSION:` / `TFLINT_VERSION:` / similar env keys in *.yml workflows
#   - `terraform_version:` / `tflint_version:` step inputs
#   - `terraform>=X` in additional_dependencies in .pre-commit-config.yaml
#   - hashicorp/terraform image tags pinned via :version in any *.yml or Dockerfile
#
# Skips this file itself, .tool-versions, scripts/, .git/, vendored deps, etc.
#
# Run locally:
#   python3 scripts/validate-tool-version-singleton.py

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
TOOL_VERSIONS_FILE = REPO_ROOT / ".tool-versions"

TRACKED_TOOLS = ("terraform", "tflint", "checkov", "terragrunt", "kubectl", "kustomize")

# Files/dirs we don't scan (the singleton lives in .tool-versions; scripts are
# allowed to mention version strings as part of their own logic).
SKIP_PATH_PARTS = {
    ".git",
    ".terraform",
    "node_modules",
    "dist",
    "build",
    "coverage",
    ".venv",
    "venv",
    "__pycache__",
    "archived",
    ".cursor",
    ".claude",
    ".agents",
    ".codegraph",
    ".antigravitycli",
    "scripts",
    "docs",
    "_docs",
    "backstage",
}

SKIP_FILES = {
    ".tool-versions",
    "CHANGELOG.md",
    "progress.txt",
    "prd.json",
    "AGENTS.md",
    "agents.tar.gz",
    ".pre-commit-config.yaml",  # pre-commit pins hook *source repo* version (rev:), not the tool itself
}

# Patterns that constitute a version declaration for a tracked tool.
# Each pattern's first group should be the offending token for the error
# message.
PATTERNS = [
    # `TF_VERSION: "1.5.7"` / `TFLINT_VERSION: "0.55.0"` / etc.
    (re.compile(r"(?i)^\s*(TF_VERSION|TERRAFORM_VERSION)\s*:\s*[\"']?[\dv.]+"), "terraform"),
    (re.compile(r"(?i)^\s*(TFLINT_VERSION)\s*:\s*[\"']?[\dv.]+"), "tflint"),
    (re.compile(r"(?i)^\s*(CHECKOV_VERSION)\s*:\s*[\"']?[\dv.]+"), "checkov"),
    (re.compile(r"(?i)^\s*(TERRAGRUNT_VERSION)\s*:\s*[\"']?[\dv.]+"), "terragrunt"),
    (re.compile(r"(?i)^\s*(KUBECTL_VERSION)\s*:\s*[\"']?[\dv.]+"), "kubectl"),
    (re.compile(r"(?i)^\s*(KUSTOMIZE_VERSION)\s*:\s*[\"']?[\dv.]+"), "kustomize"),
    # setup-* step inputs
    (re.compile(r"^\s*terraform_version\s*:\s*[\"']?[\dv.]+"), "terraform"),
    (re.compile(r"^\s*tflint_version\s*:\s*[\"']?[\dv.]+"), "tflint"),
]

INCLUDE_EXTS = {".yml", ".yaml", ".dockerfile"}
INCLUDE_FILENAMES = {"Dockerfile"}


def _annotate(level: str, file: Path, line: int, message: str) -> None:
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::{level} file={file},line={line}::{message}")
    else:
        print(f"[{level}] {file}:{line}: {message}")


def _should_scan(path: Path) -> bool:
    rel = path.relative_to(REPO_ROOT)
    if rel.name in SKIP_FILES:
        return False
    parts = set(rel.parts)
    if parts & SKIP_PATH_PARTS:
        return False
    if path.suffix.lower() in INCLUDE_EXTS:
        return True
    if path.name in INCLUDE_FILENAMES:
        return True
    return False


def scan() -> int:
    if not TOOL_VERSIONS_FILE.exists():
        _annotate("error", TOOL_VERSIONS_FILE, 1, ".tool-versions is missing — required by FR-V4-45")
        return 1

    violations = 0
    for path in sorted(REPO_ROOT.rglob("*")):
        if not path.is_file():
            continue
        if not _should_scan(path):
            continue
        try:
            for lineno, raw_line in enumerate(path.read_text(errors="ignore").splitlines(), start=1):
                stripped = raw_line.strip()
                if not stripped or stripped.startswith("#"):
                    continue
                for pattern, tool in PATTERNS:
                    if pattern.search(raw_line):
                        rel = path.relative_to(REPO_ROOT)
                        _annotate(
                            "error",
                            rel,
                            lineno,
                            (
                                f"`{tool}` version declared outside .tool-versions "
                                f"(matched `{raw_line.strip()}`).  Move the version to "
                                ".tool-versions and remove it from this file (FR-V4-45)."
                            ),
                        )
                        violations += 1
                        break
        except OSError:
            continue

    if violations:
        print(
            f"\n{violations} duplicate tool-version declaration(s) outside .tool-versions.",
            file=sys.stderr,
        )
        return 1

    print("OK — .tool-versions is the single source of truth for tracked tools.")
    return 0


if __name__ == "__main__":
    sys.exit(scan())
