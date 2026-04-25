import type { FormEvent } from 'react'

export interface RequirementIntakeForm {
  title: string
  summary: string
  description: string
  source: string
}

interface RequirementIntakeProps {
  requirementCount: number
  form: RequirementIntakeForm
  onFormChange: (form: RequirementIntakeForm) => void
  creating: boolean
  showForm: boolean
  onToggleForm: () => void
  onSubmit: (e: FormEvent) => void
  onReset: () => void
  /**
   * 'card' (default) renders with the full card wrapper, header, and toggle button.
   * 'inline' renders just the form fields inside a slim wrapper — the toggle is
   * controlled externally (e.g. by the sidebar header button).
   */
  variant?: 'card' | 'inline'
}

/**
 * Requirement-intake card for the Planning Workspace.
 *
 * The form is always visible when there are zero requirements (onboarding
 * shortcut); once at least one requirement exists the form collapses behind
 * a "+ New Requirement" toggle. Sequential disclosure is part of the
 * 2026-04-21 progressive-disclosure decision.
 */
const intakeForm = (
  form: RequirementIntakeForm,
  onFormChange: (f: RequirementIntakeForm) => void,
  creating: boolean,
  onSubmit: (e: FormEvent) => void,
  onReset: () => void,
) => (
  <form onSubmit={onSubmit}>
    <div className="form-group">
      <label>Title *</label>
      <input
        value={form.title}
        onChange={e => onFormChange({ ...form, title: e.target.value })}
        placeholder="Improve sync failure recovery UX"
      />
    </div>
    <div className="form-group">
      <label>Summary</label>
      <input
        value={form.summary}
        onChange={e => onFormChange({ ...form, summary: e.target.value })}
        placeholder="One-line planning summary"
      />
    </div>
    <div className="form-group">
      <label>Description</label>
      <textarea
        value={form.description}
        onChange={e => onFormChange({ ...form, description: e.target.value })}
        placeholder="Describe what the system should do before tasks are created."
        rows={5}
      />
    </div>
    <div className="form-group">
      <label>Source</label>
      <input
        value={form.source}
        onChange={e => onFormChange({ ...form, source: e.target.value })}
        placeholder="human or agent:name"
      />
    </div>
    <div className="modal-actions">
      <button type="button" className="btn btn-ghost" onClick={onReset}>
        Reset
      </button>
      <button
        type="submit"
        className="btn btn-primary"
        disabled={creating || !form.title.trim()}
      >
        {creating ? 'Capturing…' : 'Capture Requirement'}
      </button>
    </div>
  </form>
)

export function RequirementIntake({
  requirementCount,
  form,
  onFormChange,
  creating,
  showForm,
  onToggleForm,
  onSubmit,
  onReset,
  variant = 'card',
}: RequirementIntakeProps) {
  // In inline mode the form is always shown (controlled externally).
  // In card mode: always open if no requirements yet, otherwise toggle-driven.
  const formOpen = variant === 'inline' ? true : (requirementCount === 0 || showForm)

  if (variant === 'inline') {
    return (
      <div className="planning-inline-form">
        {formOpen && intakeForm(form, onFormChange, creating, onSubmit, onReset)}
      </div>
    )
  }

  return (
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ marginBottom: requirementCount > 0 ? 0 : '0.25rem' }}>Requirement Intake</h3>
          {requirementCount === 0 && (
            <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
              Capture product or implementation intent here first. This keeps Phase 2 draft-first and avoids creating tasks too early.
            </p>
          )}
        </div>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <span className="badge badge-todo">P2-03</span>
          {requirementCount > 0 && (
            <button
              className="btn btn-ghost btn-sm"
              onClick={onToggleForm}
            >
              {showForm ? '▲ Hide' : '+ New Requirement'}
            </button>
          )}
        </div>
      </div>

      {formOpen && (
        <div style={{ marginTop: '1rem' }}>
          {intakeForm(form, onFormChange, creating, onSubmit, onReset)}
        </div>
      )}
    </div>
  )
}
