#!/usr/bin/env python3
# validate-workflow-call-contract.py — smoke-test the workflow_call interface
# between top-level callers and reusable workflows (US-V4-03, FR-V4-10..14).
#
# This is the static equivalent of an `act --workflow_call` smoke test: it
# parses every workflow file in .github/workflows/ (including reusable/
# subdirectory), and for every `uses: ./.github/workflows/...` jobcall, asserts:
#
#   1. The referenced reusable workflow declares `on.workflow_call`.
#   2. Every key passed under `with:` is declared in the callee's `inputs:`.
#   3. Every required input declared by the callee is supplied by the caller.
#   4. Every key passed under `secrets:` is declared in the callee's
#      `on.workflow_call.secrets`.
#
# If `act` is available locally, runs `act --list -W <workflow>` to confirm the
# graph parses cleanly. If not, the static checks above are sufficient — they
# catch the same drift act would surface.
#
# Run locally:
#   python3 scripts/validate-workflow-call-contract.py
# CI: see .github/workflows/terraform-ci.yml job `validate-workflow-call-contract`.

from __future__ import annotations

import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
WORKFLOWS_DIR = REPO_ROOT / ".github" / "workflows"

try:
    import yaml  # type: ignore[import-not-found]

    _HAS_YAML = True
except ImportError:
    _HAS_YAML = False


def _annotate(level: str, file: Path, line: int, message: str) -> None:
    if os.environ.get("GITHUB_ACTIONS") == "true":
        print(f"::{level} file={file},line={line}::{message}")
    else:
        print(f"[{level}] {file}:{line}: {message}")


def _load_yaml(path: Path) -> dict:
    text = path.read_text(encoding="utf-8")
    if _HAS_YAML:
        return yaml.safe_load(text) or {}
    return _parse_minimal_yaml(text)


# --------------------------------------------------------------------------
# Minimal YAML subset parser (fallback for environments without pyyaml).
# Handles the keys workflow_call workflows actually use: nested maps, scalar
# values, `true`/`false`, and the `inputs:` / `secrets:` / `with:` shapes.
# --------------------------------------------------------------------------


def _parse_minimal_yaml(text: str) -> dict:
    root: dict = {}
    stack: list[tuple[int, dict | list]] = [(-1, root)]
    pending_key: list[str | None] = [None]

    def _coerce(v: str):
        v = v.strip()
        if v == "" or v == "~" or v.lower() == "null":
            return None
        if v.lower() in ("true", "yes"):
            return True
        if v.lower() in ("false", "no"):
            return False
        if (v.startswith("'") and v.endswith("'")) or (
            v.startswith('"') and v.endswith('"')
        ):
            return v[1:-1]
        try:
            return int(v)
        except ValueError:
            return v

    lines = text.splitlines()
    for raw in lines:
        # Strip comments unless inside quotes (heuristic — workflow files don't
        # embed '#' in scalar values in our scope).
        stripped_line = raw.rstrip()
        if not stripped_line.strip() or stripped_line.lstrip().startswith("#"):
            continue
        indent = len(stripped_line) - len(stripped_line.lstrip(" "))
        body = stripped_line.lstrip(" ")

        # Pop until parent indent < indent.
        while stack and stack[-1][0] >= indent:
            stack.pop()
            pending_key.pop()
        parent = stack[-1][1]
        parent_key = pending_key[-1] if pending_key else None

        if body.startswith("- "):
            item_body = body[2:].strip()
            if isinstance(parent, dict) and parent_key is not None:
                lst = parent.get(parent_key)
                if not isinstance(lst, list):
                    lst = []
                    parent[parent_key] = lst
                parent = lst
            if isinstance(parent, list):
                if ":" in item_body:
                    sub: dict = {}
                    parent.append(sub)
                    stack.append((indent, sub))
                    pending_key.append(None)
                    k, _, v = item_body.partition(":")
                    coerced = _coerce(v) if v.strip() else None
                    if coerced is None and v.strip() == "":
                        sub[k.strip()] = {}
                        stack.append((indent + 1, sub[k.strip()]))
                        pending_key.append(None)
                        # Track the unresolved nested key
                        pending_key[-2] = k.strip()
                    else:
                        sub[k.strip()] = coerced
                else:
                    parent.append(_coerce(item_body))
            continue

        if ":" not in body:
            continue
        key, _, value = body.partition(":")
        key = key.strip()
        value = value.strip()

        if isinstance(parent, list):
            # Should have been handled by '- ' branch; treat as new dict in list.
            new: dict = {}
            parent.append(new)
            parent = new
            stack[-1] = (indent, new)

        if value == "":
            parent[key] = {}
            stack.append((indent, parent[key]))
            pending_key.append(key)
        else:
            parent[key] = _coerce(value)

    return root


