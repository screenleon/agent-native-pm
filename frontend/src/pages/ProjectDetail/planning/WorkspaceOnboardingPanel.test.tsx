import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { WorkspaceOnboardingPanel } from './WorkspaceOnboardingPanel'

vi.mock('../../../api/client', () => ({
  createRequirement: vi.fn(),
  getPlanningProviderOptions: vi.fn(),
  createPlanningRun: vi.fn(),
  demoSeed: vi.fn(),
}))

import { createRequirement, getPlanningProviderOptions, createPlanningRun } from '../../../api/client'

const createReqMock = createRequirement as ReturnType<typeof vi.fn>
const getProvMock = getPlanningProviderOptions as ReturnType<typeof vi.fn>
const createRunMock = createPlanningRun as ReturnType<typeof vi.fn>

function makeProviderOptions(canRun: boolean) {
  return {
    data: {
      can_run: canRun,
      default_selection: canRun ? { provider_id: 'openai', model_id: 'gpt-4o', selection_source: 'server_default' } : null,
      providers: [],
      credential_mode: 'shared',
      available_execution_modes: ['server_provider'],
      paired_connector_available: false,
      allow_model_override: false,
    },
  }
}

function renderPanel(overrides: Partial<React.ComponentProps<typeof WorkspaceOnboardingPanel>> = {}) {
  const props = {
    projectId: 'p1',
    onRunCreated: vi.fn(),
    planningRunsCount: 0,
    ...overrides,
  }
  return render(
    <MemoryRouter>
      <WorkspaceOnboardingPanel {...props} />
    </MemoryRouter>,
  )
}

describe('<WorkspaceOnboardingPanel />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('T-6a-A1-1: renders input and primary action button', () => {
    renderPanel({ planningRunsCount: 1 })
    expect(screen.getByLabelText(/What are you building/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Generate backlog/i })).toBeInTheDocument()
  })

  it('T-6a-A1-3: when provider has no usable selection → button disabled and link visible', async () => {
    const { default: userEvent } = await import('@testing-library/user-event')
    createReqMock.mockResolvedValue({ data: { id: 'r1' } })
    getProvMock.mockResolvedValue(makeProviderOptions(false))
    renderPanel({ planningRunsCount: 1 })

    const input = screen.getByLabelText(/What are you building/i)
    await userEvent.type(input, 'My feature')
    await userEvent.click(screen.getByRole('button', { name: /Generate backlog/i }))

    await waitFor(() => {
      expect(screen.getByRole('link', { name: /Set one up/i })).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /Generate backlog/i })).toBeDisabled()
  })

  it('T-6a-A1-5: after run created, calls onRunCreated callback', async () => {
    const { default: userEvent } = await import('@testing-library/user-event')
    const onRunCreated = vi.fn()
    createReqMock.mockResolvedValue({ data: { id: 'req-1' } })
    getProvMock.mockResolvedValue(makeProviderOptions(true))
    createRunMock.mockResolvedValue({ data: { id: 'run-1' } })

    renderPanel({ planningRunsCount: 1, onRunCreated })

    const input = screen.getByLabelText(/What are you building/i)
    await userEvent.type(input, 'My feature')
    await userEvent.click(screen.getByRole('button', { name: /Generate backlog/i }))

    await waitFor(() => {
      expect(onRunCreated).toHaveBeenCalledWith('req-1', 'run-1')
    })
  })
})
