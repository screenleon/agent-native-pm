import { useState, useEffect, useCallback, useRef } from 'react'
import type { FormEvent } from 'react'
import type {
  Requirement,
  Task,
  PlanningRun,
  BacklogCandidate,
  PlanningProviderOptions,
  PlanningExecutionMode,
  AccountBinding,
} from '../../../../types'
import {
  createRequirement,
  updateRequirement,
  getPlanningProviderOptions,
  listPlanningRuns,
  createPlanningRun,
  cancelPlanningRun,
  listPlanningRunBacklogCandidates,
  updateBacklogCandidate,
  applyBacklogCandidate,
  listAccountBindings,
} from '../../../../api/client'
import type { RequirementIntakeForm } from '../RequirementIntake'
import type { CandidateReviewForm } from '../CandidateReviewPanel'

export interface UsePlanningWorkspaceDataInput {
  projectId: string
  requirements: Requirement[]
  tasks: Task[]
  onReload: () => Promise<void>
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
  onRequirementsChange: (requirements: Requirement[]) => void
}

/**
 * Central data + state + handler hook for the Planning Workspace shell.
 *
 * Owns:
 *   - the planning runs / candidates / provider-options fetch loops,
 *   - the selection triple (requirement → run → candidate),
 *   - the form state for requirement intake + candidate review,
 *   - every handler that mutates planning-domain state.
 *
 * The hook is intentionally fat — it replaces the pre-S1 monolithic state
 * graph inside PlanningTab. The acceptance criterion in the Phase 2 design
 * doc (§8) is that PlanningTab.tsx itself stays under 200 LOC after this
 * extraction; the complexity lives here where it can be read / tested /
 * eventually split further without touching the JSX shell.
 */
