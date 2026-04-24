import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import type { DriftSignal, Document, DocumentLink, DocumentContent, CandidateEvidenceSummary } from '../../types'
import { getDocumentContent, listCandidatesByEvidenceDriftSignal } from '../../api/client'
import { formatRelativeTime } from '../../utils/formatters'

type DriftFilter = 'open' | 'all' | 'resolved' | 'dismissed'
type DriftSort = 'severity' | 'created_at'

interface DriftTabProps {
  projectId: string
  driftSignals: DriftSignal[]
  documents: Document[]
  documentLinksByDocumentId: Record<string, DocumentLink[]>
  documentLinkLoadErrors: Record<string, boolean>
  onViewDoc: (doc: Document) => void
  onManageLinks: (doc: Document) => void
  onResolveDrift: (signalId: string) => Promise<void>
  onDismissDrift: (signalId: string) => Promise<void>
  onBulkResolveDrift: () => Promise<void>
}

function triggerTypeLabel(triggerType: DriftSignal['trigger_type']) {
  if (triggerType === 'code_change') return 'Code change'
  if (triggerType === 'time_decay') return 'Time decay'
  return 'Manual'
}

function changedFilesFromSignal(signal: DriftSignal): Array<{ path: string; change_type: string }> {
  if (signal.trigger_meta?.changed_files?.length) {
    return signal.trigger_meta.changed_files
  }
  const detail = signal.trigger_detail
  const prefixes = ['Files changed:', 'File changed:']
  const prefix = prefixes.find(p => detail.startsWith(p))
  if (!prefix) return []
  return detail
    .slice(prefix.length)
    .split(',')
    .map(token => token.trim())
    .filter(Boolean)
    .map(token => {
      const match = token.match(/^(.*)\s+\(([MADR])\)$/)
      if (match) return { path: match[1].trim(), change_type: match[2] }
      return { path: token, change_type: '' }
    })
}

function severityLabel(severity: number): string {
  if (severity >= 3) return 'High'
  if (severity === 2) return 'Medium'
  return 'Low'
}

function severityBadgeClass(severity: number): string {
  if (severity >= 3) return 'badge-stale'
  if (severity === 2) return 'badge-high'
  return 'badge-low'
}

function confidenceBadgeClass(confidence: string | undefined): string {
  if (confidence === 'high') return 'badge-fresh'
  if (confidence === 'medium') return 'badge-medium'
  return 'badge-low'
}

