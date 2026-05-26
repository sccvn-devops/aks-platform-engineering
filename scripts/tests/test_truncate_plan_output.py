"""Unit tests for scripts/truncate-plan-output.py (US-V4-03, FR-V4-13).

Exercises the single-threshold contract:
  * <= MAX_BYTES: pass-through.
  * > MAX_BYTES: head + footer pointing at the artifact URL.
  * Footer is included in MAX_BYTES budget (combined output stays within limit).

Invoked via `python3 -m unittest scripts.tests.test_truncate_plan_output`.
"""

from __future__ import annotations

import importlib.util
import io
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path


def _load_truncate_module():
    """Import truncate-plan-output.py despite its hyphenated filename."""
    repo_root = Path(__file__).resolve().parent.parent.parent
    script_path = repo_root / "scripts" / "truncate-plan-output.py"
    spec = importlib.util.spec_from_file_location("truncate_plan_output", script_path)
    assert spec and spec.loader, "spec_from_file_location returned None"
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class TestTruncatePlan(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.mod = _load_truncate_module()

    def test_short_input_passes_through_unchanged(self):
        plan = "Plan: 3 to add, 1 to change, 0 to destroy.\n"
        self.assertEqual(self.mod.truncate(plan), plan)

    def test_exactly_max_bytes_passes_through(self):
        plan = "x" * self.mod.MAX_BYTES
        self.assertEqual(self.mod.truncate(plan), plan)

    def test_oversize_is_truncated_with_footer(self):
        plan = "y" * (self.mod.MAX_BYTES + 5_000)
        artifact = "https://example/run/123/artifact"
        result = self.mod.truncate(plan, artifact_url=artifact)
        self.assertLessEqual(len(result), self.mod.MAX_BYTES)
        self.assertIn(artifact, result)
        self.assertIn("output truncated", result)
        # Head of the original payload is preserved.
        self.assertTrue(result.startswith("y" * 100))

    def test_oversize_no_link_uses_no_link_footer(self):
        plan = "z" * (self.mod.MAX_BYTES + 1)
        result = self.mod.truncate(plan, artifact_url=None)
        self.assertLessEqual(len(result), self.mod.MAX_BYTES)
        self.assertIn("output truncated", result)
        self.assertNotIn("http", result)

    def test_main_with_stdin(self):
        plan = "stdin-plan\n"
        stdin_backup = sys.stdin
        try:
            sys.stdin = io.StringIO(plan)
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = self.mod.main(["truncate-plan-output.py"])
            self.assertEqual(rc, 0)
            self.assertEqual(buf.getvalue(), plan)
        finally:
            sys.stdin = stdin_backup

    def test_main_with_file_input(self):
        plan = "a" * (self.mod.MAX_BYTES + 100)
        repo_root = Path(__file__).resolve().parent.parent.parent
        # Use a tempfile-style sibling under tests/ to avoid touching scripts/.
        import tempfile

        with tempfile.NamedTemporaryFile("w", delete=False, suffix=".txt") as fh:
            fh.write(plan)
            tmp = fh.name
        try:
            buf = io.StringIO()
            argv = [
                "truncate-plan-output.py",
                "--input",
                tmp,
                "--artifact-url",
                "https://example/artifact",
            ]
            with redirect_stdout(buf):
                rc = self.mod.main(argv)
            self.assertEqual(rc, 0)
            out = buf.getvalue()
            self.assertLessEqual(len(out), self.mod.MAX_BYTES)
            self.assertIn("https://example/artifact", out)
        finally:
            Path(tmp).unlink(missing_ok=True)

    def test_main_io_error_returns_2(self):
        buf = io.StringIO()
        argv = ["truncate-plan-output.py", "--input", "/nonexistent/plan.txt"]
        with redirect_stdout(buf):
            rc = self.mod.main(argv)
        self.assertEqual(rc, 2)

    def test_max_bytes_is_60k(self):
        # FR-V4-13 fixes the threshold at 60_000 bytes; tests guard against drift.
        self.assertEqual(self.mod.MAX_BYTES, 60_000)


if __name__ == "__main__":
    unittest.main()
