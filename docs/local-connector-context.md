---
title: Local Connector Planning Context (context.v1)
status: approved-with-changes
owner: feature-planner
phase: planning
source: "[agent:feature-planner]"
last_updated: 2026-04-20
revision: v2
---

# Local Connector Planning Context (`context.v1`)

## 1. Summary

The local connector path (`exec-json` adapter invoked via `POST /api/connector/claim-next-run`) currently forwards only `{ Run, Requirement }` to external adapter processes. This plan adds a versioned, sanitized, byte-bounded `planning_context` payload so the adapter can produce high-quality backlog candidates grounded in the project's real state (open tasks, recent documents, drift signals, latest sync run, recent agent runs).

This is directly tied to the product's core MVP capability: **"Agent auto-decomposes requirements into backlog candidates."** Without context, the adapter only sees the requirement title/body and cannot avoid duplicating existing tasks, cannot cite documents as evidence, and cannot react to drift — all of which are part of why this product exists. Phase A is therefore scoped as foundational MVP work, not speculative infrastructure.

v2 incorporates critic review fixes: wire types live in a leaf package to eliminate the `models ↔ planning` import cycle and keep the connector binary free of server-only code; `ContextBudget`, `directives`, and `overrides_hash` are deferred to later phases; sanitizer regexes are tightened to stop eating commit SHAs.

## 2. Goals and Non-goals

### Goals

- Attach a structured `planning_context` payload to the local connector claim response.
- Carry `planning_context` through the adapter stdin (`exec-json`) as an opaque pass-through from the connector's perspective.
- Apply a minimal content sanitizer to redact secret-shaped substrings in free-form fields.
- Enforce a hard byte ceiling on the `sources` block and record what was dropped.
- Version the contract (`context.v1`) with a forward-compat "ignore unknown fields" rule.
- Reuse existing ranking and compaction helpers (`planningDocumentRelevanceScore`, `requirementKeywords`, `compactDocumentsForPrompt`, `compactAgentRunsForPrompt`, `compactSyncRunForPrompt`, `compactDriftSignalsForPrompt`).
- Preserve current per-source limits (`planningTaskContextLimit`, `planningDocumentContextLimit`, `planningDriftContextLimit`, `planningAgentRunContextLimit`) as Phase A defaults so behavior does not regress.
- Keep the sanitized context a deep-copied value — never mutate the caller's `PlanningContext`.

### Non-goals (Phase A)

- No embeddings or vector retrieval.
- No repo-wide symbol map / code graph ranking.
- No MCP migration or protocol change for the adapter contract.
- No streaming context (request/response remains single JSON).
- No automatic summarization of documents, tasks, or agent runs.
- No full document body transmission — metadata only, mirroring `compactDocumentsForPrompt`.
- No user-declared `@file` / `@folder` picker.
- No `ContextBudget` struct yet — Phase A uses package constants.
- No `directives` field on the wire — Phase A omits it entirely; Phase C re-introduces it if needed.
- No `meta.overrides_hash` field on the wire — Phase C only, alongside connector overrides.

## 3. Affected Modules / Files

### Server (Go)

- `backend/internal/planning/wire/` — **new leaf package**. Contains pure DTO types, sanitizer, byte-cap reducer. Imports `models` only.
  - `backend/internal/planning/wire/context_v1.go` — types, constants, `ContextSchemaV1`, `SanitizerVersion`.
  - `backend/internal/planning/wire/sanitizer.go` — `SanitizePlanningContextV1`, regex set.
  - `backend/internal/planning/wire/reducer.go` — byte-cap reducer for `sources`.
