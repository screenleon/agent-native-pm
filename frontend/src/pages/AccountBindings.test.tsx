import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import type { AccountBinding } from '../types'
import AccountBindings from './AccountBindings'

const mockListAccountBindings = vi.fn()
const mockCreateAccountBinding = vi.fn()
const mockUpdateAccountBinding = vi.fn()
const mockDeleteAccountBinding = vi.fn()
const mockGetMeta = vi.fn()
const mockListLocalConnectors = vi.fn()
const mockProbeBindingOnConnector = vi.fn()
const mockGetCliProbeResult = vi.fn()
const mockFetchRemoteModels = vi.fn()
const mockProbeModel = vi.fn()

vi.mock('../api/client', () => ({
  listAccountBindings: (...args: unknown[]) => mockListAccountBindings(...args),
  createAccountBinding: (...args: unknown[]) => mockCreateAccountBinding(...args),
  updateAccountBinding: (...args: unknown[]) => mockUpdateAccountBinding(...args),
  deleteAccountBinding: (...args: unknown[]) => mockDeleteAccountBinding(...args),
  getMeta: (...args: unknown[]) => mockGetMeta(...args),
  listLocalConnectors: (...args: unknown[]) => mockListLocalConnectors(...args),
  probeBindingOnConnector: (...args: unknown[]) => mockProbeBindingOnConnector(...args),
  getCliProbeResult: (...args: unknown[]) => mockGetCliProbeResult(...args),
  fetchRemoteModels: (...args: unknown[]) => mockFetchRemoteModels(...args),
  probeModel: (...args: unknown[]) => mockProbeModel(...args),
}))

function makeCliBinding(overrides: Partial<AccountBinding> = {}): AccountBinding {
  return {
    id: 'cb1',
    user_id: 'u1',
    provider_id: 'cli:claude',
    label: 'My Claude',
    base_url: '',
    model_id: 'claude-sonnet-4-5',
    configured_models: [],
    api_key_configured: false,
    is_active: true,
    cli_command: 'claude',
    is_primary: true,
    created_at: '2026-04-23T00:00:00Z',
    updated_at: '2026-04-23T00:00:00Z',
    last_probe_at: null,
    last_probe_ok: null,
    last_probe_ms: null,
    ...overrides,
  }
}

function localMeta() {
  return { data: { local_mode: true, project_id: 'p1', project_name: 'agent-native-pm', port: '3100' } }
}

function serverMeta() {
  return { data: { local_mode: false, project_id: '', project_name: '', port: '8080' } }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockCreateAccountBinding.mockResolvedValue({ data: makeCliBinding() })
  mockUpdateAccountBinding.mockResolvedValue({ data: makeCliBinding() })
  mockDeleteAccountBinding.mockResolvedValue({ data: null })
})

async function renderAndWait(bindings: AccountBinding[] = [], isLocal = true) {
  mockListAccountBindings.mockResolvedValue({ data: bindings })
  mockGetMeta.mockResolvedValue(isLocal ? localMeta() : serverMeta())

  render(<MemoryRouter><AccountBindings /></MemoryRouter>)

  await waitFor(() => {
    expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
  })
}