export function DriftTab({
  projectId,
  driftSignals,
  documents,
  documentLinksByDocumentId,
  documentLinkLoadErrors,
  onViewDoc,
  onManageLinks,
  onResolveDrift,
  onDismissDrift,
  onBulkResolveDrift,
}: DriftTabProps) {
  const [driftFilter, setDriftFilter] = useState<DriftFilter>('open')
  const [driftSort, setDriftSort] = useState<DriftSort>('severity')
  const [selectedDriftId, setSelectedDriftId] = useState<string | null>(null)
  const [showDriftPreview, setShowDriftPreview] = useState(false)
  const [selectedDriftPreview, setSelectedDriftPreview] = useState<DocumentContent | null>(null)
  const [selectedDriftPreviewLoading, setSelectedDriftPreviewLoading] = useState(false)
  const [selectedDriftPreviewError, setSelectedDriftPreviewError] = useState<string | null>(null)
  const [evidenceByDrift, setEvidenceByDrift] = useState<Record<string, CandidateEvidenceSummary[]>>({})
  const [evidenceModalDriftId, setEvidenceModalDriftId] = useState<string | null>(null)

  useEffect(() => {
    if (driftSignals.length === 0) return
    let active = true
    Promise.allSettled(
      driftSignals.map(ds =>
        listCandidatesByEvidenceDriftSignal(projectId, ds.id).then(r => ({ id: ds.id, data: r.data ?? [] }))
      )
    ).then(results => {
      if (!active) return
      const map: Record<string, CandidateEvidenceSummary[]> = {}
      for (const r of results) {
        if (r.status === 'fulfilled') map[r.value.id] = r.value.data
      }
      setEvidenceByDrift(map)
    })
    return () => { active = false }
  }, [projectId, driftSignals])

  const filteredDriftSignals = driftSignals
    .filter(signal => (driftFilter === 'all' ? true : signal.status === driftFilter))
    .sort((a, b) => {
      if (driftSort === 'severity') {
        const diff = (b.severity ?? 1) - (a.severity ?? 1)
        if (diff !== 0) return diff
      }
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    })

  const selectedDriftSignal = filteredDriftSignals.find(signal => signal.id === selectedDriftId) ?? filteredDriftSignals[0] ?? null
  const selectedDriftDocument = selectedDriftSignal?.document_id
    ? documents.find(document => document.id === selectedDriftSignal.document_id) ?? null
    : null
  const selectedDriftLinks = selectedDriftDocument ? documentLinksByDocumentId[selectedDriftDocument.id] ?? [] : []
  const selectedDriftChangedFiles = selectedDriftSignal ? changedFilesFromSignal(selectedDriftSignal) : []
  const selectedDriftCoverageBreakdown = selectedDriftLinks.reduce(
    (acc, link) => {
      acc[link.link_type] = (acc[link.link_type] ?? 0) + 1
      return acc
    },
    {} as Record<DocumentLink['link_type'], number>,
  )

  useEffect(() => {
    if (filteredDriftSignals.length === 0) {
      if (selectedDriftId !== null) setSelectedDriftId(null)
      return
    }
    if (!selectedDriftId || !filteredDriftSignals.some(signal => signal.id === selectedDriftId)) {
      setSelectedDriftId(filteredDriftSignals[0].id)
    }
  }, [filteredDriftSignals, selectedDriftId])

  useEffect(() => {
    let mounted = true
    async function loadSelectedDriftPreview() {
      if (!selectedDriftDocument || !selectedDriftDocument.file_path) {
        if (mounted) {
          setSelectedDriftPreview(null)
          setSelectedDriftPreviewError(null)
          setSelectedDriftPreviewLoading(false)
        }
        return
      }
      setSelectedDriftPreviewLoading(true)
      setSelectedDriftPreviewError(null)
      try {
        const res = await getDocumentContent(selectedDriftDocument.id)
        if (mounted) setSelectedDriftPreview(res.data)
      } catch {
        if (mounted) {
          setSelectedDriftPreview(null)
          setSelectedDriftPreviewError('Unable to load inline document preview.')
        }
      } finally {
        if (mounted) setSelectedDriftPreviewLoading(false)
      }
    }
    loadSelectedDriftPreview()
    return () => { mounted = false }
  }, [selectedDriftDocument])

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
          {(['open', 'all', 'resolved', 'dismissed'] as DriftFilter[]).map(filter => {
            const count = filter === 'all'
              ? driftSignals.length
              : driftSignals.filter(signal => signal.status === filter).length
            return (
              <button
                key={filter}
                className={`btn ${driftFilter === filter ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => setDriftFilter(filter)}
              >
                {filter} ({count})
              </button>
            )
          })}
          <span style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginLeft: '0.5rem' }}>Sort:</span>
          {(['severity', 'created_at'] as DriftSort[]).map(s => (
            <button
              key={s}
              className={`btn btn-sm ${driftSort === s ? 'btn-primary' : 'btn-ghost'}`}
              onClick={() => setDriftSort(s)}
            >
              {s === 'severity' ? 'By Severity' : 'By Date'}
            </button>
          ))}
        </div>
        {driftSignals.some(s => s.status === 'open') && (
          <button className="btn btn-primary" onClick={onBulkResolveDrift}>Resolve All Open</button>
        )}
      </div>

      {driftSignals.length === 0 ? (
        <div className="empty-state">
          <h3>No drift signals</h3>
          <p>No documentation drift has been detected.</p>
        </div>
      ) : filteredDriftSignals.length === 0 ? (
        <div className="empty-state">
          <h3>No signals in this filter</h3>
          <p>Try switching filters to review other drift states.</p>
        </div>
      ) : (
        <div className="drift-layout">
          <div className="card-list" style={{ marginBottom: 0 }}>
            {filteredDriftSignals.map(signal => {
              const driftCandidates = evidenceByDrift[signal.id]
              return (
                <div
                  key={signal.id}
                  className="card"
                  style={{
                    cursor: 'pointer',
                    borderColor: selectedDriftSignal?.id === signal.id ? 'var(--primary)' : 'var(--border)',
                  }}
                  onClick={() => { setSelectedDriftId(signal.id); setShowDriftPreview(false) }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}>
                    <h4>{signal.document_title || 'Unlinked document'}</h4>
                    <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
                      {driftCandidates && driftCandidates.length > 0 && (
                        <button
                          className="badge badge-low"
                          style={{ cursor: 'pointer', border: 'none', background: 'none', fontSize: '0.75rem' }}
                          onClick={e => { e.stopPropagation(); setEvidenceModalDriftId(signal.id) }}
                          title="View candidates that cite this drift signal"
                        >
                          cited in {driftCandidates.length}
                        </button>
                      )}
                      <span className={`badge ${severityBadgeClass(signal.severity ?? 1)}`}>
                        {severityLabel(signal.severity ?? 1)}
                      </span>
                      <span className={`badge ${signal.status === 'open' ? 'badge-stale' : 'badge-fresh'}`}>{signal.status}</span>
                    </div>
                  </div>
                  <p style={{ marginTop: '0.5rem', fontSize: '0.875rem', color: 'var(--text-muted)' }}>
                    {signal.trigger_type === 'code_change' && signal.trigger_meta?.changed_files?.length
                      ? `${signal.trigger_meta.changed_files.length} file${signal.trigger_meta.changed_files.length === 1 ? '' : 's'} changed`
                      : signal.trigger_detail}
                  </p>
                  <div style={{ marginTop: '0.75rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                    {new Date(signal.created_at).toLocaleString()}
                  </div>
                </div>
              )
            })}
          </div>

          {selectedDriftSignal && (
            <div className="card" style={{ marginBottom: 0 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
                <div>
                  <h3 style={{ marginBottom: '0.25rem' }}>{selectedDriftSignal.document_title || 'Unlinked document'}</h3>
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                    {selectedDriftDocument?.file_path || 'No document file path registered'}
                  </div>
                </div>
                <span className={`badge ${selectedDriftSignal.status === 'open' ? 'badge-stale' : 'badge-fresh'}`}>{selectedDriftSignal.status}</span>
              </div>

              <div style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
                  <div>
                    <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Severity</div>
                    <span className={`badge ${severityBadgeClass(selectedDriftSignal.severity ?? 1)}`}>
                      {severityLabel(selectedDriftSignal.severity ?? 1)}
                    </span>
                  </div>
                  <div>
                    <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Trigger Type</div>
                    <span className={`badge ${selectedDriftSignal.trigger_type === 'time_decay' ? 'badge-high' : selectedDriftSignal.trigger_type === 'manual' ? 'badge-medium' : 'badge-low'}`}>
                      {triggerTypeLabel(selectedDriftSignal.trigger_type)}
                    </span>
                  </div>
                  {selectedDriftSignal.trigger_meta?.confidence && (
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Confidence</div>
                      <span className={`badge ${confidenceBadgeClass(selectedDriftSignal.trigger_meta.confidence)}`}>
                        {selectedDriftSignal.trigger_meta.confidence}
                      </span>
                    </div>
                  )}
                </div>
                <div>
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Trigger</div>
                  <div>
                    {selectedDriftSignal.trigger_type === 'time_decay' && selectedDriftSignal.trigger_meta?.days_stale
                      ? `Stale for over ${selectedDriftSignal.trigger_meta.days_stale} days`
                      : selectedDriftSignal.trigger_detail}
                  </div>
                </div>
                {selectedDriftChangedFiles.length > 0 && (
                  <div>
                    <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                      Impacted Files ({selectedDriftChangedFiles.length})
                    </div>
                    <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap', marginTop: '0.25rem' }}>
                      {selectedDriftChangedFiles.map(f => (
                        <span
                          key={f.path}
                          className={`badge ${f.change_type === 'D' || f.change_type === 'R' ? 'badge-stale' : 'badge-low'}`}
                          title={`Change type: ${f.change_type || 'unknown'}`}
                        >
                          {f.change_type && <strong style={{ marginRight: '0.25rem' }}>[{f.change_type}]</strong>}
                          {f.path}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
                <div>
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Created</div>
                  <div>{new Date(selectedDriftSignal.created_at).toLocaleString()} ({formatRelativeTime(selectedDriftSignal.created_at)})</div>
                </div>
                <div>
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Coverage</div>
                  {selectedDriftDocument ? (
                    documentLinkLoadErrors[selectedDriftDocument.id] ? (
                      <div>Unable to load links right now.</div>
                    ) : selectedDriftLinks.length === 0 ? (
                      <div style={{ color: '#fca5a5' }}>No document links yet. Add coverage before the next sync for more precise drift detection.</div>
                    ) : (
                      <>
                        <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>
                          {selectedDriftLinks.length} linked path{selectedDriftLinks.length === 1 ? '' : 's'}
                          {Object.entries(selectedDriftCoverageBreakdown).length > 0 && (
                            <> • {Object.entries(selectedDriftCoverageBreakdown).map(([kind, count]) => `${kind}:${count}`).join(', ')}</>
                          )}
                        </div>
                        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.25rem' }}>
                          {selectedDriftLinks.map(link => (
                            <span key={link.id} className="badge badge-low">{link.code_path}</span>
                          ))}
                        </div>
                      </>
                    )
                  ) : (
                    <div>This signal is not linked to a registered document.</div>
                  )}
                </div>
                <div>
                  <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>Document Preview</div>
                  {!selectedDriftDocument ? (
                    <div>No linked document to preview.</div>
                  ) : (
                    <>
                      <button
                        className="btn btn-ghost btn-sm"
                        style={{ marginTop: '0.35rem' }}
                        onClick={() => setShowDriftPreview(prev => !prev)}
                      >
                        {showDriftPreview ? 'Hide Preview' : 'Show Document Preview'}
                      </button>
                      {showDriftPreview && (
                        selectedDriftPreviewLoading ? (
                          <div className="loading" style={{ padding: '0.4rem 0' }}>Loading preview...</div>
                        ) : selectedDriftPreviewError ? (
                          <div style={{ color: '#fca5a5' }}>{selectedDriftPreviewError}</div>
                        ) : selectedDriftPreview ? (
                          <div style={{ marginTop: '0.35rem' }}>
                            <pre style={{
                              maxHeight: '180px',
                              overflow: 'auto',
                              background: 'var(--bg)',
                              border: '1px solid var(--border)',
                              borderRadius: '0.4rem',
                              padding: '0.65rem',
                              fontSize: '0.78rem',
                              lineHeight: 1.45,
                              whiteSpace: 'pre-wrap',
                            }}>
                              {selectedDriftPreview.content}
                            </pre>
                            {selectedDriftPreview.truncated && (
                              <div style={{ color: 'var(--text-muted)', fontSize: '0.78rem', marginTop: '0.3rem' }}>
                                Preview truncated. Open full document for complete content.
                              </div>
                            )}
                          </div>
                        ) : (
                          <div>No preview available.</div>
                        )
                      )}
                    </>
                  )}
                </div>
              </div>

              <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                {selectedDriftDocument && (
                  <>
                    <button className="btn btn-ghost btn-sm" onClick={() => onViewDoc(selectedDriftDocument)} disabled={!selectedDriftDocument.file_path}>
                      View Doc
                    </button>
                    <button className="btn btn-ghost btn-sm" onClick={() => onManageLinks(selectedDriftDocument)}>
                      Manage Links
                    </button>
                  </>
                )}
                {selectedDriftSignal.status === 'open' && (
                  <>
                    <button className="btn btn-primary btn-sm" onClick={() => onResolveDrift(selectedDriftSignal.id)}>Mark Resolved</button>
                    <button className="btn btn-ghost btn-sm" onClick={() => onDismissDrift(selectedDriftSignal.id)}>Dismiss</button>
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {evidenceModalDriftId && (() => {
        const signal = driftSignals.find(s => s.id === evidenceModalDriftId)
        const candidates = evidenceByDrift[evidenceModalDriftId] ?? []
        return (
          <div className="modal-overlay" onClick={() => setEvidenceModalDriftId(null)}>
            <div className="modal" onClick={e => e.stopPropagation()}>
              <h3>Candidates citing "{signal?.document_title || 'this drift signal'}"</h3>
              {candidates.length === 0 ? (
                <p style={{ color: 'var(--text-muted)' }}>No candidates reference this drift signal.</p>
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
                          onClick={() => setEvidenceModalDriftId(null)}
                        >
                          View →
                        </Link>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              <div className="modal-actions">
                <button className="btn btn-primary" onClick={() => setEvidenceModalDriftId(null)}>Close</button>
              </div>
            </div>
          </div>
        )
      })()}
    </div>
  )
}
