# v1 hardening: Tier 1–4 pre-release pass

> Source: `[agent:backend-architect]` review pass before cutting v1. Folds the
> earlier Tier 1 (security/correctness), Tier 2 (degraded-mode signals),
> Tier 3 (interface + egress unification), and Tier 4 (docs/tooling) work
> into a single PR since no formal v1 has shipped yet.

## Summary

This PR is the final hardening pass before v1. It tightens the server-side
LLM egress path so it matches the local-connector path, unifies sanitization
on the `wire.PlanningContextV1` contract, propagates `context.Context`
end-to-end through the planning pipeline, fixes a CORS misconfiguration,
restores executable bits on reference adapters, gives the planning context
builder a structured degraded-mode signal, and updates the docs/Makefile
that drift relative to the new behavior. Two large follow-ups
(`ProjectDetail.tsx` split, SSE/WebSocket transport) are explicitly
deferred and recorded in `DECISIONS.md`.

## Tier 1 – security & correctness

- `wire.RedactSecrets` and `wire.TruncateRunes` are now exported so non-wire
  callers can apply the same v1 redaction without owning the regex set
  (`backend/internal/planning/wire/sanitizer.go`).
- `OpenAICompatibleProvider` enforces a 256 KiB cap
  (`defaultOpenAICompatibleMaxRequestBytes`) on the marshalled HTTP body
  before egress, returning a typed error instead of silently shipping an
  oversized payload (`backend/internal/planning/openai_compatible_provider.go`).
- Router CORS replaced the `AllowedOrigins:["*"] + AllowCredentials:true`
  combination (which browsers reject) with an env-driven allowlist
  (`CORS_ALLOWED_ORIGINS`) and safe localhost defaults. A literal `*`
  allowlist disables credentialed CORS instead of silently breaking auth
  (`backend/internal/config/config.go`,
  `backend/internal/router/router.go`,
  `backend/cmd/server/main.go`).
- Reference adapters (`adapters/{backlog_adapter.py, whatsnext_adapter.py,
  dispatcher_adapter.py}`) committed with executable bits to fix the
  exit-126 failure mode on a fresh checkout. Connector now emits a
  diagnostic if the spawned adapter exits 126/127
  (`backend/internal/connector/app.go`).

## Tier 2 – degraded-mode signal

- `ProjectContextBuilder` no longer silently swallows store errors for
  documents/drift/sync/agent-runs. It now logs and accumulates per-source
  warnings via `BuildResult.Warnings`
  (`backend/internal/planning/context_builder.go`).
- `BuildContextV1` propagates those warnings into
  `wire.PlanningContextMeta.Warnings`, giving adapters a deterministic
  degraded-mode signal
  (`backend/internal/planning/context_v1_builder.go`,
  `backend/internal/planning/wire/context_v1.go`,
  `backend/internal/planning/wire/wire_test.go`).

## Tier 3 – interface and egress unification (this round)

- **T3.A** `Provider.Generate` now takes `(ctx context.Context, ...)`.
  `DraftPlanner`, `ContextualPlanner`, `SettingsBackedPlanner`,
  `Orchestrator.Run`, and `candidateGenerator.Generate` forward the request
  ctx down to the provider. `OpenAICompatibleProvider.Generate` uses
  `http.NewRequestWithContext`, so HTTP-level cancellation and deadlines
  propagate from the handler into the LLM call. The deterministic provider
  accepts ctx as a no-op for interface uniformity. Planning HTTP handlers
  now pass `r.Context()` through `Orchestrator.Run`
  (`backend/internal/handlers/planning_runs.go`).
- **T3.B** `OpenAICompatibleProvider.Generate` builds a sanitized
  `*wire.PlanningContextV1` via a new internal `sanitizeForOpenAIEgress`
  helper before constructing the prompt: it translates `PlanningContext`
  to wire types, runs `wire.SanitizePlanningContextV1`, and applies
  `wire.ReduceSources` against `wire.DefaultLimits().MaxSourcesBytes`. The
  user prompt and all `compactX*FromWire` helpers consume this sanitized
  DTO, so the server- and connector-egress paths are now governed by the
  same wire contract. The 256 KiB request-body guard remains as
  defense-in-depth on the marshalled HTTP body.
- **T3.C / T3.D (deferred — see DECISIONS 2026-04-22)**:
  `ProjectDetail.tsx` split and SSE/WebSocket transport for live updates
  are explicitly out of scope for v1. Rationale recorded in DECISIONS.

## Tier 4 – docs and tooling

- `docs/mvp-scope.md` carries a HISTORICAL banner.
- `Makefile` `lint` now chains `go vet` + frontend lint; new
  `lint-backend` target.
- `docs/api-surface.md` documents the `CORS_ALLOWED_ORIGINS` env var and
  the wildcard-disables-credentials rule.
- `docs/local-connector-context.md` documents
  `PlanningContextMeta.Warnings`.
- `DECISIONS.md` records the 2026-04-21 entry (Tier 1+2) and the new
  2026-04-22 entry (Tier 3 + deferrals).

## Breaking changes

- **None for existing local-connector adapters.** `Meta.Warnings` is
  additive; adapters MUST tolerate the field but MAY ignore it.
- **Operations:** production deployments MUST set `CORS_ALLOWED_ORIGINS`
  to the canonical UI host(s); leaving it unset preserves the
  localhost-only default which is unsafe for any non-development
  deployment.
- **Internal Go API:** `planning.Provider.Generate` and
  `planning.Orchestrator.Run` now take `context.Context`. No external
  consumers; all in-repo call sites updated in this PR.

## Validation steps

```sh
cd backend && go vet ./...
make lint                                # go vet + frontend ESLint
./scripts/test-with-postgres.sh          # full backend test suite via PG
```

## Test results

- `go test ./internal/planning/...` → PASS
- `./scripts/test-with-postgres.sh` (full suite, all 20 migrations applied)
  → PASS, including `internal/store` integration tests.
- `make lint` (`go vet ./...` + frontend `eslint .`) → PASS, 0 warnings.

## Follow-ups (post-v1)

- Split `frontend/src/pages/ProjectDetail.tsx` (3206 LOC) under
  `frontend/src/pages/ProjectDetail/` with extracted components/hooks.
  No behavior change. Recorded in DECISIONS 2026-04-22.
- Evaluate SSE/WebSocket transport for notification/run-state updates
  once multi-tab fan-out becomes a real concern. Polling cadence
  (20s) and the `anpm:refresh-notifications` event name remain stable
  contracts that any migration must preserve.
