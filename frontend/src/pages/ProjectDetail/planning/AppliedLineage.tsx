import { useEffect, useState } from 'react'
import type { AppliedLineageEntry } from '../../../api/client'
import { listProjectTaskLineage } from '../../../api/client'
import { formatRelativeTime } from '../../../utils/formatters'

interface AppliedLineageProps {
  projectId: string
  /**
   * Any change to this value triggers a refetch. Callers derive it from
   * whatever upstream state signals "lineage might be stale" — e.g. a
   * composite string over every task's id + status + updated_at, a
   * monotonic counter bumped after apply-candidate, etc. The component
   * only requires that a change in value means "reload me".
   */
  reloadSignal: string | number
  onJumpToRequirement: (requirementId: string) => void
  onJumpToTasks: () => void
}

type LoadState = 'idle' | 'loading' | 'error' | 'ready'

/**
 * Applied-task lineage lane (Phase 2 slice S4).
 *
 * Lists tasks that came out of candidate-apply in this project, with the
 * visible chain `requirement → run status → candidate → task`. Data comes
 * from a single `GET /api/projects/:id/task-lineage` endpoint that joins
 * task_lineage with the four source tables server-side, so no N+1 client
 * fetches are needed.
 *
 * Deep-linking semantics (intentional trade-off for S4 scope):
 *
 *   - Requirement title is clickable → selects that requirement in the
 *     Requirement Queue. Downstream run + candidate selection still
 *     follows the user's subsequent clicks inside the workspace.
 *   - Task title is clickable → switches to the Tasks tab.
 *   - Run status and candidate title render as plain text. Making them
 *     clickable would require a pending-selection state machine inside
 *     usePlanningWorkspaceData (three async load steps to chain); that is
 *     deferred to S5 polish or a follow-up slice.
 */
export function AppliedLineage({
  projectId,
  reloadSignal,
  onJumpToRequirement,
  onJumpToTasks,
}: AppliedLineageProps) {
  const [entries, setEntries] = useState<AppliedLineageEntry[]>([])
  const [state, setState] = useState<LoadState>('idle')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    async function load() {
      setState('loading')
      setErrorMessage(null)
      try {
        const res = await listProjectTaskLineage(projectId)
        if (!active) return
        setEntries(res.data)
        setState('ready')
      } catch (err) {
        if (!active) return
        setEntries([])
        setErrorMessage(err instanceof Error ? err.message : 'Failed to load applied-task lineage')
        setState('error')
      }
    }
    load()
    return () => { active = false }
  }, [projectId, reloadSignal])

  return (
    <div className="card applied-lineage-card" style={{ marginTop: '1rem' }}>
      <div className="planning-stage-header">
        <div>
          <h3 style={{ marginBottom: '0.25rem' }}>Applied-task lineage</h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            Tasks that came out of applying a planning candidate in this project, with the chain back to the source requirement, run, and candidate.
          </p>
        </div>
        {state === 'ready' && <span className="badge badge-low">{entries.length} traceable</span>}
      </div>

      {state === 'loading' && <div className="loading" style={{ padding: '1rem 0 0.5rem' }}>Loading lineage…</div>}
      {state === 'error' && <div className="error-banner" style={{ marginTop: '1rem' }}>{errorMessage}</div>}
      {state === 'ready' && entries.length === 0 && (
        <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
          <h3>No applied candidates yet</h3>
          <p>Once a planning candidate is applied to a task, the chain back to its requirement appears here.</p>
        </div>
      )}
      {state === 'ready' && entries.length > 0 && (
        <div style={{ display: 'grid', gap: '0.55rem', marginTop: '0.75rem' }} data-testid="applied-lineage-list">
          {entries.map(entry => (
            <div
              key={entry.lineage_id}
              className="applied-lineage-row"
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr auto',
                gap: '0.75rem',
                alignItems: 'center',
                padding: '0.65rem 0.85rem',
                border: '1px solid var(--border)',
                borderRadius: '0.55rem',
                background: 'var(--bg)',
              }}
            >
              <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '0.4rem', fontSize: '0.9rem' }}>
                {entry.requirement_id ? (
                  <button
                    type="button"
                    className="applied-lineage-link"
                    onClick={() => onJumpToRequirement(entry.requirement_id!)}
                    aria-label={`Open requirement ${entry.requirement_title || entry.requirement_id}`}
                    style={{
                      padding: 0,
                      background: 'none',
                      border: 'none',
                      color: 'var(--link, #60a5fa)',
                      textDecoration: 'underline',
                      cursor: 'pointer',
                      fontSize: '0.9rem',
                    }}
                  >
                    {entry.requirement_title || `Requirement ${entry.requirement_id}`}
                  </button>
                ) : (
                  <span style={{ color: 'var(--text-muted)' }}>(no requirement)</span>
                )}
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                <span className="badge badge-low" title={entry.planning_run_id || 'no run'}>
                  run {entry.planning_run_status || '—'}
                </span>
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                <span title={entry.backlog_candidate_id || 'no candidate'}>
                  {entry.backlog_candidate_title || '(unnamed candidate)'}
                </span>
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                <button
                  type="button"
                  className="applied-lineage-link"
                  onClick={onJumpToTasks}
                  aria-label={`Open task ${entry.task_title}`}
                  style={{
                    padding: 0,
                    background: 'none',
                    border: 'none',
                    color: 'var(--link, #60a5fa)',
                    textDecoration: 'underline',
                    cursor: 'pointer',
                    fontSize: '0.9rem',
                  }}
                >
                  {entry.task_title}
                </button>
                <span className={`badge ${entry.task_status === 'done' ? 'badge-fresh' : entry.task_status === 'cancelled' ? 'badge-stale' : entry.task_status === 'in_progress' ? 'badge-low' : 'badge-todo'}`}>
                  {entry.task_status.replace('_', ' ')}
                </span>
              </div>
              <span style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                applied {formatRelativeTime(entry.created_at)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
