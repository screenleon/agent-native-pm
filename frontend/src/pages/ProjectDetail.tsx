import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import type { Project, Task, Document, ProjectSummary, SyncRun, AgentRun, DriftSignal, DocumentContent, DocumentLink, ProjectRepoMapping, MirrorRepoCandidate, MirrorRepoDiscovery } from '../types'
import {
  discoverMirrorRepos,
  getProject,
  getProjectSummary,
  listTasks,
  createTask,
  updateTask,
  deleteTask,
  listDocuments,
  createDocument,
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
  createProjectRepoMapping,
  deleteProjectRepoMapping,
} from '../api/client'
import { syncRunGuidance, type SyncGuidance } from '../utils/syncGuidance'

type Tab = 'tasks' | 'documents' | 'drift' | 'agents'
type DriftFilter = 'open' | 'all' | 'resolved' | 'dismissed'
type DriftSort = 'severity' | 'created_at'

function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [summary, setSummary] = useState<ProjectSummary | null>(null)
  const [tasks, setTasks] = useState<Task[]>([])
  const [documents, setDocuments] = useState<Document[]>([])
  const [syncRuns, setSyncRuns] = useState<SyncRun[]>([])
  const [agentRuns, setAgentRuns] = useState<AgentRun[]>([])
  const [driftSignals, setDriftSignals] = useState<DriftSignal[]>([])
  const [tab, setTab] = useState<Tab>('tasks')
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [driftFilter, setDriftFilter] = useState<DriftFilter>('open')
  const [driftSort, setDriftSort] = useState<DriftSort>('severity')
  const [selectedDriftId, setSelectedDriftId] = useState<string | null>(null)

  const [showTaskForm, setShowTaskForm] = useState(false)
  const [taskForm, setTaskForm] = useState({ title: '', description: '', priority: 'medium' as Task['priority'], assignee: '', source: 'human' })
  const [editingTask, setEditingTask] = useState<Task | null>(null)
  const [editTaskForm, setEditTaskForm] = useState<{ title: string; description: string; status: Task['status']; priority: Task['priority']; assignee: string }>({ title: '', description: '', status: 'todo', priority: 'medium', assignee: '' })
  const [taskSort, setTaskSort] = useState<string>('created_at')
  const [taskOrder, setTaskOrder] = useState<string>('desc')

  const [showDocForm, setShowDocForm] = useState(false)
  const [docForm, setDocForm] = useState({ title: '', file_path: '', doc_type: 'general' as Document['doc_type'], source: 'human' })
  const [viewingDoc, setViewingDoc] = useState<Document | null>(null)
  const [docContent, setDocContent] = useState<DocumentContent | null>(null)
  const [docLoading, setDocLoading] = useState(false)
  const [managingLinksDoc, setManagingLinksDoc] = useState<Document | null>(null)
  const [docLinks, setDocLinks] = useState<DocumentLink[]>([])
  const [docLinksLoading, setDocLinksLoading] = useState(false)
  const [newLink, setNewLink] = useState({ code_path: '', link_type: 'covers' as DocumentLink['link_type'] })
  const [documentLinksByDocumentId, setDocumentLinksByDocumentId] = useState<Record<string, DocumentLink[]>>({})
  const [documentLinkLoadErrors, setDocumentLinkLoadErrors] = useState<Record<string, boolean>>({})
  const [selectedDriftPreview, setSelectedDriftPreview] = useState<DocumentContent | null>(null)
  const [selectedDriftPreviewLoading, setSelectedDriftPreviewLoading] = useState(false)
  const [selectedDriftPreviewError, setSelectedDriftPreviewError] = useState<string | null>(null)
  const [repoMappings, setRepoMappings] = useState<ProjectRepoMapping[]>([])
  const [repoMirrorDiscovery, setRepoMirrorDiscovery] = useState<MirrorRepoDiscovery | null>(null)
  const [repoMirrorLoading, setRepoMirrorLoading] = useState(false)
  const [repoMirrorLoadError, setRepoMirrorLoadError] = useState<string | null>(null)
  const [showRepoMappingForm, setShowRepoMappingForm] = useState(false)
  const [repoMappingForm, setRepoMappingForm] = useState({ alias: '', repo_path: '', default_branch: '', is_primary: false })

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
      const [projRes, summaryRes, tasksRes, docsRes] = await Promise.all([
        getProject(id),
        getProjectSummary(id),
        listTasks(id, 1, 100, taskSort, taskOrder),
        listDocuments(id, 1, 100),
      ])
      setProject(projRes.data)
      setSummary(summaryRes.data)
      setTasks(tasksRes.data)
      setDocuments(docsRes.data)

      const [syncRes, agentRes, driftRes, repoMappingsRes] = await Promise.allSettled([
        listSyncRuns(id),
        listAgentRuns(id),
        listDriftSignals(id),
        listProjectRepoMappings(id),
      ])

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
          if (!result.ok) {
            nextErrors[result.documentId] = true
          }
        }
        setDocumentLinksByDocumentId(nextLinks)
        setDocumentLinkLoadErrors(nextErrors)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load project')
    } finally {
      setLoading(false)
    }
  }, [id, loadRepoMirrorDiscovery, taskSort, taskOrder])

  useEffect(() => {
    loadData()
  }, [loadData])

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

  async function handleCreateTask(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !taskForm.title.trim()) return
    try {
      await createTask(id, taskForm)
      setTaskForm({ title: '', description: '', priority: 'medium', assignee: '', source: 'human' })
      setShowTaskForm(false)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create task')
    }
  }

  function openEditTask(task: Task) {
    setEditingTask(task)
    setEditTaskForm({ title: task.title, description: task.description, status: task.status, priority: task.priority, assignee: task.assignee })
  }

  function closeEditTask() {
    setEditingTask(null)
  }

  async function handleSaveTask(e: React.FormEvent) {
    e.preventDefault()
    if (!editingTask) return
    try {
      await updateTask(editingTask.id, editTaskForm)
      setEditingTask(null)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update task')
    }
  }

  async function handleDeleteEditingTask() {
    if (!editingTask || !confirm('Delete this task?')) return
    try {
      await deleteTask(editingTask.id)
      setEditingTask(null)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete task')
    }
  }

  async function handleCreateDoc(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !docForm.title.trim()) return
    try {
      await createDocument(id, docForm)
      setDocForm({ title: '', file_path: '', doc_type: 'general', source: 'human' })
      setShowDocForm(false)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create document')
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

  async function handleCreateRepoMapping(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !repoMappingForm.alias.trim() || !repoMappingForm.repo_path.trim()) return
    try {
      await createProjectRepoMapping(id, {
        alias: repoMappingForm.alias.trim(),
        repo_path: repoMappingForm.repo_path.trim(),
        default_branch: repoMappingForm.default_branch.trim(),
        is_primary: repoMappingForm.is_primary,
      })
      setRepoMappingForm({ alias: '', repo_path: '', default_branch: '', is_primary: false })
      setShowRepoMappingForm(false)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create repo mapping')
    }
  }

  async function handleDeleteRepoMapping(mappingId: string) {
    if (!confirm('Delete this repo mapping?')) return
    try {
      await deleteProjectRepoMapping(mappingId)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete repo mapping')
    }
  }

  function handleUseDiscoveredMirror(repo: MirrorRepoCandidate) {
    setRepoMappingForm({
      alias: repo.suggested_alias,
      repo_path: repo.repo_path,
      default_branch: repo.detected_default_branch || project?.default_branch || 'main',
      is_primary: repoMappings.length === 0,
    })
    setShowRepoMappingForm(true)
  }

  function healthClass(score: number) {
    if (score >= 0.7) return 'health-good'
    if (score >= 0.4) return 'health-ok'
    return 'health-bad'
  }

  function syncBadgeClass(status: SyncRun['status']) {
    if (status === 'completed') return 'badge-fresh'
    if (status === 'failed') return 'badge-stale'
    return 'badge-low'
  }

  function formatDateTime(value: string | null | undefined) {
    if (!value) return '—'
    return new Date(value).toLocaleString()
  }

  function formatSyncDuration(run: SyncRun) {
    if (!run.completed_at) return 'In progress'
    const started = new Date(run.started_at).getTime()
    const completed = new Date(run.completed_at).getTime()
    const diffMs = Math.max(0, completed - started)
    const seconds = Math.round(diffMs / 1000)
    if (seconds < 60) return `${seconds}s`
    const minutes = Math.floor(seconds / 60)
    const remainingSeconds = seconds % 60
    return remainingSeconds === 0 ? `${minutes}m` : `${minutes}m ${remainingSeconds}s`
  }

  function guidanceBadgeClass(tone: SyncGuidance['tone']) {
    if (tone === 'success') return 'badge-fresh'
    if (tone === 'warning') return 'badge-low'
    if (tone === 'danger') return 'badge-stale'
    return 'badge-todo'
  }

  function formatRelativeTime(value: string | null | undefined) {
    if (!value) return '—'
    const diffMs = Date.now() - new Date(value).getTime()
    if (diffMs < 60 * 1000) return 'just now'
    const minutes = Math.floor(diffMs / (60 * 1000))
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    return `${days}d ago`
  }

  function triggerTypeLabel(triggerType: DriftSignal['trigger_type']) {
    if (triggerType === 'code_change') return 'Code change'
    if (triggerType === 'time_decay') return 'Time decay'
    return 'Manual'
  }

  /** Returns an array of {path, change_type} for a drift signal's impacted files.
   *  Prefers structured trigger_meta.changed_files; falls back to parsing the
   *  legacy trigger_detail string so old records still display correctly. */
  function changedFilesFromSignal(signal: DriftSignal): Array<{ path: string; change_type: string }> {
    if (signal.trigger_meta?.changed_files?.length) {
      return signal.trigger_meta.changed_files
    }
    // Legacy fallback: parse "File changed: path (M)" / "Files changed: p1 (M), p2 (A)"
    const detail = signal.trigger_detail
    const prefixes = ['Files changed:', 'File changed:']
    const prefix = prefixes.find(p => detail.startsWith(p))
    if (!prefix) return []
    return detail
      .slice(prefix.length)
      .split(',')
      .map(token => token.trim())
      .filter(Boolean)
      .map(token => {
        const match = token.match(/^(.*)\s+\(([MADR])\)$/)
        if (match) return { path: match[1].trim(), change_type: match[2] }
        return { path: token, change_type: '' }
      })
  }

  function severityLabel(severity: number): string {
    if (severity >= 3) return 'High'
    if (severity === 2) return 'Medium'
    return 'Low'
  }

  function severityBadgeClass(severity: number): string {
    if (severity >= 3) return 'badge-stale'   // red
    if (severity === 2) return 'badge-high'    // amber
    return 'badge-low'                         // muted
  }

  function confidenceBadgeClass(confidence: string | undefined): string {
    if (confidence === 'high') return 'badge-fresh'
    if (confidence === 'medium') return 'badge-medium'
    return 'badge-low'
  }

  const latestSyncRun = syncRuns[0] ?? null
  const recentSyncRuns = syncRuns.slice(0, 3)
  const openDriftCount = driftSignals.filter(signal => signal.status === 'open').length
  const hasRepoSource = Boolean(project?.repo_path || project?.repo_url || repoMappings.length > 0)
  const latestSyncGuidance: SyncGuidance | null = latestSyncRun ? syncRunGuidance(latestSyncRun, openDriftCount) : null
  const filteredDriftSignals = driftSignals
    .filter(signal => (driftFilter === 'all' ? true : signal.status === driftFilter))
    .sort((a, b) => {
      if (driftSort === 'severity') {
        const diff = (b.severity ?? 1) - (a.severity ?? 1)
        if (diff !== 0) return diff
      }
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    })
  const selectedDriftSignal = filteredDriftSignals.find(signal => signal.id === selectedDriftId) ?? filteredDriftSignals[0] ?? null
  const selectedDriftDocument = selectedDriftSignal?.document_id
    ? documents.find(document => document.id === selectedDriftSignal.document_id) ?? null
    : null
  const selectedDriftLinks = selectedDriftDocument ? documentLinksByDocumentId[selectedDriftDocument.id] ?? [] : []
  const selectedDriftChangedFiles = selectedDriftSignal ? changedFilesFromSignal(selectedDriftSignal) : []
  const selectedDriftCoverageBreakdown = selectedDriftLinks.reduce(
    (acc, link) => {
      acc[link.link_type] = (acc[link.link_type] ?? 0) + 1
      return acc
    },
    {} as Record<DocumentLink['link_type'], number>,
  )

  useEffect(() => {
    if (filteredDriftSignals.length === 0) {
      if (selectedDriftId !== null) {
        setSelectedDriftId(null)
      }
      return
    }
    if (!selectedDriftId || !filteredDriftSignals.some(signal => signal.id === selectedDriftId)) {
      setSelectedDriftId(filteredDriftSignals[0].id)
    }
  }, [filteredDriftSignals, selectedDriftId])

  useEffect(() => {
    let mounted = true
    async function loadSelectedDriftPreview() {
      if (!selectedDriftDocument || !selectedDriftDocument.file_path) {
        if (mounted) {
          setSelectedDriftPreview(null)
          setSelectedDriftPreviewError(null)
          setSelectedDriftPreviewLoading(false)
        }
        return
      }

      setSelectedDriftPreviewLoading(true)
      setSelectedDriftPreviewError(null)
      try {
        const res = await getDocumentContent(selectedDriftDocument.id)
        if (mounted) {
          setSelectedDriftPreview(res.data)
        }
      } catch {
        if (mounted) {
          setSelectedDriftPreview(null)
          setSelectedDriftPreviewError('Unable to load inline document preview.')
        }
      } finally {
        if (mounted) {
          setSelectedDriftPreviewLoading(false)
        }
      }
    }
    loadSelectedDriftPreview()
    return () => {
      mounted = false
    }
  }, [selectedDriftDocument])

  if (loading) return <div className="loading">Loading project...</div>
  if (!project) return <div className="error-message">Project not found</div>

  return (
    <div>
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
        </div>
      )}

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
              </div>
            )}
          </div>

          <div style={{ minWidth: '240px', flex: '0 1 280px' }}>
            <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginBottom: '0.5rem' }}>Next action</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <button className="btn btn-primary" onClick={handleSync} disabled={syncing || !hasRepoSource}>
                {syncing ? 'Syncing...' : 'Run Sync Now'}
              </button>
              <button className="btn btn-ghost" onClick={() => setTab('drift')} disabled={openDriftCount === 0}>
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
                    <button className="btn btn-ghost btn-sm" onClick={() => setTab('drift')}>
                      Open Drift
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>

      <div className="card" style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <div>
            <h3 style={{ marginBottom: '0.2rem' }}>Repo Mappings</h3>
            <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              Bind one or more mirror repositories to this project. Use alias-prefixed paths like <strong>docs-repo/path/to/file.md</strong> for secondary repos.
            </p>
          </div>
          <button className="btn btn-primary" onClick={() => setShowRepoMappingForm(true)}>+ Add Repo Mapping</button>
        </div>

        <div style={{ marginTop: '1rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.85rem', background: 'var(--bg)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.5rem' }}>
            <div>
              <strong>Mounted Mirrors</strong>
              <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                Auto-load mirrors already visible inside the container and prefill the repo mapping form.
              </div>
            </div>
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => id && loadRepoMirrorDiscovery(id)} disabled={repoMirrorLoading}>
              {repoMirrorLoading ? 'Loading…' : 'Reload'}
            </button>
          </div>

          {repoMirrorLoadError && <div className="error-banner">{repoMirrorLoadError}</div>}

          {repoMirrorLoading ? (
            <div className="loading" style={{ padding: '0.75rem 0' }}>Loading mounted mirrors…</div>
          ) : !repoMirrorDiscovery || repoMirrorDiscovery.repos.length === 0 ? (
            <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              No mounted mirrors were discovered for this project context.
            </p>
          ) : (
            <div style={{ display: 'grid', gap: '0.65rem' }}>
              {repoMirrorDiscovery.repos.map(repo => (
                <div key={repo.repo_path} style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', flexWrap: 'wrap' }}>
                  <div>
                    <strong>{repo.repo_name}</strong>
                    <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.82rem' }}>{repo.repo_path}</div>
                    <div style={{ marginTop: '0.35rem', display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
                      <span className="badge badge-low">alias {repo.suggested_alias}</span>
                      <span className="badge badge-low">branch {repo.detected_default_branch || project?.default_branch || 'main'}</span>
                      {repo.is_primary_for_project && <span className="badge badge-fresh">primary</span>}
                      {repo.is_mapped_to_project && !repo.is_primary_for_project && <span className="badge badge-low">already mapped</span>}
                    </div>
                  </div>
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    disabled={repo.is_mapped_to_project}
                    onClick={() => handleUseDiscoveredMirror(repo)}
                  >
                    {repo.is_mapped_to_project ? 'Added' : 'Use This Mirror'}
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>

        {showRepoMappingForm && (
          <form onSubmit={handleCreateRepoMapping} style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.75rem' }}>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label>Alias</label>
                <input value={repoMappingForm.alias} onChange={e => setRepoMappingForm({ ...repoMappingForm, alias: e.target.value })} placeholder="app, docs-repo, shared-lib" />
              </div>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label>Repo Path</label>
                <input value={repoMappingForm.repo_path} onChange={e => setRepoMappingForm({ ...repoMappingForm, repo_path: e.target.value })} placeholder="/mirrors/agent-native-pm" />
              </div>
              <div className="form-group" style={{ marginBottom: 0 }}>
                <label>Default Branch</label>
                <input value={repoMappingForm.default_branch} onChange={e => setRepoMappingForm({ ...repoMappingForm, default_branch: e.target.value })} placeholder="main" />
              </div>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
              <input type="checkbox" checked={repoMappingForm.is_primary} onChange={e => setRepoMappingForm({ ...repoMappingForm, is_primary: e.target.checked })} />
              Set as primary repo
            </label>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <button type="submit" className="btn btn-primary">Save Mapping</button>
              <button type="button" className="btn btn-ghost" onClick={() => setShowRepoMappingForm(false)}>Cancel</button>
            </div>
          </form>
        )}

        {repoMappings.length === 0 ? (
          <p style={{ marginTop: '1rem', color: 'var(--text-muted)' }}>No repo mappings yet. Add a primary mapping like <strong>/mirrors/agent-native-pm</strong> to enable mirror-based scanning.</p>
        ) : (
          <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
            {repoMappings.map(mapping => (
              <div key={mapping.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', flexWrap: 'wrap', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem 0.9rem', background: 'var(--bg)' }}>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                    <strong>{mapping.alias}</strong>
                    {mapping.is_primary && <span className="badge badge-fresh">primary</span>}
                    <span className="badge badge-low">{mapping.default_branch || project?.default_branch || 'main'}</span>
                  </div>
                  <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>{mapping.repo_path}</div>
                </div>
                <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteRepoMapping(mapping.id)}>Delete</button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', gap: '1rem', marginBottom: '1.5rem', borderBottom: '1px solid var(--border)', paddingBottom: '0.5rem' }}>
        <button className={`btn ${tab === 'tasks' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setTab('tasks')}>Tasks ({tasks.length})</button>
        <button className={`btn ${tab === 'documents' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setTab('documents')}>Documents ({documents.length})</button>
        <button className={`btn ${tab === 'drift' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setTab('drift')}>
          Drift Signals ({driftSignals.filter(s => s.status === 'open').length})
        </button>
        <button className={`btn ${tab === 'agents' ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setTab('agents')}>Agent Activity ({agentRuns.length})</button>
      </div>

      {tab === 'tasks' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', gap: '1rem' }}>
            <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
              <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Sort by:</label>
              <select value={taskSort} onChange={e => setTaskSort(e.target.value)} style={{ padding: '0.4rem 0.6rem' }}>
                <option value="created_at">Created Date</option>
                <option value="updated_at">Updated Date</option>
                <option value="priority">Priority</option>
                <option value="status">Status</option>
                <option value="title">Title</option>
              </select>
              <select value={taskOrder} onChange={e => setTaskOrder(e.target.value)} style={{ padding: '0.4rem 0.6rem' }}>
                <option value="desc">Descending</option>
                <option value="asc">Ascending</option>
              </select>
            </div>
            <button className="btn btn-primary" onClick={() => setShowTaskForm(true)}>+ New Task</button>
          </div>

          {showTaskForm && (
            <div className="modal-overlay" onClick={() => setShowTaskForm(false)}>
              <div className="modal" onClick={e => e.stopPropagation()}>
                <h3>Create Task</h3>
                <form onSubmit={handleCreateTask}>
                  <div className="form-group">
                    <label>Title *</label>
                    <input value={taskForm.title} onChange={e => setTaskForm({ ...taskForm, title: e.target.value })} autoFocus />
                  </div>
                  <div className="form-group">
                    <label>Description</label>
                    <textarea value={taskForm.description} onChange={e => setTaskForm({ ...taskForm, description: e.target.value })} />
                  </div>
                  <div className="form-group">
                    <label>Priority</label>
                    <select value={taskForm.priority} onChange={e => setTaskForm({ ...taskForm, priority: e.target.value as Task['priority'] })}>
                      <option value="low">Low</option>
                      <option value="medium">Medium</option>
                      <option value="high">High</option>
                    </select>
                  </div>
                  <div className="form-group">
                    <label>Assignee</label>
                    <input value={taskForm.assignee} onChange={e => setTaskForm({ ...taskForm, assignee: e.target.value })} placeholder="human or agent:name" />
                  </div>
                  <div className="modal-actions">
                    <button type="button" className="btn btn-ghost" onClick={() => setShowTaskForm(false)}>Cancel</button>
                    <button type="submit" className="btn btn-primary">Create</button>
                  </div>
                </form>
              </div>
            </div>
          )}

          {editingTask && (
            <div className="modal-overlay" onClick={closeEditTask}>
              <div className="modal" onClick={e => e.stopPropagation()}>
                <h3>Edit Task</h3>
                <form onSubmit={handleSaveTask}>
                  <div className="form-group">
                    <label>Title *</label>
                    <input value={editTaskForm.title} onChange={e => setEditTaskForm({ ...editTaskForm, title: e.target.value })} autoFocus />
                  </div>
                  <div className="form-group">
                    <label>Description</label>
                    <textarea value={editTaskForm.description} onChange={e => setEditTaskForm({ ...editTaskForm, description: e.target.value })} rows={4} />
                  </div>
                  <div className="form-group">
                    <label>Status</label>
                    <select value={editTaskForm.status} onChange={e => setEditTaskForm({ ...editTaskForm, status: e.target.value as Task['status'] })}>
                      <option value="todo">To Do</option>
                      <option value="in_progress">In Progress</option>
                      <option value="done">Done</option>
                      <option value="cancelled">Cancelled</option>
                    </select>
                  </div>
                  <div className="form-group">
                    <label>Priority</label>
                    <select value={editTaskForm.priority} onChange={e => setEditTaskForm({ ...editTaskForm, priority: e.target.value as Task['priority'] })}>
                      <option value="low">Low</option>
                      <option value="medium">Medium</option>
                      <option value="high">High</option>
                    </select>
                  </div>
                  <div className="form-group">
                    <label>Assignee</label>
                    <input value={editTaskForm.assignee} onChange={e => setEditTaskForm({ ...editTaskForm, assignee: e.target.value })} placeholder="human or agent:name" />
                  </div>
                  <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginBottom: '0.5rem' }}>
                    Source: {editingTask.source} &nbsp;·&nbsp; Created: {new Date(editingTask.created_at).toLocaleString()}
                  </div>
                  <div className="modal-actions">
                    <button type="button" className="btn btn-ghost" style={{ color: 'var(--danger)', marginRight: 'auto' }} onClick={handleDeleteEditingTask}>Delete</button>
                    <button type="button" className="btn btn-ghost" onClick={closeEditTask}>Cancel</button>
                    <button type="submit" className="btn btn-primary">Save</button>
                  </div>
                </form>
              </div>
            </div>
          )}

          {tasks.length === 0 ? (
            <div className="empty-state">
              <h3>No tasks yet</h3>
              <p>Create your first task to get started.</p>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Status</th>
                  <th>Priority</th>
                  <th>Assignee</th>
                  <th>Updated</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map(task => (
                  <tr key={task.id} style={{ cursor: 'pointer' }} onClick={() => openEditTask(task)}>
                    <td>{task.title}</td>
                    <td><span className={`badge badge-${task.status === 'done' ? 'fresh' : task.status === 'in_progress' ? 'low' : task.status === 'cancelled' ? 'stale' : 'todo'}`}>{task.status.replace('_', ' ')}</span></td>
                    <td><span className={`badge badge-${task.priority}`}>{task.priority}</span></td>
                    <td style={{ color: 'var(--text-muted)' }}>{task.assignee || '—'}</td>
                    <td style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{new Date(task.updated_at).toLocaleDateString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {tab === 'documents' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '1rem' }}>
            <button className="btn btn-primary" onClick={() => setShowDocForm(true)}>+ Register Document</button>
          </div>

          {showDocForm && (
            <div className="modal-overlay" onClick={() => setShowDocForm(false)}>
              <div className="modal" onClick={e => e.stopPropagation()}>
                <h3>Register Document</h3>
                <form onSubmit={handleCreateDoc}>
                  <div className="form-group">
                    <label>Title *</label>
                    <input value={docForm.title} onChange={e => setDocForm({ ...docForm, title: e.target.value })} autoFocus />
                  </div>
                  <div className="form-group">
                    <label>File Path</label>
                    <input value={docForm.file_path} onChange={e => setDocForm({ ...docForm, file_path: e.target.value })} placeholder="docs/api-surface.md" />
                  </div>
                  <div className="form-group">
                    <label>Type</label>
                    <select value={docForm.doc_type} onChange={e => setDocForm({ ...docForm, doc_type: e.target.value as Document['doc_type'] })}>
                      <option value="general">General</option>
                      <option value="api">API</option>
                      <option value="architecture">Architecture</option>
                      <option value="guide">Guide</option>
                      <option value="adr">ADR</option>
                    </select>
                  </div>
                  <div className="modal-actions">
                    <button type="button" className="btn btn-ghost" onClick={() => setShowDocForm(false)}>Cancel</button>
                    <button type="submit" className="btn btn-primary">Register</button>
                  </div>
                </form>
              </div>
            </div>
          )}

          {documents.length === 0 ? (
            <div className="empty-state">
              <h3>No documents registered</h3>
              <p>Register documents to track their freshness.</p>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Type</th>
                  <th>File Path</th>
                  <th>Status</th>
                  <th>Coverage</th>
                  <th>Drift</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {documents.map(doc => (
                  <tr key={doc.id}>
                    <td>{doc.title}</td>
                    <td><span className="badge badge-todo">{doc.doc_type}</span></td>
                    <td style={{ fontSize: '0.8rem', opacity: 0.7 }}>{doc.file_path || '—'}</td>
                    <td>
                      <span className={`badge ${doc.is_stale ? 'badge-stale' : 'badge-fresh'}`}>
                        {doc.is_stale ? `Stale (${doc.staleness_days}d)` : 'Fresh'}
                      </span>
                    </td>
                    <td>
                      {documentLinkLoadErrors[doc.id] ? (
                        <span className="badge badge-low">Unknown</span>
                      ) : (documentLinksByDocumentId[doc.id]?.length ?? 0) === 0 ? (
                        <span className="badge badge-stale">Unlinked</span>
                      ) : (
                        <span className="badge badge-fresh">
                          {(documentLinksByDocumentId[doc.id]?.length ?? 0) === 1
                            ? '1 link'
                            : `${documentLinksByDocumentId[doc.id]?.length ?? 0} links`}
                        </span>
                      )}
                    </td>
                    <td>
                      {driftSignals.some(signal => signal.document_id === doc.id && signal.status === 'open') ? (
                        <span className="badge badge-stale">Open drift</span>
                      ) : (
                        <span className="badge badge-fresh">No drift</span>
                      )}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.5rem' }}>
                        <button className="btn btn-ghost btn-sm" onClick={() => handleViewDoc(doc)} disabled={!doc.file_path}>View</button>
                        <button className="btn btn-ghost btn-sm" onClick={() => openLinksManager(doc)}>Links</button>
                        <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteDoc(doc.id)} style={{ color: 'var(--danger)' }}>Delete</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

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
                  <table className="table" style={{ marginTop: '1rem' }}>
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
                )}

                <div className="modal-actions" style={{ marginTop: '1rem' }}>
                  <button type="button" className="btn btn-ghost" onClick={closeLinksManager}>Close</button>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {tab === 'drift' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
              {(['open', 'all', 'resolved', 'dismissed'] as DriftFilter[]).map(filter => {
                const count = filter === 'all'
                  ? driftSignals.length
                  : driftSignals.filter(signal => signal.status === filter).length
                return (
                  <button
                    key={filter}
                    className={`btn ${driftFilter === filter ? 'btn-primary' : 'btn-ghost'}`}
                    onClick={() => setDriftFilter(filter)}
                  >
                    {filter} ({count})
                  </button>
                )
              })}
              <span style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginLeft: '0.5rem' }}>Sort:</span>
              {(['severity', 'created_at'] as DriftSort[]).map(s => (
                <button
                  key={s}
                  className={`btn btn-sm ${driftSort === s ? 'btn-primary' : 'btn-ghost'}`}
                  onClick={() => setDriftSort(s)}
                >
                  {s === 'severity' ? 'By Severity' : 'By Date'}
                </button>
              ))}
            </div>
            {driftSignals.some(s => s.status === 'open') && (
              <button className="btn btn-primary" onClick={handleBulkResolveDrift}>Resolve All Open</button>
            )}
          </div>
          {driftSignals.length === 0 ? (
            <div className="empty-state">
              <h3>No drift signals</h3>
              <p>No documentation drift has been detected.</p>
            </div>
          ) : filteredDriftSignals.length === 0 ? (
            <div className="empty-state">
              <h3>No signals in this filter</h3>
              <p>Try switching filters to review other drift states.</p>
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(280px, 0.9fr) minmax(320px, 1.1fr)', gap: '1rem' }}>
              <div className="card-list" style={{ marginBottom: 0 }}>
                {filteredDriftSignals.map(signal => (
                  <div
                    key={signal.id}
                    className="card"
                    style={{
                      cursor: 'pointer',
                      borderColor: selectedDriftSignal?.id === signal.id ? 'var(--primary)' : 'var(--border)',
                    }}
                    onClick={() => setSelectedDriftId(signal.id)}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}>
                      <h4>{signal.document_title || 'Unlinked document'}</h4>
                      <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
                        <span className={`badge ${severityBadgeClass(signal.severity ?? 1)}`}>
                          {severityLabel(signal.severity ?? 1)}
                        </span>
                        <span className={`badge ${signal.status === 'open' ? 'badge-stale' : 'badge-fresh'}`}>{signal.status}</span>
                      </div>
                    </div>
                    <p style={{ marginTop: '0.5rem', fontSize: '0.875rem', color: 'var(--text-muted)' }}>
                      {signal.trigger_type === 'code_change' && signal.trigger_meta?.changed_files?.length
                        ? `${signal.trigger_meta.changed_files.length} file${signal.trigger_meta.changed_files.length === 1 ? '' : 's'} changed`
                        : signal.trigger_detail}
                    </p>
                    <div style={{ marginTop: '0.75rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                      {new Date(signal.created_at).toLocaleString()}
                    </div>
                  </div>
                ))}
              </div>

              {selectedDriftSignal && (
                <div className="card" style={{ marginBottom: 0 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
                    <div>
                      <h3 style={{ marginBottom: '0.25rem' }}>{selectedDriftSignal.document_title || 'Unlinked document'}</h3>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                        {selectedDriftDocument?.file_path || 'No document file path registered'}
                      </div>
                    </div>
                    <span className={`badge ${selectedDriftSignal.status === 'open' ? 'badge-stale' : 'badge-fresh'}`}>{selectedDriftSignal.status}</span>
                  </div>

                  <div style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
                    <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
                      <div>
                        <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Severity</div>
                        <span className={`badge ${severityBadgeClass(selectedDriftSignal.severity ?? 1)}`}>
                          {severityLabel(selectedDriftSignal.severity ?? 1)}
                        </span>
                      </div>
                      <div>
                        <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Trigger Type</div>
                        <span className={`badge ${selectedDriftSignal.trigger_type === 'time_decay' ? 'badge-high' : selectedDriftSignal.trigger_type === 'manual' ? 'badge-medium' : 'badge-low'}`}>
                          {triggerTypeLabel(selectedDriftSignal.trigger_type)}
                        </span>
                      </div>
                      {selectedDriftSignal.trigger_meta?.confidence && (
                        <div>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Confidence</div>
                          <span className={`badge ${confidenceBadgeClass(selectedDriftSignal.trigger_meta.confidence)}`}>
                            {selectedDriftSignal.trigger_meta.confidence}
                          </span>
                        </div>
                      )}
                    </div>
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Trigger</div>
                      <div>
                        {selectedDriftSignal.trigger_type === 'time_decay' && selectedDriftSignal.trigger_meta?.days_stale
                          ? `Stale for over ${selectedDriftSignal.trigger_meta.days_stale} days`
                          : selectedDriftSignal.trigger_detail}
                      </div>
                    </div>
                    {selectedDriftChangedFiles.length > 0 && (
                      <div>
                        <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                          Impacted Files ({selectedDriftChangedFiles.length})
                        </div>
                        <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap', marginTop: '0.25rem' }}>
                          {selectedDriftChangedFiles.map(f => (
                            <span
                              key={f.path}
                              className={`badge ${f.change_type === 'D' || f.change_type === 'R' ? 'badge-stale' : 'badge-low'}`}
                              title={`Change type: ${f.change_type || 'unknown'}`}
                            >
                              {f.change_type && <strong style={{ marginRight: '0.25rem' }}>[{f.change_type}]</strong>}
                              {f.path}
                            </span>
                          ))}
                        </div>
                      </div>
                    )}
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Created</div>
                      <div>{new Date(selectedDriftSignal.created_at).toLocaleString()} ({formatRelativeTime(selectedDriftSignal.created_at)})</div>
                    </div>
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Coverage</div>
                      {selectedDriftDocument ? (
                        documentLinkLoadErrors[selectedDriftDocument.id] ? (
                          <div>Unable to load links right now.</div>
                        ) : selectedDriftLinks.length === 0 ? (
                          <div style={{ color: '#fca5a5' }}>No document links yet. Add coverage before the next sync for more precise drift detection.</div>
                        ) : (
                          <>
                            <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                              {selectedDriftLinks.length} linked path{selectedDriftLinks.length === 1 ? '' : 's'}
                              {Object.entries(selectedDriftCoverageBreakdown).length > 0 && (
                                <> • {Object.entries(selectedDriftCoverageBreakdown).map(([kind, count]) => `${kind}:${count}`).join(', ')}</>
                              )}
                            </div>
                            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.25rem' }}>
                              {selectedDriftLinks.map(link => (
                                <span key={link.id} className="badge badge-low">{link.code_path}</span>
                              ))}
                            </div>
                          </>
                        )
                      ) : (
                        <div>This signal is not linked to a registered document.</div>
                      )}
                    </div>
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Document Preview</div>
                      {!selectedDriftDocument ? (
                        <div>No linked document to preview.</div>
                      ) : selectedDriftPreviewLoading ? (
                        <div className="loading" style={{ padding: '0.4rem 0' }}>Loading preview...</div>
                      ) : selectedDriftPreviewError ? (
                        <div style={{ color: '#fca5a5' }}>{selectedDriftPreviewError}</div>
                      ) : selectedDriftPreview ? (
                        <div style={{ marginTop: '0.35rem' }}>
                          <pre style={{
                            maxHeight: '180px',
                            overflow: 'auto',
                            background: 'var(--bg)',
                            border: '1px solid var(--border)',
                            borderRadius: '0.4rem',
                            padding: '0.65rem',
                            fontSize: '0.78rem',
                            lineHeight: 1.45,
                            whiteSpace: 'pre-wrap',
                          }}>
                            {selectedDriftPreview.content}
                          </pre>
                          {selectedDriftPreview.truncated && (
                            <div style={{ color: 'var(--text-muted)', fontSize: '0.78rem', marginTop: '0.3rem' }}>
                              Preview truncated. Open full document for complete content.
                            </div>
                          )}
                        </div>
                      ) : (
                        <div>No preview available.</div>
                      )}
                    </div>
                  </div>

                  <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                    {selectedDriftDocument && (
                      <>
                        <button className="btn btn-ghost btn-sm" onClick={() => handleViewDoc(selectedDriftDocument)} disabled={!selectedDriftDocument.file_path}>
                          View Doc
                        </button>
                        <button className="btn btn-ghost btn-sm" onClick={() => openLinksManager(selectedDriftDocument)}>
                          Manage Links
                        </button>
                      </>
                    )}
                    {selectedDriftSignal.status === 'open' && (
                      <>
                        <button className="btn btn-primary btn-sm" onClick={() => handleResolveDrift(selectedDriftSignal.id)}>Mark Resolved</button>
                        <button className="btn btn-ghost btn-sm" onClick={() => handleDismissDrift(selectedDriftSignal.id)}>Dismiss</button>
                      </>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {tab === 'agents' && (
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
      )}
    </div>
  )
}

export default ProjectDetail
