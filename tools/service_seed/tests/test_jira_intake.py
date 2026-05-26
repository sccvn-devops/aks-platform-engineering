"""US-V4-07: jira_intake.py contract tests.

Covers the *parse* concern: Jira REST fetch (with mocked urlopen),
ServiceRequest validation via __post_init__, helper inference functions, and
the typed JiraIntakeError surface used by the CLI to emit non-zero exit
messages.
"""

from __future__ import annotations

import json
import unittest
from unittest import mock

from tools.service_seed.jira_intake import (
    EXPECTED_ISSUE_TYPE,
    HTTP_TIMEOUT_S,
    VALID_SLO_CLASSES,
    JiraIntakeError,
    ServiceRequest,
    adf_to_text,
    infer_service_name,
    infer_slo_class,
    jira_get_issue,
    parse_service_request,
    slugify,
)


def _issue(
    *,
    service_name: str = "Payments API",
    slo_class: str = "gold",
    issue_type: str = "IDP Service Request",
    key: str = "IDP-24",
) -> dict:
    return {
        "key": key,
        "fields": {
            "issuetype": {"name": issue_type},
            "summary": service_name,
            "description": {
                "type": "doc",
                "content": [
                    {"type": "paragraph", "content": [{"type": "text", "text": f"Service Name: {service_name}"}]},
                    {"type": "paragraph", "content": [{"type": "text", "text": f"SLO Class: {slo_class}"}]},
                ],
            },
        },
    }


class ServiceRequestPostInitTest(unittest.TestCase):
    """AC2: __post_init__ rejects malformed input with a typed error."""

    def _valid(self, **overrides) -> dict:
        base = dict(
            issue_key="IDP-1",
            issue_type=EXPECTED_ISSUE_TYPE,
            service_name="Foo",
            service_slug="foo",
            slo_class="gold",
            summary="Foo",
            description="",
        )
        base.update(overrides)
        return base

    def test_valid_request_constructs(self) -> None:
        req = ServiceRequest(**self._valid())
        self.assertEqual(req.service_slug, "foo")

    def test_rejects_empty_issue_key(self) -> None:
        with self.assertRaises(JiraIntakeError) as cm:
            ServiceRequest(**self._valid(issue_key=""))
        self.assertEqual(cm.exception.field, "issue_key")

    def test_rejects_unexpected_issue_type(self) -> None:
        with self.assertRaises(JiraIntakeError) as cm:
            ServiceRequest(**self._valid(issue_type="Bug"))
        self.assertEqual(cm.exception.field, "issue_type")

    def test_rejects_unknown_slo_class(self) -> None:
        with self.assertRaises(JiraIntakeError) as cm:
            ServiceRequest(**self._valid(slo_class="platinum"))
        self.assertEqual(cm.exception.field, "slo_class")

    def test_rejects_malformed_slug(self) -> None:
        with self.assertRaises(JiraIntakeError) as cm:
            ServiceRequest(**self._valid(service_slug="Foo Bar"))
        self.assertEqual(cm.exception.field, "service_slug")

    def test_dataclass_is_frozen(self) -> None:
        req = ServiceRequest(**self._valid())
        with self.assertRaises(Exception):  # noqa: BLE001
            req.service_slug = "bar"  # type: ignore[misc]


