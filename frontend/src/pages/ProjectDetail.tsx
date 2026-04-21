import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import type { Project, Task, Document, ProjectDashboardSummary, SyncRun, AgentRun, DriftSignal, DocumentContent, DocumentLink, ProjectRepoMapping, MirrorRepoCandidate, MirrorRepoDiscovery, Requirement, PlanningRun, BacklogCandidate, PlanningProviderOptions, PlanningExecutionMode, PlanningDispatchStatus } from '../types'
import {
  discoverMirrorRepos,
  getProject,
  getProjectDashboardSummary,
  getProjectSummary,
  updateProject,
  listRequirements,
  createRequirement,
  getPlanningProviderOptions,
  listPlanningRuns,
  createPlanningRun,
  cancelPlanningRun,
  listPlanningRunBacklogCandidates,
  updateBacklogCandidate,
  applyBacklogCandidate,
  listTasksFiltered,
  createTask,
  updateTask,
  deleteTask,
  batchUpdateTasks,
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
  updateProjectRepoMapping,
  deleteProjectRepoMapping,
} from '../api/client'
import { syncRunGuidance, type SyncGuidance } from '../utils/syncGuidance'
import { PlanningStepper } from '../components/PlanningStepper'

type Tab = 'overview' | 'planning' | 'tasks' | 'documents' | 'drift' | 'agents' | 'settings'
type DriftFilter = 'open' | 'all' | 'resolved' | 'dismissed'
type DriftSort = 'severity' | 'created_at'
type TaskFilterState = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string }
type BatchTaskFormState = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string; clearAssignee: boolean }

