import { useState } from 'react'
import type { Requirement } from '../../../types'
import { formatRelativeTime } from '../../../utils/formatters'
import { requirementStatusBadgeClass } from './labels'

interface RequirementQueueProps {
  requirements: Requirement[]
  selectedRequirementId: string | null
  onSelectRequirement: (id: string) => void
  planningLoadError: string | null
  onArchiveRequirement?: (id: string) => Promise<void>
  archivingRequirementId?: string | null
  /** When true, omits the outer card wrapper and verbose header — renders just the list */
  compact?: boolean
  /** New: permanently delete a requirement that has no applied tasks */
  onDiscardRequirement?: (id: string) => Promise<void>
  discardingRequirementId?: string | null
  /** New: set of requirement IDs that have applied task lineage (used to choose Archive vs Discard) */
  requirementIdsWithAppliedTasks?: Set<string>
}

/**
 * Requirement queue listing all draft / planned / archived requirements for
 * the project. Clicking a card selects it; the selected requirement drives
 * the downstream PlanningLauncher + PlanningRunList + CandidateReviewPanel
 * surfaces.
 *
 * Action button per active requirement:
 *  - If requirement has applied tasks → Archive only (permanent delete not safe)
 *  - If onDiscardRequirement provided and lineage data loaded and no lineage → Discard (with confirmation)
 *  - Otherwise → Archive
 *
 * Archived requirements are hidden behind a collapsible "N archived" toggle.
 */
