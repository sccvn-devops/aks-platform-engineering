"""US-V4-07: gitops_pr.py contract tests.

Covers the *git* concern: Bitbucket REST client (mocked urlopen), bounded
subprocess timeouts on clone/git, authenticated remote URL injection, and the
two orchestration helpers (stage_gitops_pr + create_service_repository) that
the CLI threads together.
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock
from urllib import error as urlerror

from tools.service_seed.gitops_pr import (
    HTTP_TIMEOUT_S,
    SUBPROCESS_TIMEOUT_S,
    BitbucketClient,
    authenticated_remote,
    clone_repo,
    create_service_repository,
    git,
    stage_gitops_pr,
)
from tools.service_seed.jira_intake import ServiceRequest


class _FakeResp:
    def __init__(self, body: bytes = b"{}") -> None:
        self._body = body

    def __enter__(self):
        return self

    def __exit__(self, *_a):
        return False

    def read(self) -> bytes:
        return self._body


class BitbucketClientTest(unittest.TestCase):
    def setUp(self) -> None:
        self.client = BitbucketClient("https://api.bitbucket.org/", "ws", "user", "tok")

    def test_create_repository_includes_project(self) -> None:
        captured: dict = {}

        def _fake(req, timeout=None):
            captured["url"] = req.full_url
            captured["timeout"] = timeout
            captured["body"] = json.loads(req.data.decode("utf-8"))
            return _FakeResp(b'{"slug": "svc"}')

        with mock.patch("tools.service_seed.gitops_pr.request.urlopen", side_effect=_fake):
            result = self.client.create_repository("svc", project_key="PLT")

        self.assertEqual(result, {"slug": "svc"})
        self.assertEqual(captured["timeout"], HTTP_TIMEOUT_S)
        self.assertEqual(captured["body"]["project"]["key"], "PLT")
        self.assertTrue(captured["body"]["is_private"])

    def test_create_repository_treats_400_as_existing(self) -> None:
        def _fake(req, timeout=None):
            raise urlerror.HTTPError(req.full_url, 400, "Already exists", hdrs=None, fp=None)

        with mock.patch("tools.service_seed.gitops_pr.request.urlopen", side_effect=_fake):
            result = self.client.create_repository("svc")

        self.assertEqual(result, {})

    def test_request_raises_runtime_error_on_other_4xx(self) -> None:
        class _ErrFp:
            def read(self) -> bytes:
                return b'{"error":"forbidden"}'

            def close(self) -> None:
                return None

        def _fake(req, timeout=None):
            raise urlerror.HTTPError(req.full_url, 403, "Forbidden", hdrs=None, fp=_ErrFp())

        with mock.patch("tools.service_seed.gitops_pr.request.urlopen", side_effect=_fake):
            with self.assertRaises(RuntimeError) as cm:
                self.client.create_pull_request("svc", "title", "desc", "feat")

        msg = str(cm.exception)
        self.assertIn("403", msg)
        # FR-V4-43 secondary guarantee: credential material is not echoed in
        # the error string.
        self.assertNotIn("tok", msg)

    def test_create_pull_request_payload_shape(self) -> None:
        captured: dict = {}

        def _fake(req, timeout=None):
            captured["body"] = json.loads(req.data.decode("utf-8"))
            return _FakeResp(b'{"id": 7}')

        with mock.patch("tools.service_seed.gitops_pr.request.urlopen", side_effect=_fake):
            self.client.create_pull_request("svc", "title", "desc", "branch", destination_branch="main")

        self.assertEqual(captured["body"]["source"]["branch"]["name"], "branch")
        self.assertEqual(captured["body"]["destination"]["branch"]["name"], "main")
        self.assertTrue(captured["body"]["close_source_branch"])


class SubprocessTimeoutTest(unittest.TestCase):
    """FR-V4-43: every subprocess call passes ``timeout=SUBPROCESS_TIMEOUT_S``."""

    def test_clone_repo(self) -> None:
        captured: dict = {}

        def _fake(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with mock.patch("tools.service_seed.gitops_pr.subprocess.run", side_effect=_fake):
            clone_repo("https://example.com/r.git", Path("/tmp/x"))

        self.assertEqual(captured["timeout"], SUBPROCESS_TIMEOUT_S)

    def test_git_helper(self) -> None:
        captured: dict = {}

        def _fake(*args, **kwargs):
            captured.update(kwargs)
            return mock.MagicMock(returncode=0)

        with mock.patch("tools.service_seed.gitops_pr.subprocess.run", side_effect=_fake):
            git("status", cwd=Path("/tmp"))

        self.assertEqual(captured["timeout"], SUBPROCESS_TIMEOUT_S)


class AuthenticatedRemoteTest(unittest.TestCase):
    def test_injects_basic_auth_into_https_url(self) -> None:
        url = authenticated_remote("https://bitbucket.org/acme/repo.git", "x-token-auth", "abc")
        self.assertEqual(url, "https://x-token-auth:abc@bitbucket.org/acme/repo.git")

    def test_url_encodes_special_chars(self) -> None:
        url = authenticated_remote("https://bitbucket.org/a/r.git", "u@e", "p:wd")
        self.assertIn("u%40e", url)
        self.assertIn("p%3Awd", url)


class StageGitopsPRTest(unittest.TestCase):
    def test_calls_bitbucket_create_pr_with_derived_slug(self) -> None:
        gitops_calls: list = []

        def _fake_run(*args, **kwargs):
            # Capture argv for assertion; treat everything as success.
            gitops_calls.append(args[0])
            return mock.MagicMock(returncode=0)

        class _Bitbucket:
            def __init__(self):
                self.calls: list = []

            def create_pull_request(self, repo_slug, title, description, source_branch, destination_branch="main"):
                self.calls.append((repo_slug, title, source_branch))
                return {}

        bb = _Bitbucket()

        with mock.patch("tools.service_seed.gitops_pr.subprocess.run", side_effect=_fake_run):
            stage_gitops_pr(
                repo_url="https://bitbucket.org/acme/platform-gitops.git",
                repo_username="x-token-auth",
                repo_token="tok",
                branch_name="seed/svc-infra-idp-1",
                pr_title="t",
                pr_description="d",
                path_root="apps",
                files={"apps/svc/infra/base/x.yaml": "x: y"},
                service_slug="svc",
                bitbucket=bb,
            )

        # The Bitbucket repo slug is the URL tail without .git.
        self.assertEqual(bb.calls, [("platform-gitops", "t", "seed/svc-infra-idp-1")])
        # git was invoked for clone + checkout + add + config x2 + commit + push.
        flat = [tuple(c) for c in gitops_calls]
        self.assertTrue(any(c[:2] == ("git", "clone") for c in flat))
        self.assertTrue(any(c[:2] == ("git", "push") for c in flat))


class CreateServiceRepositoryTest(unittest.TestCase):
    def test_drives_cookiecutter_render_and_git_push(self) -> None:
        req = ServiceRequest(
            issue_key="IDP-7",
            issue_type="IDP Service Request",
            service_name="Carts",
            service_slug="carts",
            slo_class="silver",
            summary="Carts",
            description="",
        )
        git_calls: list = []
        render_calls: list = []

        def _fake_git(*args, **kwargs):
            git_calls.append((args, kwargs))

        def _fake_render(template_dir, destination, context):
            render_calls.append((template_dir, destination, dict(context)))
            target = destination / context["service_slug"]
            target.mkdir(parents=True, exist_ok=True)
            return target

        with tempfile.TemporaryDirectory() as tmp:
            tpl = Path(tmp) / "tpl"
            tpl.mkdir()
            with mock.patch("tools.service_seed.gitops_pr.git", side_effect=_fake_git), \
                 mock.patch(
                     "tools.service_seed.gitops_pr.render_cookiecutter_template",
                     side_effect=_fake_render,
                 ):
                create_service_repository(
                    template_dir=tpl,
                    req=req,
                    repo_url="https://bitbucket.org/acme/carts.git",
                    username="x-token-auth",
                    token="tok",
                )

        self.assertEqual(len(render_calls), 1)
        ctx = render_calls[0][2]
        self.assertEqual(ctx["service_slug"], "carts")
        self.assertEqual(ctx["service_name"], "Carts")

        # Initial scaffold push: git init + add + commit + remote add + push.
        subcommands = [c[0][0] for c in git_calls if c[0]]
        self.assertIn("init", subcommands)
        self.assertIn("push", subcommands)
        self.assertIn("commit", subcommands)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
