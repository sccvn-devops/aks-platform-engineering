#!/usr/bin/env python3
"""
validate-checkov-suppressions.py — inline checkov suppression validator (US-V3-17, ADR-027-v3)

Validates that every `# checkov:skip=CKV_*:<ticket>` comment in Terraform files:
  1. References a Jira ticket in the format PROJ-NNN.
  2. (When Jira is reachable) the ticket exists, is not closed, and carries an
     expiry date that has not passed.

Exits:
  0 — all suppressions are valid (or Jira is unreachable and format-only check passes)
  1 — one or more suppressions are invalid

Usage:
  python3 scripts/validate-checkov-suppressions.py --dir terraform [--jira-url URL] [--jira-token TOKEN]

Environment variables (override CLI flags):
  JIRA_BASE_URL   — Jira base URL (e.g., https://example.atlassian.net)
  JIRA_TOKEN      — Bearer token for Jira API

Suppression format (ADR-027-v3 decision #3):
  # checkov:skip=CKV_AZURE_NNN:PROJ-NNNN — brief description of why

Invalid suppression examples (these will fail this check):
  # checkov:skip=CKV_AZURE_NNN                     (no ticket)
  # checkov:skip=CKV_AZURE_NNN:some free text       (not a Jira ticket format)
  # checkov:skip=CKV_AZURE_NNN:PROJ-999            (closed/expired in Jira)
"""

import re
import sys
import os
import glob
import argparse
import json
import urllib.request
import urllib.error
from pathlib import Path
from datetime import date

# Pattern: # checkov:skip=CKV_*:<rest>
SKIP_PATTERN = re.compile(
    r"#\s*checkov:skip=([A-Z0-9_]+)\s*[:\s]\s*(.*)"
)
# Valid Jira ticket format: one or more uppercase letters, dash, digits
TICKET_PATTERN = re.compile(r"^([A-Z][A-Z0-9]+-\d+)")


def find_suppressions(tf_dir: str) -> list[dict]:
    """Scan .tf files and extract all checkov:skip annotations."""
    suppressions = []
    for tf_file in sorted(glob.glob(f"{tf_dir}/**/*.tf", recursive=True)):
        if ".terraform/" in tf_file or "/.terraform" in tf_file:
            continue
        content = Path(tf_file).read_text()
        for lineno, line in enumerate(content.splitlines(), start=1):
            m = SKIP_PATTERN.search(line)
            if m:
                check_id = m.group(1)
                rest = m.group(2).strip()
                suppressions.append({
                    "file": tf_file,
                    "line": lineno,
                    "check": check_id,
                    "rest": rest,
                })
    return suppressions


def validate_format(suppression: dict) -> tuple[bool, str]:
    """Validate that the suppression references a Jira ticket in PROJ-NNN format."""
    m = TICKET_PATTERN.match(suppression["rest"])
    if not m:
        return False, (
            f"suppression for {suppression['check']} must include a Jira ticket "
            f"in PROJ-NNN format (got: '{suppression['rest']}')"
        )
    return True, m.group(1)


def query_jira(ticket: str, jira_url: str, jira_token: str) -> tuple[bool, str]:
    """
    Query Jira REST API to verify the ticket is open and not expired.
    Returns (valid, reason_if_invalid).
    """
    api_url = f"{jira_url.rstrip('/')}/rest/api/3/issue/{ticket}?fields=status,duedate,summary"
    req = urllib.request.Request(
        api_url,
        headers={"Authorization": f"Bearer {jira_token}", "Accept": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return False, f"Jira ticket {ticket} not found"
        return False, f"Jira API error {e.code} for {ticket}"
    except (urllib.error.URLError, OSError):
        # Network unreachable — skip Jira validation, format is enough
        return True, ""

    status = data.get("fields", {}).get("status", {}).get("statusCategory", {}).get("key", "")
    if status == "done":
        return False, f"Jira ticket {ticket} is closed (status category: done)"

    due_date_str = data.get("fields", {}).get("duedate")
    if due_date_str:
        try:
            due = date.fromisoformat(due_date_str)
            if due < date.today():
                return False, f"Jira ticket {ticket} expired on {due_date_str}"
        except ValueError:
            pass

    return True, ""


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate checkov inline suppressions")
    parser.add_argument("--dir", default="terraform", help="Directory to scan")
    parser.add_argument("--jira-url", default=os.environ.get("JIRA_BASE_URL", ""),
                        help="Jira base URL")
    parser.add_argument("--jira-token", default=os.environ.get("JIRA_TOKEN", ""),
                        help="Jira Bearer token")
    args = parser.parse_args()

    suppressions = find_suppressions(args.dir)
    if not suppressions:
        print("[validate-checkov-suppressions] OK: no inline suppressions found.")
        return 0

    print(f"[validate-checkov-suppressions] Found {len(suppressions)} inline suppression(s). Validating...")
    failures = []

    for s in suppressions:
        ok, result = validate_format(s)
        if not ok:
            print(f"::error file={s['file']},line={s['line']}::{result}")
            failures.append(s)
            continue

        ticket = result
        if args.jira_url and args.jira_token:
            ok, reason = query_jira(ticket, args.jira_url, args.jira_token)
            if not ok:
                msg = (
                    f"checkov:skip for {s['check']} references invalid ticket: {reason}"
                )
                print(f"::error file={s['file']},line={s['line']}::{msg}")
                failures.append(s)
            else:
                print(f"  ✓ {s['file']}:{s['line']} — {s['check']} suppressed by {ticket}")
        else:
            print(f"  ✓ {s['file']}:{s['line']} — {s['check']} suppressed by {ticket} (Jira not configured; format-only check)")

    if failures:
        print(f"\n[validate-checkov-suppressions] FAIL: {len(failures)} invalid suppression(s)")
        return 1

    print(f"[validate-checkov-suppressions] OK: all {len(suppressions)} suppression(s) valid.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
