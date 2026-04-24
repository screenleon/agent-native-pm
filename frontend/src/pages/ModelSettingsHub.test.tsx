import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import ModelSettingsHub from './ModelSettingsHub'

vi.mock('../api/client', () => ({
  getMeta: vi.fn().mockResolvedValue({ data: { local_mode: false } }),
  listAccountBindings: vi.fn(),
  updateAccountBinding: vi.fn(),
  listLocalConnectors: vi.fn(),
  listConnectorCliConfigs: vi.fn(),
}))

import {
  listAccountBindings,
  updateAccountBinding,
  listLocalConnectors,
  listConnectorCliConfigs,
} from '../api/client'

const listBindingsMock = listAccountBindings as ReturnType<typeof vi.fn>
const updateMock = updateAccountBinding as ReturnType<typeof vi.fn>
const listConnectorsMock = listLocalConnectors as ReturnType<typeof vi.fn>
const listCliConfigsMock = listConnectorCliConfigs as ReturnType<typeof vi.fn>

function makeBinding(overrides: Record<string, unknown> = {}) {
  return {
    id: 'b1',
    user_id: 'u1',
    provider_id: 'openai',
    label: 'My OpenAI',
    base_url: '',
    model_id: 'gpt-4o',
    configured_models: [],
    api_key_configured: true,
    is_active: true,
    cli_command: '',
    is_primary: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    last_probe_at: null,
    last_probe_ok: null,
    last_probe_ms: null,
    ...overrides,
  }
}

function makeConnector(overrides: Record<string, unknown> = {}) {
  return {
    id: 'c1',
    user_id: 'u1',
    label: 'My Laptop',
    platform: 'darwin',
    client_version: '1.0.0',
    status: 'online',
    capabilities: {},
    last_seen_at: '2026-01-01T00:00:00Z',
    last_error: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeCliConfig(overrides: Record<string, unknown> = {}) {
  return {
    id: 'cfg1',
    provider_id: 'cli:claude',
    cli_command: 'claude',
    model_id: 'claude-3-5-sonnet',
    label: 'Claude Code',
    is_primary: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderHub() {
  return render(
    <MemoryRouter>
      <ModelSettingsHub />
    </MemoryRouter>,
  )
}

describe('<ModelSettingsHub />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: no connectors, no bindings
    listConnectorsMock.mockResolvedValue({ data: [] })
    listCliConfigsMock.mockResolvedValue({ data: [] })
    listBindingsMock.mockResolvedValue({ data: [] })
  })

  // ── Existing tests (preserved) ────────────────────────────────────────────

  it('T-6a-A6-1: user with one primary API binding shows Primary badge', async () => {
    listBindingsMock.mockResolvedValue({ data: [makeBinding({ is_primary: true })] })
    renderHub()
    await waitFor(() => {
      expect(screen.getAllByText('Primary').length).toBeGreaterThan(0)
    })
  })

  it('T-6a-A6-2: user with non-primary binding → click Make primary → updateAccountBinding called', async () => {
    const binding = makeBinding({ id: 'b99', is_primary: false })
    listBindingsMock.mockResolvedValue({ data: [binding] })
    updateMock.mockResolvedValue({ data: {} })
    const { default: userEvent } = await import('@testing-library/user-event')
    renderHub()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Make primary/i })).toBeInTheDocument()
    })
    await userEvent.click(screen.getByRole('button', { name: /Make primary/i }))
    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('b99', { is_primary: true })
    })
  })

  // ── Direction A banner tests ───────────────────────────────────────────────

  it('T-A6-banner-c: online connector with CLI config → banner shows "Option C"', async () => {
    const connector = makeConnector({ id: 'c1', label: 'My Laptop', status: 'online' })
    const cliConfig = makeCliConfig({ label: 'Claude Code' })
    listConnectorsMock.mockResolvedValue({ data: [connector] })
    listCliConfigsMock.mockResolvedValue({ data: [cliConfig] })
    listBindingsMock.mockResolvedValue({ data: [] })
    renderHub()
    await waitFor(() => {
      // Banner paragraph contains "Option C (Your Machine's CLI)"
      expect(screen.getByText(/Your setup: Option C/i)).toBeInTheDocument()
    })
    expect(screen.getByText(/My Laptop is online/i)).toBeInTheDocument()
  })

  it('T-A6-banner-a: active API binding, no online connector with config → banner shows "Option A"', async () => {
    const binding = makeBinding({ is_active: true, label: 'My OpenAI', model_id: 'gpt-4o' })
    listBindingsMock.mockResolvedValue({ data: [binding] })
    listConnectorsMock.mockResolvedValue({ data: [] })
    renderHub()
    await waitFor(() => {
      // Banner paragraph contains "Option A (API Key)"
      expect(screen.getByText(/Your setup: Option A/i)).toBeInTheDocument()
    })
    // Banner shows binding label and model
    expect(screen.getByText(/My OpenAI · gpt-4o/i)).toBeInTheDocument()
  })

  it('T-A6-banner-none: no setup at all → "Not sure where to start?" guidance shown', async () => {
    listBindingsMock.mockResolvedValue({ data: [] })
    listConnectorsMock.mockResolvedValue({ data: [] })
    renderHub()
    await waitFor(() => {
      expect(screen.getByText(/Not sure where to start\?/i)).toBeInTheDocument()
    })
    expect(screen.getByText(/Claude Code or Codex/i)).toBeInTheDocument()
  })

  // ── Direction C connector list test ───────────────────────────────────────

  it('T-A6-connector-list: Option C card shows connector name and CLI config label', async () => {
    const connector = makeConnector({ id: 'c1', label: 'My Workstation', status: 'online' })
    const cliConfig = makeCliConfig({ label: 'Codex CLI' })
    listConnectorsMock.mockResolvedValue({ data: [connector] })
    listCliConfigsMock.mockResolvedValue({ data: [cliConfig] })
    listBindingsMock.mockResolvedValue({ data: [] })
    renderHub()
    await waitFor(() => {
      expect(screen.getByText('My Workstation')).toBeInTheDocument()
    })
    // Verify the CLI configs label row under the connector
    expect(screen.getByText(/CLI configs: Codex CLI/i)).toBeInTheDocument()
  })

  // ── Direction D: removed static "Still unsure?" footer ───────────────────

  it('T-A6-d-no-footer: "Still unsure?" block is not shown when setup exists', async () => {
    const binding = makeBinding({ is_active: true })
    listBindingsMock.mockResolvedValue({ data: [binding] })
    listConnectorsMock.mockResolvedValue({ data: [] })
    renderHub()
    await waitFor(() => {
      expect(screen.getByText(/Option A/i)).toBeInTheDocument()
    })
    expect(screen.queryByText(/Not sure where to start\?/i)).not.toBeInTheDocument()
  })
})
