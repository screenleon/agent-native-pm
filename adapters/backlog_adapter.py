#!/usr/bin/env python3
"""
anpm-backlog-adapter — exec-json adapter for the Agent Native PM local
connector. It reads a JSON request on stdin, invokes a local LLM CLI
(Claude Code or Codex CLI), parses ranked backlog candidates out of the
model output, and writes a JSON response on stdout.

Contract (stdin):
  {
    "run": {...},
    "requirement": {"id": ..., "title": ..., "description": ..., "summary": ...},
    "requested_max_candidates": 3,
    "planning_context": {
      "schema_version": "context.v1",
      "sources": {
        "open_tasks": [...],
        "recent_documents": [...],
        "open_drift_signals": [...],
        "latest_sync_run": {...} | null,
        "recent_agent_runs": [...]
      },
      ...
    }
  }

Contract (stdout):
  {
    "candidates": [
      {
        "title": "...",
        "description": "...",
        "rationale": "...",
        "priority_score": 0.0-1.0,
        "confidence": 0.0-1.0,
        "rank": 1,
        "evidence": ["doc:<id>", "task:<id>", ...],
        "duplicate_titles": ["..."]
      },
      ...
    ],
    "error_message": ""  // only on failure
  }

Environment variables:
  ANPM_ADAPTER_AGENT    "claude" (default) | "codex"
  ANPM_ADAPTER_MODEL    Optional model override passed to the CLI.
  ANPM_ADAPTER_TIMEOUT  Subprocess timeout seconds (default 60).
  ANPM_ADAPTER_DEBUG    If "1", write diagnostic traces to stderr.

Exits 0 on both success and handled failure (failures are reported in JSON).
Non-zero exit is reserved for hard process errors.
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
from typing import Any


# Strip ANSI escape codes from PTY output.
_ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")

DEFAULT_TIMEOUT_SEC = 300  # 5 min; override with ANPM_ADAPTER_TIMEOUT
DEFAULT_MAX_CANDIDATES = 3
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


def _debug(message: str) -> None:
    if os.environ.get("ANPM_ADAPTER_DEBUG") == "1":
        print(f"[anpm-adapter] {message}", file=sys.stderr, flush=True)


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

    # Fallback: query OpenAI API via SDK (available when Codex is installed).
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


def _requirement_snippet(requirement: dict[str, Any] | None) -> str:
    if not requirement:
        return "(no requirement provided)"
    parts = [f"Title: {requirement.get('title', '').strip() or '(untitled)'}"]
    summary = (requirement.get("summary") or "").strip()
    description = (requirement.get("description") or "").strip()
    if summary:
        parts.append(f"Summary: {summary}")
    if description:
        parts.append(f"Description: {description}")
    return "\n".join(parts)


def _context_snippet(context: dict[str, Any] | None) -> str:
    if not context:
        return "(no planning context provided)"
    sources = context.get("sources") or {}
    out: list[str] = []

    tasks = sources.get("open_tasks") or []
    if tasks:
        out.append("Open tasks:")
        for task in tasks[:20]:
            out.append(
                f"  - [{task.get('status', '')}] {task.get('title', '')} "
                f"(id={task.get('id', '')}, priority={task.get('priority', '')})"
            )

    documents = sources.get("recent_documents") or []
    if documents:
        out.append("Recent documents:")
        for doc in documents[:12]:
            stale = " STALE" if doc.get("is_stale") else ""
            out.append(
                f"  - {doc.get('title', '')} ({doc.get('file_path', '')},"
                f" type={doc.get('doc_type', '')}{stale}, id={doc.get('id', '')})"
            )

    drift = sources.get("open_drift_signals") or []
    if drift:
        out.append("Open drift signals:")
        for signal in drift[:8]:
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
            line += f", error={err}"
        out.append(line)

    agent_runs = sources.get("recent_agent_runs") or []
    if agent_runs:
        out.append("Recent agent runs:")
        for run in agent_runs[:6]:
            out.append(
                f"  - {run.get('agent_name', '')} / {run.get('action_type', '')}"
                f" ({run.get('status', '')}): {run.get('summary', '')}"
            )

    dropped = (context.get("meta") or {}).get("dropped_counts") or {}
    if any(dropped.values()):
        out.append(f"(note: some context entries were dropped under byte cap: {dropped})")

    return "\n".join(out) if out else "(context is empty)"


def _build_prompt(request: dict[str, Any]) -> str:
    requirement = request.get("requirement") or {}
    project = request.get("project") or {}
    context = request.get("planning_context")
    max_candidates = int(request.get("requested_max_candidates") or DEFAULT_MAX_CANDIDATES)
    if max_candidates <= 0:
        max_candidates = DEFAULT_MAX_CANDIDATES

    project_name = str(project.get("name") or "").strip() or "(unnamed project)"
    project_description = str(project.get("description") or "").strip()

    instructions = f"""You are a backlog planner for the software project "{project_name}".

