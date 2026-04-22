import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { PlanningProviderOptions, Requirement } from '../../../types'
import { PlanningLauncher } from './PlanningLauncher'

function makeRequirement(overrides: Partial<Requirement> = {}): Requirement {
  return {
    id: 'r1',
    project_id: 'p1',
    title: 'Improve sync failure UX',
    summary: '',
    description: '',
    status: 'draft',
    source: 'human',
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

function renderLauncher(overrides: Partial<React.ComponentProps<typeof PlanningLauncher>> = {}) {
  const base: React.ComponentProps<typeof PlanningLauncher> = {
    selectedRequirement: makeRequirement(),
    providerOptions: null,
    providerOptionsLoading: false,
    providerOptionsError: null,
    executionMode: 'server_provider',
    onExecutionModeChange: vi.fn(),
    localAdapterType: 'backlog',
    onLocalAdapterTypeChange: vi.fn(),
    localModelOverride: '',
    onLocalModelOverrideChange: vi.fn(),
    planningModelOverride: '',
    onPlanningModelOverrideChange: vi.fn(),
    creatingRun: false,
    runningWhatsnext: false,
    runsLoading: false,
    runReady: true,
    runBlockedReason: null,
    onStartRun: vi.fn(),
    onRefreshRuns: vi.fn(),
    onRunWhatsnext: vi.fn(),
  }
  return {
    props: base,
    ...render(
      <MemoryRouter>
        <PlanningLauncher {...base} {...overrides} />
      </MemoryRouter>,
    ),
  }
}

describe('<PlanningLauncher />', () => {
  it('renders the requirement header and start / refresh buttons', () => {
    renderLauncher()
    expect(screen.getByText('Improve sync failure UX')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Start Planning Run/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Refresh Runs/i })).toBeInTheDocument()
  })

  it('disables Start Planning Run when runReady is false', () => {
    renderLauncher({ runReady: false })
    expect(screen.getByRole('button', { name: /Start Planning Run/i })).toBeDisabled()
  })

  it('surfaces the connector-offline state when executionMode is local_connector without pairing', () => {
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: false,
      active_connector_label: null,
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: false })
    expect(screen.getByText(/No live connector/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Go to My Connector/i })).toBeInTheDocument()
  })

  it('fires onStartRun when the Start button is clicked and runReady is true', async () => {
    const onStartRun = vi.fn()
    const { default: userEvent } = await import('@testing-library/user-event')
    renderLauncher({ onStartRun })
    await userEvent.click(screen.getByRole('button', { name: /Start Planning Run/i }))
    expect(onStartRun).toHaveBeenCalledTimes(1)
  })
})
