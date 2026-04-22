import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import type { Project, Task, Document, ProjectDashboardSummary, SyncRun, AgentRun, DriftSignal, DocumentContent, DocumentLink, ProjectRepoMapping, MirrorRepoDiscovery, Requirement } from '../types'
import {
  discoverMirrorRepos,
  getProject,
  getProjectDashboardSummary,
  getProjectSummary,
  updateProject,
  listRequirements,
  listTasksFiltered,
  deleteDocument,
  getDocumentContent,
  triggerSync,
  listSyncRuns,
  listAgentRuns,
  listDriftSignals,
  updateDriftSignal,
  bulkResolveDriftSignals,
  listDocumentLinks,
  createDocumentLink,
  deleteDocumentLink,
  listProjectRepoMappings,
  updateProjectRepoMapping,
  listDocuments,
} from '../api/client'
import { syncRunGuidance, type SyncGuidance } from '../utils/syncGuidance'
import { SyncStatusPanel } from '../components/SyncStatusPanel'
import { ProjectOverviewTab } from '../components/ProjectOverviewTab'
import { TasksTab } from '../components/TasksTab'
import { DocumentsTab } from '../components/DocumentsTab'
import { DriftTab } from '../components/DriftTab'
import { AgentsTab } from '../components/AgentsTab'
import { SettingsTab } from '../components/SettingsTab'
import { PlanningTab } from '../components/PlanningTab'

type Tab = 'overview' | 'planning' | 'tasks' | 'documents' | 'drift' | 'agents' | 'settings'
type TaskFilterState = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string }

