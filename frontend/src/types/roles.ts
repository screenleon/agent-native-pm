// Phase 6c PR-2: hand-maintained mirror of the Go role catalog
// (`backend/internal/roles/catalog.go`). Frontend code that needs to
// know "is this string a valid catalog role id" imports this constant
// instead of opening every component up to round-trip an `/api/roles`
// fetch first. The runtime drift test in `roles.test.ts` calls the
// real `/api/roles` endpoint and asserts the id sets match — so any
// catalog change that lands without a frontend update fails CI.
//
// Adding a new role: edit BOTH this file AND
// `backend/internal/roles/catalog.go` AND
// `backend/internal/prompts/roles/<id>.md` in the same PR.
//
// Removing or renaming a role: same — plus document the migration
// path for existing candidate.execution_role rows that reference the
// old id (server-side claim-next-task already transitions stale-role
// tasks to failed via MarkTaskRoleNotFound).
export const KNOWN_ROLE_IDS = [
  'backend-architect',
  'ui-scaffolder',
  'db-schema-designer',
  'api-contract-writer',
  'test-writer',
  'code-reviewer',
] as const;

export type KnownRoleId = typeof KNOWN_ROLE_IDS[number];

// isKnownRoleId narrows an arbitrary string to KnownRoleId so callers
// can branch on "operator/router/legacy data referenced a known role"
// vs "needs stale-role warning". Use this rather than ad-hoc
// `KNOWN_ROLE_IDS.includes(x as ...)` casts at call sites.
export function isKnownRoleId(value: string | null | undefined): value is KnownRoleId {
  if (!value) return false;
  return (KNOWN_ROLE_IDS as readonly string[]).includes(value);
}
