import type { Requirement, Task } from '../../types'
import { PlanningStepper } from './PlanningStepper'
import { AttentionRow } from './planning/AttentionRow'
import { RequirementIntake } from './planning/RequirementIntake'
import { RequirementQueue } from './planning/RequirementQueue'
import { PlanningLauncher } from './planning/PlanningLauncher'
import { PlanningRunList } from './planning/PlanningRunList'
import { CandidateReviewPanel } from './planning/CandidateReviewPanel'
import { AppliedLineage } from './planning/AppliedLineage'
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
        onRunWhatsnext={ws.onRunWhatsnext}
        runningWhatsnext={ws.runningWhatsnext}
        whatsnextReady={ws.planningRunReady}
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
          onArchiveRequirement={ws.onArchiveRequirement}
          archivingRequirementId={ws.archivingRequirementId}
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
              cliBindings={ws.cliBindings}
              cliBindingsLoading={ws.cliBindingsLoading}
              selectedCliBindingId={ws.selectedCliBindingId}
              onCliBindingChange={ws.onCliBindingChange}
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

            <AppliedLineage
              projectId={projectId}
              reloadSignal={lineageReloadSignal}
              onSelectLineage={(requirementId, runId, candidateId) => {
                ws.onSelectLineage(requirementId, runId, candidateId)
                // Scroll priority: candidate > requirement. If the click
                // carried a candidate id we want the review panel visible;
                // otherwise the requirement queue is the correct landing.
                if (candidateId) {
                  jumpToCandidates()
                } else {
                  jumpToRequirements()
                }
              }}
              onJumpToTasks={onNavigateToTasks}
            />
          </div>
        ) : (
          <div style={{ padding: '1.5rem 0.5rem', display: 'flex', flexDirection: 'column', gap: '1rem', alignItems: 'flex-start' }}>
            <div>
              <h4 style={{ margin: '0 0 0.35rem' }}>Start here</h4>
              <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                Select a requirement above to plan a specific feature, or run a full project health check to surface the most urgent open work across tasks, drift signals, and stale docs.
              </p>
            </div>
            {ws.planningRunReady ? (
              <button
                type="button"
                className="btn btn-primary"
                onClick={ws.onRunWhatsnext}
                disabled={ws.runningWhatsnext}
              >
                {ws.runningWhatsnext ? 'Starting analysis…' : "Run What's Next — full project health check"}
              </button>
            ) : (
              <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                Configure a planning provider in Model Settings or connect a local connector to enable health-check runs.
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
