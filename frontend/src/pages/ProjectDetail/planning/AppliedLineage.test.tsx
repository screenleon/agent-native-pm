import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppliedLineage } from './AppliedLineage'

const mockListProjectTaskLineage = vi.fn()

vi.mock('../../../api/client', () => ({
  listProjectTaskLineage: (...args: unknown[]) => mockListProjectTaskLineage(...args),
}))

function sampleEntry(overrides: Record<string, unknown> = {}) {
  return {
    lineage_id: 'tl-1',
    project_id: 'p1',
    task_id: 't-1',
    task_title: 'Land the recovery feature',
    task_status: 'in_progress',
    requirement_id: 'req-1',
    requirement_title: 'Improve sync failure UX',
    planning_run_id: 'run-1',
    planning_run_status: 'completed',
    backlog_candidate_id: 'c-1',
    backlog_candidate_title: 'Persist recovery options',
    lineage_kind: 'applied_candidate',
    created_at: '2026-04-22T10:00:00Z',
    ...overrides,
  }
}

describe('<AppliedLineage />', () => {
  beforeEach(() => {
    mockListProjectTaskLineage.mockReset()
  })

  it('renders the empty state when the API returns zero entries', async () => {
    mockListProjectTaskLineage.mockResolvedValue({ data: [] })
    render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => {
      expect(screen.getByText(/No applied candidates yet/i)).toBeInTheDocument()
    })
  })

  it('renders an error banner when the API rejects', async () => {
    mockListProjectTaskLineage.mockRejectedValue(new Error('boom'))
    render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => {
      expect(screen.getByText(/boom/i)).toBeInTheDocument()
    })
  })

  it('renders the lineage chain and task count when entries are present', async () => {
    mockListProjectTaskLineage.mockResolvedValue({ data: [sampleEntry()] })
    render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => {
      expect(screen.getByTestId('applied-lineage-list')).toBeInTheDocument()
    })
    expect(screen.getByText('Improve sync failure UX')).toBeInTheDocument()
    expect(screen.getByText(/run completed/i)).toBeInTheDocument()
    expect(screen.getByText('Persist recovery options')).toBeInTheDocument()
    expect(screen.getByText('Land the recovery feature')).toBeInTheDocument()
    expect(screen.getByText(/1 traceable/i)).toBeInTheDocument()
  })

  it('fires onJumpToRequirement with the requirement id when the requirement link is clicked', async () => {
    mockListProjectTaskLineage.mockResolvedValue({ data: [sampleEntry()] })
    const onJumpToRequirement = vi.fn()
    render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={onJumpToRequirement}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Open requirement Improve sync failure UX/i })).toBeInTheDocument()
    })
    await userEvent.click(screen.getByRole('button', { name: /Open requirement Improve sync failure UX/i }))
    expect(onJumpToRequirement).toHaveBeenCalledWith('req-1')
  })

  it('fires onJumpToTasks when the task link is clicked', async () => {
    mockListProjectTaskLineage.mockResolvedValue({ data: [sampleEntry()] })
    const onJumpToTasks = vi.fn()
    render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={onJumpToTasks}
      />,
    )
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Open task Land the recovery feature/i })).toBeInTheDocument()
    })
    await userEvent.click(screen.getByRole('button', { name: /Open task Land the recovery feature/i }))
    expect(onJumpToTasks).toHaveBeenCalledTimes(1)
  })

  it('refetches when reloadSignal changes', async () => {
    mockListProjectTaskLineage.mockResolvedValue({ data: [] })
    const { rerender } = render(
      <AppliedLineage
        projectId="p1"
        reloadSignal={0}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => expect(mockListProjectTaskLineage).toHaveBeenCalledTimes(1))
    rerender(
      <AppliedLineage
        projectId="p1"
        reloadSignal={1}
        onJumpToRequirement={vi.fn()}
        onJumpToTasks={vi.fn()}
      />,
    )
    await waitFor(() => expect(mockListProjectTaskLineage).toHaveBeenCalledTimes(2))
  })
})
