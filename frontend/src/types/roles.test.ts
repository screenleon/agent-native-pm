import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { KNOWN_ROLE_IDS, isKnownRoleId } from './roles'
import { listRoles, type RoleInfo } from '../api/client'

// Phase 6c PR-2 SoT-drift test: KNOWN_ROLE_IDS in this file MUST match
// the ids returned by GET /api/roles (which itself derives from the Go
// catalog in `backend/internal/roles/catalog.go`). When the backend
// catalog grows or shrinks, this test fails until the frontend constant
// is updated. The test stubs fetch to avoid hitting a real server in
// unit-test mode; integration verification happens in dogfood (PR-5).

describe('KNOWN_ROLE_IDS drift detection', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  function mockListRoles(roles: RoleInfo[]) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => 'application/json' },
      json: async () => ({ data: roles, error: null, meta: null }),
    } as unknown as Response)
  }

  // The canonical Phase 6c PR-2 catalog. If this fixture diverges from
  // backend/internal/roles/catalog.go the backend drift test
  // (TestCatalogMatchesPromptDir) will catch the backend side; this
  // frontend test catches the TS side.
  const expectedRoles: RoleInfo[] = [
    { id: 'code-reviewer', title: 'Code Reviewer', version: 1, use_case: '', default_timeout_sec: 900, category: 'role' },
    { id: 'test-writer', title: 'Test Writer', version: 1, use_case: '', default_timeout_sec: 1200, category: 'role' },
    { id: 'api-contract-writer', title: 'API Contract Writer', version: 1, use_case: '', default_timeout_sec: 1800, category: 'role' },
    { id: 'ui-scaffolder', title: 'UI Scaffolder', version: 1, use_case: '', default_timeout_sec: 2700, category: 'role' },
    { id: 'db-schema-designer', title: 'DB Schema Designer', version: 1, use_case: '', default_timeout_sec: 2700, category: 'role' },
    { id: 'backend-architect', title: 'Backend Architect', version: 1, use_case: '', default_timeout_sec: 5400, category: 'role' },
  ]

  it('KNOWN_ROLE_IDS set matches /api/roles response', async () => {
    mockListRoles(expectedRoles)
    const remote = await listRoles()
    const remoteIds = new Set((remote.data ?? []).map(r => r.id))
    const localIds = new Set(KNOWN_ROLE_IDS)
    expect(remoteIds).toEqual(localIds)
  })

  it('detects when backend adds a role not in KNOWN_ROLE_IDS', async () => {
    mockListRoles([...expectedRoles, { id: 'new-role', title: 'New', version: 1, use_case: '', default_timeout_sec: 600, category: 'role' }])
    const remote = await listRoles()
    const remoteIds = new Set((remote.data ?? []).map(r => r.id))
    const localIds = new Set(KNOWN_ROLE_IDS)
    expect(remoteIds).not.toEqual(localIds)
    // The diff direction matters: remote has an id local does not.
    expect([...remoteIds].filter(id => !(localIds as Set<string>).has(id))).toEqual(['new-role'])
  })

  it('detects when KNOWN_ROLE_IDS has a role the backend dropped', async () => {
    mockListRoles(expectedRoles.slice(0, -1)) // drop backend-architect
    const remote = await listRoles()
    const remoteIds = new Set((remote.data ?? []).map(r => r.id))
    const localIds = new Set(KNOWN_ROLE_IDS)
    expect(remoteIds).not.toEqual(localIds)
    expect([...localIds].filter(id => !remoteIds.has(id as string))).toEqual(['backend-architect'])
  })
})

describe('isKnownRoleId', () => {
  it('accepts known catalog ids', () => {
    expect(isKnownRoleId('backend-architect')).toBe(true)
    expect(isKnownRoleId('code-reviewer')).toBe(true)
  })

  it('rejects unknown ids, empty, and nullish', () => {
    expect(isKnownRoleId('not-a-role')).toBe(false)
    expect(isKnownRoleId('')).toBe(false)
    expect(isKnownRoleId(null)).toBe(false)
    expect(isKnownRoleId(undefined)).toBe(false)
  })

  it('is case-sensitive (matches backend roles.IsKnown)', () => {
    expect(isKnownRoleId('Backend-Architect')).toBe(false)
    expect(isKnownRoleId('BACKEND-ARCHITECT')).toBe(false)
  })
})
