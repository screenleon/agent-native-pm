#!/usr/bin/env python3
"""
anpm-whatsnext-adapter — exec-json adapter for the Agent Native PM local
connector. Instead of decomposing a single requirement into tasks, this
adapter analyses the *current project state* (open tasks, drift signals,
stale documents, sync failures, recent agent activity) and returns a
prioritised list of the most important things to work on next.

Use this adapter when you want the connector to answer the question
"What should I focus on right now?" rather than "How do I build this feature?".

Contract (stdin):  identical to backlog_adapter — the connector sends the
  same exec-json payload regardless of which adapter is configured.

  {
    "run":    {...},
    "project": {"id": ..., "name": ..., "description": ...},
    "requirement": {
      "id": ..., "title": ..., "description": ..., "summary": ...
    },
    "requested_max_candidates": 5,
    "planning_context": {
      "schema_version": "context.v1",
      "sources": {
        "open_tasks":         [...],
        "recent_documents":   [...],
        "open_drift_signals": [...],
        "latest_sync_run":    {...} | null,
        "recent_agent_runs":  [...]
      }
    }
  }

  The "requirement" field is treated as an *optional focus scope*.
  If the title/description is empty or generic the adapter ignores it and
  performs a full project-wide analysis.

Contract (stdout):
  {
    "candidates": [
      {
        "title":          string (<= 120 chars),
        "description":    string,
        "rationale":      string,
        "priority_score": 0.0-1.0,
        "confidence":     0.0-1.0,
        "rank":           integer starting at 1 (lower = higher urgency),
        "evidence":       ["doc:<id>", "task:<id>", "drift:<id>", ...],
        "duplicate_titles": [string, ...]
      },
      ...
    ],
    "error_message": ""   // only on failure
  }

Environment variables:
  ANPM_ADAPTER_AGENT    "claude" (default) | "codex"
  ANPM_ADAPTER_MODEL    Optional model override passed to the CLI.
  ANPM_ADAPTER_TIMEOUT  Subprocess timeout seconds (default 60).
  ANPM_ADAPTER_DEBUG    If "1", write diagnostic traces to stderr.

Exits 0 on both success and handled failure.
"""

from __future__ import annotations

import json
import os
import pty
import re
import select
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

# Make the sibling `_prompt_loader.py` importable when this file is run
# directly as a script (the connector spawns it via `python3 path/to/...`).
sys.path.insert(0, str(Path(__file__).resolve().parent))
import _prompt_loader  # noqa: E402  (intentional sys.path insert above)


# Strip ANSI escape codes from PTY output.
_ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")

DEFAULT_TIMEOUT_SEC = 300  # 5 min; override with ANPM_ADAPTER_TIMEOUT
DEFAULT_MAX_CANDIDATES = 5
DEFAULT_CLAUDE_MODEL = "claude-sonnet-4-6"

# Preference order when picking from discovered Codex model list.
# Use exact IDs as shown by `codex models`; order = best first.
_CODEX_MODEL_PREFERENCE = [
    "gpt-5.4",           # latest frontier agentic coding model
    "gpt-5.3-codex",
    "gpt-5.2-codex",
    "gpt-5.1-codex-max",
    "gpt-5.4-mini",
    "gpt-5.2",
    "gpt-5.1-codex-mini",
]

# Generic/placeholder titles that signal "no real scope given"
_GENERIC_TITLES = {
    "", "what's next", "whats next", "next steps", "next step",
    "what to do next", "what should i do", "analysis", "review",
    "backlog review", "project review", "triage",
}


def _debug(message: str) -> None:
    if os.environ.get("ANPM_ADAPTER_DEBUG") == "1":
        print(f"[anpm-whatsnext] {message}", file=sys.stderr, flush=True)


def _read_request() -> dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        raise ValueError("empty stdin payload")
    return json.loads(raw)


def _write_response(candidates: list[dict[str, Any]], error_message: str = "",
                    cli_info: dict[str, Any] | None = None) -> None:
    payload: dict[str, Any] = {"candidates": candidates}
    if error_message:
        payload["error_message"] = error_message
    if cli_info:
        payload["cli_info"] = cli_info
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    sys.stdout.flush()


def _pick_best_codex_model(output: str) -> str:
    """Return the highest-preference model found in CLI output, or empty string.

    Uses a negative lookahead so 'gpt-5.4' does not match 'gpt-5.4-mini'.
    """
    for model in _CODEX_MODEL_PREFERENCE:
        pattern = re.escape(model) + r"(?![\w.-])"
        if re.search(pattern, output, re.IGNORECASE):
            return model
    return ""