export function RequirementQueue({
  requirements,
  selectedRequirementId,
  onSelectRequirement,
  planningLoadError,
  onArchiveRequirement,
  archivingRequirementId,
  compact,
  onDiscardRequirement,
  discardingRequirementId,
  requirementIdsWithAppliedTasks,
}: RequirementQueueProps) {
  const [confirmDiscardId, setConfirmDiscardId] = useState<string | null>(null)
  const [showArchived, setShowArchived] = useState(false)

  const draftCount = requirements.filter(r => r.status === 'draft').length
  const plannedCount = requirements.filter(r => r.status === 'planned').length
  const archivedCount = requirements.filter(r => r.status === 'archived').length

  const activeRequirements = requirements.filter(r => r.status === 'draft' || r.status === 'planned')
  const archivedRequirements = requirements.filter(r => r.status === 'archived')

  function renderActionButton(requirement: Requirement) {
    if (requirement.status === 'archived') return null

    // canDiscard: only when handler is provided AND lineage data has been loaded
    // (requirementIdsWithAppliedTasks !== undefined) AND this requirement has no lineage.
    // If requirementIdsWithAppliedTasks is undefined (not yet loaded or not passed),
    // we default to Archive (safe fallback).
    const canDiscard = Boolean(
      onDiscardRequirement &&
      requirementIdsWithAppliedTasks !== undefined &&
      !requirementIdsWithAppliedTasks.has(requirement.id)
    )

    // If requirement has applied tasks → Archive only
    const hasLineage = requirementIdsWithAppliedTasks?.has(requirement.id) ?? false

    if (confirmDiscardId === requirement.id) {
      return (
        <div style={{
          position: 'absolute', top: '0.5rem', right: '0.5rem',
          display: 'flex', alignItems: 'center', gap: '0.35rem',
          background: 'var(--bg-card)', padding: '0.15rem 0.35rem',
          borderRadius: '0.35rem', border: '1px solid var(--border)',
          fontSize: '0.78rem',
        }}>
          <span style={{ color: 'var(--text-muted)' }}>Discard?</span>
          <button
            type="button"
            className="btn btn-sm"
            style={{ background: 'var(--danger)', color: '#fff', padding: '0.15rem 0.45rem' }}
            disabled={discardingRequirementId === requirement.id}
            onClick={e => {
              e.stopPropagation()
              void onDiscardRequirement?.(requirement.id)
              setConfirmDiscardId(null)
            }}
          >
            {discardingRequirementId === requirement.id ? '…' : 'Yes'}
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            style={{ padding: '0.15rem 0.4rem' }}
            onClick={e => { e.stopPropagation(); setConfirmDiscardId(null) }}
          >
            Cancel
          </button>
        </div>
      )
    }

    if (hasLineage || !canDiscard) {
      // Show Archive (has lineage, or discard not available)
      if (!onArchiveRequirement) return null
      return (
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          title="Archive this requirement"
          disabled={archivingRequirementId === requirement.id}
          onClick={e => {
            e.stopPropagation()
            void onArchiveRequirement(requirement.id)
          }}
          style={{
            position: 'absolute',
            top: '0.5rem',
            right: '0.5rem',
            padding: '0.2rem 0.45rem',
            fontSize: '0.75rem',
            color: 'var(--text-muted)',
            opacity: archivingRequirementId === requirement.id ? 0.4 : 0.7,
          }}
        >
          {archivingRequirementId === requirement.id ? '…' : 'Archive'}
        </button>
      )
    }

    // No lineage and discard handler available → show Discard
    return (
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        title="Permanently discard this requirement"
        onClick={e => { e.stopPropagation(); setConfirmDiscardId(requirement.id) }}
        style={{
          position: 'absolute',
          top: '0.5rem',
          right: '0.5rem',
          padding: '0.2rem 0.45rem',
          fontSize: '0.75rem',
          color: 'var(--text-muted)',
          opacity: 0.7,
        }}
      >
        Discard
      </button>
    )
  }

  const listContent = (
    <>
      {planningLoadError && <div className="error-banner" style={{ marginTop: compact ? 0 : '1rem' }}>{planningLoadError}</div>}

      {requirements.length === 0 ? (
        compact ? (
          <p style={{ margin: '0.5rem 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>No requirements yet.</p>
        ) : (
          <div className="empty-state">
            <h3>No requirements yet</h3>
            <p>Use the intake form to capture the first planning requirement for this project.</p>
          </div>
        )
      ) : (
        <>
          <div className="requirement-list">
            {activeRequirements.map(requirement => (
              <div
                key={requirement.id}
                style={{ position: 'relative' }}
              >
                <button
                  type="button"
                  className={`requirement-card ${selectedRequirementId === requirement.id ? 'is-active' : ''}`}
                  onClick={() => onSelectRequirement(requirement.id)}
                  style={{ width: '100%', paddingRight: (onArchiveRequirement || onDiscardRequirement) ? '3.5rem' : undefined }}
                >
                  <div className="requirement-card-top">
                    <strong>{requirement.title}</strong>
                    <span className={`badge ${requirementStatusBadgeClass(requirement.status)}`}>{requirement.status}</span>
                  </div>
                  {requirement.summary && <p>{requirement.summary}</p>}
                  {requirement.description && <div className="requirement-description">{requirement.description}</div>}
                  <div className="requirement-meta">
                    <span>{requirement.source}</span>
                    <span>Updated {formatRelativeTime(requirement.updated_at)}</span>
                  </div>
                </button>

                {renderActionButton(requirement)}
              </div>
            ))}
          </div>

          {archivedRequirements.length > 0 && (
            <div style={{ borderTop: '1px solid var(--border)', marginTop: '0.5rem', paddingTop: '0.5rem' }}>
              <button
                type="button"
                onClick={() => setShowArchived(v => !v)}
                style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.78rem', color: 'var(--text-muted)', padding: '0.15rem 0', width: '100%', textAlign: 'left' }}
              >
                {archivedRequirements.length} archived {showArchived ? '▴' : '▾'}
              </button>
              {showArchived && (
                <div style={{ display: 'grid', gap: '0.25rem', marginTop: '0.35rem' }}>
                  {archivedRequirements.map(r => (
                    <div key={r.id} style={{ fontSize: '0.8rem', color: 'var(--text-muted)', padding: '0.25rem 0.5rem', opacity: 0.7 }}>
                      {r.title}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </>
      )}
    </>
  )

  if (compact) {
    return <div className="planning-req-compact">{listContent}</div>
  }

  return (
    <div className="card">
      <div className="planning-stage-header">
        <div>
          <h3 style={{ marginBottom: '0.25rem' }}>Requirement Queue</h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            Draft requirements stay here until they move through planning runs, candidate review, and apply-to-task flow.
          </p>
        </div>
        <div className="planning-stats">
          <span className="badge badge-todo">{draftCount} draft</span>
          <span className="badge badge-fresh">{plannedCount} planned</span>
          <span className="badge badge-low">{archivedCount} archived</span>
        </div>
      </div>

      {listContent}
    </div>
  )
}
