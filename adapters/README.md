# Local connector adapters

The Agent Native PM local connector (`bin/anpm-connector`) executes planning
runs by spawning a subprocess that implements the `exec-json` contract: one
JSON request on stdin, one JSON response on stdout. This directory ships a
reference adapter that delegates the work to a local LLM CLI.

## Scope model (multi-project by design)

A paired local connector is **scoped to your user account, not to a single
project**. The server's `claim-next-run` endpoint picks the oldest queued
planning run across all of your projects, so one device transparently serves
every project you own. Concurrency rules:

- One device = one run at a time (FIFO). Queue two planning runs on two
  different projects and the connector will process them sequentially.
- Need parallelism? Pair additional machines. Each machine holds its own
  token and claims runs independently, so two paired devices can work on
  two different projects simultaneously.
- The claim response includes the owning `project` (`{id, name, …}`) and
  the `planning_context` v1 payload, so the adapter always knows which
  project the current run belongs to.

## `backlog_adapter.py`

A Python 3 script that invokes the [Claude Code](https://docs.anthropic.com/claude/docs/claude-code)
CLI (default) or the [Codex CLI](https://github.com/openai/codex) and parses
ranked backlog candidates out of the model's response.

### Requirements

- Python 3.10+.
- `claude` or `codex` on `PATH`, already authenticated (e.g. `claude login`).

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `ANPM_ADAPTER_AGENT` | `claude` | `claude` or `codex`. |
| `ANPM_ADAPTER_MODEL` | _(unset)_ | Optional `--model` value passed to the CLI. |
| `ANPM_ADAPTER_TIMEOUT` | `60` | Subprocess timeout in seconds. |
| `ANPM_ADAPTER_DEBUG` | _(unset)_ | Set to `1` for stderr trace. |

### Standalone smoke test

```bash
cat <<'EOF' | ANPM_ADAPTER_AGENT=claude python3 adapters/backlog_adapter.py
{
  "run": {"id": "run-1"},
  "requirement": {
    "id": "req-1",
    "title": "Improve sync recovery UX",
    "description": "Users report that failed sync runs leave the dashboard in an ambiguous state."
  },
  "requested_max_candidates": 2,
  "planning_context": {
    "schema_version": "context.v1",
    "sources": {
      "open_tasks": [{"id": "task-1", "title": "Add retry button", "status": "open", "priority": "medium"}],
      "recent_documents": [{"id": "doc-1", "title": "Sync Recovery Guide", "file_path": "docs/sync-recovery.md", "doc_type": "guide", "is_stale": true}],
      "open_drift_signals": [{"id": "drift-1", "document_title": "Sync Recovery Guide", "trigger_type": "code_change", "severity": "high"}],
      "latest_sync_run": {"id": "sync-1", "status": "failed", "error_message": "unknown revision"},
      "recent_agent_runs": []
    }
  }
}
EOF
```

The expected stdout is a JSON object of the form
`{"candidates": [{"title": ..., "rank": 1, ...}, ...]}`.

## End-to-end runbook

1. **Build the server and connector**

   ```bash
   make build-backend           # produces bin/server
   make build-connector         # produces bin/anpm-connector
   ```

2. **Start the backend** (PostgreSQL + server). In a separate terminal:

   ```bash
   make dev                     # or docker compose up
   ```

3. **Log in and create a project + requirement** in the web UI
   (`http://localhost:5173`).

4. **Generate a pairing code** on the "My Connector" page and copy it.

5. **Pair the local connector** (only needed once per machine):

   ```bash
   bin/anpm-connector pair \
     --server http://localhost:18765 \
     --code   <code-from-ui> \
     --adapter-command   "$(pwd)/adapters/backlog_adapter.py" \
     --adapter-working-dir "$(pwd)" \
     --adapter-timeout   120
   ```

6. **Run the connector in the foreground**:

   ```bash
   ANPM_ADAPTER_AGENT=claude bin/anpm-connector serve
   ```

   Leave it running. It heartbeats every 30s and polls for runs every 5s.

7. **In the UI**, open the requirement, set *Execution mode* to
   *Run on this machine*, and click **Run planning**.

8. The connector will claim the run, execute the adapter (which calls
   `claude -p ...`), and POST the candidates back. The project detail page
   auto-refreshes every 3 seconds while the run is queued/leased, so the
   candidates show up without a manual reload.

## Writing a custom adapter

Any executable that honors the `exec-json` contract works — language is
unimportant. Given stdin:

```json
{
  "run": {...},
  "requirement": {...},
  "requested_max_candidates": 3,
  "planning_context": {
    "schema_version": "context.v1",
    "sources": {"open_tasks": [...], "recent_documents": [...], "open_drift_signals": [...], "latest_sync_run": {...}, "recent_agent_runs": [...]}
  }
}
```

return stdout:

```json
{
  "candidates": [
    {
      "suggestion_type": "new_task",
      "title": "...",
      "description": "...",
      "rationale": "...",
      "priority_score": 0.0,
      "confidence": 0.0,
      "rank": 1,
      "evidence": ["doc:<id>", "task:<id>", "drift:<id>", "sync:<id>", "agent_run:<id>"],
      "duplicate_titles": ["..."]
    }
  ]
}
```

On failure, return exit code 0 and `{"candidates": [], "error_message": "..."}`.
The connector forwards `error_message` to the server so the UI can surface it.

## Docker / docker-compose deployment

**Server side** (backend + Postgres + frontend): fully supported by the
repository's `docker-compose.yml`. Run `docker compose up --build` to bring
up the public API that accepts planning runs.

**Connector side**: the connector is deliberately *not* deployed via
`docker-compose`. It must run on the host where you have already authenticated
the agent CLI (for example, where `claude login` cached your Claude Code
credentials). Packaging it into a container would require baking or volume-
mounting host credentials into the container, which is brittle and leaks
secrets into image layers. Run the connector directly on your workstation:

```bash
ANPM_ADAPTER_AGENT=claude bin/anpm-connector serve
```

Typical topologies:

| Topology | Server | Connector |
| --- | --- | --- |
| Single user, single machine | `docker compose up` on laptop | `bin/anpm-connector serve` on same laptop |
| Remote server | `docker compose up` on VPS / Kubernetes | `bin/anpm-connector serve` on each developer's workstation, pointing at the remote server URL |
| Multi-project, parallel planning | Same server deployment | One `bin/anpm-connector serve` per machine; the server fans runs out to the first connector that polls |

Nothing stops you from running the connector inside a container if you
manage the CLI auth yourself — the contract is just "read stdin, write
stdout" — but it is not the supported deployment path.