function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [dashboardSummary, setDashboardSummary] = useState<ProjectDashboardSummary | null>(null)
  const [requirements, setRequirements] = useState<Requirement[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [documents, setDocuments] = useState<Document[]>([])
  const [syncRuns, setSyncRuns] = useState<SyncRun[]>([])
  const [agentRuns, setAgentRuns] = useState<AgentRun[]>([])
  const [driftSignals, setDriftSignals] = useState<DriftSignal[]>([])
  // Per Phase 2 S5 / design-decision D1: the Planning Workspace is the
  // per-project default landing surface (the operator's "what needs my
  // review?" surface). Overview remains at its tab index for read-only
  // status browsing. Tab-index values are unchanged so deep links like
  // `?tab=overview` still resolve as before.
  const [tab, setTab] = useState<Tab>('planning')
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [planningLoadError, setPlanningLoadError] = useState<string | null>(null)

  // task filters stay here — loadData depends on them
  const [taskSort, setTaskSort] = useState<string>('created_at')
  const [taskOrder, setTaskOrder] = useState<string>('desc')
  const [taskFilters, setTaskFilters] = useState<TaskFilterState>({ status: '', priority: '', assignee: '' })

  // document/links modal state — rendered globally so works from any tab
  const [viewingDoc, setViewingDoc] = useState<Document | null>(null)
  const [docContent, setDocContent] = useState<DocumentContent | null>(null)
  const [docLoading, setDocLoading] = useState(false)
  const [managingLinksDoc, setManagingLinksDoc] = useState<Document | null>(null)
  const [docLinks, setDocLinks] = useState<DocumentLink[]>([])
  const [docLinksLoading, setDocLinksLoading] = useState(false)
  const [newLink, setNewLink] = useState({ code_path: '', link_type: 'covers' as DocumentLink['link_type'] })
  const [documentLinksByDocumentId, setDocumentLinksByDocumentId] = useState<Record<string, DocumentLink[]>>({})
  const [documentLinkLoadErrors, setDocumentLinkLoadErrors] = useState<Record<string, boolean>>({})

  const [repoMappings, setRepoMappings] = useState<ProjectRepoMapping[]>([])
  const [repoMirrorDiscovery, setRepoMirrorDiscovery] = useState<MirrorRepoDiscovery | null>(null)
  const [repoMirrorLoading, setRepoMirrorLoading] = useState(false)
  const [repoMirrorLoadError, setRepoMirrorLoadError] = useState<string | null>(null)
  const [projectBranchForm, setProjectBranchForm] = useState('')
  const [savingProjectBranch, setSavingProjectBranch] = useState(false)

  const loadRepoMirrorDiscovery = useCallback(async (projectID: string) => {
    try {
      setRepoMirrorLoading(true)
      setRepoMirrorLoadError(null)
      const res = await discoverMirrorRepos(projectID)
      setRepoMirrorDiscovery(res.data)
    } catch (err) {
      setRepoMirrorDiscovery(null)
      setRepoMirrorLoadError(err instanceof Error ? err.message : 'Failed to load mounted mirrors')
    } finally {
      setRepoMirrorLoading(false)
    }
  }, [])

  const loadData = useCallback(async () => {
    if (!id) return
    try {
      setLoading(true)
      const dashboardPromise = getProjectDashboardSummary(id).catch(async () => {
        const summaryResponse = await getProjectSummary(id)
        return {
          data: {
            project_id: id,
            summary: summaryResponse.data,
            latest_sync_run: null,
            open_drift_count: -1,
            recent_agent_runs: [],
          },
        }
      })

      const [projRes, summaryRes, tasksRes, docsRes] = await Promise.all([
        getProject(id),
        dashboardPromise,
        listTasksFiltered(id, 1, 100, taskSort, taskOrder, {
          status: taskFilters.status || undefined,
          priority: taskFilters.priority || undefined,
          assignee: taskFilters.assignee.trim() || undefined,
        }),
        listDocuments(id, 1, 100),
      ])
      setProject(projRes.data)
      setDashboardSummary(summaryRes.data)
      setTasks(tasksRes.data)
      setDocuments(docsRes.data)

      const [requirementsRes, syncRes, agentRes, driftRes, repoMappingsRes] = await Promise.allSettled([
        listRequirements(id),
        listSyncRuns(id),
        listAgentRuns(id),
        listDriftSignals(id),
        listProjectRepoMappings(id),
      ])

      if (requirementsRes.status === 'fulfilled') {
        setRequirements(requirementsRes.value.data)
        setPlanningLoadError(null)
      } else {
        setRequirements([])
        setPlanningLoadError(requirementsRes.reason instanceof Error ? requirementsRes.reason.message : 'Failed to load requirements')
      }
      setSyncRuns(syncRes.status === 'fulfilled' ? syncRes.value.data : [])
      setAgentRuns(agentRes.status === 'fulfilled' ? agentRes.value.data : [])
      setDriftSignals(driftRes.status === 'fulfilled' ? driftRes.value.data : [])
      setRepoMappings(repoMappingsRes.status === 'fulfilled' ? repoMappingsRes.value.data : [])
      await loadRepoMirrorDiscovery(id)

      if (docsRes.data.length === 0) {
        setDocumentLinksByDocumentId({})
        setDocumentLinkLoadErrors({})
      } else {
        const linkResults = await Promise.all(
          docsRes.data.map(async doc => {
            try {
              const res = await listDocumentLinks(doc.id)
              return { documentId: doc.id, links: res.data, ok: true as const }
            } catch {
              return { documentId: doc.id, links: [] as DocumentLink[], ok: false as const }
            }
          }),
        )

        const nextLinks: Record<string, DocumentLink[]> = {}
        const nextErrors: Record<string, boolean> = {}
        for (const result of linkResults) {
          nextLinks[result.documentId] = result.links
          if (!result.ok) nextErrors[result.documentId] = true
        }
        setDocumentLinksByDocumentId(nextLinks)
        setDocumentLinkLoadErrors(nextErrors)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load project')
    } finally {
      setLoading(false)
    }
  }, [id, loadRepoMirrorDiscovery, taskSort, taskOrder, taskFilters])

  useEffect(() => {
    loadData()
  }, [loadData])

  useEffect(() => {
    setProjectBranchForm(project?.default_branch ?? '')
  }, [project?.id, project?.default_branch])

  async function handleSync() {
    if (!id || syncing) return
    setSyncing(true)
    try {
      await triggerSync(id)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sync failed')
    } finally {
      setSyncing(false)
    }
  }

  async function saveProjectBranch(nextBranch: string, source: 'manual' | 'detected' = 'manual') {
    if (!id || !project) return
    const normalizedBranch = nextBranch.trim()
    const currentBranch = (project.default_branch || '').trim()
    if (normalizedBranch === currentBranch) return

    try {
      setSavingProjectBranch(true)
      setError(null)
      const response = await updateProject(id, { default_branch: normalizedBranch })
      setProject(response.data)
      setProjectBranchForm(response.data.default_branch || '')
      setSuccessMessage(normalizedBranch === ''
        ? 'Project default branch cleared. Sync will auto-detect when possible.'
        : source === 'detected'
          ? `Applied detected branch ${normalizedBranch}. Rerun sync to verify the fix.`
          : `Project default branch updated to ${normalizedBranch}.`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update project branch')
    } finally {
      setSavingProjectBranch(false)
    }
  }

  async function handleSaveProjectBranch() {
    await saveProjectBranch(projectBranchForm, 'manual')
  }

  async function saveRepoMappingBranch(mappingId: string, nextBranch: string) {
    const normalizedBranch = nextBranch.trim()
    const mapping = repoMappings.find(item => item.id === mappingId)
    if (!mapping || normalizedBranch === (mapping.default_branch || '').trim()) return

    const response = await updateProjectRepoMapping(mappingId, { default_branch: normalizedBranch })
    setRepoMappings(prev => prev.map(item => item.id === mappingId ? response.data : item))
    return response.data
  }

  async function handleApplyDetectedBranchAndRerunSync() {
    if (!id || !detectedSyncBranch) return
    try {
      setSavingProjectBranch(true)
      setSyncing(true)
      setError(null)
      setSuccessMessage(null)

      if (quickFixBranchTarget?.type === 'repo-mapping') {
        await saveRepoMappingBranch(quickFixBranchTarget.mapping.id, detectedSyncBranch)
      } else {
        await saveProjectBranch(detectedSyncBranch, 'detected')
      }

      await triggerSync(id)
      await loadData()
      setSuccessMessage(
        quickFixBranchTarget?.type === 'repo-mapping'
          ? `Applied detected branch ${detectedSyncBranch} to primary repo mapping and reran sync.`
          : `Applied detected branch ${detectedSyncBranch} to project settings and reran sync.`,
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to apply detected branch and rerun sync')
    } finally {
      setSavingProjectBranch(false)
      setSyncing(false)
    }
  }

  async function handleResolveDrift(signalId: string) {
    try {
      await updateDriftSignal(signalId, { status: 'resolved', resolved_by: 'human' })
      setDriftSignals(prev => prev.map(s => (s.id === signalId ? { ...s, status: 'resolved' } : s)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to resolve signal')
    }
  }

  async function handleDismissDrift(signalId: string) {
    try {
      await updateDriftSignal(signalId, { status: 'dismissed', resolved_by: 'human' })
      setDriftSignals(prev => prev.map(s => (s.id === signalId ? { ...s, status: 'dismissed' } : s)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to dismiss signal')
    }
  }

  async function handleBulkResolveDrift() {
    if (!id || !confirm('Resolve all open drift signals?')) return
    try {
      await bulkResolveDriftSignals(id)
      setDriftSignals(prev => prev.map(s => s.status === 'open' ? { ...s, status: 'resolved' } : s))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to bulk resolve drift signals')
    }
  }

  async function handleViewDoc(doc: Document) {
    setViewingDoc(doc)
    setDocLoading(true)
    setDocContent(null)
    try {
      const resp = await getDocumentContent(doc.id)
      setDocContent(resp.data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load document content')
    } finally {
      setDocLoading(false)
    }
  }

  function closeDocViewer() {
    setViewingDoc(null)
    setDocContent(null)
    setDocLoading(false)
  }

  async function openLinksManager(doc: Document) {
    setManagingLinksDoc(doc)
    setDocLinksLoading(true)
    setDocLinks([])
    setNewLink({ code_path: '', link_type: 'covers' })
    try {
      const res = await listDocumentLinks(doc.id)
      setDocLinks(res.data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load document links')
    } finally {
      setDocLinksLoading(false)
    }
  }

  function closeLinksManager() {
    setManagingLinksDoc(null)
    setDocLinks([])
    setNewLink({ code_path: '', link_type: 'covers' })
  }

  async function handleCreateLink(e: React.FormEvent) {
    e.preventDefault()
    if (!managingLinksDoc || !newLink.code_path.trim()) return
    try {
      const res = await createDocumentLink(managingLinksDoc.id, {
        code_path: newLink.code_path.trim(),
        link_type: newLink.link_type,
      })
      setDocLinks(prev => [res.data, ...prev])
      setDocumentLinksByDocumentId(prev => ({
        ...prev,
        [managingLinksDoc.id]: [res.data, ...(prev[managingLinksDoc.id] ?? [])],
      }))
      setDocumentLinkLoadErrors(prev => ({ ...prev, [managingLinksDoc.id]: false }))
      setNewLink(prev => ({ ...prev, code_path: '' }))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create document link')
    }
  }

  async function handleDeleteLink(linkId: string) {
    if (!confirm('Delete this document link?')) return
    try {
      await deleteDocumentLink(linkId)
      setDocLinks(prev => prev.filter(link => link.id !== linkId))
      if (managingLinksDoc) {
        setDocumentLinksByDocumentId(prev => ({
          ...prev,
          [managingLinksDoc.id]: (prev[managingLinksDoc.id] ?? []).filter(link => link.id !== linkId),
        }))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete document link')
    }
  }

  async function handleDeleteDoc(docId: string) {
    if (!confirm('Delete this document?')) return
    try {
      await deleteDocument(docId)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete document')
    }
  }

  function healthClass(score: number) {
    if (score >= 0.7) return 'health-good'
    if (score >= 0.4) return 'health-ok'
    return 'health-bad'
  }

  const summary = dashboardSummary?.summary ?? null
  const latestSyncRun = dashboardSummary?.latest_sync_run ?? syncRuns[0] ?? null
  const recentSyncRuns = syncRuns.slice(0, 3)
  const openDriftCount = dashboardSummary && dashboardSummary.open_drift_count >= 0
    ? dashboardSummary.open_drift_count
    : driftSignals.filter(signal => signal.status === 'open').length
  const recentDashboardAgentRuns = dashboardSummary?.recent_agent_runs?.length ? dashboardSummary.recent_agent_runs : agentRuns.slice(0, 5)
  const hasRepoSource = Boolean(project?.repo_path || project?.repo_url || repoMappings.length > 0)
  const latestSyncGuidance: SyncGuidance | null = latestSyncRun ? syncRunGuidance(latestSyncRun, openDriftCount) : null
  const hasActiveTaskFilters = Boolean(taskFilters.status || taskFilters.priority || taskFilters.assignee.trim())
  const taskTabCount = hasActiveTaskFilters && summary ? `${tasks.length}/${summary.total_tasks}` : `${summary?.total_tasks ?? tasks.length}`
  const primaryRepoMapping = repoMappings.find(mapping => mapping.is_primary) ?? null
  const detectedMirrorBranch = primaryRepoMapping
    ? repoMirrorDiscovery?.repos.find(repo => repo.repo_path === primaryRepoMapping.repo_path)?.detected_default_branch || primaryRepoMapping.default_branch || ''
    : project?.repo_path
      ? repoMirrorDiscovery?.repos.find(repo => repo.repo_path === project.repo_path)?.detected_default_branch || ''
      : ''
  const detectedErrorBranchMatch = latestSyncRun?.error_message?.match(/detected default branch is "([^"]+)"/i)
  const detectedErrorBranch = detectedErrorBranchMatch?.[1]?.trim() || ''
  const detectedSyncBranch = detectedErrorBranch || detectedMirrorBranch
  const detectedProjectBranch = detectedSyncBranch
  const detectedPrimaryRepoMappingBranch = primaryRepoMapping ? detectedSyncBranch : ''
  const branchFormChanged = project !== null && projectBranchForm.trim() !== (project.default_branch || '').trim()
  const branchResolutionError = Boolean(
    latestSyncRun?.status === 'failed' &&
    latestSyncRun.error_message &&
    (
      latestSyncRun.error_message.toLowerCase().includes('detected default branch is') ||
      latestSyncRun.error_message.toLowerCase().includes('unknown revision') ||
      latestSyncRun.error_message.toLowerCase().includes('ambiguous argument') ||
      latestSyncRun.error_message.toLowerCase().includes('needed a single revision')
    ),
  )
  const canApplyDetectedBranchQuickFix = Boolean(branchResolutionError && detectedSyncBranch)
  const quickFixBranchTarget = primaryRepoMapping && (primaryRepoMapping.default_branch || '').trim() !== ''
    ? { type: 'repo-mapping' as const, mapping: primaryRepoMapping }
    : { type: 'project' as const }
  const quickFixAlreadyApplied = quickFixBranchTarget.type === 'repo-mapping'
    ? (quickFixBranchTarget.mapping.default_branch || '').trim() === detectedSyncBranch
    : (project?.default_branch || '').trim() === detectedSyncBranch
  const canApplyDetectedBranchAndRerun = canApplyDetectedBranchQuickFix && !quickFixAlreadyApplied

  if (loading) return <div className="loading">Loading project...</div>
  if (!project) return <div className="error-message">Project not found</div>

  return (
    <div className="project-detail-page">
      <div style={{ marginBottom: '0.5rem' }}>
        <Link to="/">&larr; Back to Projects</Link>
      </div>

      <div className="page-header">
        <div>
          <h2>{project.name}</h2>
          {project.description && <p style={{ color: 'var(--text-muted)', marginTop: '0.25rem' }}>{project.description}</p>}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <button className="btn btn-primary" onClick={handleSync} disabled={syncing}>
            {syncing ? 'Syncing...' : 'Sync Now'}
          </button>
          {summary && (
            <div className={`health-score ${healthClass(summary.health_score)}`}>
              {Math.round(summary.health_score * 100)}%
            </div>
          )}
        </div>
      </div>

      {error && <div className="error-message">{error}</div>}
      {successMessage && <div className="alert alert-success">{successMessage}</div>}

      {summary && (
        <div className="grid-4" style={{ marginBottom: '2rem' }}>
          <div className="stat-card">
            <div className="stat-value">{summary.total_tasks}</div>
            <div className="stat-label">Total Tasks</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: 'var(--info)' }}>{summary.tasks_in_progress}</div>
            <div className="stat-label">In Progress</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: 'var(--success)' }}>{summary.tasks_done}</div>
            <div className="stat-label">Done</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: summary.stale_documents > 0 ? 'var(--danger)' : 'var(--success)' }}>
              {summary.stale_documents}/{summary.total_documents}
            </div>
            <div className="stat-label">Stale Docs</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: openDriftCount > 0 ? 'var(--danger)' : 'var(--success)' }}>{openDriftCount}</div>
            <div className="stat-label">Open Drift</div>
          </div>
          <div className="stat-card">
            <div className="stat-value" style={{ color: recentDashboardAgentRuns.length > 0 ? 'var(--info)' : 'var(--text-muted)' }}>{recentDashboardAgentRuns.length}</div>
            <div className="stat-label">Recent Agent Runs</div>
          </div>
        </div>
      )}

      <SyncStatusPanel
        project={project}
        latestSyncRun={latestSyncRun}
        recentSyncRuns={recentSyncRuns}
        openDriftCount={openDriftCount}
        hasRepoSource={hasRepoSource}
        syncing={syncing}
        latestSyncGuidance={latestSyncGuidance}
        canApplyDetectedBranchAndRerun={canApplyDetectedBranchAndRerun}
        detectedSyncBranch={detectedSyncBranch}
        quickFixBranchTarget={quickFixBranchTarget}
        savingProjectBranch={savingProjectBranch}
        projectBranchForm={projectBranchForm}
        branchFormChanged={branchFormChanged}
        detectedProjectBranch={detectedProjectBranch}
        onSync={handleSync}
        onApplyDetectedBranchAndRerunSync={handleApplyDetectedBranchAndRerunSync}
        onNavigateToDrift={() => setTab('drift')}
        onProjectBranchFormChange={setProjectBranchForm}
        onSaveProjectBranch={handleSaveProjectBranch}
        onClearProjectBranch={() => setProjectBranchForm('')}
        onUseDetectedBranch={branch => setProjectBranchForm(branch)}
      />

      <div className="project-rail-layout">
        <nav className="project-rail" aria-label="Project sections">
          <button className={tab === 'overview' ? 'is-active' : ''} onClick={() => setTab('overview')}>
            <span>Overview</span>
          </button>
          <button className={tab === 'planning' ? 'is-active' : ''} onClick={() => setTab('planning')}>
            <span>Workspace</span>
            <span className="rail-count">{requirements.length}</span>
          </button>
          <button className={tab === 'tasks' ? 'is-active' : ''} onClick={() => setTab('tasks')}>
            <span>Tasks</span>
            <span className="rail-count">{taskTabCount}</span>
          </button>
          <button className={tab === 'documents' ? 'is-active' : ''} onClick={() => setTab('documents')}>
            <span>Documents</span>
            <span className="rail-count">{documents.length}</span>
          </button>
          <button className={tab === 'drift' ? 'is-active' : ''} onClick={() => setTab('drift')}>
            <span>Drift</span>
            <span className="rail-count">{driftSignals.filter(s => s.status === 'open').length}</span>
          </button>
          <button className={tab === 'agents' ? 'is-active' : ''} onClick={() => setTab('agents')}>
            <span>Activity</span>
            <span className="rail-count">{agentRuns.length}</span>
          </button>
          <button className={tab === 'settings' ? 'is-active' : ''} onClick={() => setTab('settings')}>
            <span>Settings</span>
            <span className="rail-count">{repoMappings.length}</span>
          </button>
        </nav>
        <div className="project-rail-content">

          {tab === 'overview' && (
            <ProjectOverviewTab
              requirements={requirements}
              openDriftCount={openDriftCount}
              driftSignals={driftSignals}
              agentRuns={agentRuns}
              summary={summary}
              onSetTab={setTab}
            />
          )}

          {tab === 'planning' && (
            <PlanningTab
              projectId={id!}
              requirements={requirements}
              tasks={tasks}
              openDriftCount={openDriftCount}
              planningLoadError={planningLoadError}
              onReload={loadData}
              onError={setError}
              onSuccess={setSuccessMessage}
              onRequirementsChange={setRequirements}
              onNavigateToTasks={() => setTab('tasks')}
              onNavigateToDrift={() => setTab('drift')}
              onViewDocumentById={(documentId) => {
                const doc = documents.find(d => d.id === documentId)
                if (doc) {
                  handleViewDoc(doc)
                  return
                }
                setError(`Unable to open document: "${documentId}" is no longer registered in this project.`)
              }}
              onViewDriftSignal={() => setTab('drift')}
            />
          )}

          {tab === 'tasks' && (
            <TasksTab
              projectId={id!}
              tasks={tasks}
              summary={summary}
              taskSort={taskSort}
              taskOrder={taskOrder}
              taskFilters={taskFilters}
              onSortChange={setTaskSort}
              onOrderChange={setTaskOrder}
              onFilterChange={setTaskFilters}
              onReload={loadData}
              onError={setError}
              onSuccess={setSuccessMessage}
            />
          )}

          {tab === 'documents' && (
            <DocumentsTab
              projectId={id!}
              documents={documents}
              driftSignals={driftSignals}
              documentLinksByDocumentId={documentLinksByDocumentId}
              documentLinkLoadErrors={documentLinkLoadErrors}
              onReload={loadData}
              onError={setError}
              onViewDoc={handleViewDoc}
              onManageLinks={openLinksManager}
              onDeleteDoc={handleDeleteDoc}
            />
          )}

          {tab === 'drift' && (
            <DriftTab
              driftSignals={driftSignals}
              documents={documents}
              documentLinksByDocumentId={documentLinksByDocumentId}
              documentLinkLoadErrors={documentLinkLoadErrors}
              onViewDoc={handleViewDoc}
              onManageLinks={openLinksManager}
              onResolveDrift={handleResolveDrift}
              onDismissDrift={handleDismissDrift}
              onBulkResolveDrift={handleBulkResolveDrift}
            />
          )}

          {tab === 'agents' && (
            <AgentsTab agentRuns={agentRuns} syncRuns={syncRuns} />
          )}

          {tab === 'settings' && (
            <SettingsTab
              projectId={id!}
              project={project}
              primaryRepoMapping={primaryRepoMapping}
              repoMappings={repoMappings}
              repoMirrorDiscovery={repoMirrorDiscovery}
              repoMirrorLoading={repoMirrorLoading}
              repoMirrorLoadError={repoMirrorLoadError}
              detectedPrimaryRepoMappingBranch={detectedPrimaryRepoMappingBranch}
              onLoadRepoMirrorDiscovery={() => id && loadRepoMirrorDiscovery(id)}
              onReload={loadData}
              onError={setError}
              onSuccess={setSuccessMessage}
            />
          )}

        </div>
      </div>

      {/* Global document viewer modal — works from any tab */}
      {viewingDoc && (
        <div className="modal-overlay" onClick={closeDocViewer}>
          <div className="modal doc-viewer-modal" onClick={e => e.stopPropagation()}>
            <h3>{viewingDoc.title}</h3>
            <p className="doc-viewer-meta">{viewingDoc.file_path || 'No file path registered'}</p>
            {docLoading ? (
              <div className="loading">Loading document...</div>
            ) : docContent ? (
              <>
                <pre className="doc-viewer-content">{docContent.content}</pre>
                {docContent.truncated && (
                  <p className="doc-viewer-hint">Preview truncated to keep the UI responsive.</p>
                )}
              </>
            ) : (
              <div className="error-message" style={{ marginTop: '1rem' }}>Unable to load document content.</div>
            )}
            <div className="modal-actions">
              <button type="button" className="btn btn-primary" onClick={closeDocViewer}>Close</button>
            </div>
          </div>
        </div>
      )}

      {/* Global document links modal — works from any tab */}
      {managingLinksDoc && (
        <div className="modal-overlay" onClick={closeLinksManager}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Manage Links: {managingLinksDoc.title}</h3>
            <form onSubmit={handleCreateLink}>
              <div className="form-group">
                <label>Code Path *</label>
                <input
                  value={newLink.code_path}
                  onChange={e => setNewLink(prev => ({ ...prev, code_path: e.target.value }))}
                  placeholder="backend/internal/git/sync_service.go"
                  autoFocus
                />
              </div>
              <div className="form-group">
                <label>Link Type</label>
                <select
                  value={newLink.link_type}
                  onChange={e => setNewLink(prev => ({ ...prev, link_type: e.target.value as DocumentLink['link_type'] }))}
                >
                  <option value="covers">covers</option>
                  <option value="references">references</option>
                  <option value="depends_on">depends_on</option>
                </select>
              </div>
              <div className="modal-actions">
                <button type="submit" className="btn btn-primary">Add Link</button>
              </div>
            </form>

            {docLinksLoading ? (
              <div className="loading" style={{ marginTop: '1rem' }}>Loading links...</div>
            ) : docLinks.length === 0 ? (
              <p style={{ marginTop: '1rem', color: 'var(--text-muted)' }}>No links yet.</p>
            ) : (
              <div className="table-wrap" style={{ marginTop: '1rem' }}>
                <table className="table">
                  <thead>
                    <tr>
                      <th>Code Path</th>
                      <th>Type</th>
                      <th>Created</th>
                      <th>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {docLinks.map(link => (
                      <tr key={link.id}>
                        <td style={{ fontSize: '0.85rem' }}>{link.code_path}</td>
                        <td><span className="badge badge-todo">{link.link_type}</span></td>
                        <td style={{ fontSize: '0.85rem' }}>{new Date(link.created_at).toLocaleString()}</td>
                        <td>
                          <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteLink(link.id)} style={{ color: 'var(--danger)' }}>Delete</button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <div className="modal-actions" style={{ marginTop: '1rem' }}>
              <button type="button" className="btn btn-ghost" onClick={closeLinksManager}>Close</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default ProjectDetail
