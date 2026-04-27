// Package roles is the canonical catalog of execution roles available to
// the role_dispatch loop. The catalog is the single source of truth for
// (role_id, default timeout) pairs consumed by:
//
//   - The connector dispatch loop (per-role wall-clock timeout selection)
//   - The server's claim-next-task handler (Phase 6c PR-2 will enforce
//     IsKnown to skip stale role references)
//   - The frontend apply panel (Phase 6c PR-2 will fetch via /api/roles
//     and render "預估 N 分鐘" hints)
//
// The catalog is hand-maintained as a literal slice. Drift between this
// slice and the markdown files in backend/internal/prompts/roles/ is
// detected by TestCatalogMatchesPromptDir, which runs as part of `go
// test ./...` and therefore as part of `make pre-pr`.
//
// Adding a new role requires editing BOTH the markdown file AND this
// catalog in the same PR. The drift test enforces this by failing if
// either set differs.
package roles

import (
	"os"
	"strconv"
	"time"
)

// Role describes a single execution role.
//
// Category distinguishes the two layers Phase 6c introduces:
//
//   - "role" — a task-execution role (backend-architect, code-reviewer,
//     etc). Surfaces in the apply panel dropdown; runs the role prompt
//     against a task's title/description/context.
//   - "meta" — a non-executing helper role (e.g. the dispatcher, which
//     only classifies tasks, never executes them). Filtered OUT of
//     `/api/roles` and the apply UI. Phase 6c PR-3 introduces the
//     dispatcher meta-role; future meta-roles share this category.
type Role struct {
	ID                string
	Title             string
	Version           int
	UseCase           string
	DefaultTimeoutSec int
	Category          string // "role" | "meta"
}

const (
	CategoryRole = "role"
	CategoryMeta = "meta"
)

// catalog is the hand-maintained source of truth. The drift test in
// catalog_test.go ensures this matches the markdown files under
// backend/internal/prompts/roles/.
//
// DefaultTimeoutSec values reflect the typical maximum wall-clock for
// each role on a real Claude/Codex CLI invocation. They were chosen
// based on role complexity and validated during Phase 6c dogfooding —
// see docs/phase6c-plan.md §3 C2 and DECISIONS.md "Phase 6c scope".
var catalog = []Role{
	{
		ID:                "code-reviewer",
		Title:             "Code Reviewer",
		Version:           1,
		UseCase:           "Adversarial pre-merge review against a diff. Finds bugs the author did not consider — not style polish.",
		DefaultTimeoutSec: 900, // 15 min — read + comment, smallest surface
		Category:          CategoryRole,
	},
	{
		ID:                "test-writer",
		Title:             "Test Writer",
		Version:           1,
		UseCase:           "Write tests for a specific code surface — unit, integration, or contract — matching the project's existing test style.",
		DefaultTimeoutSec: 1200, // 20 min
		Category:          CategoryRole,
	},
	{
		ID:                "api-contract-writer",
		Title:             "API Contract Writer",
		Version:           1,
		UseCase:           "Write a precise API contract — endpoint, request/response shape, error cases — BEFORE the implementation lands.",
		DefaultTimeoutSec: 1800, // 30 min
		Category:          CategoryRole,
	},
	{
		ID:                "ui-scaffolder",
		Title:             "UI Scaffolder",
		Version:           1,
		UseCase:           "Scaffold a new page, component, or form. React/Vue/Svelte stack-aware, but defaults to the project's existing framework.",
		DefaultTimeoutSec: 2700, // 45 min
		Category:          CategoryRole,
	},
	{
		ID:                "db-schema-designer",
		Title:             "DB Schema Designer",
		Version:           1,
		UseCase:           "Propose a DB schema change — new tables, column additions, constraints, indexes — and emit the migration file.",
		DefaultTimeoutSec: 2700, // 45 min
		Category:          CategoryRole,
	},
	{
		ID:                "backend-architect",
		Title:             "Backend Architect",
		Version:           1,
		UseCase:           "Scaffold a new backend service or add a new module to an existing one. Go/Node/Python stack-aware.",
		DefaultTimeoutSec: 5400, // 90 min — large refactors / multi-file scaffolding
		Category:          CategoryRole,
	},
	// Phase 6c PR-3: meta-role — classification only, never executes a task.
	// Filtered out of /api/roles and the apply-panel dropdown (Category=meta).
	{
		ID:                "dispatcher",
		Title:             "Role Dispatcher",
		Version:           1,
		UseCase:           "Classify a task and suggest the best execution role from the catalog. Advisory only — the operator confirms before any role is applied.",
		DefaultTimeoutSec: 60, // 1 min — classification prompt, not code execution
		Category:          CategoryMeta,
	},
}

// fallbackTimeoutSec is used when a role lookup misses the catalog.
// This protects the dispatcher against typos and against role-rename
// races — even an unknown role gets a sane bound rather than running
// forever.
const fallbackTimeoutSec = 1800 // 30 min

// All returns a defensive copy of the catalog. Callers may mutate.
func All() []Role {
	out := make([]Role, len(catalog))
	copy(out, catalog)
	return out
}

// ByID looks up a role by its ID. The boolean indicates whether the
// role was found.
func ByID(id string) (Role, bool) {
	for _, r := range catalog {
		if r.ID == id {
			return r, true
		}
	}
	return Role{}, false
}

// IsKnown reports whether the given role ID is in the catalog. Empty
// strings, role IDs containing path separators, and unknown IDs all
// return false.
func IsKnown(id string) bool {
	if id == "" {
		return false
	}
	_, ok := ByID(id)
	return ok
}

// TimeoutFor returns the wall-clock timeout to use when dispatching a
// task with the given role ID. Resolution order:
//
//  1. ANPM_DISPATCH_TIMEOUT > 0  → that many seconds (global override
//     for unusually long tasks the operator pre-knows about).
//  2. ANPM_DISPATCH_TIMEOUT == 0 → return 0 (caller must interpret as
//     "no timeout" — escape hatch for "let it run as long as needed").
//  3. ANPM_DISPATCH_TIMEOUT < 0 or unset → catalog DefaultTimeoutSec.
//  4. role not in catalog       → fallbackTimeoutSec (30 min).
//
// A return value of 0 explicitly signals "do not apply a timeout"; the
// caller MUST check for this and skip the context.WithTimeout wrap.
// Any positive return value is a duration the caller should enforce.
func TimeoutFor(roleID string) time.Duration {
	if v := os.Getenv("ANPM_DISPATCH_TIMEOUT"); v != "" {
		// Parse as seconds. Reject obvious garbage (non-integers fall
		// through to catalog) but treat 0 and positive values as the
		// operator's explicit choice.
		if n, err := strconv.Atoi(v); err == nil {
			if n == 0 {
				return 0
			}
			if n > 0 {
				return time.Duration(n) * time.Second
			}
			// n < 0 → fall through to catalog
		}
	}
	if r, ok := ByID(roleID); ok {
		return time.Duration(r.DefaultTimeoutSec) * time.Second
	}
	return time.Duration(fallbackTimeoutSec) * time.Second
}
