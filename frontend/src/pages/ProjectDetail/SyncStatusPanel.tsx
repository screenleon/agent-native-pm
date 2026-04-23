import { useState } from 'react'
import type { Project, ProjectRepoMapping, SyncRun } from '../../types'
import type { SyncGuidance } from '../../utils/syncGuidance'
import { syncRunGuidance } from '../../utils/syncGuidance'
import { formatDateTime, formatRelativeTime, formatSyncDuration, syncBadgeClass, guidanceBadgeClass } from '../../utils/formatters'

interface SyncStatusPanelProps {
  project: Project
  latestSyncRun: SyncRun | null
  recentSyncRuns: SyncRun[]
  openDriftCount: number
  hasRepoSource: boolean
  syncing: boolean
  latestSyncGuidance: SyncGuidance | null
  canApplyDetectedBranchAndRerun: boolean
  detectedSyncBranch: string
  quickFixBranchTarget: { type: 'repo-mapping'; mapping: ProjectRepoMapping } | { type: 'project' }
  savingProjectBranch: boolean
  projectBranchForm: string
  branchFormChanged: boolean
  detectedProjectBranch: string
  onSync: () => void
  onApplyDetectedBranchAndRerunSync: () => void
  onNavigateToDrift: () => void
  onProjectBranchFormChange: (value: string) => void
  onSaveProjectBranch: () => void
  onClearProjectBranch: () => void
  onUseDetectedBranch: (branch: string) => void
}

