# Phase 3A — Connector Feasibility Spike: Findings

**Status**: complete · 2026-04-27 · `[agent:feature-planner]`
**Input**: Phase 3B shipped 2026-04-27; Phase 6c shipped 2026-04-27.
**Question**: Is the connector route viable for broader vendor support (Copilot, ChatGPT)?
           What are the highest-value improvements to make next?

---

## 1. Connector Route — Vendor Compatibility Matrix

| Vendor | CLI automation surface | Viable via connector? | Recommendation |
|--------|----------------------|----------------------|----------------|
| Claude CLI (`claude`) | ✅ Stable, tested | ✅ **Primary path** | Current default; no change needed |
| Codex CLI (`codex`) | ✅ Stable adapter exists | ✅ Viable | Already supported via same adapter contract |
| OpenCode | ✅ stdin/stdout CLI | ✅ Viable with adapter | Adapter needs wrapping; low effort |
| GitHub Copilot | ❌ `gh copilot` is explain/suggest only; no prompt-to-task CLI | ❌ Not viable | Use `server_provider` with OpenAI-compatible endpoint |
| ChatGPT (OpenAI) | ❌ No official CLI | ❌ Not viable | Use `server_provider` mode (API key binding) |
| VS Code Copilot | ❌ Extension only, no headless API | ❌ Not viable | Out of scope entirely |

**Conclusion**: The connector route is a **CLI-tool route**, not a subscription-session route. Vendors
without a prompt-in / structured-JSON-out CLI cannot be supported here. The `server_provider`
mode (OpenAI-compatible API) is the correct path for ChatGPT and Copilot API.

This finding closes the "should we invest in Copilot/ChatGPT connector?" question: **no**.
Phase 4 (Connector MVP completeness) should focus on CLI-tool adapters only.

---

## 2. Gap Inventory (as of 2026-04-27)

### Gap 1: Evidence Panel frontend missing (CRITICAL)

**Impact**: Users cannot see what context the LLM received when evaluating candidate quality.
Cannot answer "Was this bad candidate due to wrong context?" — the core Phase 3B PR-2 promise.

**Backend state**: ✅ Complete
- `GET /api/planning-runs/:id/context-snapshot` endpoint exists
- `ContextSnapshotResponse` returns: pack_id, schema_version, role, intent_mode, task_scale,
  source_of_truth[], sources_bytes, dropped_counts, open_task_count, document_count,
  drift_count, agent_run_count, has_sync_run, available

**Frontend state**: ❌ Not started
- No `getContextSnapshot` in `api/client.ts`
- No `ContextSnapshot` type in `types/index.ts`
- No `PlanningRunContextDrawer` UI component
- `PlanningRunList` shows quality summary row but no evidence drawer

**Remediation**: Implement the Evidence Panel frontend (see §4.1).

---

### Gap 2: Connector dispatch still sends context-pack v1 (MEDIUM)

**Impact**: Connectors don't receive role, intent_mode, task_scale, source_of_truth.
The adapter prompt cannot be enriched with v2 envelope metadata.

**State**: `ClaimNextRun` builds and saves a v2 snapshot but the `PlanningContextV1` wire payload
is what gets sent to the adapter via stdin. The v2 struct is stored in the DB snapshot but
**never forwarded** to the connector as the dispatch payload.

**Remediation**: Upgrade `ClaimNextRun` to serialize v2 to stdin when the run has a pack_id
and the snapshot was saved (i.e. migration 032 ran). Backward-compatible: old adapters that
read `"sources"` / `"meta"` / `"limits"` see them at the same JSON path in v2.

---

### Gap 3: Phase 6d prerequisites not yet met

**Trigger**: ≥5 real `role_dispatch` executions in dogfood + ≥1 week of Phase 6c running.
**Current state**: Phase 6c shipped 2026-04-27. Zero dogfood data collected yet.
**Action**: Dogfood Phase 6c for at least one week before opening Phase 6d planning.

---

### Gap 4: `approved_scope` field still missing from context-pack v2