- `backend/internal/planning/context_builder.go` — add `BuildContextV1(requirement)` returning `*wire.PlanningContextV1`, composed from existing `Build`, sanitizer, reducer, ranking.
- `backend/internal/planning/provider.go` — unchanged (keeps in-memory `PlanningContext`).
- `backend/internal/planning/openai_compatible_provider.go` — unchanged in Phase A.
- `backend/internal/models/local_connector.go` — extend `LocalConnectorClaimNextRunResponse` to hold `*wire.PlanningContextV1`. Valid because `models` ← `wire` ← `models` is NOT a cycle: `wire` only imports `models`, never vice versa.
- `backend/internal/handlers/local_connectors.go` — `ClaimNextRun` calls `BuildContextV1`, attaches payload. On builder error: log + attach nothing, claim still succeeds.
- `backend/internal/router/router.go` — **Phase D only** registers snapshot endpoint.
- `backend/db/migrations/` — no migration required in Phase A.

### Connector (Go)

- `backend/internal/connector/adapter.go` — `ExecJSONInput.PlanningContext *wire.PlanningContextV1`. Connector imports `wire` (leaf), NOT `planning`.
- `backend/internal/connector/service.go` — pass payload through unchanged.
- `backend/internal/connector/state.go` / `client.go` / `cmd/connector/main.go` — Phase C only.

### Frontend

- Phase D only. No changes in Phase A.

### Docs

- `docs/data-model.md` — Phase A: document `PlanningContextV1` wire shape.
- `docs/api-surface.md` — Phase D only.
- `docs/subscription-connector-mvp.md` — Phase A: adapter contract section updated.
- `DECISIONS.md` — Phase A entries per §10.

## 4. Data Contract: `context.v1`

### 4.1 Wire JSON (server → connector claim response)

```json
{
  "data": {
    "run": { "id": "...", "requirement_id": "...", "execution_mode": "local_connector" },
    "requirement": { "id": "...", "title": "...", "body": "..." },
    "planning_context": {
      "schema_version": "context.v1",
      "generated_at": "2026-04-20T12:34:56Z",
      "generated_by": "server",
      "sanitizer_version": "v1",
      "limits": {
        "max_open_tasks": 100,
        "max_recent_documents": 8,
        "max_open_drift_signals": 6,
        "max_recent_agent_runs": 6,
        "include_latest_sync_run": true,
        "max_sources_bytes": 262144
      },
      "sources": {
        "open_tasks": [
          { "id": "...", "title": "...", "status": "open", "priority": 2, "updated_at": "..." }
        ],
        "recent_documents": [
          {
            "id": "...",
            "title": "...",
            "file_path": "docs/...",
            "doc_type": "spec",
            "is_stale": false,
            "staleness_days": 0
          }
        ],
        "open_drift_signals": [
          {
            "id": "...",
            "document_title": "...",
            "trigger_type": "...",
            "trigger_detail": "...",
            "severity": "high",
            "opened_at": "..."
          }
        ],
        "latest_sync_run": {
          "id": "...",
          "status": "success",
          "started_at": "...",
          "completed_at": "...",
          "error_message": ""
        },
        "recent_agent_runs": [
          {
            "id": "...",
            "agent_name": "backend-architect",
            "action_type": "plan",
            "status": "succeeded",
            "started_at": "...",
            "summary": "..."
          }
        ]
      },
      "meta": {
        "ranking": {
          "documents": "relevance_v1",
          "tasks": "updated_at_desc",
          "drift_signals": "severity_desc_then_opened_at_desc",
          "agent_runs": "started_at_desc_excluding_self_planner"
        },
        "dropped_counts": {
          "open_tasks": 0,
          "recent_documents": 0,
          "open_drift_signals": 0,
          "recent_agent_runs": 0
        },
        "sources_bytes": 18421
      }
    }
  },
  "error": null,
  "meta": { "request_id": "..." }
}
```

Notes:

