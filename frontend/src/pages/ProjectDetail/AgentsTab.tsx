import type { AgentRun, SyncRun } from '../../types'

interface AgentsTabProps {
  agentRuns: AgentRun[]
  syncRuns: SyncRun[]
}

export function AgentsTab({ agentRuns, syncRuns }: AgentsTabProps) {
  return (
    <div>
      {agentRuns.length === 0 ? (
        <div className="empty-state">
          <h3>No agent activity</h3>
          <p>Agent run history will appear here.</p>
        </div>
      ) : (
        <div className="card-list">
          {agentRuns.map(run => (
            <div key={run.id} className="card">
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <h4>{run.agent_name}</h4>
                <span className="badge badge-todo">{run.action_type}</span>
              </div>
              <p style={{ marginTop: '0.5rem' }}>{run.summary}</p>
              <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                {run.files_affected?.slice(0, 5).map(f => (
                  <span key={f} className="badge badge-low">{f}</span>
                ))}
              </div>
              <div style={{ marginTop: '0.5rem', color: 'var(--text-muted)' }}>
                {new Date(run.created_at).toLocaleString()}
              </div>
            </div>
          ))}
        </div>
      )}

      {syncRuns.length > 0 && (
        <div className="card" style={{ marginTop: '1rem' }}>
          <h4>Recent Sync Runs</h4>
          <ul style={{ marginTop: '0.5rem' }}>
            {syncRuns.slice(0, 5).map(run => (
              <li key={run.id}>
                {run.status} • commits {run.commits_scanned} • files {run.files_changed} • {new Date(run.started_at).toLocaleString()}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
