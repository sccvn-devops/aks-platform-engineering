# .checkov.baseline metadata schema (US-V4-11, FR-V4-47)

The baseline file at `.checkov.baseline` is consumed by Checkov via
`--baseline .checkov.baseline` and by `scripts/validate-checkov-baseline.py` in
CI.  Every entry under `results.skipped_checks` MUST carry the three metadata
fields below, or CI fails the PR.

## Required metadata fields per finding

| Field       | Type                   | Meaning                                                                  |
|-------------|------------------------|--------------------------------------------------------------------------|
| `rationale` | string (non-empty)     | Why this finding is suppressed.  One sentence; reviewers read this.      |
| `owner`     | string (non-empty)     | GitHub team or @-handle responsible for re-evaluating before expiry.     |
| `expiry`    | string (`YYYY-MM-DD`)  | Calendar date after which the suppression auto-fails the PR (UTC).      |

## Schema header

The top of the baseline carries a `_schema` block.  Checkov ignores unknown
top-level keys; the validator reads `_schema.metadata_required_per_finding` so
the schema can be evolved without editing the validator.

## Example

```json
{
  "_schema": {
    "version": "us-v4-11",
    "doc": "docs/checkov-baseline-metadata-schema.md",
    "validator": "scripts/validate-checkov-baseline.py",
    "metadata_required_per_finding": ["rationale", "owner", "expiry"]
  },
  "results": {
    "passed_checks": [],
    "failed_checks": [],
    "skipped_checks": [
      {
        "file": "/terraform/storage.tf",
        "check_id": "CKV_AZURE_206",
        "resource": "azurerm_storage_account.state_lease",
        "rationale": "GZRS is overkill for the lease container; LRS is the documented choice in ADR-022 (singleton blob lease, regional failover via mgmt-ne promotion).",
        "owner": "@sccvn-devops/platform-team",
        "expiry": "2026-09-30"
      }
    ],
    "parsing_errors": []
  }
}
```

## Lifecycle

1. A reviewer accepts a checkov finding into the baseline only after attaching
   rationale, owner, and an expiry date (max 90 days out per ADR-027-v3 spirit).
2. `scripts/validate-checkov-baseline.py` runs on every PR.  If any
   `skipped_checks` entry is missing a required field or `expiry < today`, the
   job fails with a pointer to the offending entry.
3. Before expiry, the owner either fixes the underlying finding, files a fresh
   Jira ticket and extends `expiry`, or removes the suppression.

## Why not inline `checkov:skip` comments?

Inline suppressions are still supported (see
`scripts/validate-checkov-suppressions.py`).  They suit transient skips during
active work.  The baseline is for findings that need a paper trail and a
calendar deadline.  Both paths run in CI.