- **Envelope preserved.** `planning_context` is nested under `data`, alongside `run` and `requirement`.
- **Metadata-only documents.** `recent_documents` has no `body` field. Matches `compactDocumentsForPrompt`.
- **Stable ranking taxonomy.** `meta.ranking` values are wire-stable names, not Go function identifiers. Renaming internal functions does not break the contract.
- **Byte accounting scope.** `max_sources_bytes` applies to the marshaled `sources` object, NOT the entire `planning_context` or the HTTP envelope. Scaffolding overhead is excluded from the cap.
- **`directives` omitted in Phase A.** Phase C will introduce it. Phase A adapters must not expect it.
- **`overrides_hash` omitted in Phase A.** Phase C only.

### 4.2 Go types (leaf package)

```go
// backend/internal/planning/wire/context_v1.go
package wire

import (
    "time"

    "github.com/screenleon/agent-native-pm/internal/models"
)

const (
    ContextSchemaV1        = "context.v1"
    SanitizerVersion       = "v1"
    DefaultMaxSourcesBytes = 256 * 1024 // 256 KiB
)

// PlanningContextV1 is the wire shape of planning context sent to the local
// connector adapter. It is a pure DTO; it has no methods that touch stores
// or make network calls. The connector binary imports this type directly.
type PlanningContextV1 struct {
    SchemaVersion    string                 `json:"schema_version"`
    GeneratedAt      time.Time              `json:"generated_at"`
    GeneratedBy      string                 `json:"generated_by"`
    SanitizerVersion string                 `json:"sanitizer_version"`
    Limits           PlanningContextLimits  `json:"limits"`
    Sources          PlanningContextSources `json:"sources"`
    Meta             PlanningContextMeta    `json:"meta"`
}

type PlanningContextLimits struct {
    MaxOpenTasks         int  `json:"max_open_tasks"`
    MaxRecentDocuments   int  `json:"max_recent_documents"`
    MaxOpenDriftSignals  int  `json:"max_open_drift_signals"`
    MaxRecentAgentRuns   int  `json:"max_recent_agent_runs"`
    IncludeLatestSyncRun bool `json:"include_latest_sync_run"`
    MaxSourcesBytes      int  `json:"max_sources_bytes"`
}

type PlanningContextSources struct {
    OpenTasks        []WireTask        `json:"open_tasks"`
    RecentDocuments  []WireDocument    `json:"recent_documents"`
    OpenDriftSignals []WireDriftSignal `json:"open_drift_signals"`
    LatestSyncRun    *WireSyncRun      `json:"latest_sync_run,omitempty"`
    RecentAgentRuns  []WireAgentRun    `json:"recent_agent_runs"`
}

type WireTask struct {
    ID        string    `json:"id"`
    Title     string    `json:"title"`
    Status    string    `json:"status"`
    Priority  int       `json:"priority"`
    UpdatedAt time.Time `json:"updated_at"`
}

type WireDocument struct {
    ID            string `json:"id"`
    Title         string `json:"title"`
    FilePath      string `json:"file_path"`
    DocType       string `json:"doc_type"`
    IsStale       bool   `json:"is_stale"`
    StalenessDays int    `json:"staleness_days"`
}

type WireDriftSignal struct {
    ID            string    `json:"id"`
    DocumentTitle string    `json:"document_title"`
    TriggerType   string    `json:"trigger_type"`
    TriggerDetail string    `json:"trigger_detail"`
    Severity      string    `json:"severity"`
    OpenedAt      time.Time `json:"opened_at"`
}

type WireSyncRun struct {
    ID           string     `json:"id"`
    Status       string     `json:"status"`
    StartedAt    time.Time  `json:"started_at"`
    CompletedAt  *time.Time `json:"completed_at,omitempty"`
    ErrorMessage string     `json:"error_message"` // truncated to 240 chars upstream
}

type WireAgentRun struct {
    ID         string    `json:"id"`
    AgentName  string    `json:"agent_name"`
    ActionType string    `json:"action_type"`
    Status     string    `json:"status"`
    StartedAt  time.Time `json:"started_at"`
    Summary    string    `json:"summary"` // truncated to 180 chars upstream
}

type PlanningContextMeta struct {
    Ranking       map[string]string `json:"ranking"`
    DroppedCounts map[string]int    `json:"dropped_counts"`
    SourcesBytes  int               `json:"sources_bytes"`
    // Warnings carries non-fatal context-build signals (for example, a
    // store read failure for documents). Always present; empty slice when
    // nothing went wrong. Adapters MUST tolerate the field but MAY ignore
    // it; surfacing it in adapter logs is recommended for diagnostics.
    // See DECISIONS 2026-04-21.
    Warnings []string `json:"warnings"`
}

// Reference back to model constants used by callers that build this DTO.
var _ = models.TaskStatusOpen // compile-time check the dependency exists
```