def _discover_codex_model(binary: str, timeout_sec: int = 15) -> str:
    """Query the Codex CLI or OpenAI API for available models; return best match."""
    for subcmd in [["models"], ["--list-models"], ["models", "list"]]:
        try:
            proc = subprocess.run(
                [binary] + subcmd,
                capture_output=True, text=True, timeout=timeout_sec, check=False,
            )
            if proc.returncode == 0 and proc.stdout.strip():
                found = _pick_best_codex_model(proc.stdout)
                if found:
                    _debug(f"codex model discovered via CLI: {found}")
                    return found
        except (subprocess.TimeoutExpired, OSError):
            pass

    try:
        import openai  # noqa: PLC0415
        client = openai.OpenAI()
        available = {m.id for m in client.models.list().data}
        for preferred in _CODEX_MODEL_PREFERENCE:
            if preferred in available:
                _debug(f"codex model discovered via OpenAI API: {preferred}")
                return preferred
    except Exception:  # noqa: BLE001
        pass

    return ""


def _is_generic_scope(requirement: dict[str, Any] | None) -> bool:
    if not requirement:
        return True
    title = (requirement.get("title") or "").strip().lower()
    return title in _GENERIC_TITLES


def _scope_snippet(requirement: dict[str, Any] | None) -> str:
    if _is_generic_scope(requirement):
        return ""
    parts = [f"Focus scope: {requirement.get('title', '').strip()}"]  # type: ignore[union-attr]
    description = (requirement.get("description") or "").strip()  # type: ignore[union-attr]
    summary = (requirement.get("summary") or "").strip()  # type: ignore[union-attr]
    if summary:
        parts.append(f"Scope detail: {summary}")
    elif description:
        parts.append(f"Scope detail: {description}")
    return "\n".join(parts)


def _context_snapshot(context: dict[str, Any] | None) -> str:
    if not context:
        return "(no planning context provided)"
    sources = context.get("sources") or {}
    out: list[str] = []

    tasks = sources.get("open_tasks") or []
    if tasks:
        out.append(f"Open tasks ({len(tasks)} shown):")
        for task in tasks[:25]:
            out.append(
                f"  - [{task.get('status', '')}][{task.get('priority', 'none')}] "
                f"{task.get('title', '')} (id={task.get('id', '')})"
            )

    documents = sources.get("recent_documents") or []
    if documents:
        stale_docs = [d for d in documents if d.get("is_stale")]
        fresh_docs = [d for d in documents if not d.get("is_stale")]
        if stale_docs:
            out.append(f"STALE documents ({len(stale_docs)}):")
            for doc in stale_docs[:8]:
                out.append(
                    f"  - [STALE] {doc.get('title', '')} "
                    f"({doc.get('file_path', '')}, type={doc.get('doc_type', '')}, id={doc.get('id', '')})"
                )
        if fresh_docs:
            out.append(f"Recent documents ({len(fresh_docs)} fresh):")
            for doc in fresh_docs[:8]:
                out.append(
                    f"  - {doc.get('title', '')} "
                    f"({doc.get('file_path', '')}, type={doc.get('doc_type', '')}, id={doc.get('id', '')})"
                )

    drift = sources.get("open_drift_signals") or []
    if drift:
        out.append(f"Open drift signals ({len(drift)}):")
        for signal in drift[:10]:
            out.append(
                f"  - [{signal.get('severity', '')}] {signal.get('trigger_type', '')}"
                f" on {signal.get('document_title', '')}: {signal.get('trigger_detail', '')}"
                f" (id={signal.get('id', '')})"
            )

    sync_run = sources.get("latest_sync_run")
    if sync_run:
        line = f"Latest sync run: status={sync_run.get('status', '')}"
        err = (sync_run.get("error_message") or "").strip()
        if err:
            line += f", error={err[:200]}"
        out.append(line)

    agent_runs = sources.get("recent_agent_runs") or []
    if agent_runs:
        out.append(f"Recent agent runs ({len(agent_runs)}):")
        for run in agent_runs[:6]:
            out.append(
                f"  - {run.get('agent_name', '')} / {run.get('action_type', '')}"
                f" ({run.get('status', '')}): {run.get('summary', '')}"
            )

    dropped = (context.get("meta") or {}).get("dropped_counts") or {}
    if any(dropped.values()):
        out.append(f"(note: some context entries were truncated: {dropped})")

    return "\n".join(out) if out else "(project context is empty)"


