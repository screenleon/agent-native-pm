import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { Requirement } from '../types'
import { PlanningTab } from './PlanningTab'

vi.mock('../api/client', () => ({
  createRequirement: vi.fn().mockResolvedValue({ data: null }),
  getPlanningProviderOptions: vi.fn().mockResolvedValue({
    data: {
      providers: [],
      default_provider: 'deterministic',
      remote_provider_enabled: false,
      remote_provider_id: '',
    },
  }),
  listPlanningRuns: vi.fn().mockResolvedValue({ data: { runs: [] } }),
  createPlanningRun: vi.fn().mockResolvedValue({ data: null }),
  cancelPlanningRun: vi.fn().mockResolvedValue({ data: null }),
  listPlanningRunBacklogCandidates: vi.fn().mockResolvedValue({ data: { candidates: [] } }),
  updateBacklogCandidate: vi.fn().mockResolvedValue({ data: null }),
  applyBacklogCandidate: vi.fn().mockResolvedValue({ data: null }),
}))

vi.mock('./PlanningStepper', () => ({
  PlanningStepper: () => null,
}))

function makeRequirement(overrides: Partial<Requirement> = {}): Requirement {
  return {
    id: 'req-1',
    project_id: 'p1',
    title: 'Auto-decompose requirements into backlog',
    summary: '',
    description: '',
    status: 'draft',
    source: 'user',
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

const baseProps = {
  projectId: 'p1',
  tasks: [],
  planningLoadError: null as string | null,
  onReload: vi.fn().mockResolvedValue(undefined),
  onError: vi.fn(),
  onSuccess: vi.fn(),
  onRequirementsChange: vi.fn(),
}

function renderPlanningTab(props: React.ComponentProps<typeof PlanningTab>) {
  return render(
    <MemoryRouter>
      <PlanningTab {...props} />
    </MemoryRouter>,
  )
}

describe('<PlanningTab />', () => {
  it('renders without crashing for an empty project', () => {
    renderPlanningTab({ ...baseProps, requirements: [] })
    // Anchor: some part of the requirement-intake form / empty section always renders
    expect(document.body.textContent?.length ?? 0).toBeGreaterThan(0)
  })

  it('renders the requirement title when one exists', () => {
    renderPlanningTab({ ...baseProps, requirements: [makeRequirement()] })
    // Title may surface both in queue and in auto-selected detail view
    expect(screen.getAllByText('Auto-decompose requirements into backlog').length).toBeGreaterThan(0)
  })

  it('surfaces the planningLoadError banner when provided', () => {
    renderPlanningTab({ ...baseProps, requirements: [], planningLoadError: 'planning index unavailable' })
    expect(screen.getByText(/planning index unavailable/i)).toBeInTheDocument()
  })
})
