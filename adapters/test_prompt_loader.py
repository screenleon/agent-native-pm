"""Tests for the shared prompt loader + Python↔Go rendering parity."""
from __future__ import annotations

import os
import subprocess
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import _prompt_loader  # noqa: E402


BACKLOG_FIXTURE_VARS = {
    "PROJECT_NAME": "CrossLang",
    "PROJECT_DESCRIPTION_LINE": "Description: Parity-check project.",
    "REQUIREMENT": "Requirement title: do X.\nSummary: do X better.",
    "MAX_CANDIDATES": "4",
    "CONTEXT": "=== Context ===\n(no items)",
    "SCHEMA_VERSION": "context.v1",
}


class PromptLoaderTests(unittest.TestCase):
    # --- loader unit tests ------------------------------------------------

    def test_render_backlog_substitutes_known_vars(self):
        out = _prompt_loader.render("backlog", BACKLOG_FIXTURE_VARS)
        self.assertIn("CrossLang", out)
        self.assertNotIn("{{PROJECT_NAME}}", out)
        self.assertIn("Requirement title: do X.", out)
        # JSON schema single-braces survive.
        self.assertIn('"candidates":', out)

    def test_render_leaves_unknown_vars_as_is(self):
        out = _prompt_loader.render(
            "backlog",
            {"PROJECT_NAME": "X", "MAX_CANDIDATES": "1"},
        )
        self.assertIn("{{REQUIREMENT}}", out)

    def test_render_does_not_reexpand_value_braces(self):
        # A malicious value that itself looks like a template MUST NOT be
        # re-expanded — single-pass substitution is a safety invariant.
        out = _prompt_loader.render(
            "backlog",
            {
                "PROJECT_NAME": "{{REQUIREMENT}}",
                "PROJECT_DESCRIPTION_LINE": "",
                "REQUIREMENT": "REAL-REQ",
                "MAX_CANDIDATES": "1",
                "CONTEXT": "ctx",
                "SCHEMA_VERSION": "v1",
            },
        )
        self.assertIn("{{REQUIREMENT}}", out)
        self.assertIn("REAL-REQ", out)

    def test_render_strips_frontmatter(self):
        out = _prompt_loader.render(
            "backlog",
            {
                "PROJECT_NAME": "X",
                "PROJECT_DESCRIPTION_LINE": "",
                "REQUIREMENT": "r",
                "MAX_CANDIDATES": "1",
                "CONTEXT": "c",
                "SCHEMA_VERSION": "v1",
            },
        )
        self.assertFalse(out.startswith("---"))
        head = out[:200]
        self.assertNotIn("title:", head)
        self.assertNotIn("category:", head)

    def test_role_prompts_loadable(self):
        for role in [
            "backend-architect",
            "ui-scaffolder",
            "db-schema-designer",
            "api-contract-writer",
            "test-writer",
            "code-reviewer",
        ]:
            body = _prompt_loader.render(
                f"roles/{role}",
                {
                    "TASK_TITLE": "demo",
                    "TASK_DESCRIPTION": "demo",
                    "PROJECT_CONTEXT": "demo",
                    "REQUIREMENT": "demo",
                },
            )
            self.assertTrue(body.strip(), f"role {role} rendered empty")
            self.assertFalse(body.startswith("---"), f"role {role} frontmatter leaked")

    # --- cross-language parity -------------------------------------------
    #
    # Parity is enforced by a PINNED GOLDEN file: adapters/testdata/
    # backlog_render_golden.txt. Both the Python test here and the Go
    # test in backend/internal/prompts/render_test.go render the same
    # BACKLOG_FIXTURE_VARS and compare against the golden. If either
    # side drifts, the mismatching test fails. If both sides move in
    # lockstep the golden file has to be regenerated deliberately — which
    # is the whole point of pinning.

    GOLDEN_PATH = Path(__file__).resolve().parent / "testdata" / "backlog_render_golden.txt"

    def test_python_render_matches_golden(self):
        py_rendered = _prompt_loader.render("backlog", BACKLOG_FIXTURE_VARS)
        if os.environ.get("UPDATE_PROMPT_GOLDEN") == "1":
            self.GOLDEN_PATH.parent.mkdir(parents=True, exist_ok=True)
            self.GOLDEN_PATH.write_text(py_rendered, encoding="utf-8")
            self.skipTest("golden regenerated")
        if not self.GOLDEN_PATH.exists():
            self.fail(
                f"golden fixture missing at {self.GOLDEN_PATH} — run the test suite "
                f"with UPDATE_PROMPT_GOLDEN=1 to regenerate"
            )
        expected = self.GOLDEN_PATH.read_text(encoding="utf-8")
        self.assertEqual(
            py_rendered,
            expected,
            msg="Python-rendered backlog prompt drifted from the pinned golden; "
            "if the drift is intentional re-run with UPDATE_PROMPT_GOLDEN=1",
        )


if __name__ == "__main__":
    unittest.main()
