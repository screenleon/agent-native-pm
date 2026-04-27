import type { AgentRun, DriftSignal, ProjectSummary, Requirement } from '../../types'

type Tab = 'overview' | 'planning' | 'tasks' | 'documents' | 'drift' | 'agents' | 'settings'

interface ProjectOverviewTabProps {
  requirements: Requirement[]
  openDriftCount: number
  driftSignals: DriftSignal[]
  agentRuns: AgentRun[]
  summary: ProjectSummary | null
  onSetTab: (tab: Tab) => void
  avgPlanningAcceptanceRate?: number
  planningRunsReviewedCount?: number
}

export function ProjectOverviewTab({
  requirements,
  openDriftCount,
  driftSignals,
  agentRuns,
  summary,
  onSetTab,
  avgPlanningAcceptanceRate,
  planningRunsReviewedCount,
}: ProjectOverviewTabProps) {
  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <h3 style={{ marginTop: 0 }}>What's happening</h3>
      <p style={{ color: 'var(--text-muted)', marginTop: 0 }}>
        The status numbers above summarise this project. Use the side rail to dive in.
      </p>
      <div className="grid-2" style={{ marginTop: '1rem' }}>
        <div>
          <h4 style={{ marginBottom: '0.5rem' }}>Backlog & planning</h4>
          <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
            {requirements.length === 0
              ? 'No requirements submitted yet. Open the Planning tab to file the first one.'
              : `${requirements.length} requirement${requirements.length === 1 ? '' : 's'} on file. Open Planning to dispatch new runs or review candidates.`}
          </p>
          {planningRunsReviewedCount != null && planningRunsReviewedCount > 0 && avgPlanningAcceptanceRate != null && (
            <p style={{ marginTop: '0.25rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              7-day acceptance rate: <strong>{Math.round(avgPlanningAcceptanceRate * 100)}%</strong> across {planningRunsReviewedCount} reviewed run{planningRunsReviewedCount === 1 ? '' : 's'}.
            </p>
          )}
          <button className="btn btn-ghost btn-sm" onClick={() => onSetTab('planning')}>Open Planning →</button>
        </div>
        <div>
          <h4 style={{ marginBottom: '0.5rem' }}>Documentation drift</h4>
          <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
            {openDriftCount === 0
              ? 'No open drift signals. Documentation looks aligned with code.'
              : `${openDriftCount} open drift signal${openDriftCount === 1 ? '' : 's'} need triage.`}
          </p>
          <button className="btn btn-ghost btn-sm" onClick={() => onSetTab('drift')} disabled={driftSignals.length === 0}>Open Drift →</button>
        </div>
        <div>
          <h4 style={{ marginBottom: '0.5rem' }}>Tasks</h4>
          <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
            {summary ? `${summary.tasks_in_progress} in progress · ${summary.tasks_done} done · ${summary.total_tasks} total.` : 'Run sync to populate task counts.'}
          </p>
          <button className="btn btn-ghost btn-sm" onClick={() => onSetTab('tasks')}>Open Tasks →</button>
        </div>
        <div>
          <h4 style={{ marginBottom: '0.5rem' }}>Agent activity</h4>
          <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
            {agentRuns.length === 0
              ? 'No agent runs recorded yet.'
              : `${agentRuns.length} agent run${agentRuns.length === 1 ? '' : 's'} on record.`}
          </p>
          <button className="btn btn-ghost btn-sm" onClick={() => onSetTab('agents')} disabled={agentRuns.length === 0}>Open Activity →</button>
        </div>
      </div>
    </div>
  )
}