export function SyncStatusPanel({
  project,
  latestSyncRun,
  recentSyncRuns,
  openDriftCount,
  hasRepoSource,
  syncing,
  latestSyncGuidance,
  canApplyDetectedBranchAndRerun,
  detectedSyncBranch,
  quickFixBranchTarget,
  savingProjectBranch,
  projectBranchForm,
  branchFormChanged,
  detectedProjectBranch,
  onSync,
  onApplyDetectedBranchAndRerunSync,
  onNavigateToDrift,
  onProjectBranchFormChange,
  onSaveProjectBranch,
  onClearProjectBranch,
  onUseDetectedBranch,
}: SyncStatusPanelProps) {
  const [expanded, setExpanded] = useState<boolean>(() =>
    !hasRepoSource || !latestSyncRun || latestSyncRun.status === 'failed' || canApplyDetectedBranchAndRerun,
  )

  if (!expanded) {
    return (
      <div className="card" style={{ marginBottom: '1.5rem', padding: '0.75rem 1rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <span style={{ fontWeight: 600, fontSize: '0.9rem' }}>Sync</span>
          {latestSyncRun ? (
            <span className={`badge ${syncBadgeClass(latestSyncRun.status)}`}>{latestSyncRun.status}</span>
          ) : (
            <span className="badge badge-todo">not started</span>
          )}
          {latestSyncRun && (
            <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>{formatRelativeTime(latestSyncRun.started_at)}</span>
          )}
          {latestSyncRun?.error_message && (
            <span style={{ color: '#fca5a5', fontSize: '0.85rem' }}>Sync error — check details</span>
          )}
          {openDriftCount > 0 && (
            <span className="badge badge-stale">{openDriftCount} open drift</span>
          )}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
            <button className="btn btn-primary btn-sm" onClick={onSync} disabled={syncing || !hasRepoSource}>
              {syncing ? 'Syncing...' : 'Sync Now'}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => setExpanded(true)}>
              Details ▼
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}>
        <div style={{ flex: '1 1 420px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.5rem' }}>
            <h3 style={{ marginBottom: 0 }}>Sync Status</h3>
            {latestSyncRun ? (
              <span className={`badge ${syncBadgeClass(latestSyncRun.status)}`}>{latestSyncRun.status}</span>
            ) : (
              <span className="badge badge-todo">not started</span>
            )}
            <button className="btn btn-ghost btn-sm" style={{ marginLeft: 'auto' }} onClick={() => setExpanded(false)}>
              ▲ Collapse
            </button>
          </div>

          {!hasRepoSource ? (
            <p>This project has no repository source configured yet. Add a primary mirror mapping, a repository URL, or a manual path before running sync.</p>
          ) : !project.repo_path && project.repo_url ? (
            <p>This project is configured for managed clone mode. The first sync will clone the repository inside the container automatically.</p>
          ) : !latestSyncRun ? (
            <p>No sync has been run yet for this project. Run sync to detect changed files and drift signals.</p>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '0.75rem', marginTop: '0.75rem' }}>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Started</div>
                <div>{formatDateTime(latestSyncRun.started_at)}</div>
              </div>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Completed</div>
                <div>{formatDateTime(latestSyncRun.completed_at)}</div>
              </div>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Duration</div>
                <div>{formatSyncDuration(latestSyncRun)}</div>
              </div>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Commits</div>
                <div>{latestSyncRun.commits_scanned}</div>
              </div>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Files Changed</div>
                <div>{latestSyncRun.files_changed}</div>
              </div>
              <div>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Open Drift</div>
                <div>{openDriftCount}</div>
              </div>
            </div>
          )}

          {latestSyncRun?.error_message && (
            <div className="error-message" style={{ marginTop: '0.75rem' }}>
              {latestSyncRun.error_message}
            </div>
          )}

          {latestSyncGuidance && (
            <div style={{ marginTop: '0.75rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', background: 'var(--bg)' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                <span className={`badge ${guidanceBadgeClass(latestSyncGuidance.tone)}`}>{latestSyncGuidance.tone}</span>
                <strong>{latestSyncGuidance.headline}</strong>
              </div>
              <p style={{ marginTop: '0.45rem', marginBottom: 0 }}>{latestSyncGuidance.detail}</p>
              <p style={{ marginTop: '0.45rem', marginBottom: 0, fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                Next: {latestSyncGuidance.nextAction}
              </p>
              {canApplyDetectedBranchAndRerun && (
                <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                  <button className="btn btn-primary btn-sm" onClick={onApplyDetectedBranchAndRerunSync} disabled={savingProjectBranch || syncing}>
                    {(savingProjectBranch || syncing) ? 'Applying and syncing…' : `Apply detected branch ${detectedSyncBranch} and rerun sync`}
                  </button>
                  <span style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                    Quick fix: update {quickFixBranchTarget.type === 'repo-mapping' ? 'the primary repo mapping branch' : 'the project branch setting'} and immediately rerun sync.
                  </span>
                </div>
              )}
            </div>
          )}

          <div style={{ marginTop: '0.75rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', background: 'var(--bg)' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
              <div>
                <strong>Project Default Branch</strong>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem', marginTop: '0.25rem' }}>
                  Used as the fallback branch for sync. Leave blank to auto-detect from repo HEAD/default branch. Repo mappings with their own branch still override this setting.
                </div>
              </div>
              <span className="badge badge-low">Current {project.default_branch || 'auto-detect'}</span>
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.75rem', alignItems: 'center' }}>
              <input
                value={projectBranchForm}
                onChange={e => onProjectBranchFormChange(e.target.value)}
                placeholder="leave blank to auto-detect"
                style={{ padding: '0.5rem 0.75rem', minWidth: '220px', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
              />
              <button className="btn btn-primary btn-sm" onClick={onSaveProjectBranch} disabled={!branchFormChanged || savingProjectBranch}>
                {savingProjectBranch ? 'Saving…' : 'Save Branch'}
              </button>
              <button className="btn btn-ghost btn-sm" onClick={onClearProjectBranch} disabled={savingProjectBranch || projectBranchForm === ''}>
                Clear to Auto-Detect
              </button>
              {detectedProjectBranch && detectedProjectBranch !== projectBranchForm.trim() && (
                <button className="btn btn-ghost btn-sm" onClick={() => onUseDetectedBranch(detectedProjectBranch)} disabled={savingProjectBranch}>
                  Use Detected {detectedProjectBranch}
                </button>
              )}
            </div>
            {detectedProjectBranch && (
              <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginTop: '0.45rem' }}>
                Detected repo branch: {detectedProjectBranch}
              </div>
            )}
          </div>
        </div>

        <div style={{ minWidth: '240px', flex: '0 1 280px' }}>
          <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginBottom: '0.5rem' }}>Next action</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <button className="btn btn-primary" onClick={onSync} disabled={syncing || !hasRepoSource}>
              {syncing ? 'Syncing...' : 'Run Sync Now'}
            </button>
            <button className="btn btn-ghost" onClick={onNavigateToDrift} disabled={openDriftCount === 0}>
              {openDriftCount > 0 ? `Review ${openDriftCount} Open Drift Signal${openDriftCount === 1 ? '' : 's'}` : 'No Open Drift Signals'}
            </button>
          </div>
          {latestSyncRun && latestSyncRun.status === 'completed' && latestSyncRun.files_changed === 0 && (
            <p style={{ marginTop: '0.75rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
              Latest sync found no changed files in the current scan window.
            </p>
          )}
        </div>
      </div>

      {recentSyncRuns.length > 0 && (
        <div style={{ marginTop: '1rem', borderTop: '1px solid var(--border)', paddingTop: '1rem' }}>
          <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginBottom: '0.75rem' }}>Recent sync history</div>
          <div style={{ display: 'grid', gap: '0.75rem' }}>
            {recentSyncRuns.map(run => {
              const driftCountForRun = run.id === latestSyncRun?.id ? openDriftCount : 0
              return (
                <div key={run.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem 0.9rem' }}>
                  <div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                      <span className={`badge ${syncBadgeClass(run.status)}`}>{run.status}</span>
                      <span style={{ fontWeight: 500 }}>{formatDateTime(run.started_at)}</span>
                    </div>
                    <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                      commits {run.commits_scanned} • files {run.files_changed} • duration {formatSyncDuration(run)}
                    </div>
                    <div style={{ marginTop: '0.35rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                      {syncRunGuidance(run, driftCountForRun).nextAction}
                    </div>
                    {run.error_message && (
                      <div style={{ marginTop: '0.35rem', color: '#fca5a5', fontSize: '0.85rem' }}>{run.error_message}</div>
                    )}
                  </div>
                  <button className="btn btn-ghost btn-sm" onClick={onNavigateToDrift}>
                    Open Drift
                  </button>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
