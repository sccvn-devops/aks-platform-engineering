"""Robustness-pass tests for service_seed (US-V4-10, FR-V4-43..44).

Covers:
    * Explicit timeouts on every external call (HTTP urlopen and subprocess.run).
    * Path-traversal rejection in the cookiecutter fallback renderer.

These tests exercise the seams without making real network calls: urlopen is
patched to capture the ``timeout`` keyword, subprocess.run is patched to
capture the ``timeout`` keyword, and the fallback renderer is invoked with a
synthetic template tree on disk.
"""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools.service_seed.gitops_pr import (
    HTTP_TIMEOUT_S,
    SUBPROCESS_TIMEOUT_S,
    BitbucketClient,
    clone_repo,
    git,
)
from tools.service_seed.jira_intake import jira_get_issue
from tools.service_seed.service_template import (
    TemplatePathTraversalError,
    _safe_join,
    render_cookiecutter_fallback,
    render_cookiecutter_template,
)


class TimeoutInvariantsTest(unittest.TestCase):
    """FR-V4-43: every external call passes an explicit timeout."""

    def test_http_timeout_constant_matches_contract(self) -> None:
        self.assertEqual(HTTP_TIMEOUT_S, 30)

    def test_subprocess_timeout_constant_matches_contract(self) -> None:
        self.assertEqual(SUBPROCESS_TIMEOUT_S, 300)

    def test_bitbucket_request_passes_http_timeout(self) -> None:
        client = BitbucketClient("https://api.bitbucket.org", "ws", "user", "tok")

        captured: dict[str, object] = {}

        class _FakeResp:
            def __enter__(self_inner):
                return self_inner

            def __exit__(self_inner, *_a):
                return False

            def read(self_inner):
                return b"{}"

        def _fake_urlopen(req, timeout=None):
            captured["timeout"] = timeout
            captured["url"] = req.full_url
            return _FakeResp()

        with mock.patch("tools.service_seed.gitops_pr.request.urlopen", side_effect=_fake_urlopen):
            client.create_repository("svc")

        self.assertEqual(captured["timeout"], HTTP_TIMEOUT_S)
        self.assertIn("/2.0/repositories/ws/svc", captured["url"])

    def test_jira_get_issue_passes_http_timeout(self) -> None:
        captured: dict[str, object] = {}

        class _FakeResp:
            def __enter__(self_inner):
                return self_inner

            def __exit__(self_inner, *_a):
                return False

            def read(self_inner):
                return b'{"key":"X-1"}'

        def _fake_urlopen(req, timeout=None):
            captured["timeout"] = timeout
            return _FakeResp()

        with mock.patch("tools.service_seed.jira_intake.request.urlopen", side_effect=_fake_urlopen):
            jira_get_issue("https://jira.example", "X-1", "e@example", "tok")

        self.assertEqual(captured["timeout"], HTTP_TIMEOUT_S)

    def test_clone_repo_passes_subprocess_timeout(self) -> None:
        captured: dict[str, object] = {}

        def _fake_run(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with mock.patch("tools.service_seed.gitops_pr.subprocess.run", side_effect=_fake_run):
            clone_repo("https://example.com/repo.git", Path("/tmp/x"))

        self.assertEqual(captured.get("timeout"), SUBPROCESS_TIMEOUT_S)
        self.assertTrue(captured.get("check"))

    def test_git_helper_passes_subprocess_timeout(self) -> None:
        captured: dict[str, object] = {}

        def _fake_run(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with mock.patch("tools.service_seed.gitops_pr.subprocess.run", side_effect=_fake_run):
            git("status", cwd=Path("/tmp"))

        self.assertEqual(captured.get("timeout"), SUBPROCESS_TIMEOUT_S)

    def test_render_cookiecutter_subprocess_passes_timeout(self) -> None:
        captured: dict[str, object] = {}

        def _fake_run(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with tempfile.TemporaryDirectory() as tmp:
            template_dir = Path(tmp) / "tpl"
            template_dir.mkdir()
            (template_dir / "cookiecutter.json").write_text(json.dumps({"service_slug": "svc"}))
            (template_dir / "{{cookiecutter.service_slug}}").mkdir()
            destination = Path(tmp) / "out"
            destination.mkdir()
            with mock.patch("tools.service_seed.service_template.subprocess.run", side_effect=_fake_run):
                render_cookiecutter_template(
                    template_dir,
                    destination,
                    {"service_slug": "svc"},
                )

        self.assertEqual(captured.get("timeout"), SUBPROCESS_TIMEOUT_S)


class PathTraversalTest(unittest.TestCase):
    """FR-V4-44: cookiecutter fallback rejects path-traversal templates."""

    def test_safe_join_accepts_simple_relative_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            dest = Path(tmp)
            joined = _safe_join(dest, Path("a/b/c.txt"))
            self.assertTrue(str(joined).startswith(str(dest.resolve())))

    def test_safe_join_rejects_dotdot_component(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(TemplatePathTraversalError) as cm:
                _safe_join(Path(tmp), Path("../outside.txt"))
            self.assertIn("traversal", str(cm.exception))

    def test_safe_join_rejects_absolute_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(TemplatePathTraversalError):
                _safe_join(Path(tmp), Path("/etc/passwd"))

    def test_safe_join_rejects_nested_dotdot_escape(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(TemplatePathTraversalError):
                _safe_join(Path(tmp), Path("a/../../b.txt"))

    def _build_traversal_template(self, root: Path, malicious_name: str) -> Path:
        """Build a minimal cookiecutter template tree with a malicious
        filename injected via the service_slug placeholder.  The template
        directory itself is well-formed; rendering substitutes the
        malicious value and produces ``malicious_name`` as a path
        component.
        """
        tpl = root / "tpl"
        tpl.mkdir()
        (tpl / "cookiecutter.json").write_text(json.dumps({"service_slug": "default"}))
        # Use the service_slug variable as the filename so rendering
        # produces the malicious value.
        slug_dir = tpl / "{{cookiecutter.service_slug}}"
        slug_dir.mkdir()
        (slug_dir / "README.md").write_text("hello {{cookiecutter.service_slug}}")
        return tpl

    def test_fallback_renderer_raises_on_path_traversal_slug(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tpl = self._build_traversal_template(root, "..")
            destination = root / "out"
            destination.mkdir()

            # service_slug containing '..' would otherwise render the
            # tree one directory level above destination.  The fallback
            # must refuse before any file is written.
            sentinel_before = list(destination.iterdir())
            with self.assertRaises(TemplatePathTraversalError) as cm:
                render_cookiecutter_fallback(
                    tpl,
                    destination,
                    {"service_slug": "../escape"},
                )
            self.assertIn("traversal", str(cm.exception).lower() + "absolute")
            sentinel_after = list(destination.iterdir())
            # No file should have been written into the destination.
            self.assertEqual(sentinel_before, sentinel_after)
            # And nothing escaped above destination either.
            self.assertFalse((root / "escape").exists())
            self.assertFalse((root / "..escape").exists())

    def test_fallback_renderer_raises_on_absolute_slug(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tpl = self._build_traversal_template(root, "/etc/evil")
            destination = root / "out"
            destination.mkdir()

            with self.assertRaises(TemplatePathTraversalError):
                render_cookiecutter_fallback(
                    tpl,
                    destination,
                    {"service_slug": "/etc/evil"},
                )

    def test_fallback_renderer_writes_when_slug_is_safe(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tpl = self._build_traversal_template(root, "good")
            destination = root / "out"
            destination.mkdir()

            render_cookiecutter_fallback(
                tpl,
                destination,
                {"service_slug": "good"},
            )
            # Safe path produces the rendered tree under destination/good.
            self.assertTrue((destination / "good" / "README.md").is_file())


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