def _build_prompt(request: dict[str, Any]) -> str:
    requirement = request.get("requirement") or {}
    project = request.get("project") or {}
    context = request.get("planning_context")
    max_candidates = int(request.get("requested_max_candidates") or DEFAULT_MAX_CANDIDATES)
    if max_candidates <= 0:
        max_candidates = DEFAULT_MAX_CANDIDATES

    project_name = str(project.get("name") or "").strip() or "(unnamed project)"
    project_description = str(project.get("description") or "").strip()

    scope_block = _scope_snippet(requirement)
    scope_section = f"\n=== Focus scope ===\n{scope_block}\n" if scope_block else ""

    return _prompt_loader.render(
        "whatsnext",
        {
            "PROJECT_NAME": project_name,
            "PROJECT_DESCRIPTION_LINE": (
                "Description: " + project_description if project_description else ""
            ),
            "SCOPE_SECTION": scope_section,
            "MAX_CANDIDATES": str(max_candidates),
            "CONTEXT": _context_snapshot(context),
            "SCHEMA_VERSION": str((context or {}).get("schema_version", "none")),
            # Whatsnext does not substitute REQUIREMENT directly — the scope
            # section already carries it — but the shared variable contract
            # exposes it for consistency with other prompts.
            "REQUIREMENT": "",
        },
    )


def _run_with_pty(argv: list[str], timeout_sec: int) -> tuple[str, str]:
    """Run *argv* inside a pseudo-terminal so CLIs that require a TTY work.

    Returns (stdout_clean, error_message).  error_message is empty on success.
    """
    try:
        master_fd, slave_fd = pty.openpty()
    except OSError as exc:
        return "", f"pty unavailable: {exc}"

    slave_closed = False
    try:
        proc = subprocess.Popen(
            argv,
            stdin=slave_fd,
            stdout=slave_fd,
            stderr=slave_fd,
            close_fds=True,
            start_new_session=True,  # isolate process group so killpg works
        )
        os.close(slave_fd)
        slave_closed = True

        chunks: list[bytes] = []
        deadline = time.monotonic() + timeout_sec

        while True:
            left = deadline - time.monotonic()
            if left <= 0:
                try:
                    os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
                except (ProcessLookupError, PermissionError):
                    proc.kill()
                proc.wait()
                return "", f"codex CLI timed out after {timeout_sec}s"

            ready, _, _ = select.select([master_fd], [], [], min(left, 0.5))
            if ready:
                try:
                    data = os.read(master_fd, 4096)
                    if data:
                        chunks.append(data)
                except OSError:
                    break

            if proc.poll() is not None:
                for _ in range(30):
                    try:
                        r, _, _ = select.select([master_fd], [], [], 0.05)
                        if not r:
                            break
                        data = os.read(master_fd, 4096)
                        if data:
                            chunks.append(data)
                    except OSError:
                        break
                break

        rc = proc.wait()
        raw = b"".join(chunks).decode("utf-8", errors="replace")
        clean = _ANSI_RE.sub("", raw).replace("\r\n", "\n").replace("\r", "\n")
        if rc != 0:
            return "", f"codex CLI failed (exit {rc}): {clean[-600:]}"
        return clean, ""
    finally:
        try:
            os.close(master_fd)
        except OSError:
            pass
        if not slave_closed:
            try:
                os.close(slave_fd)
            except OSError:
                pass


