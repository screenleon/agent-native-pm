# Migration Log

Tracks changes imported from `agent-playbook-template` into `agent-native-pm`.
This file is the upgrade log — it records what arrived, when, and why. For
active architectural constraints, see `DECISIONS.md`.

---

## 2026-04-22 — Import governance rules and scripts from agent-playbook-template

Selectively adopted hardened rules + tooling from the template without bringing
in the full skills/evals/harness surface (the project explicitly executes
skill behaviors natively per `prompt-budget.yml`).

### New files

| Path | Purpose |
|------|---------|
| `rules/global/prompt-injection.md` | GSEC-PI-001: agent behavior rule for external-content injection attempts |
| `rules/global/security-baseline.md` | GSEC-001..007: secrets, injection, auth, supply chain, input validation, secure defaults, logging |
| `rules/global/README.md` | Global-rules layer description |
| `rules/domain/README.md` | Domain-rules layer description and rule schema |
| `scripts/decisions-conflict-check.py` | Pre-plan contradiction check against `DECISIONS.md` (zero-dep, deterministic) |
| `scripts/lint-layered-rules.sh` | Rule-ID uniqueness, stability presence, supersession chain integrity |
| `scripts/lint-doc-consistency.sh` | Prompt-budget wording / doc consistency (no-ops gracefully on absent surfaces) |
| `scripts/budget-report.sh` | Estimated token cost per layer against the targets in `prompt-budget.yml` |
| `scripts/validate-prompt-budget.py` | Schema validator for `prompt-budget.yml` |
| `docs/layered-configuration.md` | Placement guide for Global / Domain / Project layer rules |

### Local changes made during import

- Patched `scripts/validate-prompt-budget.py` `extract_list_items` to strip inline YAML comments from list items (upstream parser bug; our `prompt-budget.yml` uses inline comments on skill entries).
- Removed the `## Override annotations` prose block from `project/project-manifest.md` because the bullet matched the rule-ID→rule-ID lint regex. The equivalent note lives in `## Override notes` as free-form text.
- Added `make lint-rules`, `make lint-docs`, `make validate-prompt-budget`, `make budget-report`, `make decisions-conflict-check`, `make lint-governance` to `Makefile`.
- Updated `AGENTS.md` Source-of-truth section to reference the new rule files and governance checks.

### Deliberately NOT imported

| Template surface | Reason |
|---|---|
| `skills/` directory | `prompt-budget.yml` explicitly declares skill behaviors are executed natively |
| `evals/`, `harness/`, `scripts/run-evals.sh`, `scripts/score-eval.py`, `scripts/trace-query*.py` | Over-engineered for current single-operator scope |
| `rules/domain/cloud-infra.md` | No cloud infra scope in this project |
| `.claude/agents/ui-image-implementer.md` | `prompt-budget.yml` disables the role |
| Template `CHANGELOG.md` | This project does not follow the template release cadence |

### Known follow-ups

- Rule-lint warns on "Scope: decision tracking" duplication between `GLOBAL-008` and `DOC-006`. Accepted — `DOC-006` narrows the global rule to documentation-sync scope. Not an error.

## 2026-04-22 — DECISIONS archival pass

Moved 11 Phase-1 baseline implementation decisions from `DECISIONS.md` to the new `DECISIONS_ARCHIVE.md`:

- 2026-04-14: Modular monolith architecture
- 2026-04-14: Static frontend (React + Vite)
- 2026-04-14: Drift detection as a core feature
- 2026-04-14: Agent API uses the same HTTP endpoints as the frontend
- 2026-04-14: Go Chi router for HTTP routing
- 2026-04-14: Pure-Go SQLite driver (modernc.org/sqlite)
- 2026-04-14: Unified auth context via middleware chain
- 2026-04-14: Optional route registration for phased handlers
- 2026-04-14: In-app document preview for registered project docs
- 2026-04-15: Managed repo cache for Docker-based sync
- 2026-04-15: Mirror-based multi-repo mappings for local-first sync

Each remains architecturally in force; they were archived because they are
now fully reflected in `ARCHITECTURE.md`, `project/project-manifest.md`, and
the codebase, and are no longer re-evaluated during day-to-day task planning.
Supersession chains still visible in `DECISIONS.md` (SQLite Phase 1 ↔ Move to
PostgreSQL now ↔ Dual-runtime mode) were NOT archived, to preserve the active
trail.

Entry count: 33 → 22. `DECISIONS.md` size: ~47 KB → ~40 KB.

## 2026-04-22 — UI smoke test bootstrap

Added `vitest` + `@testing-library/react` + `jsdom` + `@testing-library/jest-dom`
and wrote one smoke test file per new tab/panel component extracted from
`ProjectDetail.tsx` plus pure unit tests for `utils/formatters.ts`. Total
coverage: 9 test files, 37 tests, all green. Addresses the outstanding
follow-up from the 2026-04-22 "ProjectDetail.tsx split shipped" decision.