function ProjectDetail() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [dashboardSummary, setDashboardSummary] = useState<ProjectDashboardSummary | null>(null)
  const [requirements, setRequirements] = useState<Requirement[]>([])
  const [planningRuns, setPlanningRuns] = useState<PlanningRun[]>([])
  const [planningCandidates, setPlanningCandidates] = useState<BacklogCandidate[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [documents, setDocuments] = useState<Document[]>([])
  const [syncRuns, setSyncRuns] = useState<SyncRun[]>([])
  const [agentRuns, setAgentRuns] = useState<AgentRun[]>([])
  const [driftSignals, setDriftSignals] = useState<DriftSignal[]>([])
  const [tab, setTab] = useState<Tab>('overview')
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [planningLoadError, setPlanningLoadError] = useState<string | null>(null)
  const [planningRunsLoading, setPlanningRunsLoading] = useState(false)
  const [planningRunsError, setPlanningRunsError] = useState<string | null>(null)
  const [planningProviderOptions, setPlanningProviderOptions] = useState<PlanningProviderOptions | null>(null)
  const [planningProviderOptionsLoading, setPlanningProviderOptionsLoading] = useState(false)
  const [planningProviderOptionsError, setPlanningProviderOptionsError] = useState<string | null>(null)
  const [planningModelOverride, setPlanningModelOverride] = useState('')
  const [planningExecutionMode, setPlanningExecutionMode] = useState<PlanningExecutionMode>('server_provider')
  const [localAdapterType, setLocalAdapterType] = useState('backlog')
  const [localModelOverride, setLocalModelOverride] = useState('')
  const [planningCandidatesLoading, setPlanningCandidatesLoading] = useState(false)
  const [planningCandidatesError, setPlanningCandidatesError] = useState<string | null>(null)
  const [candidateReviewError, setCandidateReviewError] = useState<string | null>(null)
  const [candidateReviewMessage, setCandidateReviewMessage] = useState<string | null>(null)
  const [savingCandidate, setSavingCandidate] = useState(false)
  const [applyingCandidate, setApplyingCandidate] = useState(false)
  const [driftFilter, setDriftFilter] = useState<DriftFilter>('open')
  const [driftSort, setDriftSort] = useState<DriftSort>('severity')
  const [selectedDriftId, setSelectedDriftId] = useState<string | null>(null)

  const [selectedRequirementId, setSelectedRequirementId] = useState<string | null>(null)
  const [selectedPlanningRunId, setSelectedPlanningRunId] = useState<string | null>(null)
  const [selectedPlanningCandidateId, setSelectedPlanningCandidateId] = useState<string | null>(null)
  const [creatingRequirement, setCreatingRequirement] = useState(false)
  const [creatingPlanningRun, setCreatingPlanningRun] = useState(false)
  const [requirementForm, setRequirementForm] = useState({ title: '', summary: '', description: '', source: 'human' })
  const [candidateForm, setCandidateForm] = useState<{ title: string; description: string; status: BacklogCandidate['status'] }>({
    title: '',
    description: '',
    status: 'draft',
  })
  const [candidateFormSourceId, setCandidateFormSourceId] = useState<string | null>(null)
  const planningRunsRequestIdRef = useRef(0)
  const planningCandidatesRequestIdRef = useRef(0)

  const [showTaskForm, setShowTaskForm] = useState(false)
  const [taskForm, setTaskForm] = useState({ title: '', description: '', priority: 'medium' as Task['priority'], assignee: '', source: 'human' })
  const [editingTask, setEditingTask] = useState<Task | null>(null)
  const [editTaskForm, setEditTaskForm] = useState<{ title: string; description: string; status: Task['status']; priority: Task['priority']; assignee: string }>({ title: '', description: '', status: 'todo', priority: 'medium', assignee: '' })
  const [taskSort, setTaskSort] = useState<string>('created_at')
  const [taskOrder, setTaskOrder] = useState<string>('desc')
  const [taskFilters, setTaskFilters] = useState<TaskFilterState>({ status: '', priority: '', assignee: '' })
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const [batchTaskForm, setBatchTaskForm] = useState<BatchTaskFormState>({ status: '', priority: '', assignee: '', clearAssignee: false })

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
  const [projectBranchForm, setProjectBranchForm] = useState('')
  const [savingProjectBranch, setSavingProjectBranch] = useState(false)
  const [primaryRepoMappingBranchForm, setPrimaryRepoMappingBranchForm] = useState('')
  const [savingRepoMappingBranch, setSavingRepoMappingBranch] = useState(false)

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
      setPlanningProviderOptionsLoading(true)
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

      const [requirementsRes, syncRes, agentRes, driftRes, repoMappingsRes, providerOptionsRes] = await Promise.allSettled([
        listRequirements(id),
        listSyncRuns(id),
        listAgentRuns(id),
        listDriftSignals(id),
        listProjectRepoMappings(id),
        getPlanningProviderOptions(id),
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
      setPlanningProviderOptionsLoading(false)
      if (providerOptionsRes.status === 'fulfilled') {
        setPlanningProviderOptions(providerOptionsRes.value.data)
        setPlanningProviderOptionsError(null)
      } else {
        setPlanningProviderOptions(null)
        setPlanningProviderOptionsError(providerOptionsRes.reason instanceof Error ? providerOptionsRes.reason.message : 'Failed to load planning provider options')
      }
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
      setPlanningProviderOptionsLoading(false)
      setError(e instanceof Error ? e.message : 'Failed to load project')
    } finally {
      setPlanningProviderOptionsLoading(false)
      setLoading(false)
    }
  }, [id, loadRepoMirrorDiscovery, taskSort, taskOrder, taskFilters])

  useEffect(() => {
    loadData()
  }, [loadData])

  useEffect(() => {
    setProjectBranchForm(project?.default_branch ?? '')
  }, [project?.id, project?.default_branch])

  useEffect(() => {
    if (requirements.length === 0) {
      if (selectedRequirementId !== null) {
        setSelectedRequirementId(null)
      }
      return
    }

    if (!selectedRequirementId || !requirements.some(requirement => requirement.id === selectedRequirementId)) {
      setSelectedRequirementId(requirements[0].id)
    }
  }, [requirements, selectedRequirementId])

  const loadPlanningRuns = useCallback(async (requirementId: string) => {
	const requestID = planningRunsRequestIdRef.current + 1
	planningRunsRequestIdRef.current = requestID
    try {
      setPlanningRunsLoading(true)
      setPlanningRunsError(null)
      const response = await listPlanningRuns(requirementId, 1, 20)
      if (planningRunsRequestIdRef.current !== requestID) return
      setPlanningRuns(response.data)
    } catch (err) {
      if (planningRunsRequestIdRef.current !== requestID) return
      setPlanningRuns([])
      setPlanningRunsError(err instanceof Error ? err.message : 'Failed to load planning runs')
    } finally {
      if (planningRunsRequestIdRef.current === requestID) {
        setPlanningRunsLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    if (!selectedRequirementId) {
      planningRunsRequestIdRef.current += 1
      setPlanningRuns([])
      setSelectedPlanningRunId(null)
      setPlanningRunsError(null)
      setPlanningRunsLoading(false)
      return
    }

    setPlanningRuns([])
    setSelectedPlanningRunId(null)
    loadPlanningRuns(selectedRequirementId)
  }, [loadPlanningRuns, selectedRequirementId])

  useEffect(() => {
    if (planningRuns.length === 0) {
      if (selectedPlanningRunId !== null) {
        setSelectedPlanningRunId(null)
      }
      return
    }

    if (!selectedPlanningRunId || !planningRuns.some(run => run.id === selectedPlanningRunId)) {
      setSelectedPlanningRunId(planningRuns[0].id)
    }
  }, [planningRuns, selectedPlanningRunId])

  const loadPlanningCandidates = useCallback(async (planningRunId: string) => {
    const requestID = planningCandidatesRequestIdRef.current + 1
    planningCandidatesRequestIdRef.current = requestID
    try {
      setPlanningCandidatesLoading(true)
      setPlanningCandidatesError(null)
      const response = await listPlanningRunBacklogCandidates(planningRunId, 1, 50)
      if (planningCandidatesRequestIdRef.current !== requestID) return
      setPlanningCandidates(response.data)
    } catch (err) {
      if (planningCandidatesRequestIdRef.current !== requestID) return
      setPlanningCandidates([])
      setPlanningCandidatesError(err instanceof Error ? err.message : 'Failed to load planning candidates')
    } finally {
      if (planningCandidatesRequestIdRef.current === requestID) {
        setPlanningCandidatesLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    if (!selectedPlanningRunId) {
      planningCandidatesRequestIdRef.current += 1
      setPlanningCandidates([])
      setSelectedPlanningCandidateId(null)
      setPlanningCandidatesError(null)
      setPlanningCandidatesLoading(false)
      return
    }
    loadPlanningCandidates(selectedPlanningRunId)
  }, [loadPlanningCandidates, selectedPlanningRunId])

  useEffect(() => {
    if (planningCandidates.length === 0) {
      if (selectedPlanningCandidateId !== null) {
        setSelectedPlanningCandidateId(null)
      }
      return
    }

    if (!selectedPlanningCandidateId || !planningCandidates.some(candidate => candidate.id === selectedPlanningCandidateId)) {
      setSelectedPlanningCandidateId(planningCandidates[0].id)
    }
  }, [planningCandidates, selectedPlanningCandidateId])

  // Auto-refresh planning runs + candidates while a run is in-flight so that
  // local-connector results surface without a manual reload.
  useEffect(() => {
    if (!selectedRequirementId || !selectedPlanningRunId) return
    const activeRun = planningRuns.find(run => run.id === selectedPlanningRunId)
    if (!activeRun) return
    const dispatchActive = activeRun.dispatch_status === 'queued' || activeRun.dispatch_status === 'leased'
    const statusActive = activeRun.status === 'queued' || activeRun.status === 'running'
    if (!dispatchActive && !statusActive) return

    const interval = window.setInterval(() => {
      loadPlanningRuns(selectedRequirementId)
      loadPlanningCandidates(selectedPlanningRunId)
    }, 3000)
    return () => {
      window.clearInterval(interval)
    }
  }, [selectedRequirementId, selectedPlanningRunId, planningRuns, loadPlanningRuns, loadPlanningCandidates])

  // Track planning-run terminal transitions so we can:
  //   (1) tell App.tsx to refresh the unread notification badge immediately,
  //   (2) surface a one-shot success/failure banner on the run card.
  const planningRunStatusRef = useRef<Map<string, { status: PlanningRun['status']; dispatch: PlanningRun['dispatch_status'] }>>(new Map())
  const [planningRunFlash, setPlanningRunFlash] = useState<{ runId: string; kind: 'success' | 'error'; message: string } | null>(null)
  useEffect(() => {
    const previous = planningRunStatusRef.current
    const next = new Map<string, { status: PlanningRun['status']; dispatch: PlanningRun['dispatch_status'] }>()
    let flashed: { runId: string; kind: 'success' | 'error'; message: string } | null = null
    for (const run of planningRuns) {
      next.set(run.id, { status: run.status, dispatch: run.dispatch_status })
      const prev = previous.get(run.id)
      if (!prev) continue
      const wasActive = prev.status === 'queued' || prev.status === 'running' || prev.dispatch === 'queued' || prev.dispatch === 'leased'
      const isTerminal = run.status === 'completed' || run.status === 'failed' || run.status === 'cancelled'
      if (wasActive && isTerminal) {
        if (run.status === 'completed') {
          flashed = { runId: run.id, kind: 'success', message: 'Planning run completed.' }
        } else if (run.status === 'failed') {
          flashed = { runId: run.id, kind: 'error', message: run.error_message || run.dispatch_error || 'Planning run failed.' }
        }
        window.dispatchEvent(new CustomEvent('anpm:refresh-notifications'))
      }
    }
    planningRunStatusRef.current = next
    if (flashed) setPlanningRunFlash(flashed)
  }, [planningRuns])

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

  async function handleSavePrimaryRepoMappingBranch() {
    if (!primaryRepoMapping) return

    const normalizedBranch = primaryRepoMappingBranchForm.trim()
    try {
      setSavingRepoMappingBranch(true)
      setError(null)
      await saveRepoMappingBranch(primaryRepoMapping.id, normalizedBranch)
      setSuccessMessage(
        normalizedBranch === ''
          ? 'Primary repo mapping branch cleared. Sync will fall back to the project branch or auto-detect.'
          : `Primary repo mapping branch updated to ${normalizedBranch}.`,
      )
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update primary repo mapping branch')
    } finally {
      setSavingRepoMappingBranch(false)
    }
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

  async function handleCreateTask(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !taskForm.title.trim()) return
    try {
      setError(null)
      await createTask(id, taskForm)
      setTaskForm({ title: '', description: '', priority: 'medium', assignee: '', source: 'human' })
      setShowTaskForm(false)
      setSuccessMessage('Task created.')
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create task')
    }
  }

  async function handleCreateRequirement(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !requirementForm.title.trim()) return

    try {
      setCreatingRequirement(true)
      setError(null)
      setPlanningLoadError(null)
      const response = await createRequirement(id, {
        title: requirementForm.title.trim(),
        summary: requirementForm.summary.trim(),
        description: requirementForm.description.trim(),
        source: requirementForm.source.trim() || 'human',
      })
      setRequirements(prev => [response.data, ...prev])
      setSelectedRequirementId(response.data.id)
      setRequirementForm({ title: '', summary: '', description: '', source: 'human' })
      setSuccessMessage('Requirement captured. Planning workspace is ready for planning runs and candidate review.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create requirement')
    } finally {
      setCreatingRequirement(false)
    }
  }

  async function handleCreatePlanningRun() {
    if (!selectedRequirement || !planningRunReady) return

    try {
      setCreatingPlanningRun(true)
      setError(null)
      setPlanningRunsError(null)
      const requestedModelID = effectivePlanningSelection && planningExecutionModelID !== '' && planningExecutionModelID !== effectivePlanningSelection.model_id
        ? planningExecutionModelID
        : undefined
      const response = await createPlanningRun(selectedRequirement.id, {
        trigger_source: 'manual',
        ...(requestedModelID ? { model_id: requestedModelID } : {}),
        execution_mode: planningSelectedExecutionMode,
        ...(planningSelectedExecutionMode === 'local_connector' ? { adapter_type: localAdapterType } : {}),
        ...(planningSelectedExecutionMode === 'local_connector' && localModelOverride.trim() ? { model_override: localModelOverride.trim() } : {}),
      })
      setPlanningRuns(prev => [response.data, ...prev.filter(run => run.id !== response.data.id)])
      setSelectedPlanningRunId(response.data.id)
      setSuccessMessage(
        response.data.execution_mode === 'local_connector'
          ? 'Planning run queued for your paired local connector. Draft backlog candidates will appear after the connector returns results.'
          : 'Planning run recorded and draft backlog candidates are ready for review.',
      )
      await loadData()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to start planning run'
      setError(message)
      setPlanningRunsError(message)
    } finally {
      setCreatingPlanningRun(false)
    }
  }

  const [runningWhatsnext, setRunningWhatsnext] = useState(false)
  const [cancellingPlanningRunId, setCancellingPlanningRunId] = useState<string | null>(null)
  async function handleCancelPlanningRun(runId: string) {
    if (!runId) return
    if (!window.confirm('Cancel this planning run? Any in-flight local connector dispatch will be released.')) return
    try {
      setCancellingPlanningRunId(runId)
      setPlanningRunsError(null)
      const response = await cancelPlanningRun(runId)
      setPlanningRuns(prev => prev.map(run => run.id === response.data.id ? response.data : run))
      setSuccessMessage('Planning run cancelled.')
      if (selectedRequirement) await loadPlanningRuns(selectedRequirement.id)
    } catch (err) {
      setPlanningRunsError(err instanceof Error ? err.message : 'Failed to cancel planning run')
    } finally {
      setCancellingPlanningRunId(null)
    }
  }

  async function handleRunWhatsnext() {
    if (!id || !planningRunReady) return
    setRunningWhatsnext(true)
    setError(null)
    try {
      const WHATSNEXT_TITLE = "What's next"
      let req = requirements.find(r => r.title.toLowerCase().trim() === WHATSNEXT_TITLE.toLowerCase())
      if (!req) {
        const res = await createRequirement(id, {
          title: WHATSNEXT_TITLE,
          summary: 'Project-wide health check — surfaces the most urgent open work across tasks, drift signals, and stale docs.',
          source: 'analysis',
        })
        req = res.data
        setRequirements(prev => [res.data, ...prev])
      }
      setSelectedRequirementId(req.id)
      const runRes = await createPlanningRun(req.id, {
        trigger_source: 'manual',
        execution_mode: planningSelectedExecutionMode,
        adapter_type: 'whatsnext',
        ...(localModelOverride.trim() ? { model_override: localModelOverride.trim() } : {}),
      })
      setPlanningRuns(prev => [runRes.data, ...prev.filter(r => r.id !== runRes.data.id)])
      setSelectedPlanningRunId(runRes.data.id)
      setSuccessMessage("What's Next analysis queued. Your connector will surface the top priorities across the project.")
      await loadPlanningRuns(req.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start What's Next analysis")
    } finally {
      setRunningWhatsnext(false)
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
      setError(null)
      await updateTask(editingTask.id, editTaskForm)
      setEditingTask(null)
      setSuccessMessage('Task updated.')
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update task')
    }
  }

  async function handleDeleteEditingTask() {
    if (!editingTask || !confirm('Delete this task?')) return
    try {
      setError(null)
      await deleteTask(editingTask.id)
      setEditingTask(null)
      setSelectedTaskIds(prev => prev.filter(taskId => taskId !== editingTask.id))
      setSuccessMessage('Task deleted.')
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete task')
    }
  }

  function resetTaskFilters() {
    setTaskFilters({ status: '', priority: '', assignee: '' })
  }

  function toggleTaskSelection(taskId: string) {
    setSelectedTaskIds(prev => prev.includes(taskId) ? prev.filter(id => id !== taskId) : [...prev, taskId])
  }

  function toggleAllVisibleTasks() {
    if (tasks.length === 0) return
    setSelectedTaskIds(prev => prev.length === tasks.length ? [] : tasks.map(task => task.id))
  }

  async function handleApplyBatchUpdate() {
    if (!id || selectedTaskIds.length === 0) return

    const changes: Parameters<typeof batchUpdateTasks>[2] = {}
    if (batchTaskForm.status) changes.status = batchTaskForm.status
    if (batchTaskForm.priority) changes.priority = batchTaskForm.priority
    if (batchTaskForm.clearAssignee) {
      changes.assignee = ''
    } else if (batchTaskForm.assignee.trim()) {
      changes.assignee = batchTaskForm.assignee.trim()
    }

    if (Object.keys(changes).length === 0) {
      setError('Select at least one batch change before applying.')
      return
    }

    try {
      setError(null)
      const response = await batchUpdateTasks(id, selectedTaskIds, changes)
      setSelectedTaskIds([])
      setBatchTaskForm({ status: '', priority: '', assignee: '', clearAssignee: false })
      setSuccessMessage(`Updated ${response.data.updated_count} tasks.`)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update selected tasks')
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
      default_branch: repo.detected_default_branch || project?.default_branch || '',
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

  function requirementStatusBadgeClass(status: Requirement['status']) {
    if (status === 'planned') return 'badge-fresh'
    if (status === 'archived') return 'badge-low'
    return 'badge-todo'
  }

  function planningRunStatusBadgeClass(status: PlanningRun['status']) {
    if (status === 'completed') return 'badge-fresh'
    if (status === 'failed') return 'badge-stale'
    if (status === 'running') return 'badge-medium'
    if (status === 'cancelled') return 'badge-low'
    return 'badge-todo'
  }

  function backlogCandidateStatusBadgeClass(status: BacklogCandidate['status']) {
    if (status === 'approved') return 'badge-fresh'
    if (status === 'rejected') return 'badge-stale'
    if (status === 'applied') return 'badge-medium'
    return 'badge-todo'
  }

  function backlogCandidateSuggestionLabel(candidate: BacklogCandidate) {
    if (candidate.suggestion_type === 'integration') return 'Integration'
    if (candidate.suggestion_type === 'validation') return 'Validation'
    return 'Implementation'
  }

  function planningProviderLabel(providerId: string) {
    return planningProviderOptions?.providers.find(provider => provider.id === providerId)?.label ?? providerId
  }

  function planningModelLabel(providerId: string, modelId: string) {
    const provider = planningProviderOptions?.providers.find(item => item.id === providerId)
    return provider?.models.find(model => model.id === modelId)?.label ?? modelId
  }

  function planningSelectionSourceLabel(selectionSource: PlanningRun['selection_source']) {
    return selectionSource === 'request_override' ? 'Manual override' : 'Server default'
  }

  function planningBindingSourceLabel(bindingSource: PlanningRun['binding_source'] | PlanningProviderOptions['resolved_binding_source']) {
    if (bindingSource === 'personal') return 'Personal binding'
    if (bindingSource === 'shared') return 'Shared workspace credentials'
    return 'Built-in workspace fallback'
  }

  function credentialModeLabel(credentialMode: PlanningProviderOptions['credential_mode'] | undefined) {
    if (credentialMode === 'personal_required') return 'Personal required'
    if (credentialMode === 'personal_preferred') return 'Personal preferred'
    return 'Shared only'
  }

  function planningExecutionModeLabel(executionMode: PlanningExecutionMode) {
    if (executionMode === 'local_connector') return 'Run on this machine'
    if (executionMode === 'deterministic') return 'Server deterministic fallback'
    return 'Run on server provider'
  }

  function planningDispatchStatusLabel(dispatchStatus: PlanningDispatchStatus) {
    if (dispatchStatus === 'queued') return 'Queued for connector'
    if (dispatchStatus === 'leased') return 'Connector running'
    if (dispatchStatus === 'returned') return 'Connector returned result'
    if (dispatchStatus === 'expired') return 'Lease expired'
    return 'No connector dispatch'
  }

  function formatCandidateScore(value: number | undefined) {
    if (typeof value !== 'number' || Number.isNaN(value)) return '0.0'
    return value.toFixed(1)
  }

  function syncCandidateForm(candidate: BacklogCandidate | null) {
    if (!candidate) {
      setCandidateForm({ title: '', description: '', status: 'draft' })
      setCandidateFormSourceId(null)
      return
    }

    setCandidateForm({
      title: candidate.title,
      description: candidate.description,
      status: candidate.status,
    })
    setCandidateFormSourceId(candidate.id)
  }

  const summary = dashboardSummary?.summary ?? null
  const selectedRequirement = requirements.find(requirement => requirement.id === selectedRequirementId) ?? null
  const selectedPlanningRun = planningRuns.find(run => run.id === selectedPlanningRunId) ?? null
  const selectedPlanningCandidate = planningCandidates.find(candidate => candidate.id === selectedPlanningCandidateId) ?? null
  const effectivePlanningSelection = planningProviderOptions?.default_selection ?? null
  const effectivePlanningProvider = effectivePlanningSelection
    ? planningProviderOptions?.providers.find(provider => provider.id === effectivePlanningSelection.provider_id) ?? null
    : null
  const effectivePlanningModel = effectivePlanningSelection && effectivePlanningProvider
    ? effectivePlanningProvider.models.find(model => model.id === effectivePlanningSelection.model_id) ?? null
    : null
  const effectivePlanningProviderID = effectivePlanningSelection?.provider_id || ''
  const effectivePlanningDefaultModelID = effectivePlanningSelection?.model_id || ''
  const availableExecutionModes: PlanningExecutionMode[] = planningProviderOptions?.available_execution_modes?.length
    ? planningProviderOptions.available_execution_modes
    : [effectivePlanningProviderID === 'deterministic' ? 'deterministic' : 'server_provider']
  const defaultPlanningExecutionMode = availableExecutionModes[0] ?? 'server_provider'
  const planningSelectedExecutionMode = availableExecutionModes.includes(planningExecutionMode)
    ? planningExecutionMode
    : defaultPlanningExecutionMode
  const planningExecutionModelID = planningModelOverride.trim() || effectivePlanningSelection?.model_id || ''
  const planningExecutionModel = effectivePlanningProvider?.models.find(model => model.id === planningExecutionModelID) ?? effectivePlanningModel ?? null
  const planningCanRun = planningProviderOptions?.can_run ?? true
  const planningUsesLocalConnector = planningSelectedExecutionMode === 'local_connector'
  const planningRunReady = planningUsesLocalConnector
    ? Boolean(planningProviderOptions?.paired_connector_available)
    : planningCanRun
  const planningRunBlockedReason = planningUsesLocalConnector
    ? (planningProviderOptions?.paired_connector_available ? null : 'Pair a local connector before starting a local planning run.')
    : (planningCanRun ? null : planningProviderOptions?.unavailable_reason || 'This user cannot run planning with the current workspace policy.')
  const selectedPlanningCandidateApplied = selectedPlanningCandidate?.status === 'applied'
  const candidateFormDirty = Boolean(
    selectedPlanningCandidate && (
      candidateForm.title !== selectedPlanningCandidate.title ||
      candidateForm.description !== selectedPlanningCandidate.description ||
      candidateForm.status !== selectedPlanningCandidate.status
    ),
  )
  const canApplySelectedCandidate = Boolean(
    selectedPlanningCandidate &&
    selectedPlanningCandidate.status === 'approved' &&
    !candidateFormDirty &&
    !savingCandidate &&
    !applyingCandidate,
  )
  const candidateReviewDuplicates = selectedPlanningCandidate
    ? tasks.filter(task => task.title.trim().toLowerCase() === candidateForm.title.trim().toLowerCase()).slice(0, 3)
    : []
  const candidateDuplicateTitles = Array.from(new Set([
    ...(selectedPlanningCandidate?.duplicate_titles ?? []),
    ...candidateReviewDuplicates.map(task => task.title),
  ]))
  const draftRequirementCount = requirements.filter(requirement => requirement.status === 'draft').length
  const plannedRequirementCount = requirements.filter(requirement => requirement.status === 'planned').length
  const archivedRequirementCount = requirements.filter(requirement => requirement.status === 'archived').length
  const latestSyncRun = dashboardSummary?.latest_sync_run ?? syncRuns[0] ?? null
  const recentSyncRuns = syncRuns.slice(0, 3)
  const openDriftCount = dashboardSummary && dashboardSummary.open_drift_count >= 0
    ? dashboardSummary.open_drift_count
    : driftSignals.filter(signal => signal.status === 'open').length
  const recentDashboardAgentRuns = dashboardSummary?.recent_agent_runs?.length ? dashboardSummary.recent_agent_runs : agentRuns.slice(0, 5)
  const hasRepoSource = Boolean(project?.repo_path || project?.repo_url || repoMappings.length > 0)
  const latestSyncGuidance: SyncGuidance | null = latestSyncRun ? syncRunGuidance(latestSyncRun, openDriftCount) : null
  const allVisibleTasksSelected = tasks.length > 0 && selectedTaskIds.length === tasks.length
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

  useEffect(() => {
    if (effectivePlanningProviderID === '' || effectivePlanningDefaultModelID === '') {
      setPlanningModelOverride('')
      return
    }
    setPlanningModelOverride(effectivePlanningDefaultModelID)
  }, [effectivePlanningDefaultModelID, effectivePlanningProviderID])

  useEffect(() => {
    setPlanningExecutionMode(defaultPlanningExecutionMode)
  }, [defaultPlanningExecutionMode])
  const detectedProjectBranch = detectedSyncBranch
  const detectedPrimaryRepoMappingBranch = primaryRepoMapping ? detectedSyncBranch : ''
  const branchFormChanged = project !== null && projectBranchForm.trim() !== (project.default_branch || '').trim()
  const primaryRepoMappingBranchChanged = primaryRepoMapping !== null && primaryRepoMappingBranchForm.trim() !== (primaryRepoMapping.default_branch || '').trim()
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
  const canApplyDetectedBranchQuickFix = Boolean(
    branchResolutionError &&
    detectedSyncBranch,
  )
  const quickFixBranchTarget = primaryRepoMapping && (primaryRepoMapping.default_branch || '').trim() !== ''
    ? { type: 'repo-mapping' as const, mapping: primaryRepoMapping }
    : { type: 'project' as const }
  const quickFixAlreadyApplied = quickFixBranchTarget.type === 'repo-mapping'
    ? (quickFixBranchTarget.mapping.default_branch || '').trim() === detectedSyncBranch
    : (project?.default_branch || '').trim() === detectedSyncBranch
  const canApplyDetectedBranchAndRerun = canApplyDetectedBranchQuickFix && !quickFixAlreadyApplied
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
    if (!selectedPlanningCandidate) {
      syncCandidateForm(null)
      setCandidateReviewError(null)
      setCandidateReviewMessage(null)
      return
    }

    const changedCandidate = candidateFormSourceId !== selectedPlanningCandidate.id
    if (changedCandidate || !candidateFormDirty) {
      syncCandidateForm(selectedPlanningCandidate)
      if (changedCandidate) {
        setCandidateReviewError(null)
        setCandidateReviewMessage(null)
      }
    }
  }, [selectedPlanningCandidate, candidateFormSourceId, candidateFormDirty])

  useEffect(() => {
    setPrimaryRepoMappingBranchForm(primaryRepoMapping?.default_branch ?? '')
  }, [primaryRepoMapping])

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

  useEffect(() => {
    setSelectedTaskIds(prev => prev.filter(taskId => tasks.some(task => task.id === taskId)))
  }, [tasks])

  if (loading) return <div className="loading">Loading project...</div>
  if (!project) return <div className="error-message">Project not found</div>

  function confirmDiscardCandidateEdits() {
    if (!candidateFormDirty) return true
    return confirm('Discard unsaved candidate review edits?')
  }

  function handleSelectRequirement(requirementId: string) {
    if (requirementId === selectedRequirementId) return
    if (!confirmDiscardCandidateEdits()) return
    setSelectedRequirementId(requirementId)
  }

  function handleSelectPlanningRun(runId: string) {
    if (runId === selectedPlanningRunId) return
    if (!confirmDiscardCandidateEdits()) return
    setSelectedPlanningRunId(runId)
  }

  function handleSelectPlanningCandidate(candidateId: string) {
    if (candidateId === selectedPlanningCandidateId) return
    if (!confirmDiscardCandidateEdits()) return
    setSelectedPlanningCandidateId(candidateId)
  }

  function resetCandidateForm() {
    syncCandidateForm(selectedPlanningCandidate)
    setCandidateReviewError(null)
    setCandidateReviewMessage(null)
  }

  async function persistCandidateReview(nextStatus?: 'draft' | 'approved' | 'rejected') {
    if (!selectedPlanningCandidate) return

    const payload: { title?: string; description?: string; status?: 'draft' | 'approved' | 'rejected' } = {}
    const trimmedTitle = candidateForm.title.trim()

    if (trimmedTitle !== selectedPlanningCandidate.title) payload.title = trimmedTitle
    if (candidateForm.description !== selectedPlanningCandidate.description) payload.description = candidateForm.description

    const targetStatus = nextStatus ?? candidateForm.status
    if (targetStatus !== selectedPlanningCandidate.status && targetStatus !== 'applied') payload.status = targetStatus

    if (Object.keys(payload).length === 0) {
      setCandidateReviewError('Update at least one candidate field before saving.')
      setCandidateReviewMessage(null)
      return
    }

    try {
      setSavingCandidate(true)
      setCandidateReviewError(null)
      const response = await updateBacklogCandidate(selectedPlanningCandidate.id, payload)
      setPlanningCandidates(prev => prev.map(candidate => candidate.id === response.data.id ? response.data : candidate))
      syncCandidateForm(response.data)
      setCandidateReviewMessage(
        nextStatus === 'approved'
          ? 'Candidate approved.'
          : nextStatus === 'rejected'
            ? 'Candidate rejected.'
            : nextStatus === 'draft'
              ? 'Candidate returned to draft.'
              : 'Candidate review saved.',
      )
    } catch (err) {
      setCandidateReviewMessage(null)
      setCandidateReviewError(err instanceof Error ? err.message : 'Failed to update backlog candidate')
    } finally {
      setSavingCandidate(false)
    }
  }

  async function handleApplyPlanningCandidate() {
    if (!selectedPlanningCandidate) return
    if (candidateFormDirty) {
      setCandidateReviewError('Save or reset candidate edits before applying it to tasks.')
      setCandidateReviewMessage(null)
      return
    }

    try {
      setApplyingCandidate(true)
      setCandidateReviewError(null)
      setCandidateReviewMessage(null)
      const response = await applyBacklogCandidate(selectedPlanningCandidate.id)
      setPlanningCandidates(prev => prev.map(candidate => candidate.id === response.data.candidate.id ? response.data.candidate : candidate))
      syncCandidateForm(response.data.candidate)
      await Promise.all([
        loadData(),
        selectedPlanningRunId ? loadPlanningCandidates(selectedPlanningRunId) : Promise.resolve(),
      ])
      setCandidateReviewMessage(
        response.data.already_applied
          ? 'Candidate was already applied. Existing task lineage was reused.'
          : `Candidate applied to tasks. Created ${response.data.task.title}.`,
      )
    } catch (err) {
      setCandidateReviewMessage(null)
      setCandidateReviewError(err instanceof Error ? err.message : 'Failed to apply backlog candidate')
    } finally {
      setApplyingCandidate(false)
    }
  }

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
                {canApplyDetectedBranchAndRerun && (
                  <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                    <button className="btn btn-primary btn-sm" onClick={handleApplyDetectedBranchAndRerunSync} disabled={savingProjectBranch || syncing}>
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
                  onChange={e => setProjectBranchForm(e.target.value)}
                  placeholder="leave blank to auto-detect"
                  style={{ padding: '0.5rem 0.75rem', minWidth: '220px', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
                />
                <button className="btn btn-primary btn-sm" onClick={handleSaveProjectBranch} disabled={!branchFormChanged || savingProjectBranch}>
                  {savingProjectBranch ? 'Saving…' : 'Save Branch'}
                </button>
                <button className="btn btn-ghost btn-sm" onClick={() => setProjectBranchForm('')} disabled={savingProjectBranch || projectBranchForm === ''}>
                  Clear to Auto-Detect
                </button>
                {detectedProjectBranch && detectedProjectBranch !== projectBranchForm.trim() && (
                  <button className="btn btn-ghost btn-sm" onClick={() => setProjectBranchForm(detectedProjectBranch)} disabled={savingProjectBranch}>
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

      {tab === 'settings' && (
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

        {primaryRepoMapping && (
          <div style={{ marginTop: '1rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', background: 'var(--bg)' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
              <div>
                <strong>Primary Repo Mapping Branch</strong>
                <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem', marginTop: '0.25rem' }}>
                  This override wins over the project default branch for sync. Leave blank to inherit the project fallback branch and auto-detect path.
                </div>
              </div>
              <span className="badge badge-low">Current {primaryRepoMapping.default_branch || 'inherit project fallback'}</span>
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.75rem', alignItems: 'center' }}>
              <input
                value={primaryRepoMappingBranchForm}
                onChange={e => setPrimaryRepoMappingBranchForm(e.target.value)}
                placeholder="leave blank to inherit project branch"
                style={{ padding: '0.5rem 0.75rem', minWidth: '240px', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
              />
              <button className="btn btn-primary btn-sm" onClick={handleSavePrimaryRepoMappingBranch} disabled={!primaryRepoMappingBranchChanged || savingRepoMappingBranch}>
                {savingRepoMappingBranch ? 'Saving…' : 'Save Mapping Branch'}
              </button>
              <button className="btn btn-ghost btn-sm" onClick={() => setPrimaryRepoMappingBranchForm('')} disabled={savingRepoMappingBranch || primaryRepoMappingBranchForm === ''}>
                Clear to Fallback
              </button>
              {detectedPrimaryRepoMappingBranch && detectedPrimaryRepoMappingBranch !== primaryRepoMappingBranchForm.trim() && (
                <button className="btn btn-ghost btn-sm" onClick={() => setPrimaryRepoMappingBranchForm(detectedPrimaryRepoMappingBranch)} disabled={savingRepoMappingBranch}>
                  Use Detected {detectedPrimaryRepoMappingBranch}
                </button>
              )}
            </div>
            <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginTop: '0.45rem' }}>
              Precedence: primary repo mapping branch → project default branch → auto-detect.
            </div>
          </div>
        )}

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
                    <span className="badge badge-low">{mapping.default_branch || `inherits ${project?.default_branch || 'auto-detect'}`}</span>
                  </div>
                  <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>{mapping.repo_path}</div>
                </div>
                <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteRepoMapping(mapping.id)}>Delete</button>
              </div>
            ))}
          </div>
        )}
      </div>
      )}

      <div className="project-rail-layout">
        <nav className="project-rail" aria-label="Project sections">
          <button className={tab === 'overview' ? 'is-active' : ''} onClick={() => setTab('overview')}>
            <span>Overview</span>
          </button>
          <button className={tab === 'planning' ? 'is-active' : ''} onClick={() => setTab('planning')}>
            <span>Planning</span>
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
              <button className="btn btn-ghost btn-sm" onClick={() => setTab('planning')}>Open Planning →</button>
            </div>
            <div>
              <h4 style={{ marginBottom: '0.5rem' }}>Documentation drift</h4>
              <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
                {openDriftCount === 0
                  ? 'No open drift signals. Documentation looks aligned with code.'
                  : `${openDriftCount} open drift signal${openDriftCount === 1 ? '' : 's'} need triage.`}
              </p>
              <button className="btn btn-ghost btn-sm" onClick={() => setTab('drift')} disabled={driftSignals.length === 0}>Open Drift →</button>
            </div>
            <div>
              <h4 style={{ marginBottom: '0.5rem' }}>Tasks</h4>
              <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
                {summary ? `${summary.tasks_in_progress} in progress · ${summary.tasks_done} done · ${summary.total_tasks} total.` : 'Run sync to populate task counts.'}
              </p>
              <button className="btn btn-ghost btn-sm" onClick={() => setTab('tasks')}>Open Tasks →</button>
            </div>
            <div>
              <h4 style={{ marginBottom: '0.5rem' }}>Agent activity</h4>
              <p style={{ marginTop: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
                {agentRuns.length === 0
                  ? 'No agent runs recorded yet.'
                  : `${agentRuns.length} agent run${agentRuns.length === 1 ? '' : 's'} on record.`}
              </p>
              <button className="btn btn-ghost btn-sm" onClick={() => setTab('agents')} disabled={agentRuns.length === 0}>Open Activity →</button>
            </div>
          </div>
        </div>
      )}

      {tab === 'planning' && (
        <div className="planning-shell">
          <PlanningStepper
            requirementCount={requirements.length}
            selectedRequirement={selectedRequirement}
            selectedPlanningRun={selectedPlanningRun}
            candidateCount={planningCandidates.length}
            onJumpToIntake={() => {
              const el = document.querySelector('.planning-foundation-grid input') as HTMLInputElement | null
              if (el) el.focus()
            }}
            onJumpToWorkspace={() => {
              const el = document.querySelector('.planning-workspace-card')
              if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
            }}
            onJumpToCandidates={() => {
              const el = document.querySelector('.planning-candidate-panel')
              if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
            }}
          />
          <div className="planning-foundation-grid">
            <div className="card">
              <div className="planning-stage-header">
                <div>
                  <h3 style={{ marginBottom: '0.25rem' }}>Requirement Intake</h3>
                  <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                    Capture product or implementation intent here first. This keeps Phase 2 draft-first and avoids creating tasks too early.
                  </p>
                </div>
                <span className="badge badge-todo">P2-03</span>
              </div>

              <form onSubmit={handleCreateRequirement} style={{ marginTop: '1rem' }}>
                <div className="form-group">
                  <label>Title *</label>
                  <input value={requirementForm.title} onChange={e => setRequirementForm(prev => ({ ...prev, title: e.target.value }))} placeholder="Improve sync failure recovery UX" />
                </div>
                <div className="form-group">
                  <label>Summary</label>
                  <input value={requirementForm.summary} onChange={e => setRequirementForm(prev => ({ ...prev, summary: e.target.value }))} placeholder="One-line planning summary" />
                </div>
                <div className="form-group">
                  <label>Description</label>
                  <textarea value={requirementForm.description} onChange={e => setRequirementForm(prev => ({ ...prev, description: e.target.value }))} placeholder="Describe what the system should do before tasks are created." rows={5} />
                </div>
                <div className="form-group">
                  <label>Source</label>
                  <input value={requirementForm.source} onChange={e => setRequirementForm(prev => ({ ...prev, source: e.target.value }))} placeholder="human or agent:name" />
                </div>
                <div className="modal-actions">
                  <button type="button" className="btn btn-ghost" onClick={() => setRequirementForm({ title: '', summary: '', description: '', source: 'human' })}>
                    Reset
                  </button>
                  <button type="submit" className="btn btn-primary" disabled={creatingRequirement || !requirementForm.title.trim()}>
                    {creatingRequirement ? 'Capturing…' : 'Capture Requirement'}
                  </button>
                </div>
              </form>
            </div>

            <div className="card">
              <div className="planning-stage-header">
                <div>
                  <h3 style={{ marginBottom: '0.25rem' }}>Requirement Queue</h3>
                  <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                    Draft requirements stay here until they move through planning runs, candidate review, and apply-to-task flow.
                  </p>
                </div>
                <div className="planning-stats">
                  <span className="badge badge-todo">{draftRequirementCount} draft</span>
                  <span className="badge badge-fresh">{plannedRequirementCount} planned</span>
                  <span className="badge badge-low">{archivedRequirementCount} archived</span>
                </div>
              </div>

              {planningLoadError && <div className="error-banner" style={{ marginTop: '1rem' }}>{planningLoadError}</div>}

              {requirements.length === 0 ? (
                <div className="empty-state">
                  <h3>No requirements yet</h3>
                  <p>Use the intake form to capture the first planning requirement for this project.</p>
                </div>
              ) : (
                <div className="requirement-list">
                  {requirements.map(requirement => (
                    <button
                      key={requirement.id}
                      type="button"
                      className={`requirement-card ${selectedRequirementId === requirement.id ? 'is-active' : ''}`}
                      onClick={() => handleSelectRequirement(requirement.id)}
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
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="card planning-workspace-card">
            <div className="planning-stage-header">
              <div>
                <h3 style={{ marginBottom: '0.25rem' }}>Planning Workspace</h3>
                <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                  Start a tracked planning run from the selected requirement, then review and apply its persisted draft backlog candidates without leaving this page.
                </p>
              </div>
              <span className="badge badge-fresh">P2-07</span>
            </div>

            {selectedRequirement ? (
              <div className="planning-workspace-content">
                <div className="requirement-detail-panel">
                  <div className="requirement-card-top">
                    <strong>{selectedRequirement.title}</strong>
                    <span className={`badge ${requirementStatusBadgeClass(selectedRequirement.status)}`}>{selectedRequirement.status}</span>
                  </div>
                  {selectedRequirement.summary && <p style={{ marginTop: '0.6rem', color: 'var(--text-muted)' }}>{selectedRequirement.summary}</p>}
                  {selectedRequirement.description && (
                    <div className="planning-placeholder-note" style={{ marginTop: '0.75rem' }}>
                      {selectedRequirement.description}
                    </div>
                  )}

                  <div className="requirement-detail-grid">
                    <div className="requirement-detail-block">
                      <span>Source</span>
                      <strong>{selectedRequirement.source}</strong>
                    </div>
                    <div className="requirement-detail-block">
                      <span>Created</span>
                      <strong>{formatRelativeTime(selectedRequirement.created_at)}</strong>
                    </div>
                    <div className="requirement-detail-block">
                      <span>Updated</span>
                      <strong>{formatRelativeTime(selectedRequirement.updated_at)}</strong>
                    </div>
                  </div>

                  <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                    <strong>Decomposition Settings</strong>
                    <div className="planning-run-meta" style={{ display: 'grid', gap: '0.75rem', marginTop: '0.75rem' }}>
                      {planningProviderOptionsLoading && <span>Loading provider options…</span>}
                      {planningProviderOptionsError && <span style={{ color: 'var(--danger)' }}>{planningProviderOptionsError}</span>}

                      {/* ── Execution mode selector (always shown when multiple modes available) ── */}
                      {planningProviderOptions && planningProviderOptions.available_execution_modes.length > 1 && (
                        <label style={{ display: 'grid', gap: '0.35rem' }}>
                          <span>Execution source for this run</span>
                          <select value={planningSelectedExecutionMode} onChange={e => setPlanningExecutionMode(e.target.value as PlanningExecutionMode)} disabled={creatingPlanningRun || planningProviderOptionsLoading}>
                            {planningProviderOptions.available_execution_modes.map(mode => (
                              <option key={mode} value={mode}>{planningExecutionModeLabel(mode)}</option>
                            ))}
                          </select>
                        </label>
                      )}

                      {/* ── Local connector path ── */}
                      {planningUsesLocalConnector ? (
                        <>
                          {planningProviderOptions?.paired_connector_available ? (
                            <div className="connector-run-info connector-run-info-ready">
                              <div className="connector-run-row">
                                <span className="connector-badge connector-badge-ready">● Online</span>
                                <strong>{planningProviderOptions.active_connector_label ?? 'My Machine'}</strong>
                              </div>
                              <div className="connector-run-desc">
                                The run will be queued and your local connector will pick it up automatically.
                                The connector calls your local AI CLI (<code>claude</code> or <code>codex</code>).
                                Configure your connector with <code>dispatcher_adapter.py</code> to switch between adapters without restarting.
                              </div>

                              {/* What's Next quick-action */}
                              <div style={{ marginTop: '0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.65rem 0.9rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                                <div>
                                  <strong style={{ fontSize: '0.88rem' }}>Project Health Check</strong>
                                  <p style={{ margin: '0.15rem 0 0', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                                    No specific requirement? Run a full project analysis — surfaces urgent tasks, drift signals, and stale docs.
                                  </p>
                                </div>
                                <button
                                  type="button"
                                  className="btn btn-secondary btn-small"
                                  onClick={handleRunWhatsnext}
                                  disabled={runningWhatsnext || creatingPlanningRun || !planningRunReady}
                                  style={{ whiteSpace: 'nowrap' }}
                                >
                                  {runningWhatsnext ? 'Starting…' : "Run What's Next"}
                                </button>
                              </div>

                              <div style={{ marginTop: '0.75rem', display: 'grid', gap: '0.5rem' }}>
                                <label style={{ display: 'grid', gap: '0.3rem' }}>
                                  <span style={{ fontSize: '0.88rem' }}>Adapter for requirement-based run</span>
                                  <div style={{ display: 'flex', gap: '1rem' }}>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.88rem' }}>
                                      <input type="radio" name="localAdapterType" value="backlog" checked={localAdapterType === 'backlog'} onChange={() => setLocalAdapterType('backlog')} disabled={creatingPlanningRun} />
                                      Backlog Planner
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.88rem' }}>
                                      <input type="radio" name="localAdapterType" value="whatsnext" checked={localAdapterType === 'whatsnext'} onChange={() => setLocalAdapterType('whatsnext')} disabled={creatingPlanningRun} />
                                      What's Next (scoped)
                                    </label>
                                  </div>
                                  <small style={{ color: 'var(--text-muted)' }}>
                                    {localAdapterType === 'whatsnext' ? 'Analyzes project state scoped to the selected requirement.' : 'Decomposes the selected requirement into ranked backlog candidates.'}
                                  </small>
                                </label>
                                <label style={{ display: 'grid', gap: '0.3rem' }}>
                                  <span style={{ fontSize: '0.88rem' }}>Model override <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span></span>
                                  <input
                                    type="text"
                                    placeholder="leave blank to use connector default"
                                    value={localModelOverride}
                                    onChange={e => setLocalModelOverride(e.target.value)}
                                    disabled={creatingPlanningRun}
                                    style={{ padding: '0.4rem 0.6rem', fontSize: '0.88rem', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
                                  />
                                  <small style={{ color: 'var(--text-muted)' }}>e.g. <code>claude-sonnet-4-6</code> (Claude default), <code>claude-opus-4-7</code> (higher quality, more tokens), <code>gpt-5.4</code> or <code>gpt-5.3-codex</code> (Codex). Overrides ANPM_ADAPTER_MODEL for this run only.</small>
                                </label>
                              </div>
                            </div>
                          ) : (
                            <div className="connector-run-info connector-run-info-offline">
                              <div className="connector-run-row">
                                <span className="connector-badge connector-badge-offline">○ No live connector</span>
                              </div>
                              <div className="connector-run-desc">
                                No connector is currently online. Start the connector binary on this machine first, then return here to queue a run.
                                <br />
                                <Link to="/connector" style={{ fontSize: '0.88rem' }}>Go to My Connector →</Link>
                              </div>
                            </div>
                          )}
                        </>
                      ) : (
                        /* ── Server-side provider path ── */
                        <>
                          {effectivePlanningProvider && <span>Provider: {effectivePlanningProvider.label}</span>}
                          {planningProviderOptions && <span>Credential mode: {credentialModeLabel(planningProviderOptions.credential_mode)}</span>}
                          {planningProviderOptions?.resolved_binding_source && (
                            <span>
                              Resolved from: {planningBindingSourceLabel(planningProviderOptions.resolved_binding_source)}
                              {planningProviderOptions.resolved_binding_label ? ` (${planningProviderOptions.resolved_binding_label})` : ''}
                            </span>
                          )}
                          {planningExecutionModel && <span>Execution model: {planningExecutionModel.label}</span>}
                          {effectivePlanningProvider && <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>{effectivePlanningProvider.description}</span>}
                          {planningProviderOptions?.allow_model_override && effectivePlanningProvider && (
                            <label style={{ display: 'grid', gap: '0.35rem' }}>
                              <span>Model override for this run</span>
                              <select value={planningModelOverride} onChange={e => setPlanningModelOverride(e.target.value)} disabled={creatingPlanningRun || !planningRunReady}>
                                {effectivePlanningProvider.models.filter(model => model.enabled).map(model => (
                                  <option key={model.id} value={model.id}>{model.label}</option>
                                ))}
                              </select>
                              <small>Changing this only overrides the model for this one run.</small>
                            </label>
                          )}
                          {planningRunBlockedReason && (
                            <span style={{ color: 'var(--danger)' }}>
                              {planningRunBlockedReason}
                              {planningRunBlockedReason.toLowerCase().includes('connector') && (
                                <> — <Link to="/connector">Set up connector</Link></>
                              )}
                            </span>
                          )}
                          <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                            Workspace policy is managed in <Link to="/settings/models">Model Settings</Link>. Personal credentials are managed in <Link to="/account-bindings">My Bindings</Link>.
                          </span>
                        </>
                      )}
                    </div>
                  </div>

                  <div className="planning-run-actions">
                    <button className="btn btn-primary" onClick={handleCreatePlanningRun} disabled={creatingPlanningRun || !planningRunReady || planningProviderOptionsLoading}>
                      {creatingPlanningRun ? 'Starting…' : 'Start Planning Run'}
                    </button>
                    <button className="btn btn-ghost" onClick={() => loadPlanningRuns(selectedRequirement.id)} disabled={planningRunsLoading || creatingPlanningRun}>
                      {planningRunsLoading ? 'Refreshing…' : 'Refresh Runs'}
                    </button>
                  </div>

                  <div className="planning-placeholder-note">
                    Requirement review, candidate review, and candidate-to-task apply are now all available in-place. Use this workspace to move from draft planning to a traceable task set.
                  </div>

                  {planningRunsError && <div className="error-banner" style={{ marginTop: '1rem' }}>{planningRunsError}</div>}

                  {planningRunsLoading ? (
                    <div className="loading" style={{ padding: '1rem 0 0.5rem' }}>Loading planning runs…</div>
                  ) : planningRuns.length === 0 ? (
                    <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
                      <h3>No planning runs yet</h3>
                      <p>Start the first run for this requirement to establish orchestration history.</p>
                    </div>
                  ) : (
                    <div className="planning-run-list">
                      {planningRuns.map(run => {
                        const isActiveRun = run.status === 'queued' || run.status === 'running'
                        const queuedFor = run.status === 'queued' ? formatRelativeTime(run.created_at) : null
                        const isLocalConnectorWaiting = isActiveRun && run.execution_mode === 'local_connector' && (run.dispatch_status === 'queued' || run.dispatch_status === 'leased')
                        return (
                          <div key={run.id} className="planning-run-card-wrapper">
                            <button
                              type="button"
                              className={`planning-run-card ${selectedPlanningRunId === run.id ? 'is-active' : ''}`}
                              onClick={() => handleSelectPlanningRun(run.id)}
                            >
                              <div className="requirement-card-top">
                                <strong>{run.trigger_source || 'manual'}</strong>
                                <span className={`badge ${planningRunStatusBadgeClass(run.status)}`}>{run.status}</span>
                              </div>
                              <div className="planning-run-meta">
                                <span>Created {formatRelativeTime(run.created_at)}</span>
                                <span>Started {formatDateTime(run.started_at)}</span>
                                <span>Completed {formatDateTime(run.completed_at)}</span>
                                <span>{planningExecutionModeLabel(run.execution_mode)}</span>
                                <span>{planningDispatchStatusLabel(run.dispatch_status)}</span>
                                {run.connector_label && <span>Connector {run.connector_label}</span>}
                                {run.connector_cli_info && (
                                  <span title={`model_source: ${run.connector_cli_info.model_source ?? '—'}`}>
                                    CLI: {run.connector_cli_info.agent}{run.connector_cli_info.model ? ` / ${run.connector_cli_info.model}` : ''}
                                  </span>
                                )}
                                <span>{planningProviderLabel(run.provider_id)} / {planningModelLabel(run.provider_id, run.model_id)}</span>
                                <span>{planningBindingSourceLabel(run.binding_source)}{run.binding_label ? ` (${run.binding_label})` : ''}</span>
                                <span>{planningSelectionSourceLabel(run.selection_source)}</span>
                              </div>
                              {run.dispatch_error && <div className="error-banner" style={{ marginTop: '0.75rem', marginBottom: 0 }}>{run.dispatch_error}</div>}
                              {run.error_message && <div className="error-banner" style={{ marginTop: '0.75rem', marginBottom: 0 }}>{run.error_message}</div>}
                            </button>
                            {isActiveRun && (
                              <div className="planning-run-actions-row">
                                {isLocalConnectorWaiting && (
                                  <span className="planning-run-hint">
                                    Waiting for local connector{run.connector_label ? ` "${run.connector_label}"` : ''} to claim this run.
                                    {queuedFor && ` Queued ${queuedFor}.`}
                                  </span>
                                )}
                                <button
                                  type="button"
                                  className="btn btn-ghost btn-small"
                                  onClick={() => handleCancelPlanningRun(run.id)}
                                  disabled={cancellingPlanningRunId === run.id}
                                >
                                  {cancellingPlanningRunId === run.id ? 'Cancelling…' : 'Cancel run'}
                                </button>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}

                  <div className="planning-candidate-panel">
                    <div className="planning-stage-header">
                      <div>
                        <h3 style={{ marginBottom: '0.25rem' }}>Suggested Backlog</h3>
                        <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                          Review ranked backlog suggestions, inspect why each item was proposed, then approve and apply the ones worth materializing into tasks.
                        </p>
                        {selectedPlanningRun && (
                          <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                            {planningProviderLabel(selectedPlanningRun.provider_id)} / {planningModelLabel(selectedPlanningRun.provider_id, selectedPlanningRun.model_id)} via {planningBindingSourceLabel(selectedPlanningRun.binding_source).toLowerCase()}{selectedPlanningRun.binding_label ? ` (${selectedPlanningRun.binding_label})` : ''}, {planningSelectionSourceLabel(selectedPlanningRun.selection_source).toLowerCase()}.
                          </p>
                        )}
                        {selectedPlanningRun && (
                          <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                            {planningExecutionModeLabel(selectedPlanningRun.execution_mode)}. {planningDispatchStatusLabel(selectedPlanningRun.dispatch_status)}{selectedPlanningRun.connector_label ? ` on ${selectedPlanningRun.connector_label}` : ''}.
                            {selectedPlanningRun.connector_cli_info && (
                              <> CLI: <strong>{selectedPlanningRun.connector_cli_info.agent}</strong>{selectedPlanningRun.connector_cli_info.model ? <> / <strong>{selectedPlanningRun.connector_cli_info.model}</strong></> : null}{selectedPlanningRun.connector_cli_info.model_source ? ` (${selectedPlanningRun.connector_cli_info.model_source})` : ''}.</>
                            )}
                          </p>
                        )}
                      </div>
                      {selectedPlanningRun && <span className="badge badge-todo">{planningCandidates.length} candidate{planningCandidates.length === 1 ? '' : 's'}</span>}
                    </div>

                    {planningCandidatesError && <div className="error-banner" style={{ marginTop: '1rem' }}>{planningCandidatesError}</div>}
                    {candidateReviewError && <div className="error-banner" style={{ marginTop: '1rem' }}>{candidateReviewError}</div>}
                    {candidateReviewMessage && <div className="alert alert-success" style={{ marginTop: '1rem' }}>{candidateReviewMessage}</div>}
                    {planningRunFlash && selectedPlanningRun && planningRunFlash.runId === selectedPlanningRun.id && (
                      <div
                        className={planningRunFlash.kind === 'success' ? 'alert alert-success' : 'error-banner'}
                        style={{ marginTop: '1rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}
                      >
                        <span>{planningRunFlash.message}</span>
                        <button type="button" className="btn btn-secondary btn-small" onClick={() => setPlanningRunFlash(null)}>Dismiss</button>
                      </div>
                    )}

                    {!selectedPlanningRun ? (
                      <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
                        <h3>No planning run selected</h3>
                        <p>Select a planning run to inspect its ranked suggested backlog candidates.</p>
                      </div>
                    ) : planningCandidatesLoading ? (
                      <div className="loading" style={{ padding: '1rem 0 0.5rem' }}>Loading suggested backlog…</div>
                    ) : planningCandidates.length === 0 ? (
                      <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
                        <h3>No suggested backlog yet</h3>
                        <p>
                          {selectedPlanningRun.dispatch_status === 'queued' || selectedPlanningRun.dispatch_status === 'leased'
                            ? 'This local connector run is still waiting for the paired machine to return ranked backlog suggestions.'
                            : 'This run has not produced any ranked backlog suggestions.'}
                        </p>
                      </div>
                    ) : (
                      <div className="planning-candidate-review-layout">
                        <div className="planning-candidate-list">
                          {planningCandidates.map(candidate => (
                            <button
                              key={candidate.id}
                              type="button"
                              className={`planning-candidate-card ${selectedPlanningCandidateId === candidate.id ? 'is-active' : ''}`}
                              onClick={() => handleSelectPlanningCandidate(candidate.id)}
                            >
                              <div className="requirement-card-top">
                                <strong>{candidate.title}</strong>
                                <span className={`badge ${backlogCandidateStatusBadgeClass(candidate.status)}`}>{candidate.status}</span>
                              </div>
                              <div className="planning-run-meta" style={{ marginTop: '0.4rem' }}>
                                <span>#{candidate.rank}</span>
                                <span>{backlogCandidateSuggestionLabel(candidate)}</span>
                                <span>Score {formatCandidateScore(candidate.priority_score)}</span>
                                <span>Confidence {formatCandidateScore(candidate.confidence)}%</span>
                              </div>
                              {candidate.description && <div className="requirement-description">{candidate.description}</div>}
                              <div className="planning-run-meta">
                                <span>Created {formatRelativeTime(candidate.created_at)}</span>
                                <span>Updated {formatRelativeTime(candidate.updated_at)}</span>
                              </div>
                            </button>
                          ))}
                        </div>

                        <div className="planning-candidate-detail-card">
                          {selectedPlanningCandidate ? (
                            <>
                              <div className="planning-stage-header">
                                <div>
                                  <h3 style={{ marginBottom: '0.25rem' }}>Candidate Review</h3>
                                  <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                                    Inspect the ranked recommendation, persist copy changes, then approve and apply it into the task workflow.
                                  </p>
                                </div>
                                <span className={`badge ${backlogCandidateStatusBadgeClass(selectedPlanningCandidate.status)}`}>{selectedPlanningCandidate.status}</span>
                              </div>

                              <div className="planning-run-meta" style={{ marginTop: '1rem' }}>
                                <span>Rank #{selectedPlanningCandidate.rank}</span>
                                <span>{backlogCandidateSuggestionLabel(selectedPlanningCandidate)}</span>
                                <span>Score {formatCandidateScore(selectedPlanningCandidate.priority_score)}</span>
                                <span>Confidence {formatCandidateScore(selectedPlanningCandidate.confidence)}%</span>
                                <span>{planningProviderLabel(selectedPlanningRun.provider_id)} / {planningModelLabel(selectedPlanningRun.provider_id, selectedPlanningRun.model_id)}</span>
                              </div>

                              {selectedPlanningCandidateApplied && (
                                <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                                  <strong>Applied candidate</strong>
                                  <div>This candidate already materialized into a task. Review fields are now locked and apply is idempotent.</div>
                                </div>
                              )}

                              <div className="form-group" style={{ marginTop: '1rem' }}>
                                <label>Title</label>
                                <input
                                  value={candidateForm.title}
                                  onChange={e => setCandidateForm(prev => ({ ...prev, title: e.target.value }))}
                                  disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}
                                />
                              </div>

                              <div className="form-group">
                                <label>Description</label>
                                <textarea
                                  rows={7}
                                  value={candidateForm.description}
                                  onChange={e => setCandidateForm(prev => ({ ...prev, description: e.target.value }))}
                                  disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}
                                />
                              </div>

                              <div className="form-group">
                                <label>Review Status</label>
                                <select
                                  value={candidateForm.status}
                                  onChange={e => setCandidateForm(prev => ({ ...prev, status: e.target.value as BacklogCandidate['status'] }))}
                                  disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}
                                >
                                  <option value="draft">draft</option>
                                  <option value="approved">approved</option>
                                  <option value="rejected">rejected</option>
                                </select>
                              </div>

                              {selectedPlanningCandidate.rationale && <div className="planning-candidate-rationale">{selectedPlanningCandidate.rationale}</div>}

                              {selectedPlanningCandidate.validation_criteria && (
                                <div className="planning-duplicate-note" style={{ marginTop: '1rem', borderLeft: '3px solid var(--accent-green, #22c55e)', paddingLeft: '0.75rem' }}>
                                  <strong style={{ color: 'var(--accent-green, #22c55e)' }}>Validation criteria</strong>
                                  <p style={{ margin: '0.35rem 0 0', lineHeight: '1.5' }}>{selectedPlanningCandidate.validation_criteria}</p>
                                </div>
                              )}

                              {selectedPlanningCandidate.po_decision && (
                                <div className="planning-duplicate-note" style={{ marginTop: '1rem', borderLeft: '3px solid var(--accent-orange, #f97316)', paddingLeft: '0.75rem' }}>
                                  <strong style={{ color: 'var(--accent-orange, #f97316)' }}>PO decision needed</strong>
                                  <p style={{ margin: '0.35rem 0 0', lineHeight: '1.5' }}>{selectedPlanningCandidate.po_decision}</p>
                                </div>
                              )}

                              {selectedPlanningCandidate.evidence.length > 0 && (
                                <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                                  <strong>Why this was suggested</strong>
                                  <div className="planning-run-meta" style={{ display: 'grid', gap: '0.35rem' }}>
                                    {selectedPlanningCandidate.evidence.map(item => (
                                      <span key={item}>{item}</span>
                                    ))}
                                  </div>
                                </div>
                              )}

                              {(selectedPlanningCandidate.evidence_detail.summary.length > 0 ||
                                selectedPlanningCandidate.evidence_detail.documents.length > 0 ||
                                selectedPlanningCandidate.evidence_detail.drift_signals.length > 0 ||
                                selectedPlanningCandidate.evidence_detail.sync_run ||
                                selectedPlanningCandidate.evidence_detail.agent_runs.length > 0 ||
                                selectedPlanningCandidate.evidence_detail.duplicates.length > 0) && (
                                <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                                  <strong>Context Evidence Breakdown</strong>
                                  <div style={{ display: 'grid', gap: '0.85rem', marginTop: '0.75rem' }}>
                                    <div className="planning-run-meta" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '0.5rem' }}>
                                      <span>Impact {formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.impact)}%</span>
                                      <span>Urgency {formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.urgency)}%</span>
                                      <span>Dependency unlock {formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.dependency_unlock)}%</span>
                                      <span>Risk reduction {formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.risk_reduction)}%</span>
                                      <span>Effort {formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.effort)}%</span>
                                      <span>Evidence bonus +{formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.evidence_bonus)}</span>
                                      <span>Duplicate penalty -{formatCandidateScore(selectedPlanningCandidate.evidence_detail.score_breakdown.duplicate_penalty)}</span>
                                    </div>

                                    {selectedPlanningCandidate.evidence_detail.summary.length > 0 && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Evidence summary</strong>
                                        <div className="planning-run-meta" style={{ display: 'grid', gap: '0.35rem' }}>
                                          {selectedPlanningCandidate.evidence_detail.summary.map(item => (
                                            <span key={item}>{item}</span>
                                          ))}
                                        </div>
                                      </div>
                                    )}

                                    {selectedPlanningCandidate.evidence_detail.documents.length > 0 && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Documents</strong>
                                        <div style={{ display: 'grid', gap: '0.5rem' }}>
                                          {selectedPlanningCandidate.evidence_detail.documents.map(document => (
                                            <div key={document.document_id || `${document.title}-${document.file_path}`} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                              <span>{document.title || document.file_path}</span>
                                              <span>{document.doc_type || 'general'}{document.is_stale ? ` • stale ${document.staleness_days}d` : ''}</span>
                                              {document.matched_keywords.length > 0 && <span>Matched keywords: {document.matched_keywords.join(', ')}</span>}
                                              {document.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                                            </div>
                                          ))}
                                        </div>
                                      </div>
                                    )}

                                    {selectedPlanningCandidate.evidence_detail.drift_signals.length > 0 && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Drift signals</strong>
                                        <div style={{ display: 'grid', gap: '0.5rem' }}>
                                          {selectedPlanningCandidate.evidence_detail.drift_signals.map(signal => (
                                            <div key={signal.drift_signal_id} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                              <span>{signal.document_title || signal.trigger_detail || signal.trigger_type}</span>
                                              <span>Severity {signal.severity} • {signal.trigger_type}</span>
                                              {signal.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                                            </div>
                                          ))}
                                        </div>
                                      </div>
                                    )}

                                    {selectedPlanningCandidate.evidence_detail.sync_run && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Latest sync</strong>
                                        <div className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                          <span>Status {selectedPlanningCandidate.evidence_detail.sync_run.status}</span>
                                          <span>{selectedPlanningCandidate.evidence_detail.sync_run.commits_scanned} commits • {selectedPlanningCandidate.evidence_detail.sync_run.files_changed} files</span>
                                          {selectedPlanningCandidate.evidence_detail.sync_run.error_message && <span>{selectedPlanningCandidate.evidence_detail.sync_run.error_message}</span>}
                                          {selectedPlanningCandidate.evidence_detail.sync_run.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                                        </div>
                                      </div>
                                    )}

                                    {selectedPlanningCandidate.evidence_detail.agent_runs.length > 0 && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Recent agent runs</strong>
                                        <div style={{ display: 'grid', gap: '0.5rem' }}>
                                          {selectedPlanningCandidate.evidence_detail.agent_runs.map(agentRun => (
                                            <div key={agentRun.agent_run_id} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                              <span>{agentRun.agent_name || 'agent'} • {agentRun.action_type} • {agentRun.status}</span>
                                              {agentRun.summary && <span>{agentRun.summary}</span>}
                                              {agentRun.error_message && <span>{agentRun.error_message}</span>}
                                              {agentRun.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                                            </div>
                                          ))}
                                        </div>
                                      </div>
                                    )}

                                    {selectedPlanningCandidate.evidence_detail.duplicates.length > 0 && (
                                      <div>
                                        <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Duplicate overlap impact</strong>
                                        <div style={{ display: 'grid', gap: '0.5rem' }}>
                                          {selectedPlanningCandidate.evidence_detail.duplicates.map(duplicate => (
                                            <div key={duplicate.title} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                              <span>{duplicate.title}</span>
                                              {duplicate.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                                            </div>
                                          ))}
                                        </div>
                                      </div>
                                    )}
                                  </div>
                                </div>
                              )}

                              {candidateDuplicateTitles.length > 0 && (
                                <div className="planning-duplicate-note">
                                  <strong>Possible duplicate open work</strong>
                                  <div className="planning-run-meta">
                                    {candidateDuplicateTitles.map(title => (
                                      <span key={title}>{title}</span>
                                    ))}
                                  </div>
                                </div>
                              )}

                              <div className="planning-run-meta">
                                <span>Created {formatDateTime(selectedPlanningCandidate.created_at)}</span>
                                <span>Updated {formatDateTime(selectedPlanningCandidate.updated_at)}</span>
                                <span>Run {selectedPlanningRun.status}</span>
                                <span>{planningSelectionSourceLabel(selectedPlanningRun.selection_source)}</span>
                              </div>

                              <div className="planning-candidate-actions">
                                <button className="btn btn-primary" onClick={() => persistCandidateReview()} disabled={savingCandidate || applyingCandidate || !candidateFormDirty || selectedPlanningCandidateApplied}>
                                  {savingCandidate ? 'Saving…' : 'Save Changes'}
                                </button>
                                <button className="btn btn-ghost" onClick={resetCandidateForm} disabled={savingCandidate || applyingCandidate || !candidateFormDirty}>
                                  Reset
                                </button>
                                <button className="btn btn-ghost" onClick={() => persistCandidateReview('draft')} disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}>
                                  Return To Draft
                                </button>
                                <button className="btn btn-primary" onClick={() => persistCandidateReview('approved')} disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}>
                                  Approve
                                </button>
                                <button className="btn btn-danger" onClick={() => persistCandidateReview('rejected')} disabled={savingCandidate || applyingCandidate || selectedPlanningCandidateApplied}>
                                  Reject
                                </button>
                                <button className="btn btn-primary" onClick={handleApplyPlanningCandidate} disabled={!canApplySelectedCandidate}>
                                  {applyingCandidate ? 'Applying…' : selectedPlanningCandidateApplied ? 'Applied' : 'Apply To Tasks'}
                                </button>
                              </div>
                            </>
                          ) : (
                            <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
                              <h3>No candidate selected</h3>
                              <p>Select a candidate to review its draft content.</p>
                            </div>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            ) : (
              <div className="empty-state" style={{ padding: '2rem 1rem' }}>
                <h3>No requirement selected</h3>
                <p>Create or select a requirement to anchor the planning run flow.</p>
              </div>
            )}
          </div>
        </div>
      )}

      {tab === 'tasks' && (
        <div>
          <div style={{ display: 'grid', gap: '0.75rem', marginBottom: '1rem' }}>
            <div className="task-toolbar">
              <div className="task-toolbar-group">
                <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Status</label>
                <select className="toolbar-control toolbar-control--compact" value={taskFilters.status} onChange={e => setTaskFilters(prev => ({ ...prev, status: e.target.value as TaskFilterState['status'] }))}>
                  <option value="">All</option>
                  <option value="todo">To Do</option>
                  <option value="in_progress">In Progress</option>
                  <option value="done">Done</option>
                  <option value="cancelled">Cancelled</option>
                </select>
                <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Priority</label>
                <select className="toolbar-control toolbar-control--compact" value={taskFilters.priority} onChange={e => setTaskFilters(prev => ({ ...prev, priority: e.target.value as TaskFilterState['priority'] }))}>
                  <option value="">All</option>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
                <input
                  className="toolbar-control toolbar-control--wide"
                  value={taskFilters.assignee}
                  onChange={e => setTaskFilters(prev => ({ ...prev, assignee: e.target.value }))}
                  placeholder="Filter by assignee"
                />
                <button className="btn btn-ghost btn-sm" onClick={resetTaskFilters} disabled={!hasActiveTaskFilters}>Reset Filters</button>
              </div>
              <div className="task-toolbar-group">
                <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Sort by:</label>
                <select className="toolbar-control toolbar-control--compact" value={taskSort} onChange={e => setTaskSort(e.target.value)}>
                  <option value="created_at">Created Date</option>
                  <option value="updated_at">Updated Date</option>
                  <option value="priority">Priority</option>
                  <option value="status">Status</option>
                  <option value="title">Title</option>
                </select>
                <select className="toolbar-control toolbar-control--compact" value={taskOrder} onChange={e => setTaskOrder(e.target.value)}>
                  <option value="desc">Descending</option>
                  <option value="asc">Ascending</option>
                </select>
                <button className="btn btn-primary" onClick={() => setShowTaskForm(true)}>+ New Task</button>
              </div>
            </div>

            {selectedTaskIds.length > 0 && (
              <div className="task-batch-bar">
                <strong>{selectedTaskIds.length} selected</strong>
                <select className="toolbar-control toolbar-control--compact" value={batchTaskForm.status} onChange={e => setBatchTaskForm(prev => ({ ...prev, status: e.target.value as BatchTaskFormState['status'] }))}>
                  <option value="">Keep status</option>
                  <option value="todo">To Do</option>
                  <option value="in_progress">In Progress</option>
                  <option value="done">Done</option>
                  <option value="cancelled">Cancelled</option>
                </select>
                <select className="toolbar-control toolbar-control--compact" value={batchTaskForm.priority} onChange={e => setBatchTaskForm(prev => ({ ...prev, priority: e.target.value as BatchTaskFormState['priority'] }))}>
                  <option value="">Keep priority</option>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
                <input
                  className="toolbar-control toolbar-control--wide"
                  value={batchTaskForm.assignee}
                  onChange={e => setBatchTaskForm(prev => ({ ...prev, assignee: e.target.value, clearAssignee: false }))}
                  placeholder="Set assignee"
                />
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                  <input
                    type="checkbox"
                    checked={batchTaskForm.clearAssignee}
                    onChange={e => setBatchTaskForm(prev => ({ ...prev, clearAssignee: e.target.checked, assignee: e.target.checked ? '' : prev.assignee }))}
                  />
                  Clear assignee
                </label>
                <button className="btn btn-primary btn-sm" onClick={handleApplyBatchUpdate}>Apply to Selected</button>
                <button className="btn btn-ghost btn-sm" onClick={() => setSelectedTaskIds([])}>Clear Selection</button>
              </div>
            )}
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
              <h3>{hasActiveTaskFilters ? 'No tasks match the current filters' : 'No tasks yet'}</h3>
              <p>{hasActiveTaskFilters ? 'Adjust or reset filters to see more tasks.' : 'Create your first task to get started.'}</p>
            </div>
          ) : (
            <div className="table-wrap table-wrap--wide">
              <table className="table">
              <thead>
                <tr>
                  <th style={{ width: '44px' }}>
                    <input type="checkbox" checked={allVisibleTasksSelected} onChange={toggleAllVisibleTasks} />
                  </th>
                  <th>Title</th>
                  <th>Status</th>
                  <th>Priority</th>
                  <th>Assignee</th>
                  <th>Updated</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map(task => (
                  <tr key={task.id} style={{ cursor: 'pointer', background: selectedTaskIds.includes(task.id) ? 'rgba(99, 102, 241, 0.08)' : undefined }} onClick={() => openEditTask(task)}>
                    <td onClick={e => e.stopPropagation()}>
                      <input type="checkbox" checked={selectedTaskIds.includes(task.id)} onChange={() => toggleTaskSelection(task.id)} />
                    </td>
                    <td>{task.title}</td>
                    <td><span className={`badge badge-${task.status === 'done' ? 'fresh' : task.status === 'in_progress' ? 'low' : task.status === 'cancelled' ? 'stale' : 'todo'}`}>{task.status.replace('_', ' ')}</span></td>
                    <td><span className={`badge badge-${task.priority}`}>{task.priority}</span></td>
                    <td style={{ color: 'var(--text-muted)' }}>{task.assignee || '—'}</td>
                    <td style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{new Date(task.updated_at).toLocaleDateString()}</td>
                  </tr>
                ))}
              </tbody>
              </table>
            </div>
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
            <div className="table-wrap table-wrap--wide">
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
            </div>
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
            <div className="drift-layout">
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
      </div>
    </div>
  )
}

export default ProjectDetail