def _invoke_agent(prompt: str, timeout_sec: int) -> tuple[str, str, dict[str, Any]]:
    """Return (stdout, error, cli_info). error is empty on success."""
    agent = (os.environ.get("ANPM_ADAPTER_AGENT") or "claude").strip().lower()
    model = (os.environ.get("ANPM_ADAPTER_MODEL") or "").strip()
    model_source = "override" if model else "default"

    if agent == "claude":
        binary = shutil.which("claude")
        if not binary:
            return "", "claude CLI not found on PATH (install Claude Code)", {}
        if not model:
            model = DEFAULT_CLAUDE_MODEL
            model_source = "default"
        argv = [binary, "-p", prompt, "--model", model]
    elif agent == "codex":
        binary = shutil.which("codex")
        if not binary:
            return "", "codex CLI not found on PATH (install Codex CLI)", {}
        if not model:
            discovered = _discover_codex_model(binary)
            if discovered:
                model = discovered
                model_source = "subscription"
            else:
                model_source = "default"
        # Pass prompt as positional argument; no "exec" subcommand in v0.121+.
        argv = [binary, prompt]
        if model:
            argv.extend(["--model", model])
    else:
        return "", f"unsupported ANPM_ADAPTER_AGENT={agent!r} (expected 'claude' or 'codex')", {}

    cli_info: dict[str, Any] = {"agent": agent, "model": model, "model_source": model_source}
    _debug(f"invoking {agent}: {binary} model={model or '(default)'} source={model_source} (prompt_len={len(prompt)})")
    if agent == "codex":
        # Codex v0.121+ checks process.stdin.isTTY and refuses to run when stdin
        # is not a terminal.  We allocate a PTY so it believes it has one.
        output, err = _run_with_pty(argv, timeout_sec)
        return output, err, cli_info

    # Claude: use -p flag for the prompt; close stdin so it never blocks.
    try:
        proc = subprocess.run(
            argv,
            capture_output=True,
            text=True,
            timeout=timeout_sec,
            check=False,
            stdin=subprocess.DEVNULL,
        )
    except subprocess.TimeoutExpired:
        return "", f"{agent} CLI timed out after {timeout_sec}s", cli_info

    if proc.returncode != 0:
        stderr = (proc.stderr or "").strip()
        stdout = (proc.stdout or "").strip()
        detail = stderr or stdout or f"exit code {proc.returncode}"
        return "", f"{agent} CLI failed: {detail[:400]}", cli_info

    return proc.stdout or "", "", cli_info


_JSON_BLOCK_RE = re.compile(r"```(?:json)?\s*\n(.*?)```", re.DOTALL | re.IGNORECASE)


def _extract_json(text: str) -> dict[str, Any]:
    text = text.strip()
    # Strategy 1: fenced code block.
    match = _JSON_BLOCK_RE.search(text)
    if match:
        candidate = match.group(1).strip()
        try:
            return json.loads(candidate)
        except json.JSONDecodeError:
            pass
    # Strategy 2: first balanced { … } block.
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        fragment = text[start:end + 1]
        try:
            return json.loads(fragment)
        except json.JSONDecodeError:
            pass
    raise ValueError(f"no valid JSON found in agent output (first 300 chars): {text[:300]!r}")


def _validate_candidates(data: dict[str, Any]) -> list[dict[str, Any]]:
    candidates = data.get("candidates")
    if not isinstance(candidates, list):
        raise ValueError(f"'candidates' must be a list, got {type(candidates)}")
    validated: list[dict[str, Any]] = []
    for i, c in enumerate(candidates):
        if not isinstance(c, dict):
            raise ValueError(f"candidate {i} is not an object")
        title = str(c.get("title") or "").strip()
        if not title:
            raise ValueError(f"candidate {i} has empty title")
        validated.append({
            "title":               title[:120],
            "description":         str(c.get("description") or ""),
            "rationale":           str(c.get("rationale") or ""),
            "validation_criteria": str(c.get("validation_criteria") or ""),
            "po_decision":         str(c.get("po_decision") or ""),
            "priority_score":      float(c.get("priority_score") or 0.0),
            "confidence":          float(c.get("confidence") or 0.0),
            "rank":                int(c.get("rank") or (i + 1)),
            "evidence":            [str(e) for e in (c.get("evidence") or [])],
            "duplicate_titles":    [str(t) for t in (c.get("duplicate_titles") or [])],
        })
    return validated


def main() -> None:
    try:
        request = _read_request()
    except Exception as exc:
        _write_response([], f"failed to read request: {exc}")
        return

    # Per-run model override takes precedence over the connector env var.
    run_model_override = ((request.get("run") or {}).get("model_override") or "").strip()
    if run_model_override:
        os.environ["ANPM_ADAPTER_MODEL"] = run_model_override

    timeout_sec = DEFAULT_TIMEOUT_SEC
    try:
        timeout_sec = int(os.environ.get("ANPM_ADAPTER_TIMEOUT") or DEFAULT_TIMEOUT_SEC)
    except ValueError:
        pass

    _debug(f"building prompt for project={request.get('project', {}).get('name', '?')!r}")
    prompt = _build_prompt(request)
    _debug(f"prompt_len={len(prompt)}")

    raw_output, err, cli_info = _invoke_agent(prompt, timeout_sec)
    if err:
        _write_response([], err, cli_info or None)
        return

    _debug(f"agent output_len={len(raw_output)}")

    try:
        data = _extract_json(raw_output)
        candidates = _validate_candidates(data)
    except Exception as exc:
        _write_response([], f"failed to parse agent output: {exc}", cli_info)
        return

    _write_response(candidates, cli_info=cli_info)


if __name__ == "__main__":
    main()
