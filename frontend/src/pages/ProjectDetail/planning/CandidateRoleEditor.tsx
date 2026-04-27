// Phase 6c PR-2: CandidateRoleEditor surfaces the candidate's
// execution_role as an inline chip with an "edit" affordance.
// Phase 6c PR-3: adds "💡 Suggest" button that calls the LLM router
// and pre-fills the dropdown — advisory only, operator must confirm.

import { useState } from 'react'
import type { BacklogCandidate } from '../../../types'
import type { RoleInfo, SuggestRoleResult } from '../../../api/client'
import { isKnownRoleId } from '../../../types/roles'

interface CandidateRoleEditorProps {
  candidate: BacklogCandidate
  availableRoles: ReadonlyArray<RoleInfo> | null
  availableRolesError?: string | null
  onUpdateRole: (roleId: string) => Promise<void>
  // Phase 6c PR-3: optional suggest-role callback. When undefined the
  // "💡 Suggest" button is hidden (e.g. server not configured).
  onSuggestRole?: () => Promise<SuggestRoleResult>
  disabled?: boolean
}

export function CandidateRoleEditor({
  candidate,
  availableRoles,
  availableRolesError,
  onUpdateRole,
  onSuggestRole,
  disabled,
}: CandidateRoleEditorProps) {
  const [editing, setEditing] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [draftRole, setDraftRole] = useState<string>(candidate.execution_role ?? '')

  // Phase 6c PR-3: suggest state
  const [suggesting, setSuggesting] = useState(false)
  const [suggestion, setSuggestion] = useState<SuggestRoleResult | null>(null)
  const [suggestError, setSuggestError] = useState<string | null>(null)

  const currentRole = candidate.execution_role ?? ''
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

  async function handleSuggest() {
    if (!onSuggestRole) return
    setSuggesting(true)
    setSuggestError(null)
    setSuggestion(null)
    try {
      const result = await onSuggestRole()
      setSuggestion(result)
      if (result.role_id) {
        setDraftRole(result.role_id)
        setEditing(true)
      }
    } catch (e) {
      setSuggestError(e instanceof Error ? e.message : 'Suggestion failed')
    } finally {
      setSuggesting(false)
    }
  }

  async function handleSave() {
    setError(null)
    setPending(true)
    try {
      await onUpdateRole(draftRole)
      setEditing(false)
      // Clear suggestion after confirming so the note doesn't linger.
      setSuggestion(null)
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
    setSuggestion(null)
  }

  const suggestBtn = onSuggestRole && !disabled && (
    <button
      type="button"
      className="btn-link"
      onClick={handleSuggest}
      disabled={suggesting}
      title="Ask the LLM router to suggest a role (advisory — you confirm)"
      style={{
        background: 'none',
        border: 'none',
        color: 'var(--link, #60a5fa)',
        cursor: suggesting ? 'default' : 'pointer',
        fontSize: '0.78rem',
        padding: 0,
        opacity: suggesting ? 0.6 : 1,
      }}
    >
      {suggesting ? '…' : '💡 Suggest'}
    </button>
  )

  if (editing) {
    return (
      <div
        className="candidate-role-editor candidate-role-editor--editing"
        style={{
          display: 'inline-flex',
          flexDirection: 'column',
          gap: '0.3rem',
        }}
      >
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}>
          <div
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
              onChange={e => {
                setDraftRole(e.target.value)
                // Clear suggestion note if the operator manually overrides it.
                if (suggestion && e.target.value !== suggestion.role_id) {
                  setSuggestion(null)
                }
              }}
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
          </div>
          {suggestBtn}
          {suggestError && (
            <span style={{ color: 'var(--danger, #ef4444)', fontSize: '0.78rem' }}>
              {suggestError}
            </span>
          )}
        </div>
        {/* Suggestion reasoning note */}
        {suggestion && suggestion.role_id === draftRole && (
          <SuggestionNote suggestion={suggestion} availableRoles={availableRoles} />
        )}
        {error && (
          <span style={{ color: 'var(--danger, #ef4444)', fontSize: '0.78rem' }}>{error}</span>
        )}
        {availableRolesError && (
          <span role="alert" style={{ color: 'var(--danger, #ef4444)', fontSize: '0.78rem' }}>
            {availableRolesError}
          </span>
        )}
      </div>
    )
  }

  // Read-only chip view.
  return (
    <span
      className="candidate-role-editor"
      style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}
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
      {suggestBtn}
      {suggestError && (
        <span style={{ color: 'var(--danger, #ef4444)', fontSize: '0.78rem' }}>
          {suggestError}
        </span>
      )}
    </span>
  )
}

// SuggestionNote shows the dispatcher's reasoning and alternatives inline.
function SuggestionNote({
  suggestion,
  availableRoles,
}: {
  suggestion: SuggestRoleResult
  availableRoles: ReadonlyArray<RoleInfo> | null
}) {
  const confidencePct = Math.round(suggestion.confidence * 100)
  function roleTitle(id: string) {
    return availableRoles?.find(r => r.id === id)?.title ?? id
  }
  return (
    <div
      style={{
        fontSize: '0.76rem',
        color: 'var(--text-muted)',
        background: 'var(--bg-hover, rgba(255,255,255,0.03))',
        border: '1px solid var(--border)',
        borderRadius: '0.4rem',
        padding: '0.35rem 0.6rem',
        maxWidth: '360px',
      }}
    >
      <strong>💡 Dispatcher ({confidencePct}% confidence)</strong>
      {suggestion.reasoning && (
        <p style={{ margin: '0.2rem 0 0', lineHeight: 1.4 }}>{suggestion.reasoning}</p>
      )}
      {suggestion.alternatives && suggestion.alternatives.length > 0 && (
        <p style={{ margin: '0.25rem 0 0' }}>
          Alternatives:{' '}
          {suggestion.alternatives.map((a, i) => (
            <span key={a.role_id}>
              {i > 0 && ', '}
              <span title={a.reason}>
                {roleTitle(a.role_id)} ({Math.round(a.score * 100)}%)
              </span>
            </span>
          ))}
        </p>
      )}
    </div>
  )
}
