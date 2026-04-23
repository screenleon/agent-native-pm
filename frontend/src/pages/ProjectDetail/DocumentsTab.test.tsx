import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { Document, DriftSignal } from '../../types'
import { DocumentsTab } from './DocumentsTab'

function makeDoc(overrides: Partial<Document> = {}): Document {
  return {
    id: 'd1',
    project_id: 'p1',
    title: 'API surface',
    file_path: 'docs/api-surface.md',
    doc_type: 'api',
    last_updated_at: '2026-04-22T00:00:00Z',
    staleness_days: 0,
    is_stale: false,
    source: 'human',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

const baseProps = {
  projectId: 'p1',
  driftSignals: [] as DriftSignal[],
  documentLinksByDocumentId: {} as Record<string, never>,
  documentLinkLoadErrors: {} as Record<string, boolean>,
  onReload: vi.fn(),
  onError: vi.fn(),
  onViewDoc: vi.fn(),
  onManageLinks: vi.fn(),
  onDeleteDoc: vi.fn().mockResolvedValue(undefined),
  onMarkUpdated: vi.fn().mockResolvedValue(undefined),
}

describe('<DocumentsTab />', () => {
  it('renders the empty state when no documents are registered', () => {
    render(<DocumentsTab {...baseProps} documents={[]} />)
    expect(screen.getByText(/No documents registered/i)).toBeInTheDocument()
  })

  it('renders a row per document with its title and file path', () => {
    render(<DocumentsTab {...baseProps} documents={[makeDoc(), makeDoc({ id: 'd2', title: 'Guide', file_path: '' })]} />)
    expect(screen.getByText('API surface')).toBeInTheDocument()
    expect(screen.getByText('docs/api-surface.md')).toBeInTheDocument()
    expect(screen.getByText('Guide')).toBeInTheDocument()
  })

  it('disables the View action when a document has no file_path', () => {
    render(<DocumentsTab {...baseProps} documents={[makeDoc({ file_path: '' })]} />)
    expect(screen.getByRole('button', { name: /^View$/i })).toBeDisabled()
  })

  it('shows Mark as Updated only for stale documents', () => {
    render(<DocumentsTab {...baseProps} documents={[
      makeDoc({ id: 'd1', is_stale: true }),
      makeDoc({ id: 'd2', is_stale: false }),
    ]} />)
    expect(screen.getByRole('button', { name: /Mark as Updated/i })).toBeInTheDocument()
    // Only one button — the non-stale doc must not render it
    expect(screen.getAllByRole('button', { name: /Mark as Updated/i })).toHaveLength(1)
  })

  it('calls onMarkUpdated with the document id when clicked', async () => {
    const onMarkUpdated = vi.fn().mockResolvedValue(undefined)
    const { default: userEvent } = await import('@testing-library/user-event')
    render(<DocumentsTab {...baseProps} documents={[makeDoc({ id: 'd1', is_stale: true })]} onMarkUpdated={onMarkUpdated} />)
    await userEvent.click(screen.getByRole('button', { name: /Mark as Updated/i }))
    expect(onMarkUpdated).toHaveBeenCalledWith('d1')
  })
})
