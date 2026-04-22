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
  /**
   * Cross-tier deep-link. Called with (requirementId, runId?, candidateId?)
   * when the operator clicks a segment of the chain. The workspace's
   * pending-selection state machine then selects the requirement,
   * auto-selects the run after runs load, and auto-selects the candidate
   * after candidates load. Passing only requirementId is also supported
   * (requirement-level jump).
   */
  onSelectLineage: (requirementId: string, runId?: string, candidateId?: string) => void
  onJumpToTasks: () => void
}

type LoadState = 'idle' | 'loading' | 'error' | 'ready'

// Shared link style for every clickable segment in the lineage chain.
// Extracted because four segments share it; inline duplication would drift.
const lineageLinkStyle: React.CSSProperties = {
  padding: 0,
  background: 'none',
  border: 'none',
  color: 'var(--link, #60a5fa)',
  textDecoration: 'underline',
  cursor: 'pointer',
  fontSize: '0.9rem',
}

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
  onSelectLineage,
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
                    onClick={() => onSelectLineage(entry.requirement_id!)}
                    aria-label={`Open requirement ${entry.requirement_title || entry.requirement_id}`}
                    style={lineageLinkStyle}
                  >
                    {entry.requirement_title || `Requirement ${entry.requirement_id}`}
                  </button>
                ) : (
                  <span style={{ color: 'var(--text-muted)' }}>(no requirement)</span>
                )}
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                {entry.requirement_id && entry.planning_run_id ? (
                  <button
                    type="button"
                    className="applied-lineage-link applied-lineage-link-run"
                    onClick={() => onSelectLineage(entry.requirement_id!, entry.planning_run_id!)}
                    aria-label={`Open planning run ${entry.planning_run_id}`}
                    title={entry.planning_run_id}
                    style={{ ...lineageLinkStyle, textDecoration: 'none' }}
                  >
                    <span className="badge badge-low">
                      run {entry.planning_run_status || '—'}
                    </span>
                  </button>
                ) : (
                  <span className="badge badge-low" title={entry.planning_run_id || 'no run'}>
                    run {entry.planning_run_status || '—'}
                  </span>
                )}
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                {entry.requirement_id && entry.planning_run_id && entry.backlog_candidate_id ? (
                  <button
                    type="button"
                    className="applied-lineage-link"
                    onClick={() => onSelectLineage(entry.requirement_id!, entry.planning_run_id!, entry.backlog_candidate_id!)}
                    aria-label={`Open candidate ${entry.backlog_candidate_title || entry.backlog_candidate_id}`}
                    title={entry.backlog_candidate_id}
                    style={lineageLinkStyle}
                  >
                    {entry.backlog_candidate_title || '(unnamed candidate)'}
                  </button>
                ) : (
                  <span title={entry.backlog_candidate_id || 'no candidate'}>
                    {entry.backlog_candidate_title || '(unnamed candidate)'}
                  </span>
                )}
                <span style={{ color: 'var(--text-muted)' }}>→</span>
                <button
                  type="button"
                  className="applied-lineage-link"
                  onClick={onJumpToTasks}
                  aria-label={`Open task ${entry.task_title}`}
                  style={lineageLinkStyle}
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
