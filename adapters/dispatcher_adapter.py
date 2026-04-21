#!/usr/bin/env python3
"""Dispatcher adapter — routes to backlog or whatsnext based on run.adapter_type.

Usage (as connector adapter script):
  --adapter-command python3 --adapter-arg /path/to/dispatcher_adapter.py

The dispatcher reads the full request from stdin, inspects run.adapter_type,
and delegates to the appropriate adapter script in the same directory.
Defaults to backlog_adapter.py when adapter_type is absent or unrecognized.
"""

import os
import subprocess
import sys


def main() -> int:
    raw = sys.stdin.buffer.read()

    # Decode adapter_type from the raw payload without a full JSON parse so
    # we avoid importing json just to read one field.  Fall back safely.
    try:
        import json as _json
        payload = _json.loads(raw)
        adapter_type = ((payload.get("run") or {}).get("adapter_type") or "backlog").strip().lower()
    except Exception:  # noqa: BLE001
        adapter_type = "backlog"

    script_dir = os.path.dirname(os.path.abspath(__file__))
    if adapter_type == "whatsnext":
        script = os.path.join(script_dir, "whatsnext_adapter.py")
    else:
        script = os.path.join(script_dir, "backlog_adapter.py")

    proc = subprocess.run(
        [sys.executable, script],
        input=raw,
        capture_output=False,
    )
    return proc.returncode


if __name__ == "__main__":
    raise SystemExit(main())
