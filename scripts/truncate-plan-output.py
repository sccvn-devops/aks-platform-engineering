#!/usr/bin/env python3
# truncate-plan-output.py — single source of truth for terraform plan
# truncation (US-V4-03, FR-V4-13).
#
# The reusable plan workflow (.github/workflows/reusable/terraform-plan.yml)
# shells out to this script when posting a PR comment.  Any other caller that
# wants to truncate a plan must also shell out here, NOT re-implement the
# truncation inline.  FR-V4-13: "Plan-output truncation logic must exist in
# exactly one place, at one threshold (60KB)."
#
# Behaviour:
#   * Reads the raw plan from --input (file path) or stdin.
#   * If <= MAX_BYTES (default 60_000): emit unchanged.
#   * Else: emit head(MAX_BYTES) + a truncation footer pointing at --artifact-url.
#
# Exit codes:
#   0  always when invocation is well-formed (truncation is not an error)
#   2  on argument / IO error.
#
# The threshold lives in this file as MAX_BYTES; the workflow MUST NOT override
# it via flag (single threshold per FR-V4-13).  Callers may pass --artifact-url
# (link to the full plan artifact) which is embedded in the footer.
#
# Run locally:
#   python3 scripts/truncate-plan-output.py --input plan.txt --artifact-url ...
#   cat plan.txt | python3 scripts/truncate-plan-output.py --artifact-url ...

from __future__ import annotations

import argparse
import sys
from pathlib import Path

# FR-V4-13: one threshold, defined here, immutable from CLI.
MAX_BYTES = 60_000

TRUNCATION_FOOTER_TEMPLATE = (
    "\n\n... (output truncated at {max_bytes} bytes — "
    "see full plan artifact: {artifact_url})"
)
TRUNCATION_FOOTER_NO_LINK = "\n\n... (output truncated at {max_bytes} bytes)"


def truncate(plan_text: str, artifact_url: str | None = None) -> str:
    """Return plan_text unchanged if <= MAX_BYTES, else truncate + footer.

    FR-V4-13 single-threshold contract: callers MUST NOT pass a custom limit.
    """
    if len(plan_text) <= MAX_BYTES:
        return plan_text
    if artifact_url:
        footer = TRUNCATION_FOOTER_TEMPLATE.format(
            max_bytes=MAX_BYTES, artifact_url=artifact_url
        )
    else:
        footer = TRUNCATION_FOOTER_NO_LINK.format(max_bytes=MAX_BYTES)
    # Reserve room for the footer so the combined output stays within ~MAX_BYTES.
    head = plan_text[: MAX_BYTES - len(footer)]
    return head + footer


def _read_input(path: str | None) -> str:
    if path is None or path == "-":
        return sys.stdin.read()
    return Path(path).read_text(encoding="utf-8", errors="replace")


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Truncate terraform plan output for PR-comment posting (FR-V4-13).",
    )
    parser.add_argument(
        "--input",
        help="Path to plan file. If omitted or '-', reads stdin.",
        default=None,
    )
    parser.add_argument(
        "--artifact-url",
        help="Link to the full-plan artifact, embedded in the truncation footer.",
        default=None,
    )
    args = parser.parse_args(argv[1:])

    try:
        plan_text = _read_input(args.input)
    except OSError as exc:
        print(f"truncate-plan-output: cannot read input: {exc}", file=sys.stderr)
        return 2

    sys.stdout.write(truncate(plan_text, artifact_url=args.artifact_url))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
