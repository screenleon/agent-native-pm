import type { Requirement, Task } from '../types'
import { PlanningStepper } from './PlanningStepper'
import { AttentionRow } from '../pages/ProjectDetail/planning/AttentionRow'
import { RequirementIntake } from '../pages/ProjectDetail/planning/RequirementIntake'
import { RequirementQueue } from '../pages/ProjectDetail/planning/RequirementQueue'
import { PlanningLauncher } from '../pages/ProjectDetail/planning/PlanningLauncher'
import { PlanningRunList } from '../pages/ProjectDetail/planning/PlanningRunList'
import { CandidateReviewPanel } from '../pages/ProjectDetail/planning/CandidateReviewPanel'
import { usePlanningWorkspaceData } from '../pages/ProjectDetail/planning/hooks/usePlanningWorkspaceData'

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
 * handlers live in `usePlanningWorkspaceData`. Per Phase 2 S1 acceptance
 * criteria (§8 of docs/phase2-planning-workspace-design.md) this shell
 * stays under 200 LOC; growth signals an architectural drift.
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
  const ws = usePlanningWorkspaceData({
    projectId,
    requirements,
    tasks,
    onReload,
    onError,
    onSuccess,
    onRequirementsChange,
  })

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
  function focusSelector(selector: string) {
    const el = document.querySelector(selector) as HTMLElement | null
    if (el) el.focus()
  }
  const jumpToRequirements = () => scrollToSelector('.requirement-list')
  const jumpToCandidates = () => scrollToSelector('.planning-candidate-panel')
  const jumpToWorkspace = () => scrollToSelector('.planning-workspace-card')
  const jumpToIntake = () => focusSelector('.planning-foundation-grid input')

  return (
    <div className="planning-shell">
      <AttentionRow
        requirementsAwaitingPlanning={requirementsAwaitingPlanning}
        candidatesAwaitingReview={candidatesAwaitingReview}
        appliedOpenTasks={appliedOpenTasks}
        openDriftCount={openDriftCount}
        onJumpToRequirements={jumpToRequirements}
        onJumpToCandidates={jumpToCandidates}
        onJumpToTasks={onNavigateToTasks}
        onJumpToDrift={onNavigateToDrift}
      />

      <PlanningStepper
        requirementCount={requirements.length}
        selectedRequirement={ws.selectedRequirement}
        selectedPlanningRun={ws.selectedPlanningRun}
        candidateCount={ws.planningCandidates.length}
        onJumpToIntake={jumpToIntake}
        onJumpToWorkspace={jumpToWorkspace}
        onJumpToCandidates={jumpToCandidates}
      />

      <div className="planning-foundation-grid">
        <RequirementIntake
          requirementCount={requirements.length}
          form={ws.requirementForm}
          onFormChange={ws.setRequirementForm}
          creating={ws.creatingRequirement}
          showForm={ws.showRequirementIntake}
          onToggleForm={ws.onToggleRequirementIntake}
          onSubmit={ws.onCreateRequirement}
          onReset={ws.onResetRequirementForm}
        />

        <RequirementQueue
          requirements={requirements}
          selectedRequirementId={ws.selectedRequirementId}
          onSelectRequirement={ws.onSelectRequirement}
          planningLoadError={planningLoadError}
        />
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

        {ws.selectedRequirement ? (
          <div className="planning-workspace-content">
            <PlanningLauncher
              selectedRequirement={ws.selectedRequirement}
              providerOptions={ws.planningProviderOptions}
              providerOptionsLoading={ws.planningProviderOptionsLoading}
              providerOptionsError={ws.planningProviderOptionsError}
              executionMode={ws.planningSelectedExecutionMode}
              onExecutionModeChange={ws.onPlanningExecutionModeChange}
              localAdapterType={ws.localAdapterType}
              onLocalAdapterTypeChange={ws.onLocalAdapterTypeChange}
              localModelOverride={ws.localModelOverride}
              onLocalModelOverrideChange={ws.onLocalModelOverrideChange}
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
              onResetCandidateForm={ws.onResetCandidateForm}
              onViewDocumentById={onViewDocumentById}
              onViewDriftSignal={onViewDriftSignal}
            />
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
