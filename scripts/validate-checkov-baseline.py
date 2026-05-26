#!/usr/bin/env python3
# validate-checkov-baseline.py — enforce per-finding metadata on .checkov.baseline
# (US-V4-11, FR-V4-47).
#
# Every entry under results.skipped_checks MUST carry the three fields declared
# by results.<no>… actually by the top-level `_schema.metadata_required_per_finding`
# block.  Default required set: rationale, owner, expiry (YYYY-MM-DD, future).
#
# Exits 0 when the baseline is well-formed; exits 1 on the first violation
# (printing GitHub Actions error annotations so the failing finding is linked
# inline on the PR).
#
# Run locally:
#   python3 scripts/validate-checkov-baseline.py            # default path
#   python3 scripts/validate-checkov-baseline.py path/to/baseline
#
# Schema: docs/checkov-baseline-metadata-schema.md

from __future__ import annotations

import datetime as _dt
import json
import os
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_BASELINE = REPO_ROOT / ".checkov.baseline"
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
DEFAULT_REQUIRED = ("rationale", "owner", "expiry")


def _annotate(level: str, file: str, message: str) -> None:
    """Emit a GitHub Actions workflow annotation when running in CI."""
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::{level} file={file}::{message}")
    else:
        print(f"[{level}] {file}: {message}")


def _parse_expiry(value: str) -> _dt.date | None:
    if not DATE_RE.match(value):
        return None
    try:
        return _dt.date.fromisoformat(value)
    except ValueError:
        return None


def validate(baseline_path: Path) -> int:
    if not baseline_path.exists():
        _annotate("error", str(baseline_path), "baseline file not found")
        return 1

    try:
        data = json.loads(baseline_path.read_text())
    except json.JSONDecodeError as exc:
        _annotate("error", str(baseline_path), f"invalid JSON: {exc}")
        return 1

    schema = data.get("_schema", {}) or {}
    required = tuple(schema.get("metadata_required_per_finding") or DEFAULT_REQUIRED)

    results = data.get("results", {}) or {}
    skipped = results.get("skipped_checks") or []

    today = _dt.date.today()
    violations = 0

    for idx, entry in enumerate(skipped):
        if not isinstance(entry, dict):
            _annotate(
                "error",
                str(baseline_path),
                f"skipped_checks[{idx}] is not an object",
            )
            violations += 1
            continue

        check_id = entry.get("check_id", "<no check_id>")
        resource = entry.get("resource", "<no resource>")
        location = f"{check_id} on {resource}"

        for field in required:
            value = entry.get(field)
            if value is None or (isinstance(value, str) and not value.strip()):
                _annotate(
                    "error",
                    str(baseline_path),
                    f"skipped_checks[{idx}] ({location}): missing required metadata field `{field}` — see docs/checkov-baseline-metadata-schema.md",
                )
                violations += 1

        expiry_raw = entry.get("expiry")
        if isinstance(expiry_raw, str) and expiry_raw.strip():
            expiry = _parse_expiry(expiry_raw)
            if expiry is None:
                _annotate(
                    "error",
                    str(baseline_path),
                    f"skipped_checks[{idx}] ({location}): expiry `{expiry_raw}` is not YYYY-MM-DD",
                )
                violations += 1
            elif expiry < today:
                _annotate(
                    "error",
                    str(baseline_path),
                    f"skipped_checks[{idx}] ({location}): expiry {expiry_raw} is in the past — refresh the suppression or remove it",
                )
                violations += 1

    if violations:
        print(
            f"\n{baseline_path}: {violations} violation(s). "
            "Fix the entries above (see docs/checkov-baseline-metadata-schema.md) and re-run.",
            file=sys.stderr,
        )
        return 1

    print(
        f"{baseline_path}: OK — {len(skipped)} suppression(s), required fields {list(required)}, none expired."
    )
    return 0


def main(argv: list[str]) -> int:
    path = Path(argv[1]) if len(argv) > 1 else DEFAULT_BASELINE
    return validate(path)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
