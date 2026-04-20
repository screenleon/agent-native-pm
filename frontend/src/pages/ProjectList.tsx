import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  createProject,
  deleteProject,
  discoverMirrorRepos,
  getProjectDashboardSummary,
  getProjectSummary,
  listDriftSignals,
  listProjects,
  listSyncRuns,
} from '../api/client'
import { syncTriageHint } from '../utils/syncGuidance'
import type {
  CreateProjectPayload,
  DriftSignal,
  MirrorRepoCandidate,
  MirrorRepoDiscovery,
  Project,
  ProjectSummary,
  ProjectDashboardSummary,
  SyncRun,
} from '../types'

type ProjectRiskSnapshot = {
  summary: ProjectSummary | null
  openDriftCount: number
  latestSync: SyncRun | null
}

type RiskQueueSort = 'risk' | 'drift' | 'recent_sync'
type CreateSourceMode = 'mirror' | 'url' | 'path'

type ProjectRiskQueueItem = {
  project: Project
  snapshot: ProjectRiskSnapshot | undefined
  riskScore: number
  reasons: string[]
}

type RoadmapLane = 'baseline' | 'recovery' | 'review' | 'execution'

type ProjectRoadmapStage = {
  lane: RoadmapLane
  label: string
  detail: string
  cta: string
  badgeClass: string
}

type ProjectFormState = {
  name: string
  description: string
  repo_url: string
  repo_path: string
  default_branch: string
}

const defaultFormState: ProjectFormState = {
  name: '',
  description: '',
  repo_url: '',
  repo_path: '',
  default_branch: '',
}

function buildRiskQueueItem(project: Project, snapshot: ProjectRiskSnapshot | undefined): ProjectRiskQueueItem {
  let riskScore = 0
  const reasons: string[] = []

  if (!project.repo_path && !project.repo_url) {
    riskScore += 25
    reasons.push('missing repo source')
  }

  if (!snapshot || !snapshot.latestSync) {
    riskScore += 20
    reasons.push('sync baseline missing')
  } else {
    if (snapshot.latestSync.status === 'failed') {
      riskScore += 55
      reasons.push('latest sync failed')
    }
    if (snapshot.latestSync.status === 'running') {
      riskScore += 8
      reasons.push('sync currently running')
    }
  }

  if (snapshot) {
    if (snapshot.openDriftCount > 0) {
      riskScore += Math.min(40, snapshot.openDriftCount * 10)
      reasons.push(`${snapshot.openDriftCount} open drift`)
    }

    if (snapshot.summary && snapshot.summary.total_documents > 0) {
      const staleRatio = snapshot.summary.stale_documents / snapshot.summary.total_documents
      if (staleRatio >= 0.5) {
        riskScore += 20
        reasons.push('high stale document ratio')
      } else if (staleRatio > 0) {
        riskScore += 10
        reasons.push('some stale documents')
      }
    }
  }

  return { project, snapshot, riskScore, reasons }
}

function riskScoreBadgeStyle(score: number) {
  if (score >= 70) return { background: 'rgba(239, 68, 68, 0.15)', color: '#fecaca' }
  if (score >= 40) return { background: 'rgba(245, 158, 11, 0.15)', color: '#fde68a' }
  if (score >= 20) return { background: 'rgba(59, 130, 246, 0.15)', color: '#bfdbfe' }
  return { background: 'rgba(34, 197, 94, 0.15)', color: '#bbf7d0' }
}

function syncStatusBadgeClass(status: SyncRun['status']) {
  if (status === 'completed') return 'badge-fresh'
  if (status === 'failed') return 'badge-stale'
  return 'badge-low'
}

function healthScoreClass(score: number) {
  if (score >= 0.7) return 'health-good'
  if (score >= 0.4) return 'health-ok'
  return 'health-bad'
}