# --------------------------------------------------------------------------
# Validators
# --------------------------------------------------------------------------


def _walk_workflows() -> list[Path]:
    return sorted(p for p in WORKFLOWS_DIR.rglob("*.y*ml") if p.is_file())


JOBCALL_USES_RE = re.compile(r"^\s+uses:\s*(?P<ref>\./\.github/workflows/[^\s#]+)")


def _on_workflow_call(parsed: dict) -> dict | None:
    # YAML 1.1 (PyYAML default) parses bare `on:` as the boolean True. Look it
    # up under both keys so the validator works with PyYAML and the fallback
    # parser identically.
    if not parsed:
        return None
    on = parsed.get("on")
    if on is None:
        on = parsed.get(True)
    if not isinstance(on, dict):
        return None
    wc = on.get("workflow_call")
    if isinstance(wc, dict):
        return wc
    if wc is None and "workflow_call" in on:
        return {}
    return None


def _declared_inputs(wc: dict) -> dict:
    val = wc.get("inputs") if wc else None
    return val if isinstance(val, dict) else {}


def _declared_secrets(wc: dict) -> dict:
    val = wc.get("secrets") if wc else None
    return val if isinstance(val, dict) else {}


def _required_inputs(inputs: dict) -> set[str]:
    return {k for k, spec in inputs.items() if isinstance(spec, dict) and spec.get("required") is True}


def _required_secrets(secrets: dict) -> set[str]:
    return {k for k, spec in secrets.items() if isinstance(spec, dict) and spec.get("required") is True}


def validate_call(caller: Path, callee_ref: str, with_keys: set[str], secret_keys: set[str], lineno: int) -> int:
    callee = REPO_ROOT / callee_ref.removeprefix("./")
    if not callee.exists():
        _annotate("error", caller, lineno, f"reusable workflow not found: {callee_ref}")
        return 1

    parsed = _load_yaml(callee)
    wc = _on_workflow_call(parsed)
    if wc is None:
        _annotate(
            "error",
            caller,
            lineno,
            f"referenced reusable workflow {callee_ref} does not declare `on.workflow_call`",
        )
        return 1

    inputs = _declared_inputs(wc)
    secrets = _declared_secrets(wc)

    violations = 0
    for key in with_keys:
        if key not in inputs:
            _annotate(
                "error",
                caller,
                lineno,
                f"`with: {key}:` is not declared as an input in {callee_ref}",
            )
            violations += 1
    for key in _required_inputs(inputs) - with_keys:
        _annotate(
            "error",
            caller,
            lineno,
            f"required input `{key}` declared in {callee_ref} is not passed by the caller",
        )
        violations += 1
    for key in secret_keys:
        if key not in secrets:
            _annotate(
                "error",
                caller,
                lineno,
                f"`secrets: {key}:` is not declared in {callee_ref}",
            )
            violations += 1
    for key in _required_secrets(secrets) - secret_keys:
        _annotate(
            "error",
            caller,
            lineno,
            f"required secret `{key}` declared in {callee_ref} is not passed by the caller",
        )
        violations += 1
    return violations


