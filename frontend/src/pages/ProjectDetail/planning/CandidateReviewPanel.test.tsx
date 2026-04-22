import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { BacklogCandidate, PlanningRun } from '../../../types'
import { CandidateReviewPanel } from './CandidateReviewPanel'

function makeRun(): PlanningRun {
  return {
    id: 'run-1',
    project_id: 'p1',
    requirement_id: 'r1',
    status: 'completed',
    trigger_source: 'manual',
    provider_id: 'deterministic',
    model_id: 'deterministic-v1',
    selection_source: 'server_default',
    binding_source: 'shared',
    binding_label: '',
    execution_mode: 'server_provider',
    dispatch_status: 'none',
    dispatch_error: '',
    dispatch_expires_at: null,
    connector_id: null,
    connector_label: '',
    connector_cli_info: null,
    adapter_type: '',
    model_override: '',
    requested_by_user_id: 'u1',
    error_message: '',
    started_at: null,
    completed_at: null,
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
  } as unknown as PlanningRun
}

function makeCandidate(overrides: Partial<BacklogCandidate> = {}): BacklogCandidate {
  return {
    id: 'c1',
    project_id: 'p1',
    requirement_id: 'r1',
    planning_run_id: 'run-1',
    parent_candidate_id: null,
    suggestion_type: 'implementation',
    title: 'Persist recovery options',
    description: 'Expose the recovery options inline on the Sync panel.',
    status: 'draft',
    rationale: '',
    validation_criteria: '',
    po_decision: '',
    priority_score: 75,
    confidence: 80,
    rank: 1,
    evidence: [],
    evidence_detail: {
      summary: [],
      documents: [],
      drift_signals: [],
      sync_run: null,
      agent_runs: [],
      duplicates: [],
      score_breakdown: {
        impact: 0,
        urgency: 0,
        dependency_unlock: 0,
        risk_reduction: 0,
        effort: 0,
        evidence_bonus: 0,
        duplicate_penalty: 0,
        final_priority_score: 0,
        final_confidence: 0,
      },
    },
    duplicate_titles: [],
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...(overrides as Partial<BacklogCandidate>),
  } as BacklogCandidate
}

function renderPanel(overrides: Partial<React.ComponentProps<typeof CandidateReviewPanel>> = {}) {
  const base: React.ComponentProps<typeof CandidateReviewPanel> = {
    selectedRun: makeRun(),
    candidates: [makeCandidate()],
    candidatesLoading: false,
    candidatesError: null,
    selectedCandidate: makeCandidate(),
    selectedCandidateId: 'c1',
    onSelectCandidate: vi.fn(),
    candidateForm: { title: 'Persist recovery options', description: 'Expose the recovery options inline on the Sync panel.', status: 'draft' },
    onCandidateFormChange: vi.fn(),
    candidateFormDirty: false,
    selectedCandidateApplied: false,
    canApplySelectedCandidate: false,
    savingCandidate: false,
    applyingCandidate: false,
    candidateReviewError: null,
    candidateReviewMessage: null,
    candidateDuplicateTitles: [],
    runFlash: null,
    onDismissRunFlash: vi.fn(),
    providerOptions: null,
    onPersistReview: vi.fn(),
    onApplyCandidate: vi.fn(),
    onResetCandidateForm: vi.fn(),
  }
  return {
    props: base,
    ...render(<CandidateReviewPanel {...base} {...overrides} />),
  }
}

describe('<CandidateReviewPanel />', () => {
  it('renders the "no planning run selected" empty state when selectedRun is null', () => {
    renderPanel({ selectedRun: null, candidates: [], selectedCandidate: null, selectedCandidateId: null })
    expect(screen.getByText(/No planning run selected/i)).toBeInTheDocument()
  })

  it('renders the "no backlog yet" empty state when the run has no candidates', () => {
    renderPanel({ candidates: [], selectedCandidate: null, selectedCandidateId: null })
    expect(screen.getByText(/No suggested backlog yet/i)).toBeInTheDocument()
  })

  it('renders a candidate card + detail panel when candidates are present', () => {
    renderPanel()
    // Title appears in both the list and the detail form input
    expect(screen.getAllByText('Persist recovery options').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /Approve/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Reject/i })).toBeInTheDocument()
  })

  it('disables Apply to Tasks until canApplySelectedCandidate is true', () => {
    renderPanel({ canApplySelectedCandidate: false })
    expect(screen.getByRole('button', { name: /Apply To Tasks/i })).toBeDisabled()
  })
})
