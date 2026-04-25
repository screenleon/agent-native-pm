import { Link } from 'react-router-dom'
import type { Requirement, Task } from '../../types'
import { RequirementIntake } from './planning/RequirementIntake'
import { RequirementQueue } from './planning/RequirementQueue'
import { PlanningLauncher } from './planning/PlanningLauncher'
import { PlanningRunList } from './planning/PlanningRunList'
import { CandidateReviewPanel } from './planning/CandidateReviewPanel'
import { AppliedLineage } from './planning/AppliedLineage'
import { WorkspaceOnboardingPanel } from './planning/WorkspaceOnboardingPanel'
import { usePlanningWorkspaceData } from './planning/hooks/usePlanningWorkspaceData'

const APPLIED_TASK_SOURCE = 'agent:planning-orchestrator'

interface PlanningTabProps {
  projectId: string
  requirements: Requirement[]
  tasks: Task[]
  openDriftCount: number
  planningLoadError: string | null
  onReload: () => Promise<void>
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
  onRequirementsChange: (requirements: Requirement[]) => void
  onNavigateToTasks: () => void
  onNavigateToDrift: () => void
  /**
   * S3 evidence-link wiring. Pass the existing handleViewDoc-by-id and
   * setTab('drift') closures from ProjectDetail so candidate evidence
   * rows can open the document-preview modal or jump to the Drift tab.
   */
  onViewDocumentById?: (documentId: string) => void
  onViewDriftSignal?: (driftSignalId: string) => void
}

/**
 * Planning Workspace shell. Composes presentational siblings under
 * `pages/ProjectDetail/planning/`; all planning-domain state + effects +
 * handlers live in `usePlanningWorkspaceData`.
 *
 * Layout:
 *  - Empty project (requirements.length === 0): centered welcome view via WorkspaceOnboardingPanel
 *  - Non-empty project: two-panel layout (240px sidebar + flex main)
 */
