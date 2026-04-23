import type { PlanningRun, Requirement } from '../../types'

type StepState = 'done' | 'active' | 'pending' | 'attention'

interface Step {
  key: string
  label: string
  hint: string
  state: StepState
  onClick?: () => void
}

interface PlanningStepperProps {
  requirementCount: number
  selectedRequirement: Requirement | null
  selectedPlanningRun: PlanningRun | null
  candidateCount: number
  onJumpToIntake: () => void
  onJumpToWorkspace: () => void
  onJumpToCandidates: () => void
}

export function PlanningStepper({
  requirementCount,
  selectedRequirement,
  selectedPlanningRun,
  candidateCount,
  onJumpToIntake,
  onJumpToWorkspace,
  onJumpToCandidates,
}: PlanningStepperProps) {
  const hasRequirement = !!selectedRequirement
  const hasRun = !!selectedPlanningRun
  const runStatus = selectedPlanningRun?.status ?? null
  const runActive = runStatus === 'queued' || runStatus === 'running'
  const runFailed = runStatus === 'failed' || runStatus === 'cancelled'
  const runDone = runStatus === 'completed'
  const hasCandidates = candidateCount > 0
  const allCandidatesReviewed = false

  const steps: Step[] = [
    {
      key: 'capture',
      label: '1. Capture requirement',
      hint: requirementCount === 0
        ? 'Start by writing what the system should do.'
        : `${requirementCount} requirement${requirementCount === 1 ? '' : 's'} captured.`,
      state: requirementCount > 0 ? 'done' : 'active',
      onClick: onJumpToIntake,
    },
    {
      key: 'select',
      label: '2. Select & start a run',
      hint: hasRequirement
        ? hasRun
          ? `Run ${runStatus}`
          : 'Pick a requirement and start a planning run.'
        : 'Select a requirement to start.',
      state: hasRun ? 'done' : (hasRequirement ? 'active' : 'pending'),
      onClick: onJumpToWorkspace,
    },
    {
      key: 'wait',
      label: '3. Wait for results',
      hint: !hasRun
        ? 'Results will appear once the run completes.'
        : runActive
          ? selectedPlanningRun?.execution_mode === 'local_connector'
            ? 'Waiting for local connector to return ranked candidates.'
            : 'Server is generating draft candidates.'
          : runFailed
            ? selectedPlanningRun?.error_message || selectedPlanningRun?.dispatch_error || 'Run failed or was cancelled.'
            : runDone
              ? `${candidateCount} candidate${candidateCount === 1 ? '' : 's'} ready.`
              : 'Run results pending.',
      state: !hasRun
        ? 'pending'
        : runActive
          ? 'active'
          : runFailed
            ? 'attention'
            : 'done',
    },
    {
      key: 'review',
      label: '4. Review & apply candidates',
      hint: !hasRun
        ? 'Approve the candidates you want to materialize as tasks.'
        : !hasCandidates
          ? runDone ? 'No candidates produced — adjust requirement and re-run.' : 'Candidates appear after the run completes.'
          : allCandidatesReviewed
            ? 'All candidates reviewed.'
            : `${candidateCount} candidate${candidateCount === 1 ? '' : 's'} awaiting decision.`,
      state: !hasRun
        ? 'pending'
        : !hasCandidates
          ? runDone ? 'attention' : 'pending'
          : 'active',
      onClick: hasCandidates ? onJumpToCandidates : undefined,
    },
  ]

  return (
    <ol className="planning-stepper" aria-label="Planning workflow steps">
      {steps.map(step => (
        <li key={step.key} className={`planning-step is-${step.state}`}>
          {step.onClick ? (
            <button type="button" className="planning-step-btn" onClick={step.onClick}>
              <PlanningStepBody label={step.label} hint={step.hint} state={step.state} />
            </button>
          ) : (
            <div className="planning-step-btn" aria-disabled="true">
              <PlanningStepBody label={step.label} hint={step.hint} state={step.state} />
            </div>
          )}
        </li>
      ))}
    </ol>
  )
}

function PlanningStepBody({ label, hint, state }: { label: string; hint: string; state: StepState }) {
  const icon = state === 'done' ? '✓' : state === 'attention' ? '!' : state === 'active' ? '●' : '○'
  return (
    <>
      <span className="planning-step-icon" aria-hidden="true">{icon}</span>
      <span className="planning-step-text">
        <span className="planning-step-label">{label}</span>
        <span className="planning-step-hint">{hint}</span>
      </span>
    </>
  )
}

export default PlanningStepper
