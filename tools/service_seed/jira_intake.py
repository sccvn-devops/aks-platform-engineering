"""Jira intake for service_seed (US-V4-07, FR-V4-27..31).

Single owner of the *parse* concern: Jira REST fetch + ADF flattening + free-text
inference + dataclass construction.  Replaces the parse-related slice of the
legacy ``seed_job.py`` module.

Public surface:
    ServiceRequest      Frozen dataclass with ``__post_init__`` validation.
    JiraIntakeError     Typed exception raised by :func:`parse_service_request`.
    parse_service_request(...)  Build a ServiceRequest from a Jira REST payload.
    jira_get_issue(...)         Fetch a Jira issue via HTTP.
    slugify(...), adf_to_text(...), infer_service_name(...), infer_slo_class(...)

FR-V4-43: every external call passes an explicit ``timeout`` (HTTP_TIMEOUT_S).
"""

from __future__ import annotations

import base64
import json
import re
from dataclasses import dataclass
from typing import Any
from urllib import parse, request

# FR-V4-43: kept here so jira_intake-only consumers don't re-import seed_job.
HTTP_TIMEOUT_S = 30

VALID_SLO_CLASSES = frozenset({"bronze", "silver", "gold"})
EXPECTED_ISSUE_TYPE = "IDP Service Request"

_SLUG_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")


class JiraIntakeError(ValueError):
    """Raised when a Jira payload cannot be parsed into a ServiceRequest.

    Carries the offending ``field`` name when applicable so the CLI can emit
    actionable messages (AC2: typed exception with the field name).
    """

    def __init__(self, message: str, *, field: str | None = None) -> None:
        super().__init__(message)
        self.field = field


@dataclass(frozen=True)
class ServiceRequest:
    """A validated service-seed request.

    Validation runs in ``__post_init__`` so the type guarantees its own
    invariants — callers cannot construct a partially-formed instance.
    """

    issue_key: str
    issue_type: str
    service_name: str
    service_slug: str
    slo_class: str
    summary: str
    description: str

    def __post_init__(self) -> None:
        for field_name in ("issue_key", "issue_type", "service_name", "service_slug", "slo_class"):
            value = getattr(self, field_name)
            if not isinstance(value, str) or not value.strip():
                raise JiraIntakeError(
                    f"ServiceRequest.{field_name} is required and must be non-empty",
                    field=field_name,
                )
        if self.issue_type != EXPECTED_ISSUE_TYPE:
            raise JiraIntakeError(
                f"unsupported Jira issue type {self.issue_type!r}; expected {EXPECTED_ISSUE_TYPE!r}",
                field="issue_type",
            )
        if self.slo_class not in VALID_SLO_CLASSES:
            raise JiraIntakeError(
                f"unsupported slo class {self.slo_class!r}; expected one of {sorted(VALID_SLO_CLASSES)}",
                field="slo_class",
            )
        if not _SLUG_RE.match(self.service_slug):
            raise JiraIntakeError(
                f"service_slug {self.service_slug!r} must match {_SLUG_RE.pattern}",
                field="service_slug",
            )


def slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")
    if not slug:
        raise JiraIntakeError("unable to derive service slug from empty value", field="service_slug")
    return slug


def adf_to_text(node: Any) -> str:
    if isinstance(node, str):
        return node
    if isinstance(node, list):
        return "\n".join(filter(None, (adf_to_text(item) for item in node)))
    if not isinstance(node, dict):
        return ""
    text = node.get("text", "")
    child_text = adf_to_text(node.get("content", []))
    parts = [part for part in [text, child_text] if part]
    if node.get("type") in {"paragraph", "heading", "bulletList", "orderedList", "listItem"}:
        return "\n".join(parts)
    return " ".join(parts).strip()


def infer_service_name(fields: dict[str, Any], summary: str, description: str) -> str:
    explicit_candidates = [
        fields.get("serviceName"),
        fields.get("service_name"),
        fields.get("customfield_service_name"),
        fields.get("customfield_10000"),
    ]
    for candidate in explicit_candidates:
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip()

    for label in fields.get("labels", []) or []:
        if isinstance(label, str) and label.startswith("service:"):
            return label.split(":", 1)[1].strip()

    for text in (summary, description):
        match = re.search(
            r"(?:service(?:\s+name)?|app(?:lication)?)\s*[:=-]\s*([A-Za-z0-9 _-]+)",
            text,
            re.IGNORECASE,
        )
        if match:
            return match.group(1).strip()

    return summary.strip()


def infer_slo_class(fields: dict[str, Any], summary: str, description: str) -> str:
    explicit_candidates = [
        fields.get("sloClass"),
        fields.get("slo_class"),
        fields.get("customfield_slo_class"),
        fields.get("customfield_10001"),
    ]
    for candidate in explicit_candidates:
        if isinstance(candidate, dict):
            candidate = candidate.get("value")
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip().lower()

    for label in fields.get("labels", []) or []:
        if isinstance(label, str) and label.startswith("slo:"):
            return label.split(":", 1)[1].strip().lower()

    for text in (summary, description):
        match = re.search(r"slo(?:\s+class)?\s*[:=-]\s*(bronze|silver|gold)", text, re.IGNORECASE)
        if match:
            return match.group(1).lower()

    return "silver"


def parse_service_request(
    issue: dict[str, Any],
    *,
    issue_key_override: str | None = None,
    issue_type_override: str | None = None,
    service_name_override: str | None = None,
    slo_class_override: str | None = None,
) -> ServiceRequest:
    """Build a validated :class:`ServiceRequest` from a Jira REST payload.

    Raises :class:`JiraIntakeError` (with ``field=<missing>``) when a required
    value is missing or invalid.  ``__post_init__`` enforces issue_type and
    slo_class constraints so callers cannot bypass validation by constructing
    the dataclass directly.
    """

    fields = issue.get("fields", {}) or {}
    issue_key = issue_key_override or issue.get("key") or ""
    issue_type = issue_type_override or (fields.get("issuetype") or {}).get("name") or ""
    summary = fields.get("summary") or issue_key
    description = adf_to_text(fields.get("description", ""))

    if not issue_key:
        raise JiraIntakeError("Jira issue is missing 'key'", field="issue_key")

    service_name = service_name_override or infer_service_name(fields, summary, description)
    if not service_name.strip():
        raise JiraIntakeError("Jira issue does not name a service", field="service_name")

    service_slug = slugify(service_name)
    slo_class = (slo_class_override or infer_slo_class(fields, summary, description)).lower()

    return ServiceRequest(
        issue_key=issue_key,
        issue_type=issue_type,
        service_name=service_name,
        service_slug=service_slug,
        slo_class=slo_class,
        summary=summary,
        description=description,
    )


def jira_get_issue(base_url: str, issue_key: str, email: str, token: str) -> dict[str, Any]:
    """Fetch a Jira issue via the v3 REST API.

    FR-V4-43: ``timeout=HTTP_TIMEOUT_S`` is passed to urlopen so a hung Jira
    endpoint returns within the declared bound rather than tying up the seed
    pipeline indefinitely.
    """
    req = request.Request(f"{base_url.rstrip('/')}/rest/api/3/issue/{parse.quote(issue_key)}")
    req.add_header("Accept", "application/json")
    if email:
        basic = base64.b64encode(f"{email}:{token}".encode("utf-8")).decode("ascii")
        req.add_header("Authorization", f"Basic {basic}")
    else:
        req.add_header("Authorization", f"Bearer {token}")

    with request.urlopen(req, timeout=HTTP_TIMEOUT_S) as resp:
        return json.loads(resp.read().decode("utf-8"))
