import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { Document, DriftSignal } from '../types'
import { DriftTab } from './DriftTab'

vi.mock('../api/client', () => ({
  getDocumentContent: vi.fn().mockResolvedValue({ data: null }),
}))

function makeSignal(overrides: Partial<DriftSignal> = {}): DriftSignal {
  return {
    id: 'ds-1',
    project_id: 'p1',
    document_id: null,
    document_title: 'API surface',
    trigger_type: 'code_change',
    trigger_detail: 'Files changed: backend/handlers/api.go (M)',
    severity: 2,
    status: 'open',
    created_at: '2026-04-22T10:00:00Z',
    updated_at: '2026-04-22T10:00:00Z',
    resolution_note: '',
    ...(overrides as Partial<DriftSignal>),
  } as DriftSignal
}

const baseProps = {
  documents: [] as Document[],
  documentLinksByDocumentId: {} as Record<string, never>,
  documentLinkLoadErrors: {} as Record<string, boolean>,
  onViewDoc: vi.fn(),
  onManageLinks: vi.fn(),
  onResolveDrift: vi.fn().mockResolvedValue(undefined),
  onDismissDrift: vi.fn().mockResolvedValue(undefined),
  onBulkResolveDrift: vi.fn().mockResolvedValue(undefined),
}

describe('<DriftTab />', () => {
  it('renders the empty state when there are no drift signals at all', () => {
    render(<DriftTab {...baseProps} driftSignals={[]} />)
    expect(screen.getByText(/No drift signals/i)).toBeInTheDocument()
  })

  it('renders the "no signals in this filter" state when every signal is filtered out', () => {
    render(<DriftTab {...baseProps} driftSignals={[makeSignal({ status: 'resolved' })]} />)
    expect(screen.getByText(/No signals in this filter/i)).toBeInTheDocument()
  })

  it('renders the card list and shows "Resolve All Open" when open signals exist', () => {
    render(<DriftTab {...baseProps} driftSignals={[makeSignal()]} />)
    // Title renders in both the card list and the auto-selected detail pane
    expect(screen.getAllByText('API surface').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /Resolve All Open/i })).toBeInTheDocument()
    expect(screen.getAllByText(/Medium/i).length).toBeGreaterThan(0)
  })
})