describe('<AccountBindings />', () => {
  // T-S3-1: render in local mode → CLI Bindings heading visible
  it('T-S3-1: shows CLI Bindings heading in local mode', async () => {
    await renderAndWait([], true)
    expect(screen.getByRole('heading', { level: 2, name: /CLI Bindings/i })).toBeInTheDocument()
  })

  // T-S3-2: render in server mode → info card shown, no CLI form
  it('T-S3-2: shows info card in server mode, hides CLI form', async () => {
    await renderAndWait([], false)
    expect(screen.getByText(/only available in local mode/i)).toBeInTheDocument()
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
  })

  // T-S3-4: render with one CLI binding + local mode → binding card shown
  it('T-S3-4: shows existing legacy CLI binding card', async () => {
    await renderAndWait([makeCliBinding()], true)
    expect(screen.getByText('My Claude')).toBeInTheDocument()
  })

  // Phase 6a UX-A7: the legacy create flow is disabled. These assertions
  // replace the deprecated T-S3-3 / T-S3-5 / T-S3-6 tests that covered
  // auto-expand and form-submit of the CLI binding create form — those
  // paths are intentionally gone. New CLI configs live per-connector
  // under MyConnector.
  it('T-S3-3: no CLI create form is rendered (legacy section)', async () => {
    await renderAndWait([], true)
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /\+ Add CLI Binding/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /\+ Add another CLI binding/i })).not.toBeInTheDocument()
  })

  it('T-S3-5: legacy section shows the "CLI configuration has moved" banner', async () => {
    await renderAndWait([], true)
    expect(screen.getByText(/CLI configuration has moved/i)).toBeInTheDocument()
  })

  // T-S3-7: delete binding → DELETE fires → binding removed from list
  it('T-S3-7: delete fires DELETE and removes binding from list', async () => {
    mockDeleteAccountBinding.mockResolvedValue({ data: null })
    mockGetMeta.mockResolvedValue(localMeta())
    mockListAccountBindings
      .mockResolvedValueOnce({ data: [makeCliBinding()] })
      .mockResolvedValue({ data: [] })

    vi.spyOn(window, 'confirm').mockReturnValue(true)

    render(<MemoryRouter><AccountBindings /></MemoryRouter>)
    await waitFor(() => {
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: /Delete/i }))

    await waitFor(() => {
      expect(mockDeleteAccountBinding).toHaveBeenCalledWith('cb1')
    })
    await waitFor(() => {
      expect(screen.queryByText('My Claude')).not.toBeInTheDocument()
    })
  })

  // T-S3-8: "Switch to this binding" button → PATCH fires with { is_primary: true }
  it('T-S3-8: Switch to this binding button fires PATCH with is_primary true', async () => {
    const nonPrimaryBinding = makeCliBinding({ id: 'cb2', is_primary: false, label: 'Old Claude' })
    mockUpdateAccountBinding.mockResolvedValue({ data: { ...nonPrimaryBinding, is_primary: true } })
    mockGetMeta.mockResolvedValue(localMeta())
    mockListAccountBindings
      .mockResolvedValueOnce({ data: [nonPrimaryBinding] })
      .mockResolvedValue({ data: [{ ...nonPrimaryBinding, is_primary: true }] })

    render(<MemoryRouter><AccountBindings /></MemoryRouter>)
    await waitFor(() => {
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: /Switch to this binding/i }))

    await waitFor(() => {
      expect(mockUpdateAccountBinding).toHaveBeenCalledWith('cb2', { is_primary: true })
    })
  })

  // Phase 6a UX-A7 removed the legacy T-S3-9 and T-S3-10 tests: both
  // exercised preset switching + form re-expansion on the deprecated
  // create flow. Those paths no longer exist. Switch-to-this-binding
  // (Set Primary) is still covered by T-S3-8 above.

  // ── P4-3 (CLI binding inline Edit) ────────────────────────────────────────

  // T-P4-3-1: Edit opens an inline form prefilled with current values.
  it('T-P4-3-1: Edit opens inline form prefilled', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-edit', label: 'My Claude', model_id: 'claude-sonnet-4-5', cli_command: '/usr/bin/claude' })], true)

    await userEvent.click(screen.getByRole('button', { name: /^Edit$/i }))
    expect(screen.getByDisplayValue('My Claude')).toBeInTheDocument()
    expect(screen.getByDisplayValue('claude-sonnet-4-5')).toBeInTheDocument()
    expect(screen.getByDisplayValue('/usr/bin/claude')).toBeInTheDocument()
  })

  // T-P4-3-4: blocks save when model_id is cleared.
  it('T-P4-3-4: save blocked when model_id is empty', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-edit' })], true)
    await userEvent.click(screen.getByRole('button', { name: /^Edit$/i }))

    const modelInput = screen.getByDisplayValue('claude-sonnet-4-5')
    await userEvent.clear(modelInput)
    await userEvent.click(screen.getByRole('button', { name: /^Save$/i }))

    expect(screen.getByText(/Model ID is required/i)).toBeInTheDocument()
    expect(mockUpdateAccountBinding).not.toHaveBeenCalled()
  })

  // T-P4-3-2: happy-path save fires PATCH with the edited model_id.
  it('T-P4-3-2: save fires PATCH with edited values', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-edit' })], true)
    await userEvent.click(screen.getByRole('button', { name: /^Edit$/i }))

    const modelInput = screen.getByDisplayValue('claude-sonnet-4-5')
    await userEvent.clear(modelInput)
    await userEvent.type(modelInput, 'claude-sonnet-4-6')
    await userEvent.click(screen.getByRole('button', { name: /^Save$/i }))

    await waitFor(() => {
      expect(mockUpdateAccountBinding).toHaveBeenCalledWith('cb-edit', expect.objectContaining({
        model_id: 'claude-sonnet-4-6',
      }))
    })
  })

  // T-P4-3-3: Cancel collapses the form without an API call.
  it('T-P4-3-3: Cancel collapses the edit form', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-edit' })], true)
    await userEvent.click(screen.getByRole('button', { name: /^Edit$/i }))
    await userEvent.click(screen.getByRole('button', { name: /^Cancel$/i }))

    expect(screen.queryByRole('button', { name: /^Save$/i })).not.toBeInTheDocument()
    expect(mockUpdateAccountBinding).not.toHaveBeenCalled()
  })

  // ── P4-4 (Test on connector) ──────────────────────────────────────────────

  // T-P4-4-12 (frontend portion): no online connector → friendly error, no enqueue.
  it('T-P4-4-12: no online connector surfaces helpful error', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-probe' })], true)
    mockListLocalConnectors.mockResolvedValue({ data: [] })

    await userEvent.click(screen.getByRole('button', { name: /Test on connector/i }))
    await waitFor(() => {
      expect(screen.getByText(/No online connector/i)).toBeInTheDocument()
    })
    expect(mockProbeBindingOnConnector).not.toHaveBeenCalled()
  })

  // T-P4-4-12 (frontend happy path): probe enqueues, polls, renders the
  // completed result once the poll sees status=completed.
  it('T-P4-4-12: probe enqueues, polls, renders completed result', async () => {
    await renderAndWait([makeCliBinding({ id: 'cb-probe' })], true)
    mockListLocalConnectors.mockResolvedValue({ data: [{ id: 'conn-1', status: 'online', label: 'Laptop' }] })
    mockProbeBindingOnConnector.mockResolvedValue({ data: { probe_id: 'probe-42' } })
    // First poll: pending. Second poll: completed.
    mockGetCliProbeResult
      .mockResolvedValueOnce({ data: { status: 'pending' } })
      .mockResolvedValue({ data: { status: 'completed', result: { probe_id: 'probe-42', ok: true, latency_ms: 123, content: 'ok', completed_at: '2026-04-24T00:00:00Z' } } })

    await userEvent.click(screen.getByRole('button', { name: /Test on connector/i }))

    await waitFor(() => {
      expect(screen.getByText(/Connector replied in 123 ms/i)).toBeInTheDocument()
    }, { timeout: 10_000 })
    expect(mockProbeBindingOnConnector).toHaveBeenCalledWith('conn-1', 'cb-probe')
  })
})