**Impact**: LLM has no explicit list of allowed modules/files → over-broad candidates.
**Phase 3B plan §1.1 Gap 1** listed this as "out of scope for Phase 3B; Phase 4+".
**Remediation**: Add `approved_scope []string` to `PlanningContextV2` when Phase 4 ships the
approval surface. Not actionable now.

---

## 3. Phase 6d Readiness Checklist

| Pre-condition | Current state | When met |
|---|---|---|
| Phase 6c fully shipped | ✅ 2026-04-27 | Done |
| ≥5 real role_dispatch executions | ❌ 0 | After ~1 week dogfood |
| ≥1 case: high-confidence but wrong | Unknown | After dogfood |
| Phase 3B quality feedback loop | ✅ feedback_kind + quality_summary | Done |
| Evidence Panel (visible quality signal) | ❌ Missing frontend | After Gap 1 fixed |
| Evidence panel shows context truncation warnings | ❌ Missing frontend | After Gap 1 fixed |

**Estimate**: Phase 6d planning can begin ≥ 2026-05-04 (1 week of dogfood),
provided Gap 1 (Evidence Panel) is fixed before or alongside dogfood.

---

## 4. Recommended Roadmap

### Immediate (this sprint)

**4.1 Evidence Panel frontend** — closes Gap 1 (Phase 3B PR-2 completion)

Components to build:
- `ContextSnapshot` interface in `types/index.ts`
- `getContextSnapshot(runId)` in `api/client.ts`
- `PlanningRunContextDrawer` component (collapsible, lazy-loaded per run):
  - Shows available=false gracefully ("Context data not available for older runs")
  - Sources summary: N tasks · N documents · N drift signals · N agent runs · (sync run: yes/no)
  - Context pack metadata: pack_id (truncated), schema_version, role, intent_mode, task_scale
  - Byte budget: X KB used, source_of_truth files listed
  - Truncation warnings: dropped_counts > 0 → warning chip per truncated source
- Wire "Context" toggle into `PlanningRunList` on completed runs (lazy-loads on first open)

**4.2 Connector v2 dispatch upgrade** — closes Gap 2 (Phase 3B PR-1 follow-up)

- `ClaimNextRun` already saves v2 snapshot. Also pass v2 JSON to adapter stdin when
  `context_pack_id` is non-empty (run has a v2 snapshot).
- Adapter reads `schema_version` and can access role, intent_mode, task_scale.
- Fully backward-compatible: v1 path still used when `context_pack_id` is empty.

### Near-term (after 1 week dogfood)

**4.3 Phase 6d planning** — triggered by dogfood data
- `mode=role_dispatch_auto` + min_confidence threshold
- `PhaseRouting` activity value for connector
- `router_role_not_found` / `router_low_confidence` error kinds

### Future (Phase 4 — Connector MVP completeness)

- Connector task dispatch flow completeness (currently planning runs work; task dispatch is partial)
- OpenCode adapter (CLI-based, same exec-json contract)
- Lease renewal during long tasks (>30 min backend-architect runs)
- Result callback visibility (execution result detail view)
- Task retry / cancel / regenerate UX

---

## 5. Scope Decisions

| Decision | Rationale |
|---|---|
| Copilot/ChatGPT connector path: **not viable** | No stable CLI automation surface; `server_provider` is correct for these vendors |
| Phase 4 connector investment: **yes, for CLI tools** | Claude CLI proven; Codex/OpenCode viable; clear exec-json contract exists |
| Phase 6d: **wait for dogfood** | router quality unvalidated; auto-apply on unvalidated router is premature |
| `approved_scope`: **Phase 4+** | Requires project-level approval surface that doesn't exist yet |

---

*Source: inline spike analysis 2026-04-27. Reads: connector/service.go, connector/suggest.go,
planning/wire/context_v2.go, handlers/planning_runs_context.go, subscription-connector-mvp.md,
phase6c-plan.md §9, phase-3b-plan.md §6.*
