#!/usr/bin/env python3
# validate-action-pins.py — assert every `uses:` in .github/workflows/*.yml
# is pinned to a 40-character commit SHA with a human-readable tag comment.
# (US-V4-11, FR-V4-46.)
#
# Acceptable forms (one per line):
#   uses: owner/repo@<40-hex-sha>      # v1.2.3
#   uses: owner/repo/path@<40-hex-sha> # v1.2.3
#
# Rejects:
#   uses: owner/repo@v1                — float tag, not pinned
#   uses: owner/repo@main              — branch ref
#   uses: owner/repo@abcdef            — short SHA
#   uses: owner/repo@<sha>             — no tag comment
#   uses: ./.github/actions/local      — local action (allowed; no pin needed)
#   uses: docker://image:tag           — container ref (allowed; pin via SHA in image tag separately)
#
# Exits 0 if clean.  Exits 1 with GitHub Actions error annotations on the first
# violation.
#
# Run locally:
#   python3 scripts/validate-action-pins.py
#   python3 scripts/validate-action-pins.py path/to/specific.yml

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
WORKFLOWS_DIR = REPO_ROOT / ".github" / "workflows"

# Matches `uses: <ref>` and captures the ref string + trailing comment (if any).
USES_RE = re.compile(
    r"""
    ^\s*-?\s*           # optional list marker / leading whitespace
    uses:\s*            # the key
    (?P<ref>\S+)        # the reference (no whitespace)
    (?:\s+\#\s*(?P<comment>.*?))?   # optional trailing comment
    \s*$
    """,
    re.VERBOSE,
)
SHA_RE = re.compile(r"^[a-f0-9]{40}$")
TAG_RE = re.compile(r"^v?\d+(?:\.\d+){0,3}(?:[-+.][\w.]+)?$")


def _annotate(level: str, file: Path, line: int, message: str) -> None:
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::{level} file={file},line={line}::{message}")
    else:
        print(f"[{level}] {file}:{line}: {message}")


def _is_local_or_docker(ref: str) -> bool:
    return ref.startswith("./") or ref.startswith("docker://")


def _split_ref(ref: str) -> tuple[str, str] | None:
    # Strip optional quoting (YAML allows quoted scalars).
    ref = ref.strip().strip("'\"")
    if "@" not in ref:
        return None
    name, sep, rev = ref.partition("@")
    if not sep:
        return None
    return name, rev


def validate_file(path: Path) -> int:
    violations = 0
    try:
        text = path.read_text()
    except OSError as exc:
        _annotate("error", path, 1, f"cannot read file: {exc}")
        return 1

    for lineno, raw_line in enumerate(text.splitlines(), start=1):
        # Skip lines inside scripts or strings — yaml allows `uses:` only at step level.
        match = USES_RE.match(raw_line)
        if not match:
            continue
        ref = match.group("ref")
        comment = (match.group("comment") or "").strip()

        if _is_local_or_docker(ref):
            continue

        split = _split_ref(ref)
        if split is None:
            _annotate(
                "error",
                path,
                lineno,
                f"`uses: {ref}` does not include `@<sha>` — pin to a 40-char commit SHA with a tag comment",
            )
            violations += 1
            continue

        action, rev = split

        if not SHA_RE.match(rev):
            _annotate(
                "error",
                path,
                lineno,
                f"`uses: {action}@{rev}` is not a 40-character commit SHA (FR-V4-46 requires SHA pinning).  Fix: replace with the commit SHA and put the tag in a trailing comment.",
            )
            violations += 1
            continue

        if not comment:
            _annotate(
                "error",
                path,
                lineno,
                f"`uses: {action}@{rev}` is SHA-pinned but missing a tag comment.  Add `  # vX.Y.Z` so Dependabot can bump SHA + comment together.",
            )
            violations += 1
            continue

        if not TAG_RE.match(comment.split()[0]):
            _annotate(
                "warning",
                path,
                lineno,
                f"`uses: {action}@{rev}` trailing comment `{comment}` does not look like a semver tag (vX.Y.Z).  Dependabot may not be able to bump it.",
            )

    return violations


def main(argv: list[str]) -> int:
    if len(argv) > 1:
        targets = [Path(p) for p in argv[1:]]
    else:
        if not WORKFLOWS_DIR.is_dir():
            print(f"{WORKFLOWS_DIR}: no such directory", file=sys.stderr)
            return 1
        targets = sorted(p for p in WORKFLOWS_DIR.glob("*.y*ml"))

    if not targets:
        print("No workflow files to validate.", file=sys.stderr)
        return 0

    total = 0
    for path in targets:
        total += validate_file(path)

    if total:
        print(
            f"\n{total} action-pin violation(s) across {len(targets)} workflow file(s).",
            file=sys.stderr,
        )
        return 1

    print(f"OK — {len(targets)} workflow file(s) checked, all `uses:` are SHA-pinned with tag comments.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