```go
// backend/internal/models/local_connector.go (Phase A + Path B S2)
import "github.com/screenleon/agent-native-pm/internal/planning/wire"

type LocalConnectorClaimNextRunResponse struct {
    Run             *PlanningRun                  `json:"run"`
    Requirement     *Requirement                  `json:"requirement"`
    Project         *Project                      `json:"project,omitempty"`
    PlanningContext *wire.PlanningContextV1       `json:"planning_context,omitempty"`
    // Path B S2 — present iff the leased run had account_binding_id.
    // Sourced from the run's connector_cli_info.binding_snapshot, never
    // from a live binding lookup (R10 mitigation: snapshot survives
    // binding deletion). Old connectors that don't recognise this field
    // simply ignore it via standard json.Unmarshal forward-compat
    // (see §4.4 — DisallowUnknownFields is forbidden).
    CliBinding      *PlanningRunCliBindingPayload `json:"cli_binding,omitempty"`
}

type PlanningRunCliBindingPayload struct {
    ID         string `json:"id"`
    ProviderID string `json:"provider_id"`           // "cli:claude" | "cli:codex"
    ModelID    string `json:"model_id,omitempty"`
    CliCommand string `json:"cli_command,omitempty"`
    Label      string `json:"label,omitempty"`
}
```

```go
// backend/internal/connector/adapter.go (Phase A + Path B S2)
import "github.com/screenleon/agent-native-pm/internal/planning/wire"

type ExecJSONInput struct {
    Run                    *models.PlanningRun     `json:"run"`
    Requirement            *models.Requirement     `json:"requirement"`
    RequestedMaxCandidates int                     `json:"requested_max_candidates"`
    PlanningContext        *wire.PlanningContextV1 `json:"planning_context,omitempty"`
    // Path B S2 — populated by the connector when the claim response
    // carried a cli_binding block. Adapter precedence per design D4:
    // cli_selection > ANPM_ADAPTER_* env vars > built-in default.
    CliSelection           *AdapterCliSelection    `json:"cli_selection,omitempty"`
}

type AdapterCliSelection struct {
    ProviderID string `json:"provider_id"`
    ModelID    string `json:"model_id,omitempty"`
    CliCommand string `json:"cli_command,omitempty"`
}
```

The connector enforces a 264 KiB cap on the marshalled `ExecJSONInput`
envelope before subprocess spawn (256 KiB planning context ceiling +
8 KiB headroom for run/requirement/cli_selection). Overflow returns
`success=false` with an `adapter_protocol_error` hint instead of
launching a doomed subprocess (R5 / R7 mitigation).

Layering proof (no cycles):

```
models   ← no imports from planning / wire / connector / handlers
wire     ← imports models only
planning ← imports models and wire
connector← imports models and wire (never planning)
handlers ← imports models, wire, planning
```

### 4.3 Truncation constants (reused)

- `WireAgentRun.Summary` ≤ 180 chars (reused from `compactAgentRunsForPrompt`).
- `WireSyncRun.ErrorMessage` ≤ 240 chars (reused from `compactSyncRunForPrompt`).
- `WireDocument` excludes body entirely (reused from `compactDocumentsForPrompt`).