function getRoadmapStage(project: Project, snapshot: ProjectRiskSnapshot | undefined): ProjectRoadmapStage {
  if (!project.repo_path && !project.repo_url) {
    return {
      lane: 'baseline',
      label: 'Baseline Setup',
      detail: 'Add a repo source and establish the first sync baseline.',
      cta: 'Configure repository source',
      badgeClass: 'badge-medium',
    }
  }

  if (!snapshot?.latestSync) {
    return {
      lane: 'baseline',
      label: 'Sync Baseline',
      detail: 'Run the first sync so roadmap signals can be computed from real changes.',
      cta: 'Run first sync',
      badgeClass: 'badge-low',
    }
  }

  if (snapshot.latestSync.status === 'failed') {
    return {
      lane: 'recovery',
      label: 'Recovery Needed',
      detail: 'Latest sync failed. Fix sync health before using this project for planning.',
      cta: 'Review sync failure',
      badgeClass: 'badge-stale',
    }
  }

  if (snapshot.openDriftCount > 0) {
    return {
      lane: 'review',
      label: 'Drift Review',
      detail: 'Open drift signals should be triaged before pushing backlog decisions forward.',
      cta: 'Triage drift signals',
      badgeClass: 'badge-high',
    }
  }

  if ((snapshot.summary?.stale_documents ?? 0) > 0) {
    return {
      lane: 'review',
      label: 'Docs Refresh',
      detail: 'Documentation freshness is lagging behind the current code baseline.',
      cta: 'Refresh stale docs',
      badgeClass: 'badge-medium',
    }
  }

  if (((snapshot.summary?.tasks_todo ?? 0) + (snapshot.summary?.tasks_in_progress ?? 0)) > 0) {
    return {
      lane: 'execution',
      label: 'Execution Ready',
      detail: 'Baseline is healthy enough to drive the next backlog decisions from current tasks.',
      cta: 'Drive next task decisions',
      badgeClass: 'badge-fresh',
    }
  }

  return {
    lane: 'execution',
    label: 'Planning Ready',
    detail: 'Project is stable enough to accept new roadmap or planning inputs.',
    cta: 'Open project workspace',
    badgeClass: 'badge-fresh',
  }
}

