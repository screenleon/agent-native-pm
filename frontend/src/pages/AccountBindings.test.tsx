import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AccountBinding } from '../types'
import AccountBindings from './AccountBindings'

const mockListAccountBindings = vi.fn()
const mockCreateAccountBinding = vi.fn()
const mockUpdateAccountBinding = vi.fn()
const mockDeleteAccountBinding = vi.fn()
const mockGetMeta = vi.fn()

vi.mock('../api/client', () => ({
  listAccountBindings: (...args: unknown[]) => mockListAccountBindings(...args),
  createAccountBinding: (...args: unknown[]) => mockCreateAccountBinding(...args),
  updateAccountBinding: (...args: unknown[]) => mockUpdateAccountBinding(...args),
  deleteAccountBinding: (...args: unknown[]) => mockDeleteAccountBinding(...args),
  getMeta: (...args: unknown[]) => mockGetMeta(...args),
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

  render(<AccountBindings />)

  await waitFor(() => {
    expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
  })
}

describe('<AccountBindings />', () => {
  // T-S3-1: render in local mode → CLI Bindings heading visible
  it('T-S3-1: shows CLI Bindings heading in local mode', async () => {
    await renderAndWait([], true)
    expect(screen.getByText('CLI Bindings')).toBeInTheDocument()
  })

  // T-S3-2: render in server mode → info card shown, no CLI form
  it('T-S3-2: shows info card in server mode, hides CLI form', async () => {
    await renderAndWait([], false)
    expect(screen.getByText(/only available in local mode/i)).toBeInTheDocument()
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
  })

  // T-S3-3: render with empty CLI bindings + local mode → CLI add form shown
  it('T-S3-3: shows Add CLI Binding button with no existing CLI bindings', async () => {
    await renderAndWait([], true)
    expect(screen.getByRole('button', { name: /\+ Add CLI Binding/i })).toBeInTheDocument()
  })

  // T-S3-4: render with one CLI binding + local mode → binding card shown, form collapsed
  it('T-S3-4: shows existing CLI binding card and form is collapsed by default', async () => {
    await renderAndWait([makeCliBinding()], true)
    expect(screen.getByText('My Claude')).toBeInTheDocument()
    expect(screen.queryByText('New CLI Binding')).not.toBeInTheDocument()
  })

  // T-S3-5: submit form with valid Claude binding → POST fires with expected payload
  it('T-S3-5: submits CLI binding form and fires POST with expected payload', async () => {
    mockCreateAccountBinding.mockResolvedValue({ data: makeCliBinding() })
    mockGetMeta.mockResolvedValue(localMeta())
    mockListAccountBindings
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValue({ data: [makeCliBinding()] })

    render(<AccountBindings />)
    await waitFor(() => {
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: /\+ Add CLI Binding/i }))
    await waitFor(() => {
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
  })

  // T-S3-6: submit form with empty model_id → submit blocked (HTML5 required attr)
  it('T-S3-6: submit is blocked when model_id is empty (required field)', async () => {
    await renderAndWait([], true)

    await userEvent.click(screen.getByRole('button', { name: /\+ Add CLI Binding/i }))
    await waitFor(() => {
      expect(screen.getByText('New CLI Binding')).toBeInTheDocument()
    })

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

    render(<AccountBindings />)
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

  // T-S3-8: "Set as Primary" button → PATCH fires with { is_primary: true }
  it('T-S3-8: Set as Primary button fires PATCH with is_primary true', async () => {
    const nonPrimaryBinding = makeCliBinding({ id: 'cb2', is_primary: false, label: 'Old Claude' })
    mockUpdateAccountBinding.mockResolvedValue({ data: { ...nonPrimaryBinding, is_primary: true } })
    mockGetMeta.mockResolvedValue(localMeta())
    mockListAccountBindings
      .mockResolvedValueOnce({ data: [nonPrimaryBinding] })
      .mockResolvedValue({ data: [{ ...nonPrimaryBinding, is_primary: true }] })

    render(<AccountBindings />)
    await waitFor(() => {
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: /Set as Primary/i }))

    await waitFor(() => {
      expect(mockUpdateAccountBinding).toHaveBeenCalledWith('cb2', { is_primary: true })
    })
  })
})