### 4.4 Forward-compat

- Adapters and clients MUST ignore unknown fields. Go decoders must not use `DisallowUnknownFields` on this payload.
- Phase A adapters MUST treat `planning_context == nil` as "context unavailable — proceed with run + requirement only." This is the degraded mode when the server is older or the builder returned an error.
- Adding new fields inside the current structure is a non-breaking change; renaming or removing a field requires `context.v2`.

### 4.5 Time source

`generated_at` is set to `time.Now().UTC()` at the moment the server serializes the response. It is not sourced from DB row timestamps.

## 5. Ranking and Byte-cap Strategy

### 5.1 Per-source defaults (Phase A)

Phase A does NOT introduce a `ContextBudget` struct. Defaults are package constants in `wire`:

```go
const (
    DefaultMaxOpenTasks         = 100  // mirrors planningTaskContextLimit
    DefaultMaxRecentDocuments   = 8    // mirrors planningDocumentContextLimit
    DefaultMaxOpenDriftSignals  = 6    // mirrors planningDriftContextLimit
    DefaultMaxRecentAgentRuns   = 6    // mirrors planningAgentRunContextLimit
    DefaultIncludeLatestSyncRun = true
    DefaultMaxSourcesBytes      = 256 * 1024
)
```

These mirror the constants already used by `ProjectContextBuilder.Build`, so Phase A cannot regress existing behavior.

### 5.2 Ranking rules

- `recent_documents`: `planningDocumentRelevanceScore` seeded by `requirementKeywords(requirement)`, tie-break on `updated_at` desc.
- `open_tasks`: `updated_at` desc.
- `open_drift_signals`: `severity` desc (critical > high > medium > low), tie-break on `opened_at` desc.
- `recent_agent_runs`: `started_at` desc, excluding runs authored by the self-planner (mirrors `compactAgentRunsForPrompt` exclusion).
- `latest_sync_run`: single latest by `started_at`.

### 5.3 Byte-cap reducer (scope: `sources` only)

```
BUILD:
  1. Marshal sources to JSON.
  2. If len(sources_json) <= DefaultMaxSourcesBytes → done.
  3. Determine "largest source" = the single source in PlanningContextSources
     whose marshaled JSON byte count is largest after each round.
  4. Drop the lowest-ranked item from that source. Record in dropped_counts.
  5. Re-marshal and repeat until under cap or all sources are empty.
  6. If all sources are empty and the empty-sources JSON still exceeds cap,
     log a warning and return the empty-sources payload. In practice this
     cannot happen because the empty-sources shape is < 200 bytes.
```

"Largest source" is defined precisely as: **bytes of the marshaled JSON for that named field in `PlanningContextSources`**, re-measured each round.

The cap applies to `sources` only. `schema_version`, `limits`, `meta`, and scaffolding are NOT counted against `max_sources_bytes`. This keeps scaffolding overhead predictable and prevents pathological claim failures.

`meta.sources_bytes` records the final marshaled size of `sources`. `meta.dropped_counts` is always present with zero values even when nothing was dropped.

## 6. Error Handling (Phase A)

`BuildContextV1` may fail because a store call fails (DB hiccup, timeout). The claim handler behavior on error:

```go
ctx, err := builder.BuildContextV1(requirement)
if err != nil {
    log.Warn("planning context build failed; proceeding without context",
        "requirement_id", requirement.ID, "error", err)
    ctx = nil
}
// attach ctx; it may be nil (omitempty) — adapter handles gracefully
```

Rationale: planning availability must not be gated on the context builder. A degraded adapter run (no context) is strictly better than a failed claim that leaves the run stuck in `queued`.

## 7. Sanitizer Design

New file: `backend/internal/planning/wire/sanitizer.go`.