export function PlanningTab({
  projectId,
  requirements,
  tasks,
  openDriftCount,
  planningLoadError,
  onReload,
  onError,
  onSuccess,
  onRequirementsChange,
  onNavigateToTasks,
  onNavigateToDrift,
  onViewDocumentById,
  onViewDriftSignal,
}: PlanningTabProps) {
  // openDriftCount and onNavigateToDrift are kept in the props interface for
  // parent compatibility (ProjectDetail passes them). They were previously used
  // by AttentionRow which was removed in the two-panel redesign.
  void openDriftCount
  void onNavigateToDrift
  const ws = usePlanningWorkspaceData({
    projectId,
    requirements,
    tasks,
    onReload,
    onError,
    onSuccess,
    onRequirementsChange,
  })

  // Derive a reload signal from the fields of every task that actually
  // affect the lineage lane (id + status + updated_at). tasks.length
  // alone would miss status/title edits — e.g. an operator marking an
  // applied task "done" would leave the lane showing "in progress"
  // until the next page refresh. This signal changes on any mutation
  // that matters for the lane's rendering.
  const lineageReloadSignal = tasks
    .map(t => `${t.id}:${t.status}:${t.updated_at}`)
    .join('|')

  const requirementsAwaitingPlanning = requirements.filter(r => r.status === 'draft').length
  const candidatesAwaitingReview = ws.planningCandidates.filter(
    c => c.status === 'draft' || c.status === 'approved',
  ).length
  const appliedOpenTasks = tasks.filter(
    t => t.source === APPLIED_TASK_SOURCE && (t.status === 'todo' || t.status === 'in_progress'),
  ).length

  function scrollToSelector(selector: string) {
    const el = document.querySelector(selector) as HTMLElement | null
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
  const jumpToRequirements = () => scrollToSelector('.requirement-list')
  const jumpToCandidates = () => scrollToSelector('.planning-candidate-panel')

  const sidebarRequirements = requirements.filter(
    r => r.source !== 'analysis' && r.source !== 'system'
  )
  const isEmpty = sidebarRequirements.length === 0

  return (
    <div className="planning-shell">
      {planningLoadError && (
        <div className="error-banner" style={{ marginBottom: '1rem' }}>{planningLoadError}</div>
      )}

      {isEmpty ? (
        <>
          <WorkspaceOnboardingPanel
            projectId={projectId}
            onRunCreated={async (requirementId, runId) => {
              ws.onSelectLineage(requirementId, runId)
              await onReload()
            }}
            planningRunsCount={ws.planningRuns.length}
            planningRunReady={ws.planningRunReady}
            onRunWhatsnext={ws.onRunWhatsnext}
            runningWhatsnext={ws.runningWhatsnext}
          />
          {ws.selectedRequirement && (
            <div style={{ marginTop: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <PlanningRunList
                runs={ws.planningRuns}
                loading={ws.planningRunsLoading}
                errorMessage={ws.planningRunsError}
                selectedRunId={ws.selectedPlanningRunId}
                cancellingRunId={ws.cancellingPlanningRunId}
                providerOptions={ws.planningProviderOptions}
                onSelectRun={ws.onSelectPlanningRun}
                onCancelRun={ws.onCancelPlanningRun}
              />
              <CandidateReviewPanel
                selectedRun={ws.selectedPlanningRun}
                candidates={ws.planningCandidates}
                candidatesLoading={ws.planningCandidatesLoading}
                candidatesError={ws.planningCandidatesError}
                selectedCandidate={ws.selectedPlanningCandidate}
                selectedCandidateId={ws.selectedPlanningCandidateId}
                onSelectCandidate={ws.onSelectPlanningCandidate}
                candidateForm={ws.candidateForm}
                onCandidateFormChange={ws.setCandidateForm}
                candidateFormDirty={ws.candidateFormDirty}
                selectedCandidateApplied={ws.selectedPlanningCandidateApplied}
                canApplySelectedCandidate={ws.canApplySelectedCandidate}
                savingCandidate={ws.savingCandidate}
                applyingCandidate={ws.applyingCandidate}
                candidateReviewError={ws.candidateReviewError}
                candidateReviewMessage={ws.candidateReviewMessage}
                candidateDuplicateTitles={ws.candidateDuplicateTitles}
                runFlash={ws.planningRunFlash}
                onDismissRunFlash={ws.onDismissRunFlash}
                providerOptions={ws.planningProviderOptions}
                onPersistReview={ws.onPersistCandidateReview}
                onApplyCandidate={ws.onApplyCandidate}
                onSkipCandidate={ws.onSkipCandidate}
                onResetCandidateForm={ws.onResetCandidateForm}
                selectedExecutionMode={ws.selectedExecutionMode}
                onSelectedExecutionModeChange={ws.onSelectedExecutionModeChange}
                onViewDocumentById={onViewDocumentById}
                onViewDriftSignal={onViewDriftSignal}
              />
            </div>
          )}
        </>
      ) : (
        <div className="planning-two-panel">
          <aside className="planning-sidebar">
            <div className="planning-sidebar-header">
              <h4 style={{ margin: 0 }}>Requirements</h4>
              <button className="btn btn-primary btn-sm" onClick={ws.onToggleRequirementIntake}>
                {ws.showRequirementIntake ? '✕' : '+ New'}
              </button>
            </div>

            {ws.showRequirementIntake && (
              <RequirementIntake
                variant="inline"
                requirementCount={requirements.length}
                form={ws.requirementForm}
                onFormChange={ws.setRequirementForm}
                creating={ws.creatingRequirement}
                showForm={ws.showRequirementIntake}
                onToggleForm={ws.onToggleRequirementIntake}
                onSubmit={ws.onCreateRequirement}
                onReset={ws.onResetRequirementForm}
              />
            )}

            <RequirementQueue
              compact
              requirements={sidebarRequirements}
              selectedRequirementId={ws.selectedRequirementId}
              onSelectRequirement={ws.onSelectRequirement}
              planningLoadError={null}
              onArchiveRequirement={ws.onArchiveRequirement}
              archivingRequirementId={ws.archivingRequirementId}
              onDiscardRequirement={ws.onDiscardRequirement}
              discardingRequirementId={ws.discardingRequirementId}
              requirementIdsWithAppliedTasks={ws.requirementIdsWithAppliedTasks}
            />

            {(requirementsAwaitingPlanning > 0 || candidatesAwaitingReview > 0 || appliedOpenTasks > 0) && (
              <div className="planning-sidebar-counts">
                {requirementsAwaitingPlanning > 0 && (
                  <button type="button" className="planning-sidebar-count" onClick={jumpToRequirements}>
                    <span className="badge badge-stale">{requirementsAwaitingPlanning}</span>
                    <span>awaiting planning</span>
                  </button>
                )}
                {candidatesAwaitingReview > 0 && (
                  <button type="button" className="planning-sidebar-count" onClick={jumpToCandidates}>
                    <span className="badge badge-stale">{candidatesAwaitingReview}</span>
                    <span>to review</span>
                  </button>
                )}
                {appliedOpenTasks > 0 && (
                  <button type="button" className="planning-sidebar-count" onClick={onNavigateToTasks}>
                    <span className="badge badge-medium">{appliedOpenTasks}</span>
                    <span>applied tasks open</span>
                  </button>
                )}
              </div>
            )}
          </aside>

          <main className="planning-main">
            {ws.selectedRequirement ? (
              <>
                <PlanningLauncher
                  selectedRequirement={ws.selectedRequirement}
                  providerOptions={ws.planningProviderOptions}
                  providerOptionsLoading={ws.planningProviderOptionsLoading}
                  providerOptionsError={ws.planningProviderOptionsError}
                  executionMode={ws.planningSelectedExecutionMode}
                  onExecutionModeChange={ws.onPlanningExecutionModeChange}
                  cliConfigs={ws.cliConfigs}
                  cliConfigsLoading={ws.cliConfigsLoading}
                  selectedCliConfigKey={ws.selectedCliConfigKey}
                  onCliConfigChange={ws.onCliConfigChange}
                  planningModelOverride={ws.planningModelOverride}
                  onPlanningModelOverrideChange={ws.onPlanningModelOverrideChange}
                  creatingRun={ws.creatingPlanningRun}
                  runningWhatsnext={ws.runningWhatsnext}
                  runsLoading={ws.planningRunsLoading}
                  runReady={ws.planningRunReady}
                  runBlockedReason={ws.planningRunBlockedReason}
                  onStartRun={ws.onCreatePlanningRun}
                  onRefreshRuns={() => ws.selectedRequirement && ws.loadPlanningRuns(ws.selectedRequirement.id)}
                  onRunWhatsnext={ws.onRunWhatsnext}
                  activeRunDispatchStatus={ws.activeRunDispatchStatus}
                />

                <PlanningRunList
                  runs={ws.planningRuns}
                  loading={ws.planningRunsLoading}
                  errorMessage={ws.planningRunsError}
                  selectedRunId={ws.selectedPlanningRunId}
                  cancellingRunId={ws.cancellingPlanningRunId}
                  providerOptions={ws.planningProviderOptions}
                  onSelectRun={ws.onSelectPlanningRun}
                  onCancelRun={ws.onCancelPlanningRun}
                />

                <CandidateReviewPanel
                  selectedRun={ws.selectedPlanningRun}
                  candidates={ws.planningCandidates}
                  candidatesLoading={ws.planningCandidatesLoading}
                  candidatesError={ws.planningCandidatesError}
                  selectedCandidate={ws.selectedPlanningCandidate}
                  selectedCandidateId={ws.selectedPlanningCandidateId}
                  onSelectCandidate={ws.onSelectPlanningCandidate}
                  candidateForm={ws.candidateForm}
                  onCandidateFormChange={ws.setCandidateForm}
                  candidateFormDirty={ws.candidateFormDirty}
                  selectedCandidateApplied={ws.selectedPlanningCandidateApplied}
                  canApplySelectedCandidate={ws.canApplySelectedCandidate}
                  savingCandidate={ws.savingCandidate}
                  applyingCandidate={ws.applyingCandidate}
                  candidateReviewError={ws.candidateReviewError}
                  candidateReviewMessage={ws.candidateReviewMessage}
                  candidateDuplicateTitles={ws.candidateDuplicateTitles}
                  runFlash={ws.planningRunFlash}
                  onDismissRunFlash={ws.onDismissRunFlash}
                  providerOptions={ws.planningProviderOptions}
                  onPersistReview={ws.onPersistCandidateReview}
                  onApplyCandidate={ws.onApplyCandidate}
                  onSkipCandidate={ws.onSkipCandidate}
                  onResetCandidateForm={ws.onResetCandidateForm}
                  selectedExecutionMode={ws.selectedExecutionMode}
                  onSelectedExecutionModeChange={ws.onSelectedExecutionModeChange}
                  onViewDocumentById={onViewDocumentById}
                  onViewDriftSignal={onViewDriftSignal}
                />

                <AppliedLineage
                  projectId={projectId}
                  reloadSignal={lineageReloadSignal}
                  onSelectLineage={(requirementId, runId, candidateId) => {
                    ws.onSelectLineage(requirementId, runId, candidateId)
                    if (candidateId) {
                      jumpToCandidates()
                    } else {
                      jumpToRequirements()
                    }
                  }}
                  onJumpToTasks={onNavigateToTasks}
                />
              </>
            ) : (
              <div className="planning-main-empty">
                <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
                  Select a requirement on the left to plan a specific feature.
                </p>
                {ws.planningRunReady ? (
                  <button type="button" className="btn btn-secondary" onClick={ws.onRunWhatsnext} disabled={ws.runningWhatsnext}>
                    {ws.runningWhatsnext ? 'Starting…' : "Run What's Next — full project health check"}
                  </button>
                ) : (
                  <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                    <Link to="/settings/models">Configure a planning provider</Link> or connect a local connector to enable health-check runs.
                  </p>
                )}
              </div>
            )}
          </main>
        </div>
      )}
    </div>
  )
}
