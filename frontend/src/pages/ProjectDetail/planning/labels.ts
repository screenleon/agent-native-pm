import type {
  BacklogCandidate,
  PlanningDispatchStatus,
  PlanningExecutionMode,
  PlanningProviderOptions,
  PlanningRun,
  Requirement,
} from '../../../types'

export function requirementStatusBadgeClass(status: Requirement['status']) {
  if (status === 'planned') return 'badge-fresh'
  if (status === 'archived') return 'badge-low'
  return 'badge-todo'
}

export function planningRunStatusBadgeClass(status: PlanningRun['status']) {
  if (status === 'completed') return 'badge-fresh'
  if (status === 'failed') return 'badge-stale'
  if (status === 'running') return 'badge-medium'
  if (status === 'cancelled') return 'badge-low'
  return 'badge-todo'
}

export function backlogCandidateStatusBadgeClass(status: BacklogCandidate['status']) {
  if (status === 'approved') return 'badge-fresh'
  if (status === 'rejected') return 'badge-stale'
  if (status === 'applied') return 'badge-medium'
  return 'badge-todo'
}

export function backlogCandidateSuggestionLabel(candidate: BacklogCandidate) {
  if (candidate.suggestion_type === 'integration') return 'Integration'
  if (candidate.suggestion_type === 'validation') return 'Validation'
  return 'Implementation'
}

export function planningExecutionModeLabel(executionMode: PlanningExecutionMode) {
  if (executionMode === 'local_connector') return 'Run on this machine'
  if (executionMode === 'deterministic') return 'Server deterministic fallback'
  return 'Run on server provider'
}

export function planningDispatchStatusLabel(dispatchStatus: PlanningDispatchStatus) {
  if (dispatchStatus === 'queued') return 'Queued for connector'
  if (dispatchStatus === 'leased') return 'Connector running'
  if (dispatchStatus === 'returned') return 'Connector returned result'
  if (dispatchStatus === 'expired') return 'Lease expired'
  return 'No connector dispatch'
}

export function formatCandidateScore(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) return '0.0'
  return value.toFixed(1)
}

export function planningSelectionSourceLabel(selectionSource: PlanningRun['selection_source']) {
  return selectionSource === 'request_override' ? 'Manual override' : 'Server default'
}

export function planningBindingSourceLabel(
  bindingSource: PlanningRun['binding_source'] | PlanningProviderOptions['resolved_binding_source'],
) {
  if (bindingSource === 'personal') return 'Personal binding'
  if (bindingSource === 'shared') return 'Shared workspace credentials'
  return 'Built-in workspace fallback'
}

export function credentialModeLabel(credentialMode: PlanningProviderOptions['credential_mode'] | undefined) {
  if (credentialMode === 'personal_required') return 'Personal required'
  if (credentialMode === 'personal_preferred') return 'Personal preferred'
  return 'Shared only'
}

export function makeProviderLabeler(options: PlanningProviderOptions | null) {
  return (providerId: string) => options?.providers.find(p => p.id === providerId)?.label ?? providerId
}

export function makeModelLabeler(options: PlanningProviderOptions | null) {
  return (providerId: string, modelId: string) => {
    const provider = options?.providers.find(p => p.id === providerId)
    return provider?.models.find(m => m.id === modelId)?.label ?? modelId
  }
}