```go
// Package wire: sanitizer.go
package wire

// SanitizePlanningContextV1 returns a deep copy of ctx with secret-shaped
// substrings in free-form fields replaced by [REDACTED]. The input is never
// mutated. Phase A scope: AgentRun.Summary and SyncRun.ErrorMessage only.
func SanitizePlanningContextV1(ctx PlanningContextV1) PlanningContextV1
```

### Scope (Phase A)

- **In scope**: `WireAgentRun.Summary`, `WireSyncRun.ErrorMessage`.
- **Out of scope**: all other fields. Titles, file paths, drift trigger details are curated and low-risk; scanning them causes false positives.

### Redaction patterns (prefix-anchored, tightened)

The v1 regex set is deliberately narrow to avoid eating commit SHAs, UUIDs, and prose English.

| Pattern (case-insensitive) | Matches | Example |
|---|---|---|
| `sk-[A-Za-z0-9]{20,}` | OpenAI-style keys | `sk-abc...` |
| `AKIA[0-9A-Z]{16}` | AWS access key IDs | `AKIAIOSFODNN7EXAMPLE` |
| `-----BEGIN [A-Z ]*PRIVATE KEY-----` | PEM headers | PGP/RSA keys |
| `bearer\s+[A-Za-z0-9._\-]{16,}` | Bearer tokens (min 16 chars — avoids prose) | `Bearer eyJhbGciOiJI...` |
| `https?://[^\s/]+:[^\s/]+@` | Basic-auth URLs | `https://user:pass@host/` |
| `(password\|passwd\|secret\|token\|api[_-]?key)\s*[:=]\s*\S+` | Labeled secrets | `password=hunter2` |
| `sha256:[A-Fa-f0-9]{32,}` | Labeled SHA-256 | `sha256:abc123...` |
| `Authorization:\s*\S+` | Literal header dumps | `Authorization: Bearer ...` |

Explicitly **removed from v1**: bare `\b[A-Fa-f0-9]{32,}\b`. This was dropping 40-char git commit SHAs and legitimate hashes in error messages. The prefix-anchored variants above cover the real secret cases without damaging debugging signal.

Replacement token: `[REDACTED]`. Sanitizer version: `v1`. Pattern list lives next to the sanitizer and is covered by both a "must-redact" and "must-NOT-redact" test fixture.

### Deep-copy contract

`SanitizePlanningContextV1` MUST return a value such that mutating any field of the returned value does not affect the caller's input. Slices and maps are cloned; pointer fields (`LatestSyncRun`) are dereferenced into a new allocation.

### Source marker

- `GeneratedBy = "server"` — payload-level marker.
- `SanitizerVersion = "v1"` — rotated only when regex set changes.
- Existing `[agent:...]` markers in `Summary` MUST be preserved. The sanitizer is substring-replacement only; it does not parse or mutate marker prefixes.

## 8. UI Transparency (Phase D — read-only)

Deferred. Not part of Phase A.

When shipped:

- `GET /api/planning-runs/:id/context-snapshot` returns the current `PlanningContextV1` that would be sent for this run, recomputed on demand (no persistence).
- Read-only. Must not mutate run state, adapter input, or any persisted context.
- Authz: same as reading the planning run.
- Frontend: collapsed JSON viewer on `PlanningRunDetail.tsx`. No edit controls.
- Doc contract: "advisory snapshot, may differ from what the adapter actually received at claim time." Phase D explicitly accepts non-reproducibility as a trade-off to avoid a new migration.

`DECISIONS.md` must record "Phase D snapshot is display-only" so future work does not turn it into a context override surface.

## 9. Phased Roadmap

### Phase A — minimum viable (this plan's implementation target)

Concrete file-level changes:

