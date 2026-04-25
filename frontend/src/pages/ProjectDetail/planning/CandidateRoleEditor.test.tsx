import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { CandidateRoleEditor } from './CandidateRoleEditor'
import type { BacklogCandidate } from '../../../types'
import type { RoleInfo } from '../../../api/client'

function makeCandidate(overrides: Partial<BacklogCandidate> = {}): BacklogCandidate {
  return {
    id: 'cand-1',
    project_id: 'proj-1',
    requirement_id: 'req-1',
    planning_run_id: 'run-1',
    parent_candidate_id: '',
    suggestion_type: 'feature',
    title: 'A',
    description: 'desc',
    rationale: '',
    validation_criteria: '',
    status: 'draft',
    po_decision: '',
    priority_score: 1,
    confidence: 1,
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
        confidence_seed: 0,
        evidence_bonus: 0,
        duplicate_penalty: 0,
        final_priority_score: 0,
        final_confidence: 0,
      },
    },
    duplicate_titles: [],
    execution_role: null,
    created_at: '2026-04-26T00:00:00Z',
    updated_at: '2026-04-26T00:00:00Z',
    ...overrides,
  }
}

const roles: RoleInfo[] = [
  { id: 'backend-architect', title: 'Backend Architect', version: 1, use_case: 'Backend things', default_timeout_sec: 5400, category: 'role' },
  { id: 'code-reviewer', title: 'Code Reviewer', version: 1, use_case: 'Reviews', default_timeout_sec: 900, category: 'role' },
]

describe('<CandidateRoleEditor />', () => {
  it('shows "No role set" when execution_role is null', () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    render(<CandidateRoleEditor candidate={makeCandidate()} availableRoles={roles} onUpdateRole={onUpdate} />)
    expect(screen.getByText(/no role set/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /set role/i })).toBeInTheDocument()
  })

  it('shows the catalog title when role is set and known', () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    render(
      <CandidateRoleEditor
        candidate={makeCandidate({ execution_role: 'backend-architect' })}
        availableRoles={roles}
        onUpdateRole={onUpdate}
      />,
    )
    expect(screen.getByText(/Role: Backend Architect/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument()
  })

  it('shows stale-warning chip when execution_role is not in catalog', () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    render(
      <CandidateRoleEditor
        candidate={makeCandidate({ execution_role: 'no-longer-in-catalog' })}
        availableRoles={roles}
        onUpdateRole={onUpdate}
      />,
    )
    expect(screen.getByText(/Stale role: no-longer-in-catalog/)).toBeInTheDocument()
  })

  it('opens editor on click and saves the chosen role', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    render(<CandidateRoleEditor candidate={makeCandidate()} availableRoles={roles} onUpdateRole={onUpdate} />)
    fireEvent.click(screen.getByRole('button', { name: /set role/i }))
    const select = screen.getByLabelText(/set candidate execution role/i)
    fireEvent.change(select, { target: { value: 'code-reviewer' } })
    fireEvent.click(screen.getByRole('button', { name: /^save/i }))
    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith('code-reviewer'))
  })

  it('disables Save when nothing changed', () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    render(
      <CandidateRoleEditor
        candidate={makeCandidate({ execution_role: 'backend-architect' })}
        availableRoles={roles}
        onUpdateRole={onUpdate}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /edit/i }))
    const save = screen.getByRole('button', { name: /^save/i })
    expect(save).toBeDisabled()
  })

  it('closes editor on Cancel without calling onUpdateRole', () => {
    const onUpdate = vi.fn()
    render(<CandidateRoleEditor candidate={makeCandidate()} availableRoles={roles} onUpdateRole={onUpdate} />)
    fireEvent.click(screen.getByRole('button', { name: /set role/i }))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onUpdate).not.toHaveBeenCalled()
    expect(screen.getByText(/no role set/i)).toBeInTheDocument()
  })

  it('hides edit button when disabled', () => {
    const onUpdate = vi.fn()
    render(<CandidateRoleEditor candidate={makeCandidate()} availableRoles={roles} onUpdateRole={onUpdate} disabled />)
    expect(screen.queryByRole('button', { name: /set role/i })).not.toBeInTheDocument()
  })
})
