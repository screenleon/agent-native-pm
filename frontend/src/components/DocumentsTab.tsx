import { useState } from 'react'
import type { Document, DriftSignal, DocumentLink } from '../types'
import { createDocument } from '../api/client'

interface DocumentsTabProps {
  projectId: string
  documents: Document[]
  driftSignals: DriftSignal[]
  documentLinksByDocumentId: Record<string, DocumentLink[]>
  documentLinkLoadErrors: Record<string, boolean>
  onReload: () => void
  onError: (msg: string) => void
  onViewDoc: (doc: Document) => void
  onManageLinks: (doc: Document) => void
  onDeleteDoc: (docId: string) => Promise<void>
}

export function DocumentsTab({
  projectId,
  documents,
  driftSignals,
  documentLinksByDocumentId,
  documentLinkLoadErrors,
  onReload,
  onError,
  onViewDoc,
  onManageLinks,
  onDeleteDoc,
}: DocumentsTabProps) {
  const [showDocForm, setShowDocForm] = useState(false)
  const [docForm, setDocForm] = useState({ title: '', file_path: '', doc_type: 'general' as Document['doc_type'], source: 'human' })

  async function handleCreateDoc(e: React.FormEvent) {
    e.preventDefault()
    if (!docForm.title.trim()) return
    try {
      await createDocument(projectId, docForm)
      setDocForm({ title: '', file_path: '', doc_type: 'general', source: 'human' })
      setShowDocForm(false)
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to create document')
    }
  }

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '1rem' }}>
        <button className="btn btn-primary" onClick={() => setShowDocForm(true)}>+ Register Document</button>
      </div>

      {showDocForm && (
        <div className="modal-overlay" onClick={() => setShowDocForm(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Register Document</h3>
            <form onSubmit={handleCreateDoc}>
              <div className="form-group">
                <label>Title *</label>
                <input value={docForm.title} onChange={e => setDocForm({ ...docForm, title: e.target.value })} autoFocus />
              </div>
              <div className="form-group">
                <label>File Path</label>
                <input value={docForm.file_path} onChange={e => setDocForm({ ...docForm, file_path: e.target.value })} placeholder="docs/api-surface.md" />
              </div>
              <div className="form-group">
                <label>Type</label>
                <select value={docForm.doc_type} onChange={e => setDocForm({ ...docForm, doc_type: e.target.value as Document['doc_type'] })}>
                  <option value="general">General</option>
                  <option value="api">API</option>
                  <option value="architecture">Architecture</option>
                  <option value="guide">Guide</option>
                  <option value="adr">ADR</option>
                </select>
              </div>
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setShowDocForm(false)}>Cancel</button>
                <button type="submit" className="btn btn-primary">Register</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {documents.length === 0 ? (
        <div className="empty-state">
          <h3>No documents registered</h3>
          <p>Register documents to track their freshness.</p>
        </div>
      ) : (
        <div className="table-wrap table-wrap--wide">
          <table className="table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Type</th>
                <th>File Path</th>
                <th>Status</th>
                <th>Coverage</th>
                <th>Drift</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {documents.map(doc => (
                <tr key={doc.id}>
                  <td>{doc.title}</td>
                  <td><span className="badge badge-todo">{doc.doc_type}</span></td>
                  <td style={{ fontSize: '0.8rem', opacity: 0.7 }}>{doc.file_path || '—'}</td>
                  <td>
                    <span className={`badge ${doc.is_stale ? 'badge-stale' : 'badge-fresh'}`}>
                      {doc.is_stale ? `Stale (${doc.staleness_days}d)` : 'Fresh'}
                    </span>
                  </td>
                  <td>
                    {documentLinkLoadErrors[doc.id] ? (
                      <span className="badge badge-low">Unknown</span>
                    ) : (documentLinksByDocumentId[doc.id]?.length ?? 0) === 0 ? (
                      <span className="badge badge-stale">Unlinked</span>
                    ) : (
                      <span className="badge badge-fresh">
                        {(documentLinksByDocumentId[doc.id]?.length ?? 0) === 1
                          ? '1 link'
                          : `${documentLinksByDocumentId[doc.id]?.length ?? 0} links`}
                      </span>
                    )}
                  </td>
                  <td>
                    {driftSignals.some(signal => signal.document_id === doc.id && signal.status === 'open') ? (
                      <span className="badge badge-stale">Open drift</span>
                    ) : (
                      <span className="badge badge-fresh">No drift</span>
                    )}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button className="btn btn-ghost btn-sm" onClick={() => onViewDoc(doc)} disabled={!doc.file_path}>View</button>
                      <button className="btn btn-ghost btn-sm" onClick={() => onManageLinks(doc)}>Links</button>
                      <button className="btn btn-ghost btn-sm" onClick={() => onDeleteDoc(doc.id)} style={{ color: 'var(--danger)' }}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