export function usePlanningWorkspaceData({
  projectId,
  requirements,
  tasks,
  onReload,
  onError,
  onSuccess,
  onRequirementsChange,
}: UsePlanningWorkspaceDataInput) {
  const [planningRuns, setPlanningRuns] = useState<PlanningRun[]>([])
  const [planningCandidates, setPlanningCandidates] = useState<BacklogCandidate[]>([])
  const [planningRunsLoading, setPlanningRunsLoading] = useState(false)
  const [planningRunsError, setPlanningRunsError] = useState<string | null>(null)
  const [planningProviderOptions, setPlanningProviderOptions] = useState<PlanningProviderOptions | null>(null)
  const [planningProviderOptionsLoading, setPlanningProviderOptionsLoading] = useState(false)
  const [planningProviderOptionsError, setPlanningProviderOptionsError] = useState<string | null>(null)
  const [planningModelOverride, setPlanningModelOverride] = useState('')
  const [planningExecutionMode, setPlanningExecutionMode] = useState<PlanningExecutionMode>('server_provider')
  const [cliBindings, setCliBindings] = useState<AccountBinding[]>([])
  const [cliBindingsLoading, setCliBindingsLoading] = useState(false)
  const [selectedCliBindingId, setSelectedCliBindingId] = useState<string | null>(null)
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
  const [requirementForm, setRequirementForm] = useState<RequirementIntakeForm>({ title: '', summary: '', description: '', source: 'human' })
  const [candidateForm, setCandidateForm] = useState<CandidateReviewForm>({ title: '', description: '', status: 'draft' })
  const [candidateFormSourceId, setCandidateFormSourceId] = useState<string | null>(null)
  const [showRequirementIntake, setShowRequirementIntake] = useState(false)
  const [archivingRequirementId, setArchivingRequirementId] = useState<string | null>(null)
  const [runningWhatsnext, setRunningWhatsnext] = useState(false)
  const [cancellingPlanningRunId, setCancellingPlanningRunId] = useState<string | null>(null)
  const [planningRunFlash, setPlanningRunFlash] = useState<{ runId: string; kind: 'success' | 'error'; message: string } | null>(null)

  // pendingSelection holds the run + candidate pair the operator asked us
  // to select *after* the async load chain catches up. The requirement tier
  // is not part of the pending triple because handleSelectLineage sets
  // selectedRequirementId synchronously — only runs and candidates need the
  // pending-handoff because they load asynchronously from the requirement.
  //
  // Each field is cleared as soon as it has been applied, so subsequent
  // organic user clicks aren't overridden by stale intent.
  const [pendingSelection, setPendingSelection] = useState<{
    runId?: string
    candidateId?: string
  }>({})

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

  async function loadCliBindings() {
    setCliBindingsLoading(true)
    try {
      const res = await listAccountBindings()
      const cli = res.data.filter(b => b.provider_id.startsWith('cli:') && b.is_active)
      setCliBindings(cli)
      const primary = cli.find(b => b.is_primary) ?? cli[0] ?? null
      setSelectedCliBindingId(prev => prev ?? primary?.id ?? null)
    } catch {
      // non-critical; leave empty
    } finally {
      setCliBindingsLoading(false)
    }
  }

  useEffect(() => {
    if (requirements.length === 0) {
      if (selectedRequirementId !== null) setSelectedRequirementId(null)
      return
    }
    if (!selectedRequirementId || !requirements.some(r => r.id === selectedRequirementId)) {
      const first = requirements.find(r => r.status !== 'archived')
      setSelectedRequirementId(first?.id ?? null)
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
    // Pending deep-link from the AppliedLineage lane takes precedence over
    // "auto-select first run". If the target run is in the loaded list we
    // apply it and clear the pending slot; otherwise fall back to the
    // existing auto-select behaviour.
    if (pendingSelection.runId && planningRuns.some(run => run.id === pendingSelection.runId)) {
      setSelectedPlanningRunId(pendingSelection.runId)
      setPendingSelection(prev => ({ ...prev, runId: undefined }))
      return
    }
    if (!selectedPlanningRunId || !planningRuns.some(run => run.id === selectedPlanningRunId)) {
      setSelectedPlanningRunId(planningRuns[0].id)
    }
  }, [planningRuns, selectedPlanningRunId, pendingSelection.runId])

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
    // Same pending-selection rule for the candidate tier: apply the
    // pending target if it's in the loaded list; otherwise auto-select
    // the first candidate.
    if (pendingSelection.candidateId && planningCandidates.some(c => c.id === pendingSelection.candidateId)) {
      setSelectedPlanningCandidateId(pendingSelection.candidateId)
      setPendingSelection(prev => ({ ...prev, candidateId: undefined }))
      return
    }
    if (!selectedPlanningCandidateId || !planningCandidates.some(c => c.id === selectedPlanningCandidateId)) {
      setSelectedPlanningCandidateId(planningCandidates[0].id)
    }
  }, [planningCandidates, selectedPlanningCandidateId, pendingSelection.candidateId])

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
    if (planningSelectedExecutionMode === 'local_connector') {
      void loadCliBindings()
    }
  // loadCliBindings is defined in the same render scope but is not memoized.
  // The dep array intentionally contains only the mode to avoid re-fetching on
  // every render; the function reads current state via closure at call time.
  }, [planningSelectedExecutionMode])

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

  /**
   * Cross-tier deep-link used by the AppliedLineage lane. Clicking a
   * lineage row with (requirementId, runId, candidateId) asks the
   * workspace to land on exactly that candidate after the three-step
   * async load chain catches up:
   *
   *   select requirement → load runs → auto-select pending.runId →
   *   load candidates → auto-select pending.candidateId.
   *
   * If the operator has unsaved candidate edits we confirm first, same
   * as any other selection change. If runId / candidateId are omitted,
   * only the requirement-level selection happens.
   */
  function handleSelectLineage(requirementId: string, runId?: string, candidateId?: string) {
    if (!confirmDiscardCandidateEdits()) return
    setPendingSelection({ runId, candidateId })
    // Switching requirement triggers loadPlanningRuns via effect; the
    // auto-select effects then consume runId / candidateId.
    if (requirementId !== selectedRequirementId) {
      setSelectedRequirementId(requirementId)
    } else if (runId && planningRuns.some(run => run.id === runId)) {
      // Same-requirement deep-link: runs are already loaded, trigger the
      // selection directly so the run-id effect path applies on next tick.
      setSelectedPlanningRunId(runId)
    }
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

  async function handleCreateRequirement(e: FormEvent) {
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

  async function handleArchiveRequirement(id: string) {
    if (archivingRequirementId) return
    setArchivingRequirementId(id)
    try {
      const response = await updateRequirement(id, { status: 'archived' })
      onRequirementsChange(requirements.map(r => r.id === id ? response.data : r))
      if (selectedRequirementId === id) setSelectedRequirementId(null)
      onSuccess('Requirement archived.')
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to archive requirement')
    } finally {
      setArchivingRequirementId(null)
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
        ...(planningSelectedExecutionMode === 'local_connector' ? { adapter_type: 'backlog' } : {}),
        ...(planningSelectedExecutionMode === 'local_connector' && selectedCliBindingId ? { account_binding_id: selectedCliBindingId } : {}),
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
      let req = requirements.find(r => r.title.toLowerCase().trim() === WHATSNEXT_TITLE.toLowerCase() && r.status !== 'archived')
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
        ...(planningSelectedExecutionMode === 'local_connector' && selectedCliBindingId ? { account_binding_id: selectedCliBindingId } : {}),
      })
      setPlanningRuns(prev => [runRes.data, ...prev.filter(r => r.id !== runRes.data.id)])
      setSelectedPlanningRunId(runRes.data.id)
      onSuccess(planningSelectedExecutionMode === 'local_connector'
        ? "What's Next analysis queued. Your connector will surface the top priorities across the project."
        : "What's Next analysis started. Results will appear in the candidate panel when complete.")
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

  // Phase 5 B3: the panel lets the operator pick Manual or Auto-dispatch
  // for the upcoming Apply click. Today `role_dispatch` is a no-op on the
  // UI side (Phase 6 will dispatch). We still thread the choice through
  // so the backend records the task's `source` correctly for audit.
  const [selectedExecutionMode, setSelectedExecutionMode] = useState<'manual' | 'role_dispatch'>('manual')

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
      const response = await applyBacklogCandidate(selectedPlanningCandidate.id, {
        executionMode: selectedExecutionMode,
      })
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

  return {
    // selections
    selectedRequirement,
    selectedPlanningRun,
    selectedPlanningCandidate,
    selectedRequirementId,
    selectedPlanningRunId,
    onSelectLineage: handleSelectLineage,
    selectedPlanningCandidateId,
    onSelectRequirement: handleSelectRequirement,
    onSelectPlanningRun: handleSelectPlanningRun,
    onSelectPlanningCandidate: handleSelectPlanningCandidate,
    // runs
    planningRuns,
    planningRunsLoading,
    planningRunsError,
    cancellingPlanningRunId,
    loadPlanningRuns,
    onCancelPlanningRun: handleCancelPlanningRun,
    onCreatePlanningRun: handleCreatePlanningRun,
    onRunWhatsnext: handleRunWhatsnext,
    creatingPlanningRun,
    runningWhatsnext,
    // candidates
    planningCandidates,
    planningCandidatesLoading,
    planningCandidatesError,
    candidateForm,
    setCandidateForm,
    candidateFormDirty,
    candidateReviewError,
    candidateReviewMessage,
    candidateDuplicateTitles,
    selectedPlanningCandidateApplied,
    canApplySelectedCandidate,
    savingCandidate,
    applyingCandidate,
    onPersistCandidateReview: persistCandidateReview,
    onApplyCandidate: handleApplyPlanningCandidate,
    onResetCandidateForm: resetCandidateForm,
    // Phase 5 B3: apply execution mode + setter so the panel radio group
    // can drive the Apply request body.
    selectedExecutionMode,
    onSelectedExecutionModeChange: setSelectedExecutionMode,
    // provider options
    planningProviderOptions,
    planningProviderOptionsLoading,
    planningProviderOptionsError,
    planningSelectedExecutionMode,
    onPlanningExecutionModeChange: setPlanningExecutionMode,
    cliBindings,
    cliBindingsLoading,
    selectedCliBindingId,
    onCliBindingChange: setSelectedCliBindingId,
    planningModelOverride,
    onPlanningModelOverrideChange: setPlanningModelOverride,
    planningRunReady,
    planningRunBlockedReason,
    // requirement intake
    requirementForm,
    setRequirementForm,
    creatingRequirement,
    showRequirementIntake,
    onToggleRequirementIntake: () => setShowRequirementIntake(prev => !prev),
    onResetRequirementForm: () => setRequirementForm({ title: '', summary: '', description: '', source: 'human' }),
    onCreateRequirement: handleCreateRequirement,
    onArchiveRequirement: handleArchiveRequirement,
    archivingRequirementId,
    // run flash
    planningRunFlash,
    onDismissRunFlash: () => setPlanningRunFlash(null),
  }
}
