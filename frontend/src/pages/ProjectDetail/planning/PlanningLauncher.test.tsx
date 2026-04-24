import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { AccountBinding, PlanningProviderOptions, Requirement } from '../../../types'
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

function makeBinding(overrides: Partial<AccountBinding> = {}): AccountBinding {
  return {
    id: 'b1',
    user_id: 'u1',
    provider_id: 'cli:claude',
    label: 'My Claude',
    base_url: '',
    model_id: 'claude-sonnet-4-6',
    configured_models: [],
    api_key_configured: false,
    is_active: true,
    cli_command: 'claude',
    is_primary: true,
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    last_probe_at: null,
    last_probe_ok: null,
    last_probe_ms: null,
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
    cliBindings: [],
    cliBindingsLoading: false,
    selectedCliBindingId: null,
    onCliBindingChange: vi.fn(),
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
  beforeEach(() => {
    localStorage.clear()
  })

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
    localStorage.setItem('anpm_launcher_advanced_open', '1')
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

  it('shows the What\'s Next button when local connector is online', () => {
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true })
    expect(screen.getByRole('button', { name: /Run What's Next/i })).toBeInTheDocument()
  })

  it('fires onRunWhatsnext when the What\'s Next button is clicked', async () => {
    const onRunWhatsnext = vi.fn()
    const { default: userEvent } = await import('@testing-library/user-event')
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true, onRunWhatsnext })
    await userEvent.click(screen.getByRole('button', { name: /Run What's Next/i }))
    expect(onRunWhatsnext).toHaveBeenCalledTimes(1)
  })

  it('shows "Starting…" and disables What\'s Next when runningWhatsnext is true', () => {
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true, runningWhatsnext: true })
    const btn = screen.getByRole('button', { name: /Starting/i })
    expect(btn).toBeDisabled()
  })

  it('shows "No CLI binding configured" when cliBindings is empty and connector is online', () => {
    localStorage.setItem('anpm_launcher_advanced_open', '1')
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true, cliBindings: [], selectedCliBindingId: null })
    expect(screen.getByText(/No CLI binding configured/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Set up a CLI binding/i })).toBeInTheDocument()
  })

  it('shows CLI binding select with correct options when bindings exist', () => {
    localStorage.setItem('anpm_launcher_advanced_open', '1')
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    const binding = makeBinding({ id: 'b1', label: 'My Claude', model_id: 'claude-sonnet-4-6', is_primary: true })
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true, cliBindings: [binding], selectedCliBindingId: 'b1' })
    expect(screen.getByText('My Claude [claude-sonnet-4-6] (primary)')).toBeInTheDocument()
    expect(screen.getByLabelText(/CLI binding for this run/i)).toBeInTheDocument()
  })

  it('calls onCliBindingChange when binding is changed', async () => {
    localStorage.setItem('anpm_launcher_advanced_open', '1')
    const onCliBindingChange = vi.fn()
    const { default: userEvent } = await import('@testing-library/user-event')
    const providerOptions = {
      providers: [],
      default_selection: null,
      available_execution_modes: ['server_provider', 'local_connector'],
      paired_connector_available: true,
      active_connector_label: 'My Machine',
      credential_mode: 'shared',
      allow_model_override: false,
      can_run: true,
      unavailable_reason: '',
      resolved_binding_source: 'shared',
      resolved_binding_label: '',
    } as unknown as PlanningProviderOptions
    const binding1 = makeBinding({ id: 'b1', label: 'My Claude', model_id: 'claude-sonnet-4-6', is_primary: true })
    const binding2 = makeBinding({ id: 'b2', label: 'My Codex', model_id: 'codex-mini-latest', is_primary: false, provider_id: 'cli:codex' })
    renderLauncher({ providerOptions, executionMode: 'local_connector', runReady: true, cliBindings: [binding1, binding2], selectedCliBindingId: 'b1', onCliBindingChange })
    const bindingSelect = screen.getByLabelText(/CLI binding for this run/i)
    await userEvent.selectOptions(bindingSelect, 'b2')
    expect(onCliBindingChange).toHaveBeenCalledWith('b2')
  })

  it('T-6a-A2-1: default render hides advanced controls', () => {
    renderLauncher()
    expect(screen.queryByLabelText(/Execution mode/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/CLI binding for this run/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/Model override for this run/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Advanced/i })).toBeInTheDocument()
  })

  it('T-6a-A2-2: click Advanced shows controls and persists localStorage key', async () => {
    const { default: userEvent } = await import('@testing-library/user-event')
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
    renderLauncher({ providerOptions })
    await userEvent.click(screen.getByRole('button', { name: /Advanced/i }))
    expect(localStorage.getItem('anpm_launcher_advanced_open')).toBe('1')
    expect(screen.getByLabelText(/Execution mode/i)).toBeInTheDocument()
  })

  it('T-6a-A2-3: re-mount with localStorage 1 shows controls', () => {
    localStorage.setItem('anpm_launcher_advanced_open', '1')
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
    renderLauncher({ providerOptions })
    expect(screen.getByLabelText(/Execution mode/i)).toBeInTheDocument()
  })
})
