# Context Strategy

**Status**: Proposed  
**Date**: 2026-04-17  
**Owner**: [agent:documentation-architect]  
**Related**: `docs/subscription-connector-mvp.md`, `docs/credential-binding-design.md`, `docs/api-surface.md`, `docs/data-model.md`, `DECISIONS.md`

---

## 1. Goal

This document defines how Agent Native PM should maintain, transport, and evolve context as the product adds multiple execution surfaces, including:

- server-side deterministic planning
- server-callable model providers
- local connector execution for subscription-backed workflows
- external agent runtimes such as Codex, OpenCode, and managed-agent platforms

The main objective is stability: the same task should preserve the same constraints and source-of-truth references even when execution moves between tools.

---

## 2. Problem

Session memory is not durable enough to serve as the primary context layer for this product:

1. Different tools compact, summarize, and reload context differently.
2. Subscription-backed local execution cannot rely on the server reusing vendor sessions.
3. Long-running planning and review workflows need explicit handoff state, not raw chat replay.
4. Product rules, data contracts, and architectural decisions must remain auditable in version-controlled files.

For this repository, "context management" therefore means controlling what becomes durable project knowledge, what is task-scoped payload, and what is only temporary runtime residue.

---

## 3. Core Principles

1. **Repository files outrank session memory.** Durable guidance lives in tracked files, not in conversation history.
2. **Adapters are derived, not canonical.** Tool-specific prompts, commands, skills, and agent definitions may differ in format, but they must point back to the same source docs.
3. **Execution consumes bounded context packs.** Connectors and external runtimes should receive a structured task payload, not an unbounded transcript.
4. **Compaction output is operational, not normative.** A summary created to continue a session may preserve momentum, but it does not replace `DECISIONS.md`, `docs/api-surface.md`, or other source docs.
5. **Context egress must be explicit.** If context leaves the server or repository boundary, the system must disclose what was sent and why.
6. **Handoffs beat scrollback.** Cross-role or cross-tool transitions should pass through structured artifacts and trace IDs instead of relying on inherited chat state.

---

## 4. Source-of-Truth Hierarchy

Use the following precedence order when multiple context surfaces exist:

1. `docs/operating-rules.md`
2. `docs/agent-playbook.md`
3. `DECISIONS.md`
4. `docs/data-model.md`
5. `docs/api-surface.md`
6. `docs/product-blueprint.md`
7. `docs/mvp-scope.md`
8. Tool-specific adapter files, generated prompt assets, and runtime summaries

Implications:

- `AGENTS.md` stays a short bootstrap surface, not the only memory layer.
- Connector-local caches and local agent histories are convenience state only.
- Product state in PostgreSQL records execution and audit history, but it does not replace repository rules.

---

## 5. Context Layers

### Layer A: Canonical Repository Context

Purpose: stable, reviewable, version-controlled rules and contracts.

Artifacts:

- `AGENTS.md`
- `prompt-budget.yml`
- `docs/operating-rules.md`
- `docs/agent-playbook.md`
- `docs/agent-templates.md`
- `DECISIONS.md`
- `docs/data-model.md`
- `docs/api-surface.md`
- `docs/product-blueprint.md`
- `docs/mvp-scope.md`

This layer is the only place where durable project truth should be authored.

### Layer B: Derived Adapter Context

Purpose: translate canonical context into tool-specific instruction surfaces.

Examples:

- tool-specific rule files
- prompt templates
- agent definitions
- command files
- task-routing metadata

This layer may be regenerated or manually synchronized, but it must not become the only place where a rule exists.

### Layer C: Task-Scoped Context Pack

Purpose: bound one execution to one explicit objective.

Minimum payload should include:

- objective
- role
- intent mode
- task scale
- execution mode
- budget profile
- approved scope
- non-goals
- constraints
- source-of-truth references
- decision references
- validation requirements
- expected output contract
- trace metadata

This is the primary transport unit for connector dispatch and future CLI orchestration.

### Layer D: Runtime Residue

Purpose: preserve operational continuity without upgrading temporary artifacts into rules.

Examples:

- compacted session summaries
- local connector execution logs
- intermediate research notes
- task progress updates
- generated handoff artifacts

This layer is useful for resumability and audit, but it is never authoritative by itself.

---

## 6. Subscription And Connector Implications

The local connector path exists because subscription-backed execution cannot be treated as a normal server-side provider.

Rules for this path:

1. The server must send a bounded context pack, not raw accumulated chat history.
2. The server must not store or reuse subscription tokens.
3. The connector may enrich context locally from the checked-out repo, but it should treat repository docs as authoritative.
4. The connector must return structured output that can be validated against existing planning and review contracts.
5. Any remote-provider path must disclose context egress in product documentation and UI.

For planning specifically, the preferred sequence is:

1. server assembles canonical refs + task metadata
2. server materializes a context pack
3. connector or provider-specific adapter consumes that pack
4. adapter returns candidates and normalized errors
5. server validates and persists the result

This keeps the control plane stable even when execution surfaces change.

---

## 7. Recommended Context Pack Fields

This repository should treat the following fields as the baseline contract for future connector and CLI dispatch:

