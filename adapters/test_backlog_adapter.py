#!/usr/bin/env python3
"""Unit tests for backlog_adapter (T-S5b-1: error_kind classifier)."""
import importlib.util
import os
import sys
import types
import unittest

# Load backlog_adapter without executing its __main__ block.
spec = importlib.util.spec_from_file_location(
    "backlog_adapter",
    os.path.join(os.path.dirname(__file__), "backlog_adapter.py"),
)
mod = importlib.util.module_from_spec(spec)
# Stub subprocess so the adapter import doesn't fail in test environments.
sys.modules.setdefault("subprocess", types.ModuleType("subprocess"))
spec.loader.exec_module(mod)

_classify = mod._classify_error_kind
_VALID = mod._VALID_ERROR_KINDS


class TestClassifyErrorKind(unittest.TestCase):
    """T-S5b-1: error_kind classification in the Python adapter."""

    def test_session_expired(self):
        self.assertEqual(_classify("session expired"), "session_expired")
        self.assertEqual(_classify("not logged in"), "session_expired")
        self.assertEqual(_classify("Authentication expired"), "session_expired")

    def test_rate_limited(self):
        self.assertEqual(_classify("rate limit exceeded"), "rate_limited")
        self.assertEqual(_classify("Too Many Requests"), "rate_limited")
        self.assertEqual(_classify("quota exceeded"), "rate_limited")
        self.assertEqual(_classify("HTTP 429"), "rate_limited")

    def test_model_not_available(self):
        self.assertEqual(_classify("model not found"), "model_not_available")
        self.assertEqual(_classify("no such model"), "model_not_available")
        self.assertEqual(_classify("invalid model"), "model_not_available")

    def test_cli_not_found(self):
        self.assertEqual(_classify("not found on path"), "cli_not_found")
        self.assertEqual(_classify("command not found: claude"), "cli_not_found")
        self.assertEqual(_classify("no such file or directory"), "cli_not_found")

    def test_cli_timeout(self):
        self.assertEqual(_classify("timed out"), "cli_timeout")
        self.assertEqual(_classify("timeout"), "cli_timeout")
        self.assertEqual(_classify("operation timed out"), "cli_timeout")

    def test_adapter_protocol_error(self):
        self.assertEqual(_classify("adapter error: unexpected output"), "adapter_protocol_error")
        self.assertEqual(_classify("invalid json in output"), "adapter_protocol_error")

    def test_unknown(self):
        self.assertEqual(_classify("something completely different"), "unknown")
        self.assertEqual(_classify(""), "unknown")
        self.assertEqual(_classify(None), "unknown")  # type: ignore[arg-type]

    def test_all_returned_kinds_are_valid(self):
        samples = [
            "session expired", "rate limit", "model not found", "not found on path",
            "timed out", "adapter error", "unknown error",
        ]
        for s in samples:
            kind = _classify(s)
            self.assertIn(kind, _VALID, f"kind {kind!r} for {s!r} not in VALID_ERROR_KINDS")

    def test_precedence_session_beats_timeout(self):
        # "session expired and timed out" — session_expired should win (listed first).
        self.assertEqual(_classify("session expired and timed out"), "session_expired")


if __name__ == "__main__":
    unittest.main()