# --------------------------------------------------------------------------
# Caller-side extraction: find every `uses: ./.github/workflows/...` jobcall
# and the keys passed under `with:` / `secrets:`.
# --------------------------------------------------------------------------


def _extract_calls(path: Path) -> list[tuple[int, str, set[str], set[str]]]:
    """Return list of (lineno, callee_ref, with_keys, secret_keys)."""
    lines = path.read_text(encoding="utf-8").splitlines()
    out: list[tuple[int, str, set[str], set[str]]] = []
    i = 0
    while i < len(lines):
        m = JOBCALL_USES_RE.match(lines[i])
        if not m:
            i += 1
            continue
        # Find the indent of `uses:`.
        uses_indent = len(lines[i]) - len(lines[i].lstrip(" "))
        callee_ref = m.group("ref")
        lineno = i + 1
        with_keys: set[str] = set()
        secret_keys: set[str] = set()

        # Scan following sibling lines at the same indent until we leave the job.
        j = i + 1
        block_indent = uses_indent  # other siblings (`with:`, `secrets:`, `needs:`, `strategy:`) at same depth
        while j < len(lines):
            raw = lines[j]
            if not raw.strip() or raw.lstrip().startswith("#"):
                j += 1
                continue
            line_indent = len(raw) - len(raw.lstrip(" "))
            if line_indent < block_indent:
                break
            if line_indent == block_indent and raw.lstrip().startswith(("with:", "secrets:")):
                section = "with" if raw.lstrip().startswith("with:") else "secrets"
                # Read children at indent > block_indent.
                k = j + 1
                while k < len(lines):
                    child = lines[k]
                    if not child.strip() or child.lstrip().startswith("#"):
                        k += 1
                        continue
                    child_indent = len(child) - len(child.lstrip(" "))
                    if child_indent <= block_indent:
                        break
                    body = child.lstrip()
                    if ":" in body:
                        key = body.split(":", 1)[0].strip()
                        if key:
                            (with_keys if section == "with" else secret_keys).add(key)
                    k += 1
                j = k
                continue
            j += 1

        out.append((lineno, callee_ref, with_keys, secret_keys))
        i = j if j > i else i + 1

    return out


def _try_act(workflow_path: Path) -> int:
    """Run `act --list` if available; non-fatal — returns 0 on absence."""
    if not shutil.which("act"):
        return 0
    try:
        res = subprocess.run(
            ["act", "--list", "-W", str(workflow_path)],
            capture_output=True,
            text=True,
            timeout=60,
        )
        if res.returncode != 0:
            _annotate(
                "warning",
                workflow_path,
                1,
                f"act --list reported non-zero ({res.returncode}): {res.stderr.strip()[:200]}",
            )
            return 1
    except subprocess.TimeoutExpired:
        _annotate("warning", workflow_path, 1, "act --list timed out (60s)")
        return 1
    except OSError as exc:
        _annotate("warning", workflow_path, 1, f"act --list failed: {exc}")
        return 1
    return 0


def main() -> int:
    if not WORKFLOWS_DIR.is_dir():
        print(f"{WORKFLOWS_DIR}: no such directory", file=sys.stderr)
        return 1

    total = 0
    callers = 0
    workflows = _walk_workflows()
    for wf in workflows:
        calls = _extract_calls(wf)
        if not calls:
            continue
        callers += 1
        for lineno, callee_ref, with_keys, secret_keys in calls:
            total += validate_call(wf, callee_ref, with_keys, secret_keys, lineno)

    # Optional act smoke test for top-level workflows (non-fatal if act absent).
    for wf in workflows:
        if wf.parent.name == "workflows":  # top-level only; reusable ones aren't entrypoints
            _try_act(wf)

    if total:
        print(
            f"\n{total} workflow_call contract violation(s) across {callers} caller(s).",
            file=sys.stderr,
        )
        return 1

    print(f"OK — {callers} caller workflow(s) checked; all workflow_call inputs/secrets line up.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