1. Create `backend/internal/planning/wire/context_v1.go` with types + constants above.
2. Create `backend/internal/planning/wire/sanitizer.go` with `SanitizePlanningContextV1` + regex set.
3. Create `backend/internal/planning/wire/reducer.go` with the byte-cap reducer scoped to `sources`.
4. Create `backend/internal/planning/wire/wire_test.go` covering sanitizer (must-redact + must-NOT-redact), reducer (under-cap, over-cap, drop order, floor case), golden shape snapshot.
5. Add `BuildContextV1(requirement *models.Requirement) (*wire.PlanningContextV1, error)` to `backend/internal/planning/context_builder.go`. Implementation: call existing `Build`, translate `PlanningContext` into `wire.PlanningContextV1` using existing compaction helpers, call sanitizer, call reducer, set `GeneratedAt = time.Now().UTC()`, set `Limits` to defaults, return pointer.
6. Extend `LocalConnectorClaimNextRunResponse` in `backend/internal/models/local_connector.go`.
7. Update `backend/internal/handlers/local_connectors.go` `ClaimNextRun` per §6 error handling rule; `ClaimNextRun` test asserts presence and shape.
8. Extend `ExecJSONInput` in `backend/internal/connector/adapter.go` and pass through in `service.go`.
9. Update `docs/data-model.md` and `docs/subscription-connector-mvp.md`.
10. Record `DECISIONS.md` entries (§10).

### Phase B — `ContextBudget` plumbed through

- Introduce `ContextBudget` struct (the one deferred from Phase A).
- `BuildContextV1(requirement, budget)` signature change; default comes from package constant.
- `openai_compatible_provider` helpers take budget instead of hard-coded limits.
- Table-test the budget-variation behavior.

### Phase C — connector overrides + heartbeat capability

- `connector.json` gains `context_overrides`.
- Connector state computes `overrides_hash = sha256(canonical_json(context_overrides))[:16]`.
- Heartbeat `capabilities` carries `context_overrides_hash`.
- Handler resolves effective budget: defaults ← user planning settings ← connector overrides.
- Wire schema adds `meta.overrides_hash`. Still `context.v1` (additive).
- `doctor` prints effective budget + overrides hash.

### Phase D — optional UI snapshot

- Endpoint + Frontend panel per §8.

## 10. Validation Plan (Phase A done-definition)

### Tests

- `wire/sanitizer_test.go`:
  - Must-redact fixtures for every regex (OpenAI key, AWS key, PEM header, bearer, basic-auth URL, labeled secrets, `sha256:` label, `Authorization:` header).
  - Must-NOT-redact fixtures for: 40-char git SHA in prose, 64-char hex hash with no prefix, UUID `xxxxxxxx-xxxx-...`, English prose containing "bearer token missing", numeric-only IDs, markdown code blocks.
  - Deep-copy test: mutate the returned value; assert input is unchanged.
- `wire/reducer_test.go`:
  - Under-cap: no drops, `sources_bytes` recorded.
  - Over-cap single source: drops lowest-ranked until under cap.
  - Over-cap multi-source: drops from largest-in-bytes each round.
  - Floor: all sources empty → `dropped_counts` equals original counts, `sources_bytes` < cap.
  - Property test (lightweight): for randomized sources of varying sizes, final `sources_bytes <= DefaultMaxSourcesBytes` OR all sources were dropped.
- `wire/context_v1_test.go`: golden shape test locking field names and JSON tags. Failure prompts a `DECISIONS.md` entry.
- `handlers/local_connectors_test.go`:
  - `ClaimNextRun` happy path: response contains `planning_context`, `schema_version == "context.v1"`, `generated_by == "server"`, `sanitizer_version == "v1"`, `meta.sources_bytes > 0`.
  - `ClaimNextRun` error path: `BuildContextV1` fails → response still 200, `planning_context` omitted, warn logged.
  - No-DB-write test for Phase D endpoint is deferred to Phase D.
- `connector/adapter_test.go`:
  - `ExecJSONInput` round-trips `PlanningContext` through `json.Marshal` → `json.Unmarshal` unchanged.
  - Forward-compat: JSON with an unknown top-level key in `planning_context` decodes successfully into `PlanningContextV1`.
  - Older-server-simulation: JSON without `planning_context` decodes successfully; resulting struct field is nil.