Decompose the requirement below into AT MOST {max_candidates} concrete backlog
candidates (tasks) scoped to THIS project. Each candidate must:
  1. Be independently implementable within "{project_name}".
  2. Reference specific evidence from the project context when relevant
     (open tasks, documents, drift signals, sync failures, recent agent runs).
     Evidence items MUST be strings of the form "doc:<id>", "task:<id>",
     "drift:<id>", "sync:<id>", or "agent_run:<id>" using the exact ids from
     the context below. Omit evidence if none applies.
  3. Not duplicate any existing open task. If you think a candidate is close
     to an existing task, add that task title to "duplicate_titles".

Return STRICT JSON inside a single ```json fenced code block with this schema:
{{
  "candidates": [
    {{
      "title": string (<= 120 chars),
      "description": string,
      "rationale": string (why this is the right next step),
      "priority_score": number between 0 and 1,
      "confidence": number between 0 and 1,
      "rank": integer starting at 1 (lower = higher priority),
      "evidence": [string, ...],
      "duplicate_titles": [string, ...]
    }}
  ]
}}

Do not include any prose outside the fenced JSON block. Do not invent ids
that are not in the context.

=== Project ===
Name: {project_name}
{("Description: " + project_description) if project_description else ""}

=== Requirement ===
{_requirement_snippet(requirement)}

=== Project context (schema={(context or {}).get('schema_version', 'none')}) ===
{_context_snippet(context)}
"""
    return instructions


def _run_with_pty(argv: list[str], timeout_sec: int) -> tuple[str, str]:
    """Run *argv* inside a pseudo-terminal so CLIs that require a TTY work.

    Returns (stdout_clean, error_message).  error_message is empty on success.
    The raw output has ANSI escape codes and carriage-returns stripped so that
    the JSON extraction regex can still find the response.
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
                # Drain any remaining bytes.
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
        # Remove ANSI codes, collapse \r\n and stray \r.
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
    # Strategy 2: first balanced { ... } run.
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        fragment = text[start:end + 1]
        try:
            return json.loads(fragment)
        except json.JSONDecodeError:
            pass
    # Strategy 3: plain parse.
    return json.loads(text)


def _coerce_float(value: Any, default: float = 0.0) -> float:
    try:
        out = float(value)
    except (TypeError, ValueError):
        return default
    if out < 0:
        return 0.0
    if out > 1:
        return 1.0
    return out


def _coerce_int(value: Any, default: int = 0) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def _normalize_candidate(raw: dict[str, Any], default_rank: int) -> dict[str, Any] | None:
    if not isinstance(raw, dict):
        return None
    title = str(raw.get("title") or "").strip()
    if not title:
        return None
    return {
        "suggestion_type": str(raw.get("suggestion_type") or "new_task").strip() or "new_task",
        "title": title[:120],
        "description": str(raw.get("description") or "").strip(),
        "rationale": str(raw.get("rationale") or "").strip(),
        "priority_score": _coerce_float(raw.get("priority_score")),
        "confidence": _coerce_float(raw.get("confidence")),
        "rank": _coerce_int(raw.get("rank"), default_rank),
        "evidence": _coerce_string_list(raw.get("evidence")),
        "duplicate_titles": _coerce_string_list(raw.get("duplicate_titles")),
    }


def _normalize_candidates(parsed: dict[str, Any], max_candidates: int) -> list[dict[str, Any]]:
    raw_items = parsed.get("candidates")
    if not isinstance(raw_items, list):
        return []
    out: list[dict[str, Any]] = []
    for idx, raw in enumerate(raw_items, start=1):
        normalized = _normalize_candidate(raw, default_rank=idx)
        if normalized is None:
            continue
        out.append(normalized)
        if len(out) >= max_candidates:
            break
    return out


def main() -> int:
    try:
        request = _read_request()
    except Exception as err:  # noqa: BLE001
        _write_response([], f"adapter failed to parse request: {err}")
        return 0

    # Per-run model override takes precedence over the connector env var.
    run_model_override = ((request.get("run") or {}).get("model_override") or "").strip()
    if run_model_override:
        os.environ["ANPM_ADAPTER_MODEL"] = run_model_override

    try:
        timeout_sec = int(os.environ.get("ANPM_ADAPTER_TIMEOUT") or DEFAULT_TIMEOUT_SEC)
    except ValueError:
        timeout_sec = DEFAULT_TIMEOUT_SEC

    max_candidates = int(request.get("requested_max_candidates") or DEFAULT_MAX_CANDIDATES)
    if max_candidates <= 0:
        max_candidates = DEFAULT_MAX_CANDIDATES

    prompt = _build_prompt(request)
    output, error, cli_info = _invoke_agent(prompt, timeout_sec=timeout_sec)
    if error:
        _write_response([], error, cli_info or None)
        return 0

    try:
        parsed = _extract_json(output)
    except Exception as err:  # noqa: BLE001
        snippet = (output or "").strip().replace("\n", " ")[:240]
        _write_response([], f"adapter could not parse agent output as JSON: {err}; first 240 chars: {snippet}", cli_info)
        return 0

    candidates = _normalize_candidates(parsed, max_candidates=max_candidates)
    if not candidates:
        _write_response([], "agent returned no valid backlog candidates", cli_info)
        return 0

    _write_response(candidates, cli_info=cli_info)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
