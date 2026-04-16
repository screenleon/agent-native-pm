# Domain Rules: Backend API — Agent Native PM

## Rule entries

### Rule: API-001
- Owner layer: Domain
- Domain: backend-api
- Stability: core
- Status: active
- Scope: public HTTP handlers
- Statement: All API responses must follow the envelope contract: `{ "data": <payload>, "error": <string|null>, "meta": <object|null> }`.
- Rationale: Consistent response shape simplifies frontend parsing and agent integration.
- Verification: Integration tests assert envelope fields for both success and error responses.
- Supersedes: N/A
- Superseded by: N/A

### Rule: API-002
- Owner layer: Domain
- Domain: backend-api
- Stability: core
- Status: active
- Scope: endpoint evolution
- Statement: Additive changes to API responses are allowed. Breaking changes to existing fields require a new API version or migration path.
- Rationale: Prevent client (frontend and agent) breakage.
- Verification: Contract tests; changelog entry for any breaking change.
- Supersedes: N/A
- Superseded by: N/A

### Rule: API-003
- Owner layer: Domain
- Domain: backend-api
- Stability: core
- Status: active
- Scope: database access
- Statement: All SQL queries must use parameterized statements. No string concatenation for SQL construction.
- Rationale: Prevent SQL injection; required for SQLite and future PostgreSQL compatibility.
- Verification: Code review; static analysis via `golangci-lint`.
- Supersedes: N/A
- Superseded by: N/A

### Rule: API-004
- Owner layer: Domain
- Domain: backend-api
- Stability: behavior
- Status: active
- Scope: data access layer
- Statement: Use `database/sql` with a driver that supports both SQLite and PostgreSQL to ease Phase 4 migration.
- Rationale: Reduces migration cost when upgrading data store.
- Verification: Driver import check; no SQLite-specific SQL syntax outside migration files.
- Supersedes: N/A
- Superseded by: N/A

### Rule: API-005
- Owner layer: Domain
- Domain: backend-api
- Stability: behavior
- Status: active
- Scope: request validation
- Statement: All create and update endpoints must validate required fields and return 400 with a descriptive error on failure.
- Rationale: Agents and frontends need clear error messages to self-correct.
- Verification: Integration tests for missing/invalid fields.
- Supersedes: N/A
- Superseded by: N/A

### Rule: API-006
- Owner layer: Domain
- Domain: backend-api
- Stability: behavior
- Status: active
- Scope: Go module structure
- Statement: Module boundaries must be enforced through Go package structure. No circular imports between top-level modules (`projects`, `tasks`, `documents`, `sync`, `drift`, `agent_runs`, `summaries`).
- Rationale: Maintains modular monolith architecture; enables future extraction if needed.
- Verification: `go vet`; import graph analysis.
- Supersedes: N/A
- Superseded by: N/A
