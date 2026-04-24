import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import type { Document, DriftSignal, DocumentLink, CandidateEvidenceSummary } from '../../types'
import { createDocument, listCandidatesByEvidenceDocument } from '../../api/client'

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
  onMarkUpdated: (docId: string) => Promise<void>
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
  onMarkUpdated,
}: DocumentsTabProps) {
  const [showDocForm, setShowDocForm] = useState(false)
  const [docForm, setDocForm] = useState({ title: '', file_path: '', doc_type: 'general' as Document['doc_type'], source: 'human' })
  const [markingUpdated, setMarkingUpdated] = useState<Record<string, boolean>>({})
  const [evidenceByDoc, setEvidenceByDoc] = useState<Record<string, CandidateEvidenceSummary[]>>({})
  const [evidenceModalDocId, setEvidenceModalDocId] = useState<string | null>(null)

  useEffect(() => {
    if (documents.length === 0) return
    Promise.allSettled(
      documents.map(doc =>
        listCandidatesByEvidenceDocument(projectId, doc.id).then(r => ({ id: doc.id, data: r.data ?? [] }))
      )
    ).then(results => {
      const map: Record<string, CandidateEvidenceSummary[]> = {}
      for (const r of results) {
        if (r.status === 'fulfilled') map[r.value.id] = r.value.data
      }
      setEvidenceByDoc(map)
    })
  }, [projectId, documents])

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

  async function handleMarkUpdated(docId: string) {
    setMarkingUpdated(prev => ({ ...prev, [docId]: true }))
    try {
      await onMarkUpdated(docId)
    } finally {
      setMarkingUpdated(prev => ({ ...prev, [docId]: false }))
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
                <th>Candidates</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {documents.map(doc => {
                const candidates = evidenceByDoc[doc.id]
                return (
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
                      {candidates && candidates.length > 0 ? (
                        <button
                          className="badge badge-low"
                          style={{ cursor: 'pointer', border: 'none', background: 'none' }}
                          onClick={() => setEvidenceModalDocId(doc.id)}
                          title="View candidates that reference this document"
                        >
                          {candidates.length} candidate{candidates.length === 1 ? '' : 's'}
                        </button>
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>—</span>
                      )}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.5rem' }}>
                        <button className="btn btn-ghost btn-sm" onClick={() => onViewDoc(doc)} disabled={!doc.file_path}>View</button>
                        <button className="btn btn-ghost btn-sm" onClick={() => onManageLinks(doc)}>Links</button>
                        {doc.is_stale && (
                          <button
                            className="btn btn-ghost btn-sm"
                            onClick={() => handleMarkUpdated(doc.id)}
                            disabled={markingUpdated[doc.id]}
                          >
                            {markingUpdated[doc.id] ? 'Updating…' : 'Mark as Updated'}
                          </button>
                        )}
                        <button className="btn btn-ghost btn-sm" onClick={() => onDeleteDoc(doc.id)} style={{ color: 'var(--danger)' }}>Delete</button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {evidenceModalDocId && (() => {
        const doc = documents.find(d => d.id === evidenceModalDocId)
        const candidates = evidenceByDoc[evidenceModalDocId] ?? []
        return (
          <div className="modal-overlay" onClick={() => setEvidenceModalDocId(null)}>
            <div className="modal" onClick={e => e.stopPropagation()}>
              <h3>Candidates referencing "{doc?.title}"</h3>
              {candidates.length === 0 ? (
                <p style={{ color: 'var(--text-muted)' }}>No candidates reference this document.</p>
              ) : (
                <ul style={{ listStyle: 'none', padding: 0, margin: '0.75rem 0', display: 'grid', gap: '0.5rem' }}>
                  {candidates.map(c => (
                    <li key={c.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.5rem', padding: '0.4rem 0', borderBottom: '1px solid var(--border)' }}>
                      <div>
                        <div style={{ fontWeight: 500 }}>{c.title}</div>
                        {c.requirement_title && (
                          <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>Req: {c.requirement_title}</div>
                        )}
                      </div>
                      <div style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', flexShrink: 0 }}>
                        <span className={`badge badge-${c.status === 'applied' ? 'fresh' : c.status === 'approved' ? 'low' : c.status === 'rejected' ? 'stale' : 'todo'}`}>{c.status}</span>
                        <Link
                          className="btn btn-ghost btn-sm"
                          to={`/projects/${projectId}?tab=planning`}
                          onClick={() => setEvidenceModalDocId(null)}
                        >
                          View →
                        </Link>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              <div className="modal-actions">
                <button className="btn btn-primary" onClick={() => setEvidenceModalDocId(null)}>Close</button>
              </div>
            </div>
          </div>
        )
      })()}
    </div>
  )
}
