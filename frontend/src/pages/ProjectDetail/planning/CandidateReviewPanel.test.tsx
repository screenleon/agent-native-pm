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
        final_confidence: 0, confidence_seed: 0,
      },
    },
    duplicate_titles: [],
    execution_role: null,
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
    onSkipCandidate: vi.fn(),
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
    expect(screen.getByRole('button', { name: /Apply/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Skip/i })).toBeInTheDocument()
  })

  it('disables Apply to Tasks until canApplySelectedCandidate is true', () => {
    renderPanel({ canApplySelectedCandidate: false })
    expect(screen.getByRole('button', { name: /^Apply$/i })).toBeDisabled()
  })

  it('renders document evidence as a clickable link when onViewDocumentById is provided and the document has an id', async () => {
    const onViewDocumentById = vi.fn()
    const candidateWithDoc = makeCandidate({
      evidence_detail: {
        summary: [],
        documents: [{
          document_id: 'doc-42',
          title: 'API surface',
          file_path: 'docs/api-surface.md',
          doc_type: 'api',
          is_stale: false,
          staleness_days: 0,
          matched_keywords: [],
          contribution_reasons: [],
        }],
        drift_signals: [],
        sync_run: null,
        agent_runs: [],
        duplicates: [],
        score_breakdown: {
          impact: 0, urgency: 0, dependency_unlock: 0, risk_reduction: 0,
          effort: 0, evidence_bonus: 0, duplicate_penalty: 0,
          final_priority_score: 0, final_confidence: 0, confidence_seed: 0,
        },
      },
    } as Partial<BacklogCandidate>)
    renderPanel({ candidates: [candidateWithDoc], selectedCandidate: candidateWithDoc, onViewDocumentById })
    const { default: userEvent } = await import('@testing-library/user-event')
    const link = screen.getByRole('button', { name: /Open document preview for API surface/i })
    await userEvent.click(link)
    expect(onViewDocumentById).toHaveBeenCalledWith('doc-42')
  })

  it('renders document evidence as plain text when onViewDocumentById is not provided', () => {
    const candidateWithDoc = makeCandidate({
      evidence_detail: {
        summary: [],
        documents: [{
          document_id: 'doc-42',
          title: 'API surface',
          file_path: 'docs/api-surface.md',
          doc_type: 'api',
          is_stale: false,
          staleness_days: 0,
          matched_keywords: [],
          contribution_reasons: [],
        }],
        drift_signals: [],
        sync_run: null,
        agent_runs: [],
        duplicates: [],
        score_breakdown: {
          impact: 0, urgency: 0, dependency_unlock: 0, risk_reduction: 0,
          effort: 0, evidence_bonus: 0, duplicate_penalty: 0,
          final_priority_score: 0, final_confidence: 0, confidence_seed: 0,
        },
      },
    } as Partial<BacklogCandidate>)
    renderPanel({ candidates: [candidateWithDoc], selectedCandidate: candidateWithDoc })
    // API surface title is present, but not inside a link-shaped button
    expect(screen.getByText('API surface')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Open document preview/i })).not.toBeInTheDocument()
  })

  it('shows "Suggested Backlog" header for regular planning runs', () => {
    renderPanel({ selectedRun: makeRun() })
    expect(screen.getByRole('heading', { name: /Suggested Backlog/i })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /Suggested Focus Areas/i })).not.toBeInTheDocument()
  })

  it('shows "Suggested Focus Areas" header when adapter_type is whatsnext', () => {
    const whatsnextRun = { ...makeRun(), adapter_type: 'whatsnext' } as unknown as import('../../../types').PlanningRun
    renderPanel({ selectedRun: whatsnextRun })
    expect(screen.getByRole('heading', { name: /Suggested Focus Areas/i })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /Suggested Backlog/i })).not.toBeInTheDocument()
  })

  it('T-S5a-6: renders remediation hint banner for a failed returned run with a known error_kind, and exposes no free-text hint input', () => {
    const failedRun = {
      ...makeRun(),
      status: 'failed',
      dispatch_status: 'returned',
      connector_cli_info: {
        error_kind: 'session_expired',
        remediation_hint: 'Re-authenticate your CLI (run `claude` or `codex` once interactively) then retry the planning run.',
      },
    } as unknown as import('../../../types').PlanningRun
    renderPanel({ selectedRun: failedRun, candidates: [], selectedCandidate: null, selectedCandidateId: null })
    expect(screen.getByText(/Suggested next step:/i)).toBeInTheDocument()
    expect(screen.getByText(/Re-authenticate your CLI/i)).toBeInTheDocument()
    // The hint must not be editable — no input or textarea for the hint text.
    const inputs = document.querySelectorAll('input, textarea')
    for (const el of inputs) {
      expect(el).not.toHaveValue('Re-authenticate your CLI (run `claude` or `codex` once interactively) then retry the planning run.')
    }
  })

  it('fires onViewDriftSignal with the drift signal id when the drift evidence row is clicked', async () => {
    const onViewDriftSignal = vi.fn()
    const candidateWithDrift = makeCandidate({
      evidence_detail: {
        summary: [],
        documents: [],
        drift_signals: [{
          drift_signal_id: 'ds-7',
          document_id: 'doc-42',
          document_title: 'API surface',
          trigger_detail: 'Files changed: api.go (M)',
          trigger_type: 'code_change',
          severity: 2,
          contribution_reasons: [],
        }],
        sync_run: null,
        agent_runs: [],
        duplicates: [],
        score_breakdown: {
          impact: 0, urgency: 0, dependency_unlock: 0, risk_reduction: 0,
          effort: 0, evidence_bonus: 0, duplicate_penalty: 0,
          final_priority_score: 0, final_confidence: 0, confidence_seed: 0,
        },
      },
    } as Partial<BacklogCandidate>)
    renderPanel({ candidates: [candidateWithDrift], selectedCandidate: candidateWithDrift, onViewDriftSignal })
    const { default: userEvent } = await import('@testing-library/user-event')
    await userEvent.click(screen.getByRole('button', { name: /Jump to drift signal API surface/i }))
    expect(onViewDriftSignal).toHaveBeenCalledWith('ds-7')
  })

  // ── Phase 5 B3 ──────────────────────────────────────────────────────────

  // T-P5-B3-6: execution_role chip appears when the selected candidate
  // has an execution_role set.
  it('renders the execution_role chip when candidate.execution_role is set', () => {
    const candidate = makeCandidate({ execution_role: 'backend-architect' })
    renderPanel({ candidates: [candidate], selectedCandidate: candidate })
    expect(screen.getByText(/Role: backend-architect/i)).toBeInTheDocument()
  })

  it('omits the execution_role chip when candidate.execution_role is null', () => {
    renderPanel()
    expect(screen.queryByText(/Role: /i)).not.toBeInTheDocument()
  })

  // T-P5-B3-7 (updated Phase 6b): Manual / Auto-dispatch radio group renders
  // only when the onSelectedExecutionModeChange callback is provided.
  // Auto-dispatch is disabled when the selected candidate has no execution_role.
  it('renders the Manual + Auto-dispatch radio group only when onSelectedExecutionModeChange is provided', () => {
    const onChange = vi.fn()
    renderPanel({ selectedExecutionMode: 'manual', onSelectedExecutionModeChange: onChange })
    expect(screen.getByLabelText(/Manual/i)).toBeInTheDocument()
    // Phase 6b: "coming in Phase 6" text removed; radio is dynamically enabled by execution_role.
    expect(screen.queryByText(/coming in Phase 6/i)).not.toBeInTheDocument()
    // Auto-dispatch radio exists but is disabled when candidate has no execution_role (default fixture).
    const autoRadio = screen.getByRole('radio', { name: /Auto-dispatch/i })
    expect(autoRadio).toBeInTheDocument()
  })

  // Phase 6c PR-2: Auto-dispatch radio is ALWAYS enabled (catch-22
  // resolved). The role choice now travels in the apply payload, so
  // the radio no longer gates on candidate.execution_role being
  // pre-set. Replaces the Phase 6b "disabled when no role" test.
  it('keeps the Auto-dispatch radio enabled regardless of candidate.execution_role (Phase 6c PR-2)', () => {
    const onChange = vi.fn()
    renderPanel({ selectedExecutionMode: 'manual', onSelectedExecutionModeChange: onChange })
    const autoRadio = screen.getByRole('radio', { name: /Auto-dispatch/i })
    expect(autoRadio).not.toBeDisabled()
  })

  it('hides the execution mode radio group when onSelectedExecutionModeChange is not wired', () => {
    renderPanel()
    // No radio group rendered when callback not provided.
    expect(screen.queryByRole('radio', { name: /Auto-dispatch/i })).not.toBeInTheDocument()
  })

  // Phase 6c PR-2 T-6c-C1-F2 / F3: when role_dispatch is selected,
  // an inline role <select> appears, populated from availableRoles.
  it('renders the role <select> with version + timeout label when role_dispatch is chosen (Phase 6c PR-2)', () => {
    const onChange = vi.fn()
    const onRoleChange = vi.fn()
    const roles = [
      { id: 'backend-architect', title: 'Backend Architect', version: 1, use_case: 'BE', default_timeout_sec: 5400, category: 'role' as const },
      { id: 'code-reviewer', title: 'Code Reviewer', version: 1, use_case: 'CR', default_timeout_sec: 900, category: 'role' as const },
    ]
    renderPanel({
      selectedExecutionMode: 'role_dispatch',
      onSelectedExecutionModeChange: onChange,
      chosenExecutionRole: '',
      onChosenExecutionRoleChange: onRoleChange,
      availableRoles: roles,
    })
    const select = screen.getByLabelText(/select execution role/i)
    expect(select).toBeInTheDocument()
    // Option labels include the timeout estimate ("預估 N min" → 90 / 15 min).
    expect(screen.getByRole('option', { name: /Backend Architect.*v1.*90 min/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Code Reviewer.*v1.*15 min/i })).toBeInTheDocument()
  })

  // Phase 6c PR-2 T-6c-C1-F4: Apply button is disabled when
  // role_dispatch is selected but no role chosen yet.
  it('disables Apply when role_dispatch is selected without a chosen role (Phase 6c PR-2)', () => {
    const onChange = vi.fn()
    const onRoleChange = vi.fn()
    renderPanel({
      selectedExecutionMode: 'role_dispatch',
      onSelectedExecutionModeChange: onChange,
      chosenExecutionRole: '',
      onChosenExecutionRoleChange: onRoleChange,
      availableRoles: [],
      // canApplySelectedCandidate must be true so the Apply button
      // wouldn't be disabled by the legacy gate; we want to prove the
      // Phase 6c PR-2 gate fires independently.
      canApplySelectedCandidate: true,
    })
    const applyBtn = screen.getByRole('button', { name: /^Apply$/ })
    expect(applyBtn).toBeDisabled()
  })

  // Phase 6c PR-2 T-6c-C1-F4 (positive): Apply enabled when role chosen.
  it('enables Apply when role_dispatch + chosen role is set (Phase 6c PR-2)', () => {
    const onChange = vi.fn()
    const onRoleChange = vi.fn()
    renderPanel({
      selectedExecutionMode: 'role_dispatch',
      onSelectedExecutionModeChange: onChange,
      chosenExecutionRole: 'backend-architect',
      onChosenExecutionRoleChange: onRoleChange,
      availableRoles: [
        { id: 'backend-architect', title: 'Backend Architect', version: 1, use_case: '', default_timeout_sec: 5400, category: 'role' as const },
      ],
      canApplySelectedCandidate: true,
    })
    const applyBtn = screen.getByRole('button', { name: /^Apply$/ })
    expect(applyBtn).not.toBeDisabled()
  })

  // Phase 6c PR-2 T-6c-C1-F7: stale-role warning fires when the
  // candidate has an execution_role no longer in the catalog.
  it('shows stale-role warning when candidate.execution_role is not in catalog (Phase 6c PR-2)', () => {
    const onChange = vi.fn()
    const onRoleChange = vi.fn()
    renderPanel({
      selectedCandidate: makeCandidate({ execution_role: 'no-longer-exists' }),
      selectedExecutionMode: 'role_dispatch',
      onSelectedExecutionModeChange: onChange,
      chosenExecutionRole: '',
      onChosenExecutionRoleChange: onRoleChange,
      availableRoles: [
        { id: 'backend-architect', title: 'Backend Architect', version: 1, use_case: '', default_timeout_sec: 5400, category: 'role' as const },
      ],
    })
    expect(screen.getByText(/no longer in the catalog/i)).toBeInTheDocument()
  })
})
