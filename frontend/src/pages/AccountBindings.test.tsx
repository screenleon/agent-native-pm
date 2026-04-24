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
    expect(screen.getByRole('heading', { level: 2, name: /Server-side CLI Bindings/i })).toBeInTheDocument()
  })

  // T-S3-2: render in server mode → info card shown, no CLI form
  it('T-S3-2: shows info card in server mode, hides CLI form', async () => {
    await renderAndWait([], false)
    expect(screen.getByText(/only available in local mode/i)).toBeInTheDocument()
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
  })

  // T-S3-3: render with empty CLI bindings → form auto-expanded (no extra click needed)
  it('T-S3-3: auto-expands CLI binding form when no CLI bindings exist', async () => {
    await renderAndWait([], true)
    expect(screen.getByText('New CLI Binding')).toBeInTheDocument()
  })

  // T-S3-4: render with one CLI binding + local mode → binding card shown, form collapsed
  it('T-S3-4: shows existing CLI binding card and form is collapsed by default', async () => {
    await renderAndWait([makeCliBinding()], true)
    expect(screen.getByText('My Claude')).toBeInTheDocument()
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
  })

  // T-S3-5: submit form with valid Claude binding → POST fires with expected payload; UI refreshes
  it('T-S3-5: submits CLI binding form and fires POST with expected payload', async () => {
    mockCreateAccountBinding.mockResolvedValue({ data: makeCliBinding() })
    mockGetMeta.mockResolvedValue(localMeta())
    mockListAccountBindings
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValue({ data: [makeCliBinding()] })

    render(<MemoryRouter><AccountBindings /></MemoryRouter>)
    await waitFor(() => {
      // Form auto-expands when no CLI bindings (T-S3-3)
      expect(screen.getByText('New CLI Binding')).toBeInTheDocument()
    })

    const modelInput = screen.getByPlaceholderText('claude-sonnet-4-5')
    await userEvent.clear(modelInput)
    await userEvent.type(modelInput, 'claude-sonnet-4-5')

    await userEvent.click(screen.getByRole('button', { name: /^Create$/i }))

    await waitFor(() => {
      expect(mockCreateAccountBinding).toHaveBeenCalledWith(
        expect.objectContaining({
          provider_id: 'cli:claude',
          base_url: '',
          model_id: 'claude-sonnet-4-5',
        }),
      )
    })
    // UI refreshes: binding card appears, form collapses
    await waitFor(() => {
      expect(screen.getByText('My Claude')).toBeInTheDocument()
    })
  })

  // T-S3-6: submit form with empty model_id → submit blocked (HTML5 required attr)
  it('T-S3-6: submit is blocked when model_id is empty (required field)', async () => {
    await renderAndWait([], true)
    // Form auto-expands with no CLI bindings (T-S3-3); no extra click needed
    expect(screen.getByText('New CLI Binding')).toBeInTheDocument()

    const modelInput = screen.getByPlaceholderText('claude-sonnet-4-5')
    expect(modelInput).toHaveAttribute('required')
    expect((modelInput as HTMLInputElement).value).toBe('')

    expect(mockCreateAccountBinding).not.toHaveBeenCalled()
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

  // T-S3-9: switching preset to a provider that already has a binding → is_primary unchecked
  it('T-S3-9: switching to a preset whose provider already has a binding unchecks is_primary', async () => {
    const codexBinding = makeCliBinding({ id: 'cb-codex', provider_id: 'cli:codex', label: 'My Codex' })
    await renderAndWait([codexBinding], true)

    // Open form manually (bindings exist, form is collapsed)
    await userEvent.click(screen.getByRole('button', { name: /\+ Add another CLI binding/i }))
    await waitFor(() => expect(screen.getByText('New CLI Binding')).toBeInTheDocument())

    // Default preset is Claude Code — no existing claude binding → checkbox checked
    const checkbox = screen.getByRole('checkbox', { name: /primary/i }) as HTMLInputElement
    expect(checkbox.checked).toBe(true)

    // Switch to Codex preset — codex binding exists → checkbox should uncheck
    await userEvent.click(screen.getByRole('button', { name: /OpenAI Codex CLI/i }))
    await waitFor(() => expect(checkbox.checked).toBe(false))
  })

  // T-S3-10: after canceling the "+ Add another" form, a subsequent load() triggered by a
  // real UI action (e.g. "Switch to this binding") must NOT re-open the form.
  it('T-S3-10: form stays closed after Cancel when load() is re-triggered by a UI action', async () => {
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

    // One binding exists so form is collapsed; open it manually.
    await userEvent.click(screen.getByRole('button', { name: /\+ Add another CLI binding/i }))
    await waitFor(() => expect(screen.getByText('New CLI Binding')).toBeInTheDocument())

    // Cancel — sets userDismissedCliForm.current = true.
    await userEvent.click(screen.getByRole('button', { name: /Cancel/i }))
    await waitFor(() => {
      expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: /\+ Add another CLI binding/i })).toBeInTheDocument()
    })

    // Trigger a real load() call via "Switch to this binding".
    await userEvent.click(screen.getByRole('button', { name: /Switch to this binding/i }))

    await waitFor(() => {
      expect(mockUpdateAccountBinding).toHaveBeenCalledWith('cb2', { is_primary: true })
      expect(mockListAccountBindings).toHaveBeenCalledTimes(2)
    })

    // Form must remain closed even after the reload triggered by the UI action.
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /\+ Add another CLI binding/i })).toBeInTheDocument()
  })

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
