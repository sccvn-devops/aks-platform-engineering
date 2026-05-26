"""GitOps + Bitbucket PR plumbing for service_seed (US-V4-07, FR-V4-27..31).

Single owner of the *git* concern: Bitbucket REST API, git CLI orchestration,
worktree staging, and PR creation.  Replaces the git-related slice of the
legacy ``seed_job.py`` module.

Public surface:
    BitbucketClient            REST client for create_repository/create_pull_request.
    clone_repo / git            git CLI wrappers with bounded subprocess timeouts.
    authenticated_remote        Inject HTTPS basic-auth into a remote URL.
    stage_gitops_pr             Clone the platform-gitops repo, write files, push, open PR.
    create_service_repository   Render cookiecutter scaffold + push initial commit.

FR-V4-43: every external call passes an explicit ``timeout`` (HTTP_TIMEOUT_S or
SUBPROCESS_TIMEOUT_S).
"""

from __future__ import annotations

import base64
import json
import re
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any
from urllib import error, parse, request

from .jira_intake import ServiceRequest
from .service_template import render_cookiecutter_template, write_files

HTTP_TIMEOUT_S = 30
SUBPROCESS_TIMEOUT_S = 300


class BitbucketClient:
    def __init__(self, base_url: str, workspace: str, username: str, token: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.workspace = workspace
        self.username = username
        self.token = token

    def create_repository(self, slug: str, project_key: str | None = None) -> dict[str, Any]:
        payload: dict[str, Any] = {"scm": "git", "is_private": True}
        if project_key:
            payload["project"] = {"key": project_key}
        return self._request(
            "POST",
            f"/2.0/repositories/{self.workspace}/{slug}",
            payload,
            treat_conflict_as_success=True,
        )

    def create_pull_request(
        self,
        repo_slug: str,
        title: str,
        description: str,
        source_branch: str,
        destination_branch: str = "main",
    ) -> dict[str, Any]:
        payload = {
            "title": title,
            "description": description,
            "source": {"branch": {"name": source_branch}},
            "destination": {"branch": {"name": destination_branch}},
            "close_source_branch": True,
        }
        return self._request("POST", f"/2.0/repositories/{self.workspace}/{repo_slug}/pullrequests", payload)

    def _request(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        *,
        treat_conflict_as_success: bool = False,
    ) -> dict[str, Any]:
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        req = request.Request(f"{self.base_url}{path}", data=body, method=method)
        req.add_header("Accept", "application/json")
        if body is not None:
            req.add_header("Content-Type", "application/json")
        basic = base64.b64encode(f"{self.username}:{self.token}".encode("utf-8")).decode("ascii")
        req.add_header("Authorization", f"Basic {basic}")

        try:
            with request.urlopen(req, timeout=HTTP_TIMEOUT_S) as resp:
                raw = resp.read().decode("utf-8")
                return {} if not raw else json.loads(raw)
        except error.HTTPError as exc:
            if treat_conflict_as_success and exc.code == 400:
                return {}
            detail = exc.read().decode("utf-8", "ignore")
            # The Authorization header is not echoed back by Bitbucket; the
            # error string carries only the HTTP method + path + status +
            # response body, never the bearer/basic credential.
            raise RuntimeError(f"bitbucket api {method} {path} failed: {exc.code} {detail}") from exc


def clone_repo(remote: str, destination: Path, branch: str = "main") -> None:
    # FR-V4-43: git clone is bounded by SUBPROCESS_TIMEOUT_S so a hung
    # transport (mis-configured DNS, slow remote) cannot tie up the seed
    # job indefinitely.
    subprocess.run(
        ["git", "clone", "--depth", "1", "--branch", branch, remote, str(destination)],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=SUBPROCESS_TIMEOUT_S,
    )


def git(*args: str, cwd: Path) -> None:
    # FR-V4-43: ad-hoc git subcommands share the same SUBPROCESS_TIMEOUT_S
    # budget.  git push to a hung remote is the most common offender.
    subprocess.run(
        ["git", *args],
        cwd=str(cwd),
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=SUBPROCESS_TIMEOUT_S,
    )


def authenticated_remote(url: str, username: str, token: str) -> str:
    return re.sub(r"^https://", f"https://{parse.quote(username)}:{parse.quote(token)}@", url, count=1)


def stage_gitops_pr(
    *,
    repo_url: str,
    repo_username: str,
    repo_token: str,
    branch_name: str,
    pr_title: str,
    pr_description: str,
    path_root: str,
    files: dict[str, str],
    service_slug: str,
    bitbucket: BitbucketClient,
) -> None:
    with tempfile.TemporaryDirectory(prefix="gitops-seed-") as tmp:
        worktree = Path(tmp) / "gitops"
        clone_repo(authenticated_remote(repo_url, repo_username, repo_token), worktree)
        git("checkout", "-b", branch_name, cwd=worktree)
        root = worktree / path_root / service_slug
        if root.exists():
            shutil.rmtree(root)
        write_files(worktree, files)
        git("add", path_root, cwd=worktree)
        git("config", "user.email", "ci@platform", cwd=worktree)
        git("config", "user.name", "PlatformBot", cwd=worktree)
        git("commit", "-m", f"[ci skip] seed {service_slug} {path_root}", cwd=worktree)
        git("push", "origin", branch_name, cwd=worktree)

    repo_slug = repo_url.rstrip("/").split("/")[-1]
    if repo_slug.endswith(".git"):
        repo_slug = repo_slug[:-4]
    bitbucket.create_pull_request(repo_slug, pr_title, pr_description, branch_name)


def create_service_repository(
    *,
    template_dir: Path,
    req: ServiceRequest,
    repo_url: str,
    username: str,
    token: str,
) -> None:
    with tempfile.TemporaryDirectory(prefix="service-seed-") as tmp:
        root = Path(tmp)
        service_tree = render_cookiecutter_template(
            template_dir,
            root,
            {
                "service_name": req.service_name,
                "service_slug": req.service_slug,
                "service_description": req.summary,
            },
        )
        git("init", "-b", "main", cwd=service_tree)
        git("config", "user.email", "ci@platform", cwd=service_tree)
        git("config", "user.name", "PlatformBot", cwd=service_tree)
        git("add", ".", cwd=service_tree)
        git("commit", "-m", "Initial scaffold from Cookiecutter", cwd=service_tree)
        git("remote", "add", "origin", authenticated_remote(repo_url, username, token), cwd=service_tree)
        git("push", "-u", "origin", "main", cwd=service_tree)