### Commands

- `make test` green.
- `make lint` green.
- No new `go vet` warnings.

### Docs

- `docs/data-model.md` updated with wire types and layering note.
- `docs/subscription-connector-mvp.md` adapter contract section updated.
- `docs/api-surface.md` NOT changed in Phase A (same endpoint, additive field).

### `DECISIONS.md` entries (Phase A)

1. Adopt `context.v1` as the connector planning context wire contract.
2. Wire DTOs live in `backend/internal/planning/wire` (leaf package) to avoid import cycle and keep connector binary free of server-only code.
3. Phase A documents are metadata-only (no body transmission); matches existing `compactDocumentsForPrompt` behavior.
4. Sanitizer v1 scope limited to `AgentRun.Summary` and `SyncRun.ErrorMessage`; bare hex regex explicitly excluded to preserve commit SHAs in diagnostic text.
5. Byte cap applies to `sources` only, not to the full `planning_context` or envelope.
6. Builder error on claim → log + omit context, claim still succeeds.
7. `ContextBudget` struct, `directives`, `meta.overrides_hash` deferred to Phase B/C.
8. Phase D snapshot, when shipped, is display-only; may not mutate adapter input.

## 11. Risks & Open Questions

### Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Body-less documents hurt adapter-generated backlog quality. | Medium | Medium | Matches server provider behavior today. Re-evaluate in Phase B with body-opt-in if adapters report quality loss. |
| Sanitizer false positives still damage diagnostic text. | Low | Medium | Prefix-anchored regex set; comprehensive must-NOT-redact fixtures. Bare hex regex explicitly removed. |
| 256 KiB sources cap too tight for large projects. | Low | Medium | `dropped_counts` is observable; Phase B exposes per-source limits to users. |
| External adapter strict-decodes and rejects unknown fields. | Low | Medium | Contract mandates ignore-unknown; documented in §4.4; `subscription-connector-mvp.md` updated. |
| Concurrent claim handlers race in `ProjectContextBuilder.Build`. | Low | Low | `Build` reads through stores; stores are the existing concurrency boundary. Phase A does not add new shared state. |
| `requirementKeywords` tokenizes poorly on Chinese text. | Medium | Low | Existing behavior; not newly introduced. Phase D snapshot will let users see ranking choices; improvement deferred. |

### Open questions (tracked, not blocking Phase A)

None blocking Phase A — all critic-raised Q1–Q8 have been resolved in this v2. Remaining items are captured in §12 as explicit non-decisions.

## 12. Explicit Non-decisions (deferred past Phase A)

- Document body transmission (opt-in, chunking, summarization).
- Per-request `directives` wire path.
- Persistence of historical context snapshots.
- User-declared `@file` / `@folder` picker.
- Repo-wide symbol map / graph ranking.
- MCP migration of the adapter contract.
- Multi-tenant budget policies by subscription tier.
- Context diffing between runs.
- Non-ASCII / Chinese-aware tokenization in `requirementKeywords`.

## 13. Agent-deference Note

Local connector adapters run as external processes invoked by the `anpm-connector serve` daemon over `exec-json`. They have no access to the host agent's file search, semantic search, editor context, or terminal tools. The server-side builder, sanitizer, byte-cap, and ranking exist because the adapter cannot retrieve this context itself — not because the Copilot/Claude agent needs it duplicated.

This plan adds nothing that the Copilot/Claude agent already provides to its own workflow. When a task is handled directly inside the Copilot/Claude agent (e.g., in this editing session), the agent's native tools remain the correct path and `context.v1` does not apply.

Phase A targets the product's core MVP capability — **"Agent auto-decomposes requirements into backlog candidates"** — by giving the first real adapter the project-state context it needs to produce non-duplicate, evidence-grounded backlog drafts.