function formatRelativeTime(value: string | null | undefined) {
  if (!value) return '—'
  const diffMs = Date.now() - new Date(value).getTime()
  if (diffMs < 60 * 1000) return 'just now'
  const minutes = Math.floor(diffMs / (60 * 1000))
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function describeProjectSource(project: Project) {
  if (project.repo_path?.startsWith('/mirrors/')) {
    return `Primary mirror ${project.repo_path}`
  }
  if (project.repo_url) {
    return `Managed clone ${project.repo_url}`
  }
  if (project.repo_path) {
    return `Manual path ${project.repo_path}`
  }
  return 'No repository source configured'
}

function ProjectList() {
  const [projects, setProjects] = useState<Project[]>([])
  const [riskByProject, setRiskByProject] = useState<Record<string, ProjectRiskSnapshot>>({})
  const [loading, setLoading] = useState(true)
  const [riskLoading, setRiskLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [riskQueueSort, setRiskQueueSort] = useState<RiskQueueSort>('risk')
  const [showCreate, setShowCreate] = useState(false)
  const [createSourceMode, setCreateSourceMode] = useState<CreateSourceMode>('mirror')
  const [form, setForm] = useState<ProjectFormState>(defaultFormState)
  const [submitting, setSubmitting] = useState(false)
  const [mirrorDiscovery, setMirrorDiscovery] = useState<MirrorRepoDiscovery | null>(null)
  const [mirrorLoading, setMirrorLoading] = useState(false)
  const [mirrorLoadError, setMirrorLoadError] = useState<string | null>(null)
  const [selectedMirrorPath, setSelectedMirrorPath] = useState('')
  const [selectedMirrorAlias, setSelectedMirrorAlias] = useState('')
  const [selectedMirrorBranch, setSelectedMirrorBranch] = useState('')

  const loadRiskInbox = useCallback(async (targetProjects: Project[]) => {
    if (targetProjects.length === 0) {
      setRiskByProject({})
      return
    }

    setRiskLoading(true)
    try {
      const snapshotEntries = await Promise.all(
        targetProjects.map(async project => {
          let dashboard: ProjectDashboardSummary | null = null

          try {
            const dashboardRes = await getProjectDashboardSummary(project.id)
            dashboard = dashboardRes.data
          } catch {
            const [summaryRes, driftRes, syncRes] = await Promise.allSettled([
              getProjectSummary(project.id),
              listDriftSignals(project.id, 'open'),
              listSyncRuns(project.id),
            ])

            const summary = summaryRes.status === 'fulfilled' ? summaryRes.value.data : null
            const openDriftSignals: DriftSignal[] = driftRes.status === 'fulfilled' ? driftRes.value.data : []
            const syncRuns: SyncRun[] = syncRes.status === 'fulfilled' ? syncRes.value.data : []

            dashboard = summary ? {
              project_id: project.id,
              summary,
              latest_sync_run: syncRuns[0] ?? null,
              open_drift_count: openDriftSignals.length,
              recent_agent_runs: [],
            } : null
          }

          return [
            project.id,
            {
              summary: dashboard?.summary ?? null,
              openDriftCount: dashboard?.open_drift_count ?? 0,
              latestSync: dashboard?.latest_sync_run ?? null,
            },
          ] as const
        }),
      )

      setRiskByProject(Object.fromEntries(snapshotEntries))
    } finally {
      setRiskLoading(false)
    }
  }, [])

  const loadProjects = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const res = await listProjects()
      setProjects(res.data)
      await loadRiskInbox(res.data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load projects')
      setProjects([])
      setRiskByProject({})
    } finally {
      setLoading(false)
    }
  }, [loadRiskInbox])

  useEffect(() => {
    void loadProjects()
  }, [loadProjects])

  function resetCreateState() {
    setShowCreate(false)
    setCreateSourceMode('mirror')
    setForm(defaultFormState)
    setMirrorDiscovery(null)
    setMirrorLoadError(null)
    setSelectedMirrorPath('')
    setSelectedMirrorAlias('')
    setSelectedMirrorBranch('')
    setSubmitting(false)
  }

  async function loadMirrorCandidates() {
    try {
      setMirrorLoading(true)
      setMirrorLoadError(null)
      const resp = await discoverMirrorRepos()
      setMirrorDiscovery(resp.data)

      if (resp.data.repos.length > 0) {
        const preferredRepo = resp.data.repos.find(repo => repo.repo_path === selectedMirrorPath) ?? resp.data.repos[0]
        handleSelectMirror(preferredRepo)
      }
    } catch (err) {
      setMirrorLoadError(err instanceof Error ? err.message : 'Failed to load mounted mirror repositories')
    } finally {
      setMirrorLoading(false)
    }
  }

  function openCreateModal() {
    resetCreateState()
    setShowCreate(true)
    setCreateSourceMode('mirror')
    void loadMirrorCandidates()
  }

  function handleSelectMirror(repo: MirrorRepoCandidate) {
    setSelectedMirrorPath(repo.repo_path)
    setSelectedMirrorAlias(repo.suggested_alias)
    setSelectedMirrorBranch(repo.detected_default_branch || '')
    setForm(prev => ({
      ...prev,
      name: prev.name || repo.repo_name,
      default_branch: repo.detected_default_branch || prev.default_branch || '',
    }))
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!form.name.trim()) {
      setError('Project name is required.')
      return
    }

    const payload: CreateProjectPayload = {
      name: form.name.trim(),
      description: form.description.trim() || undefined,
      default_branch: form.default_branch.trim() || undefined,
    }

    if (createSourceMode === 'mirror') {
      if (!selectedMirrorPath) {
        setError('Select a mounted mirror repository before creating the project.')
        return
      }
      if (!selectedMirrorAlias.trim()) {
        setError('Alias is required for the primary mirror mapping.')
        return
      }
      payload.initial_repo_mapping = {
        alias: selectedMirrorAlias.trim(),
        repo_path: selectedMirrorPath,
        default_branch: selectedMirrorBranch.trim() || payload.default_branch,
      }
    }

    if (createSourceMode === 'url') {
      if (!form.repo_url.trim()) {
        setError('Repository URL is required for managed clone mode.')
        return
      }
      payload.repo_url = form.repo_url.trim()
    }

    if (createSourceMode === 'path') {
      if (!form.repo_path.trim()) {
        setError('Repository path is required for manual path mode.')
        return
      }
      payload.repo_path = form.repo_path.trim()
    }

    try {
      setSubmitting(true)
      setError(null)
      await createProject(payload)
      resetCreateState()
      await loadProjects()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this project and all its data?')) return
    try {
      setError(null)
      await deleteProject(id)
      await loadProjects()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete project')
    }
  }

  const riskSnapshots = projects.map(project => riskByProject[project.id]).filter(Boolean)
  const totalOpenDrift = riskSnapshots.reduce((sum, snapshot) => sum + (snapshot?.openDriftCount ?? 0), 0)
  const projectsAtRisk = riskSnapshots.filter(snapshot => (snapshot?.openDriftCount ?? 0) > 0).length
  const syncFailures = riskSnapshots.filter(snapshot => snapshot?.latestSync?.status === 'failed').length
  const selectedMirror = mirrorDiscovery?.repos.find(repo => repo.repo_path === selectedMirrorPath) ?? null
  const riskQueue = [...projects]
    .map(project => buildRiskQueueItem(project, riskByProject[project.id]))
    .sort((a, b) => {
      if (riskQueueSort === 'drift') {
        const driftA = a.snapshot?.openDriftCount ?? 0
        const driftB = b.snapshot?.openDriftCount ?? 0
        return driftB - driftA || b.riskScore - a.riskScore || a.project.name.localeCompare(b.project.name)
      }

      if (riskQueueSort === 'recent_sync') {
        const tsA = a.snapshot?.latestSync?.started_at ? new Date(a.snapshot.latestSync.started_at).getTime() : 0
        const tsB = b.snapshot?.latestSync?.started_at ? new Date(b.snapshot.latestSync.started_at).getTime() : 0
        return tsA - tsB || b.riskScore - a.riskScore || a.project.name.localeCompare(b.project.name)
      }

      return b.riskScore - a.riskScore || (b.snapshot?.openDriftCount ?? 0) - (a.snapshot?.openDriftCount ?? 0) || a.project.name.localeCompare(b.project.name)
    })
  const roadmapBuckets = riskQueue.reduce<Record<RoadmapLane, ProjectRiskQueueItem[]>>((acc, item) => {
    const stage = getRoadmapStage(item.project, item.snapshot)
    acc[stage.lane].push(item)
    return acc
  }, {
    baseline: [],
    recovery: [],
    review: [],
    execution: [],
  })
  const roadmapOverview = [
    {
      lane: 'baseline' as const,
      title: 'Baseline Lane',
      count: roadmapBuckets.baseline.length,
      detail: 'Projects that still need a repo source or first sync baseline.',
    },
    {
      lane: 'recovery' as const,
      title: 'Recovery Lane',
      count: roadmapBuckets.recovery.length,
      detail: 'Projects blocked by failed sync health.',
    },
    {
      lane: 'review' as const,
      title: 'Review Lane',
      count: roadmapBuckets.review.length,
      detail: 'Projects with open drift or stale documentation to resolve.',
    },
    {
      lane: 'execution' as const,
      title: 'Execution Lane',
      count: roadmapBuckets.execution.length,
      detail: 'Projects ready for backlog decisions and task flow.',
    },
  ]
  const topNextActions = riskQueue.slice(0, 3).map(item => ({
    item,
    stage: getRoadmapStage(item.project, item.snapshot),
  }))

  if (loading) return <div className="loading">Loading projects...</div>

  return (
    <div>
      <div className="page-header">
        <h2>Projects</h2>
        <button className="btn btn-primary" onClick={openCreateModal}>+ New Project</button>
      </div>

      {error && <div className="error-message">{error}</div>}

      {projects.length > 0 && (
        <div className="grid-4" style={{ marginBottom: '1.5rem' }}>
          <div className="stat-card">
            <div className="stat-value">{projects.length}</div>
            <div className="stat-label">Projects</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: totalOpenDrift > 0 ? 'var(--danger)' : 'var(--success)' }}>{totalOpenDrift}</div>
            <div className="stat-label">Open Drift</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: projectsAtRisk > 0 ? 'var(--warning)' : 'var(--success)' }}>{projectsAtRisk}</div>
            <div className="stat-label">Projects At Risk</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: syncFailures > 0 ? 'var(--danger)' : 'var(--success)' }}>{syncFailures}</div>
            <div className="stat-label">Sync Failures</div>
          </div>
        </div>
      )}

      {projects.length > 0 && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}>
            <div>
              <h3 style={{ marginBottom: '0.2rem' }}>Roadmap Command Center</h3>
              <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                Portfolio-level lanes for deciding which projects need baseline work, recovery, review, or execution next.
              </p>
            </div>
            {topNextActions[0] && (
              <Link to={`/projects/${topNextActions[0].item.project.id}`} className="btn btn-primary btn-sm">
                {topNextActions[0].stage.cta}
              </Link>
            )}
          </div>

          <div className="roadmap-grid" style={{ marginTop: '1rem' }}>
            {roadmapOverview.map(section => {
              const topProjects = roadmapBuckets[section.lane].slice(0, 3)
              return (
                <div key={section.lane} className="roadmap-lane-card">
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start' }}>
                    <div>
                      <h4>{section.title}</h4>
                      <div className="roadmap-lane-meta">{section.detail}</div>
                    </div>
                    <div className="health-score health-ok" style={{ width: '48px', height: '48px', fontSize: '1rem' }}>{section.count}</div>
                  </div>
                  <div className="roadmap-chip-list">
                    {topProjects.length === 0 ? (
                      <span className="badge badge-fresh">No projects</span>
                    ) : topProjects.map(entry => (
                      <Link key={entry.project.id} to={`/projects/${entry.project.id}`} className="badge badge-low">
                        {entry.project.name}
                      </Link>
                    ))}
                  </div>
                </div>
              )
            })}
          </div>

          {topNextActions.length > 0 && (
            <div style={{ marginTop: '1rem', borderTop: '1px solid var(--border)', paddingTop: '1rem' }}>
              <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginBottom: '0.75rem' }}>Top next actions</div>
              <div className="roadmap-next-list">
                {topNextActions.map(({ item, stage }) => (
                  <div key={item.project.id} className="roadmap-next-item">
                    <div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                        <strong>{item.project.name}</strong>
                        <span className={`badge ${stage.badgeClass}`}>{stage.label}</span>
                      </div>
                      <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.84rem' }}>{stage.detail}</div>
                      <div style={{ marginTop: '0.25rem', color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                        Risk {item.riskScore} • {item.reasons.length > 0 ? item.reasons.join(' • ') : 'healthy baseline'}
                      </div>
                    </div>
                    <Link to={`/projects/${item.project.id}`} className="btn btn-ghost btn-sm">
                      {stage.cta}
                    </Link>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {projects.length > 0 && (
        <div className="card" style={{ marginBottom: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
            <div>
              <h3 style={{ marginBottom: '0.2rem' }}>Risk Inbox Queue</h3>
              <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                Cross-project triage ordered for next actions.
              </p>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <label style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>Sort:</label>
              <select value={riskQueueSort} onChange={e => setRiskQueueSort(e.target.value as RiskQueueSort)} style={{ padding: '0.35rem 0.5rem' }}>
                <option value="risk">Highest Risk</option>
                <option value="drift">Most Open Drift</option>
                <option value="recent_sync">Oldest Sync First</option>
              </select>
            </div>
          </div>

          <div style={{ display: 'grid', gap: '0.7rem', marginTop: '0.9rem' }}>
            {riskQueue.map(item => {
              const scoreStyle = riskScoreBadgeStyle(item.riskScore)
              const latestSync = item.snapshot?.latestSync
              return (
                <div key={item.project.id} style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem 0.9rem', background: 'var(--bg)' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', flexWrap: 'wrap' }}>
                    <div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                        <strong>{item.project.name}</strong>
                        <span className="badge" style={{ ...scoreStyle, border: 'none' }}>
                          Risk {item.riskScore}
                        </span>
                        <span className="badge badge-low">Drift {item.snapshot?.openDriftCount ?? 0}</span>
                        {latestSync ? (
                          <span className={`badge ${syncStatusBadgeClass(latestSync.status)}`}>Sync {latestSync.status}</span>
                        ) : (
                          <span className="badge badge-todo">Sync not started</span>
                        )}
                      </div>
                      <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                        {item.reasons.length > 0 ? `Signals: ${item.reasons.join(' • ')}` : 'Signals: healthy baseline'}
                      </div>
                      <div style={{ marginTop: '0.25rem', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                        Last sync: {latestSync ? formatRelativeTime(latestSync.started_at) : '—'}
                      </div>
                    </div>
                    <Link to={`/projects/${item.project.id}`} className="btn btn-ghost btn-sm">
                      Open Triage
                    </Link>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {showCreate && (
        <div className="modal-overlay" onClick={resetCreateState}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Create Project</h3>
            <form onSubmit={handleCreate}>
              <div className="form-group">
                <label>Name *</label>
                <input value={form.name} onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))} autoFocus />
              </div>
              <div className="form-group">
                <label>Description</label>
                <textarea value={form.description} onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))} />
              </div>

              <div className="form-group">
                <label>Repository Source</label>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                  <button type="button" className={`btn ${createSourceMode === 'mirror' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setCreateSourceMode('mirror')}>
                    Mounted Mirror (preferred)
                  </button>
                  <button type="button" className={`btn ${createSourceMode === 'url' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setCreateSourceMode('url')}>
                    Managed Clone URL
                  </button>
                  <button type="button" className={`btn ${createSourceMode === 'path' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setCreateSourceMode('path')}>
                    Manual Path
                  </button>
                </div>
              </div>

              {createSourceMode === 'mirror' && (
                <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.85rem', marginBottom: '1rem', background: 'var(--bg)' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <div>
                      <strong>Mounted Mirrors</strong>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                        Load repositories already mounted under {mirrorDiscovery?.mirror_root || '/mirrors'} and create the primary mapping in one step.
                      </div>
                    </div>
                    <button type="button" className="btn btn-ghost btn-sm" onClick={loadMirrorCandidates} disabled={mirrorLoading}>
                      {mirrorLoading ? 'Loading…' : 'Reload'}
                    </button>
                  </div>

                  {mirrorLoadError && <div className="error-banner">{mirrorLoadError}</div>}

                  {mirrorLoading ? (
                    <div className="loading" style={{ padding: '0.75rem 0' }}>Loading mounted mirrors…</div>
                  ) : !mirrorDiscovery || mirrorDiscovery.repos.length === 0 ? (
                    <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                      No mounted git repositories were discovered. You can switch to Managed Clone URL or Manual Path below.
                    </div>
                  ) : (
                    <div style={{ display: 'grid', gap: '0.65rem' }}>
                      {mirrorDiscovery.repos.map(repo => (
                        <label key={repo.repo_path} style={{ display: 'block', border: `1px solid ${selectedMirrorPath === repo.repo_path ? 'var(--primary)' : 'var(--border)'}`, borderRadius: '0.5rem', padding: '0.75rem', cursor: 'pointer' }}>
                          <input
                            type="radio"
                            name="primary-mirror"
                            checked={selectedMirrorPath === repo.repo_path}
                            onChange={() => handleSelectMirror(repo)}
                            style={{ marginRight: '0.6rem' }}
                          />
                          <strong>{repo.repo_name}</strong>
                          <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.82rem' }}>{repo.repo_path}</div>
                          <div style={{ marginTop: '0.35rem', display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
                            <span className="badge badge-low">alias {repo.suggested_alias}</span>
                            <span className="badge badge-low">branch {repo.detected_default_branch || 'auto-detect'}</span>
                          </div>
                        </label>
                      ))}
                    </div>
                  )}

                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.75rem', marginTop: '0.85rem' }}>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                      <label>Primary Alias</label>
                      <input value={selectedMirrorAlias} onChange={e => setSelectedMirrorAlias(e.target.value)} placeholder="app" />
                    </div>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                      <label>Mirror Branch</label>
                      <input value={selectedMirrorBranch} onChange={e => setSelectedMirrorBranch(e.target.value)} placeholder={selectedMirror?.detected_default_branch || 'leave blank to auto-detect'} />
                    </div>
                  </div>
                </div>
              )}

              {createSourceMode === 'url' && (
                <div className="form-group">
                  <label>Repository URL</label>
                  <input value={form.repo_url} onChange={e => setForm(prev => ({ ...prev, repo_url: e.target.value }))} placeholder="https://github.com/org/repo.git" />
                </div>
              )}

              {createSourceMode === 'path' && (
                <div className="form-group">
                  <label>Repository Path</label>
                  <input value={form.repo_path} onChange={e => setForm(prev => ({ ...prev, repo_path: e.target.value }))} placeholder="/workspace/my-project" />
                </div>
              )}

              <div className="form-group">
                <label>Default Branch (Optional)</label>
                <input value={form.default_branch} onChange={e => setForm(prev => ({ ...prev, default_branch: e.target.value }))} placeholder="leave blank to auto-detect" />
              </div>

              <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem', marginBottom: '0.75rem' }}>
                Mirror mappings are the preferred local workflow because they can see mounted working tree changes. Leave branch blank when you want sync to auto-detect the repo default branch.
              </div>

              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={resetCreateState}>Cancel</button>
                <button type="submit" className="btn btn-primary" disabled={submitting}>
                  {submitting ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {projects.length === 0 ? (
        <div className="empty-state">
          <h3>No projects yet</h3>
          <p>Start with a mounted mirror repo so sync can see local working tree changes immediately.</p>
          <div style={{ marginTop: '1rem' }}>
            <button className="btn btn-primary" onClick={openCreateModal}>Create Mirror-Backed Project</button>
          </div>
        </div>
      ) : (
        <div className="grid-2">
          {projects.map(project => {
            const snapshot = riskByProject[project.id]
            const stage = getRoadmapStage(project, snapshot)
            const healthClass = healthScoreClass(snapshot?.summary?.health_score ?? 0)
            const healthValue = Math.round((snapshot?.summary?.health_score ?? 0) * 100)
            return (
              <div key={project.id} className="card">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem' }}>
                  <div style={{ display: 'flex', gap: '0.9rem', alignItems: 'flex-start' }}>
                    <div className={`health-score ${healthClass}`} style={{ width: '52px', height: '52px', fontSize: '0.95rem' }}>
                      {riskLoading || !snapshot?.summary ? '—' : `${healthValue}%`}
                    </div>
                    <div>
                      <Link to={`/projects/${project.id}`}><h3>{project.name}</h3></Link>
                      <div style={{ marginTop: '0.25rem', display: 'flex', gap: '0.45rem', alignItems: 'center', flexWrap: 'wrap' }}>
                        <span className={`badge ${stage.badgeClass}`}>{stage.label}</span>
                        <span className="badge badge-low">{stage.cta}</span>
                      </div>
                    </div>
                  </div>
                  <button className="btn btn-ghost btn-sm" onClick={() => handleDelete(project.id)}>Delete</button>
                </div>

                <div style={{ marginTop: '0.6rem', display: 'flex', gap: '0.45rem', alignItems: 'center', flexWrap: 'wrap' }}>
                  {riskLoading || !snapshot ? (
                    <span className="badge badge-low">Loading risk context</span>
                  ) : (
                    <>
                      <span className={`badge ${snapshot.openDriftCount > 0 ? 'badge-stale' : 'badge-fresh'}`}>
                        Drift {snapshot.openDriftCount}
                      </span>
                      {snapshot.latestSync ? (
                        <span className={`badge ${syncStatusBadgeClass(snapshot.latestSync.status)}`}>
                          Sync {snapshot.latestSync.status}
                        </span>
                      ) : (
                        <span className="badge badge-todo">Sync not started</span>
                      )}
                      {snapshot.summary && (
                        <span className="badge badge-low">Health {Math.round(snapshot.summary.health_score * 100)}%</span>
                      )}
                    </>
                  )}
                </div>

                {project.description && <p>{project.description}</p>}
                <p style={{ marginTop: '0.5rem', fontSize: '0.8rem', opacity: 0.7 }}>{describeProjectSource(project)}</p>
                <p style={{ marginTop: '0.45rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>{stage.detail}</p>
                {!riskLoading && snapshot?.latestSync && (
                  <p style={{ marginTop: '0.45rem', fontSize: '0.8rem', color: 'var(--text-muted)' }}>
                    Last sync {formatRelativeTime(snapshot.latestSync.started_at)}
                  </p>
                )}
                <p style={{ marginTop: '0.45rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                  {syncTriageHint(project, snapshot)}
                </p>

                {!riskLoading && snapshot?.summary && (
                  <div className="project-metrics">
                    <div className="project-metric">
                      <strong>{snapshot.summary.tasks_todo + snapshot.summary.tasks_in_progress}</strong>
                      <span>Active Tasks</span>
                    </div>
                    <div className="project-metric">
                      <strong>{snapshot.summary.stale_documents}/{snapshot.summary.total_documents}</strong>
                      <span>Stale Docs</span>
                    </div>
                    <div className="project-metric">
                      <strong>{snapshot.openDriftCount}</strong>
                      <span>Open Drift</span>
                    </div>
                  </div>
                )}

                <div style={{ marginTop: '0.85rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                  <Link to={`/projects/${project.id}`} className="btn btn-primary btn-sm">
                    {stage.cta}
                  </Link>
                  <Link to={`/projects/${project.id}`} className="btn btn-ghost btn-sm">
                    Open project triage
                  </Link>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

export default ProjectList