| Field | Purpose |
|------|---------|
| `schema_version` | Versioned contract for pack readers |
| `pack_id` | Stable identifier for one pack instance |
| `objective` | One-sentence goal |
| `role` | Owning role for the run |
| `intent_mode` | Current phase: analyze, implement, review, document |
| `task_scale` | Small, Medium, Large |
| `execution_mode` | supervised, semi-auto, autonomous |
| `budget_profile` | minimal, standard, full, etc. |
| `approved_scope` | Allowed files/modules and explicit non-goals |
| `constraints` | Active rules and boundaries |
| `source_of_truth` | References to canonical docs and decisions |
| `artifacts` | Relevant files, prior handoffs, compact summaries |
| `expected_output` | Required response shape and validation expectations |
| `audit` | Trace ID, source marker, parent run metadata |

The pack should prefer references and bounded summaries over bulk file inclusion unless a file is directly required for execution.

### 7.1 Current implementation status (`context.v1`)

The local connector pipeline ships `wire.PlanningContextV1` (see
`backend/internal/planning/wire/context_v1.go`). The table below maps the
recommended fields above to what is implemented today, what is planned,
and what is intentionally out of scope for the MVP.

| Recommended field | Wire field today                                              | Status    | Notes |
|-------------------|---------------------------------------------------------------|-----------|-------|
| `schema_version`  | `schema_version` (`context.v1`)                               | Live      | Stable contract; bump only on breaking changes. |
| `pack_id`         | _missing_                                                     | Planned   | Use `planning_run_id` as proxy until added; needed for cross-run replay. |
| `objective`       | upstream `requirement.title` + `requirement.summary`          | Live      | Carried alongside the context, not inside it. |
| `role`            | _missing_ (implicit `planner`)                                | Planned   | Add when other roles call the connector path. |
| `intent_mode`     | _missing_                                                     | Planned   | Currently always `implement`-equivalent. |
| `task_scale`      | _missing_                                                     | Planned   | Heuristic exists in adapters; lift into wire. |
| `execution_mode`  | implicit (`prompt-budget.yml`)                                | Out (MVP) | Repo-level, not per-run. |
| `budget_profile`  | implicit (`prompt-budget.yml`)                                | Out (MVP) | Repo-level, not per-run. |
| `approved_scope`  | _missing_                                                     | Planned   | Needs project-level approval surface. |
| `constraints`     | rule docs + `meta.warnings` (degradation only)                | Partial   | Hard rules still loaded by the agent itself, not via wire. |
| `source_of_truth` | _missing_ (adapters consult repo docs directly)               | Planned   | Add canonical doc pointers to wire payload. |
| `artifacts`       | `sources.{open_tasks, recent_documents, drift_signals, ...}`  | Live      | Metadata-only; bodies never sent. |
| `expected_output` | adapter contract (`exec-json` response schema)                | Live      | Lives in the adapter spec, not in wire. |
| `audit`           | `meta.{ranking, dropped_counts, sources_bytes, warnings}`     | Live      | Trace IDs from `planning_run_id`. |

Gaps marked `Planned` are tracked in `DECISIONS.md` and the priority
planning notes; they should be closed before claiming a richer "context
pack v2" contract.

---

## 8. Maintenance Rules

### When to update canonical context

Update repository context when one of these changes:

- a safety or workflow rule
- role routing or ownership
- API contract or request/response shape
- data model or migration expectations
- connector or execution-path behavior
- product scope boundary

### When to update adapter context

Update adapters when:

- canonical docs changed in a way that affects tool behavior
- a new execution surface is introduced
- a tool requires a different instruction transport format
- a reusable command, skill, or agent definition drifted from the source docs

### When to keep output as runtime residue only

Do not promote artifacts into source-of-truth files when they are only:

- one-off debugging notes
- session compaction text
- execution logs
- local environment quirks with no product impact
- ephemeral traces that do not change project rules or contracts

---

## 9. Ownership And Update Matrix

| Artifact class | Canonical location | Primary owner | Update trigger |
|---------------|--------------------|---------------|----------------|
| Safety / workflow rules | `docs/operating-rules.md` | documentation-architect | Rule changes |
| Role routing | `docs/agent-playbook.md` | documentation-architect | Ownership or workflow changes |
| Durable decisions | `DECISIONS.md` | role making the decision | New architectural or behavioral decision |
| API contract | `docs/api-surface.md` | backend-architect / documentation-architect | Endpoint or envelope changes |
| Data model | `docs/data-model.md` | backend-architect / documentation-architect | Schema changes |
| Tool adapters | tool-specific files | documentation-architect | Canonical doc drift or new tool |
| Context packs | generated per run | execution layer | Every dispatched task |
| Session summaries | runtime storage | execution layer | Long-running or resumed sessions |

---

## 10. Operational Guidance

When this project adds more CLI integrations, follow this rule:

1. write or update canonical context first
2. derive or sync tool adapters second
3. generate task-scoped context packs at dispatch time
4. store runtime summaries only for recovery and audit

Do not invert that order. If a rule exists only in a connector adapter, an OpenCode command, or a Multica skill, the system will drift.

---

## 11. Recommendation

For upcoming subscription-backed CLI work, this repository should standardize on:

- repository files for durable project knowledge
- a versioned context-pack contract for execution transport
- adapter-specific translation layers for each external tool
- traceable handoff artifacts instead of raw conversation replay

This gives Agent Native PM one stable context model even when execution spans local connectors, Codex, OpenCode, or a managed-agent control plane.

Source: [agent:documentation-architect]
