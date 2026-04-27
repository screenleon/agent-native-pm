// Phase 6c PR-2: CandidateRoleEditor surfaces the candidate's
// execution_role as an inline chip with an "edit" affordance.
// Operators can pre-tag candidates with a suggested role BEFORE
// reaching the apply panel — useful when triaging a long list and
// you want to record intent without applying yet.
//
// The component talks to the same PATCH /backlog-candidates/:id
// endpoint the apply panel uses; backend catalog enforcement and
// audit-row writes happen automatically inside the store layer.
//
// Out of PR-2 scope: showing "set by ${actor} at ${time}" metadata
// in the chip tooltip. The actor_audit data is captured but a
// dedicated GET endpoint to expose it on the candidate response is
// deferred to a follow-up. The chip currently shows just the role
// title; the inline-warning surfaces stale roles (not-in-catalog).

import { useState } from 'react'
import type { BacklogCandidate } from '../../../types'
import type { RoleInfo } from '../../../api/client'
import { isKnownRoleId } from '../../../types/roles'

interface CandidateRoleEditorProps {
  candidate: BacklogCandidate
  // null = catalog still loading OR fetch failed (see availableRolesError);
  // either way, the stale-warning is suppressed to avoid a
  // false-positive flash. Critic round 1 #5 / risk-reviewer L1 +
  // Copilot review #3.
  availableRoles: ReadonlyArray<RoleInfo> | null
  // When the /api/roles fetch fails the parent keeps availableRoles=null
  // AND populates this string. The chip surfaces it inline so operators
  // know the catalog never loaded (vs "loaded but empty"). null when no
  // failure has occurred.
  availableRolesError?: string | null
  // onUpdateRole receives the new role id. Empty string clears.
  // The parent persists via updateBacklogCandidate and refreshes
  // the candidate list state — this component is purely
  // presentational with respect to the persistence path so it can
  // be reused from any list view that holds candidate state.
  onUpdateRole: (roleId: string) => Promise<void>
  // disabled is set when the candidate is already applied (not
  // editable) or while another mutation is in flight.
  disabled?: boolean
}

export function CandidateRoleEditor({
  candidate,
  availableRoles,
  availableRolesError,
  onUpdateRole,
  disabled,
}: CandidateRoleEditorProps) {
  const [editing, setEditing] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [draftRole, setDraftRole] = useState<string>(candidate.execution_role ?? '')

  const currentRole = candidate.execution_role ?? ''
  // Treat the runtime /api/roles response as the source of truth once
  // it has loaded. Under a staggered deploy the backend catalog can be
  // updated before the frontend bundle ships (or vice-versa), so the
  // static KNOWN_ROLE_IDS mirror can disagree with the live server.
  // While availableRoles === null (still loading) we fall back to the
  // local mirror to suppress a false-positive stale-warning flash on
  // mount for obviously valid roles. This matches the panel-level
  // staleness check in CandidateReviewPanel.
  //
  // NOTE: this assumes availableRoles is fetched once on hook mount
  // and never mutated mid-session. If a future change adds server-push
  // catalog updates (e.g. via SSE in Phase 6c PR-4 / PR-5), the
  // operator-facing chip can become stale relative to the live catalog
  // until the next full reload — revisit this check at that point.
  const catalogReady = availableRoles !== null
  const matchedRole = catalogReady ? availableRoles!.find(r => r.id === currentRole) : undefined
  const isStale =
    currentRole !== '' &&
    (catalogReady ? matchedRole === undefined : !isKnownRoleId(currentRole))
  const dropdownRoles = availableRoles ?? []

  function openEditor() {
    if (disabled) return
    setDraftRole(currentRole)
    setError(null)
    setEditing(true)
  }

  async function handleSave() {
    setError(null)
    setPending(true)
    try {
      await onUpdateRole(draftRole)
      setEditing(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to update role')
    } finally {
      setPending(false)
    }
  }

  function handleCancel() {
    setEditing(false)
    setDraftRole(currentRole)
    setError(null)
  }

  if (editing) {
    return (
      <div
        className="candidate-role-editor candidate-role-editor--editing"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '0.4rem',
          padding: '0.25rem 0.5rem',
          background: 'var(--bg-hover, rgba(255, 255, 255, 0.05))',
          border: '1px solid var(--border)',
          borderRadius: '0.5rem',
          fontSize: '0.82rem',
        }}
      >
        <select
          aria-label="Set candidate execution role"
          value={draftRole}
          onChange={e => setDraftRole(e.target.value)}
          disabled={pending}
        >
          <option value="">— no role —</option>
          {dropdownRoles.map(r => (
            <option key={r.id} value={r.id} title={r.use_case}>
              {r.title} (v{r.version})
            </option>
          ))}
          {availableRoles === null && !availableRolesError && (
            <option disabled>Loading roles…</option>
          )}
          {availableRoles === null && availableRolesError && (
            <option disabled>Failed to load roles</option>
          )}
        </select>
        {availableRolesError && (
          <span
            role="alert"
            style={{
              color: 'var(--danger, #ef4444)',
              fontSize: '0.78rem',
            }}
          >
            {availableRolesError}
          </span>
        )}
        <button
          type="button"
          className="btn btn-primary btn-small"
          onClick={handleSave}
          disabled={pending || draftRole === currentRole}
        >
          {pending ? 'Saving…' : 'Save'}
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-small"
          onClick={handleCancel}
          disabled={pending}
        >
          Cancel
        </button>
        {error && (
          <span style={{ color: 'var(--danger, #ef4444)', fontSize: '0.78rem' }}>{error}</span>
        )}
      </div>
    )
  }

  // Read-only chip view. Three visual states:
  //   - role set + in catalog → muted chip with role title
  //   - role set + stale (not in catalog) → warning chip
  //   - no role → muted "No role" placeholder + Set role link
  return (
    <span
      className="candidate-role-editor"
      style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}
    >
      {currentRole && !isStale && (
        <span
          title={matchedRole ? matchedRole.use_case : currentRole}
          style={{
            background: 'var(--bg-hover, rgba(255, 255, 255, 0.05))',
            border: '1px solid var(--border)',
            borderRadius: '999px',
            padding: '0.1rem 0.55rem',
            fontSize: '0.78rem',
          }}
        >
          Role: {matchedRole?.title ?? currentRole}
        </span>
      )}
      {currentRole && isStale && (
        <span
          title={`Role "${currentRole}" is no longer in the catalog`}
          style={{
            background: 'var(--warning-bg, rgba(245, 158, 11, 0.08))',
            border: '1px solid var(--warning, #f59e0b)',
            borderRadius: '999px',
            padding: '0.1rem 0.55rem',
            fontSize: '0.78rem',
            color: 'var(--warning, #f59e0b)',
          }}
        >
          ⚠ Stale role: {currentRole}
        </span>
      )}
      {!currentRole && (
        <span style={{ color: 'var(--text-muted)', fontSize: '0.78rem' }}>
          No role set
        </span>
      )}
      {!disabled && (
        <button
          type="button"
          className="btn-link"
          onClick={openEditor}
          style={{
            background: 'none',
            border: 'none',
            color: 'var(--link, #60a5fa)',
            textDecoration: 'underline',
            cursor: 'pointer',
            fontSize: '0.78rem',
            padding: 0,
          }}
        >
          {currentRole ? 'Edit' : 'Set role'}
        </button>
      )}
    </span>
  )
}