class ParseServiceRequestTest(unittest.TestCase):
    def test_parses_canonical_issue(self) -> None:
        req = parse_service_request(_issue())
        self.assertEqual(req.service_slug, "payments-api")
        self.assertEqual(req.slo_class, "gold")
        self.assertEqual(req.issue_key, "IDP-24")

    def test_overrides_take_precedence(self) -> None:
        req = parse_service_request(
            _issue(slo_class="bronze"),
            service_name_override="Orders",
            slo_class_override="silver",
        )
        self.assertEqual(req.service_slug, "orders")
        self.assertEqual(req.slo_class, "silver")

    def test_missing_issue_key_raises(self) -> None:
        issue = _issue()
        issue.pop("key")
        with self.assertRaises(JiraIntakeError) as cm:
            parse_service_request(issue)
        self.assertEqual(cm.exception.field, "issue_key")

    def test_unexpected_issue_type_raises(self) -> None:
        with self.assertRaises(JiraIntakeError) as cm:
            parse_service_request(_issue(issue_type="Bug"))
        self.assertEqual(cm.exception.field, "issue_type")

    def test_slo_class_default_is_silver(self) -> None:
        # No SLO hint anywhere → default silver per infer_slo_class.
        issue = {
            "key": "IDP-9",
            "fields": {
                "issuetype": {"name": EXPECTED_ISSUE_TYPE},
                "summary": "Cart",
                "description": "Service Name: Cart",
            },
        }
        req = parse_service_request(issue)
        self.assertEqual(req.slo_class, "silver")


class InferenceHelpersTest(unittest.TestCase):
    def test_slugify_lowercases_and_dashes(self) -> None:
        self.assertEqual(slugify("Payments API"), "payments-api")

    def test_slugify_rejects_empty(self) -> None:
        with self.assertRaises(JiraIntakeError):
            slugify("...")

    def test_adf_to_text_collapses_nested_paragraphs(self) -> None:
        node = {"type": "doc", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Hello"}]}]}
        self.assertIn("Hello", adf_to_text(node))

    def test_infer_service_name_prefers_explicit_field(self) -> None:
        fields = {"service_name": "Catalog"}
        self.assertEqual(infer_service_name(fields, "x", "y"), "Catalog")

    def test_infer_service_name_label_fallback(self) -> None:
        fields = {"labels": ["service:Inventory"]}
        self.assertEqual(infer_service_name(fields, "x", "y"), "Inventory")

    def test_infer_slo_class_label_fallback(self) -> None:
        fields = {"labels": ["slo:GOLD"]}
        self.assertEqual(infer_slo_class(fields, "x", "y"), "gold")

    def test_infer_slo_class_explicit_dict(self) -> None:
        fields = {"slo_class": {"value": "bronze"}}
        self.assertEqual(infer_slo_class(fields, "x", "y"), "bronze")

    def test_valid_slo_classes_set(self) -> None:
        self.assertEqual(VALID_SLO_CLASSES, {"bronze", "silver", "gold"})


class JiraGetIssueTest(unittest.TestCase):
    """FR-V4-43: jira_get_issue passes an explicit timeout to urlopen."""

    def test_uses_basic_auth_with_email(self) -> None:
        captured: dict = {}

        class _Resp:
            def __enter__(self):
                return self

            def __exit__(self, *_a):
                return False

            def read(self):
                return b'{"key":"X-1"}'

        def _fake(req, timeout=None):
            captured["url"] = req.full_url
            captured["timeout"] = timeout
            captured["auth"] = req.get_header("Authorization")
            return _Resp()

        with mock.patch("tools.service_seed.jira_intake.request.urlopen", side_effect=_fake):
            result = jira_get_issue("https://jira.example", "X-1", "user@example.com", "tok")

        self.assertEqual(result, {"key": "X-1"})
        self.assertEqual(captured["timeout"], HTTP_TIMEOUT_S)
        self.assertTrue(captured["auth"].startswith("Basic "))
        self.assertIn("/rest/api/3/issue/X-1", captured["url"])

    def test_uses_bearer_without_email(self) -> None:
        captured: dict = {}

        class _Resp:
            def __enter__(self):
                return self

            def __exit__(self, *_a):
                return False

            def read(self):
                return b"{}"

        def _fake(req, timeout=None):
            captured["auth"] = req.get_header("Authorization")
            return _Resp()

        with mock.patch("tools.service_seed.jira_intake.request.urlopen", side_effect=_fake):
            jira_get_issue("https://jira.example", "X-2", "", "tok")

        self.assertEqual(captured["auth"], "Bearer tok")


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
