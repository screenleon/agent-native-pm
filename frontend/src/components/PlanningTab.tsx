import { useState, useEffect, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import type {
  Requirement,
  Task,
  PlanningRun,
  BacklogCandidate,
  PlanningProviderOptions,
  PlanningExecutionMode,
  PlanningDispatchStatus,
} from '../types'
import {
  createRequirement,
  getPlanningProviderOptions,
  listPlanningRuns,
  createPlanningRun,
  cancelPlanningRun,
  listPlanningRunBacklogCandidates,
  updateBacklogCandidate,
  applyBacklogCandidate,
} from '../api/client'
import { formatDateTime, formatRelativeTime } from '../utils/formatters'
import { PlanningStepper } from './PlanningStepper'

interface PlanningTabProps {
  projectId: string
  requirements: Requirement[]
  tasks: Task[]
  planningLoadError: string | null
  onReload: () => Promise<void>
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
  onRequirementsChange: (requirements: Requirement[]) => void
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

export function PlanningTab({
  projectId,
  requirements,
  tasks,
  planningLoadError,
  onReload,
  onError,
  onSuccess,
  onRequirementsChange,
}: PlanningTabProps) {
  const [planningRuns, setPlanningRuns] = useState<PlanningRun[]>([])
  const [planningCandidates, setPlanningCandidates] = useState<BacklogCandidate[]>([])
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
  const [selectedRequirementId, setSelectedRequirementId] = useState<string | null>(null)
  const [selectedPlanningRunId, setSelectedPlanningRunId] = useState<string | null>(null)
  const [selectedPlanningCandidateId, setSelectedPlanningCandidateId] = useState<string | null>(null)
  const [creatingRequirement, setCreatingRequirement] = useState(false)
  const [creatingPlanningRun, setCreatingPlanningRun] = useState(false)
  const [requirementForm, setRequirementForm] = useState({ title: '', summary: '', description: '', source: 'human' })
  const [candidateForm, setCandidateForm] = useState<{ title: string; description: string; status: BacklogCandidate['status'] }>({ title: '', description: '', status: 'draft' })
  const [candidateFormSourceId, setCandidateFormSourceId] = useState<string | null>(null)
  const [showRequirementIntake, setShowRequirementIntake] = useState(false)
  const [runningWhatsnext, setRunningWhatsnext] = useState(false)
  const [cancellingPlanningRunId, setCancellingPlanningRunId] = useState<string | null>(null)
  const [planningRunFlash, setPlanningRunFlash] = useState<{ runId: string; kind: 'success' | 'error'; message: string } | null>(null)

  const planningRunsRequestIdRef = useRef(0)
  const planningCandidatesRequestIdRef = useRef(0)
  const planningRunStatusRef = useRef<Map<string, { status: PlanningRun['status']; dispatch: PlanningRun['dispatch_status'] }>>(new Map())

  const loadProviderOptions = useCallback(async () => {
    try {
      setPlanningProviderOptionsLoading(true)
      setPlanningProviderOptionsError(null)
      const res = await getPlanningProviderOptions(projectId)
      setPlanningProviderOptions(res.data)
    } catch (err) {
      setPlanningProviderOptions(null)
      setPlanningProviderOptionsError(err instanceof Error ? err.message : 'Failed to load planning provider options')
    } finally {
      setPlanningProviderOptionsLoading(false)
    }
  }, [projectId])

  useEffect(() => {
    loadProviderOptions()
  }, [loadProviderOptions])

  useEffect(() => {
    if (requirements.length === 0) {
      if (selectedRequirementId !== null) setSelectedRequirementId(null)
      return
    }
    if (!selectedRequirementId || !requirements.some(r => r.id === selectedRequirementId)) {
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
      if (planningRunsRequestIdRef.current === requestID) setPlanningRunsLoading(false)
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
      if (selectedPlanningRunId !== null) setSelectedPlanningRunId(null)
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
      if (planningCandidatesRequestIdRef.current === requestID) setPlanningCandidatesLoading(false)
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
      if (selectedPlanningCandidateId !== null) setSelectedPlanningCandidateId(null)
      return
    }
    if (!selectedPlanningCandidateId || !planningCandidates.some(c => c.id === selectedPlanningCandidateId)) {
      setSelectedPlanningCandidateId(planningCandidates[0].id)
    }
  }, [planningCandidates, selectedPlanningCandidateId])

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
    return () => { window.clearInterval(interval) }
  }, [selectedRequirementId, selectedPlanningRunId, planningRuns, loadPlanningRuns, loadPlanningCandidates])

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

  // --- Computed values ---

  const selectedRequirement = requirements.find(r => r.id === selectedRequirementId) ?? null
  const selectedPlanningRun = planningRuns.find(run => run.id === selectedPlanningRunId) ?? null
  const selectedPlanningCandidate = planningCandidates.find(c => c.id === selectedPlanningCandidateId) ?? null

  const effectivePlanningSelection = planningProviderOptions?.default_selection ?? null
  const effectivePlanningProvider = effectivePlanningSelection
    ? planningProviderOptions?.providers.find(p => p.id === effectivePlanningSelection.provider_id) ?? null
    : null
  const effectivePlanningModel = effectivePlanningSelection && effectivePlanningProvider
    ? effectivePlanningProvider.models.find(m => m.id === effectivePlanningSelection.model_id) ?? null
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
  const planningExecutionModel = effectivePlanningProvider?.models.find(m => m.id === planningExecutionModelID) ?? effectivePlanningModel ?? null
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
  const draftRequirementCount = requirements.filter(r => r.status === 'draft').length
  const plannedRequirementCount = requirements.filter(r => r.status === 'planned').length
  const archivedRequirementCount = requirements.filter(r => r.status === 'archived').length

  // --- Label helpers (depend on planningProviderOptions) ---

  function planningProviderLabel(providerId: string) {
    return planningProviderOptions?.providers.find(p => p.id === providerId)?.label ?? providerId
  }

  function planningModelLabel(providerId: string, modelId: string) {
    const provider = planningProviderOptions?.providers.find(p => p.id === providerId)
    return provider?.models.find(m => m.id === modelId)?.label ?? modelId
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

  // --- Candidate form sync ---

  function syncCandidateForm(candidate: BacklogCandidate | null) {
    if (!candidate) {
      setCandidateForm({ title: '', description: '', status: 'draft' })
      setCandidateFormSourceId(null)
      return
    }
    setCandidateForm({ title: candidate.title, description: candidate.description, status: candidate.status })
    setCandidateFormSourceId(candidate.id)
  }

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

  // --- Handlers ---

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

  async function handleCreateRequirement(e: React.FormEvent) {
    e.preventDefault()
    if (!requirementForm.title.trim()) return
    try {
      setCreatingRequirement(true)
      const response = await createRequirement(projectId, {
        title: requirementForm.title.trim(),
        summary: requirementForm.summary.trim(),
        description: requirementForm.description.trim(),
        source: requirementForm.source.trim() || 'human',
      })
      onRequirementsChange([response.data, ...requirements])
      setSelectedRequirementId(response.data.id)
      setRequirementForm({ title: '', summary: '', description: '', source: 'human' })
      setShowRequirementIntake(false)
      onSuccess('Requirement captured. Planning workspace is ready for planning runs and candidate review.')
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to create requirement')
    } finally {
      setCreatingRequirement(false)
    }
  }

  async function handleCreatePlanningRun() {
    if (!selectedRequirement || !planningRunReady) return
    try {
      setCreatingPlanningRun(true)
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
      onSuccess(
        response.data.execution_mode === 'local_connector'
          ? 'Planning run queued for your paired local connector. Draft backlog candidates will appear after the connector returns results.'
          : 'Planning run recorded and draft backlog candidates are ready for review.',
      )
      await onReload()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to start planning run'
      onError(message)
      setPlanningRunsError(message)
    } finally {
      setCreatingPlanningRun(false)
    }
  }

  async function handleCancelPlanningRun(runId: string) {
    if (!runId) return
    if (!window.confirm('Cancel this planning run? Any in-flight local connector dispatch will be released.')) return
    try {
      setCancellingPlanningRunId(runId)
      setPlanningRunsError(null)
      const response = await cancelPlanningRun(runId)
      setPlanningRuns(prev => prev.map(run => run.id === response.data.id ? response.data : run))
      onSuccess('Planning run cancelled.')
      if (selectedRequirement) await loadPlanningRuns(selectedRequirement.id)
    } catch (err) {
      setPlanningRunsError(err instanceof Error ? err.message : 'Failed to cancel planning run')
    } finally {
      setCancellingPlanningRunId(null)
    }
  }

  async function handleRunWhatsnext() {
    if (!planningRunReady) return
    setRunningWhatsnext(true)
    try {
      const WHATSNEXT_TITLE = "What's next"
      let req = requirements.find(r => r.title.toLowerCase().trim() === WHATSNEXT_TITLE.toLowerCase())
      if (!req) {
        const res = await createRequirement(projectId, {
          title: WHATSNEXT_TITLE,
          summary: 'Project-wide health check — surfaces the most urgent open work across tasks, drift signals, and stale docs.',
          source: 'analysis',
        })
        req = res.data
        onRequirementsChange([res.data, ...requirements])
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
      onSuccess("What's Next analysis queued. Your connector will surface the top priorities across the project.")
      await loadPlanningRuns(req.id)
    } catch (err) {
      onError(err instanceof Error ? err.message : "Failed to start What's Next analysis")
    } finally {
      setRunningWhatsnext(false)
    }
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
      setPlanningCandidates(prev => prev.map(c => c.id === response.data.id ? response.data : c))
      syncCandidateForm(response.data)
      setCandidateReviewMessage(
        nextStatus === 'approved' ? 'Candidate approved.' :
        nextStatus === 'rejected' ? 'Candidate rejected.' :
        nextStatus === 'draft' ? 'Candidate returned to draft.' :
        'Candidate review saved.',
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
      setPlanningCandidates(prev => prev.map(c => c.id === response.data.candidate.id ? response.data.candidate : c))
      syncCandidateForm(response.data.candidate)
      await Promise.all([
        onReload(),
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
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div>
              <h3 style={{ marginBottom: requirements.length > 0 ? 0 : '0.25rem' }}>Requirement Intake</h3>
              {requirements.length === 0 && (
                <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                  Capture product or implementation intent here first. This keeps Phase 2 draft-first and avoids creating tasks too early.
                </p>
              )}
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
              <span className="badge badge-todo">P2-03</span>
              {requirements.length > 0 && (
                <button
                  className="btn btn-ghost btn-sm"
                  onClick={() => setShowRequirementIntake(prev => !prev)}
                >
                  {showRequirementIntake ? '▲ Hide' : '+ New Requirement'}
                </button>
              )}
            </div>
          </div>

          {(requirements.length === 0 || showRequirementIntake) && (
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
          )}
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
                            {effectivePlanningProvider.models.filter(m => m.enabled).map(m => (
                              <option key={m.id} value={m.id}>{m.label}</option>
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
                <button className="btn btn-ghost" onClick={() => selectedRequirement && loadPlanningRuns(selectedRequirement.id)} disabled={planningRunsLoading || creatingPlanningRun}>
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
  )
}
