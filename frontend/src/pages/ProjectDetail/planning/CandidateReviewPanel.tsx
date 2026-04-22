import type { BacklogCandidate, PlanningProviderOptions, PlanningRun } from '../../../types'
import { formatDateTime, formatRelativeTime } from '../../../utils/formatters'
import {
  backlogCandidateStatusBadgeClass,
  backlogCandidateSuggestionLabel,
  formatCandidateScore,
  makeModelLabeler,
  makeProviderLabeler,
  planningBindingSourceLabel,
  planningDispatchStatusLabel,
  planningExecutionModeLabel,
  planningSelectionSourceLabel,
} from './labels'

export interface CandidateReviewForm {
  title: string
  description: string
  status: BacklogCandidate['status']
}

interface CandidateReviewPanelProps {
  selectedRun: PlanningRun | null
  candidates: BacklogCandidate[]
  candidatesLoading: boolean
  candidatesError: string | null

  selectedCandidate: BacklogCandidate | null
  selectedCandidateId: string | null
  onSelectCandidate: (id: string) => void

  candidateForm: CandidateReviewForm
  onCandidateFormChange: (form: CandidateReviewForm) => void
  candidateFormDirty: boolean
  selectedCandidateApplied: boolean
  canApplySelectedCandidate: boolean

  savingCandidate: boolean
  applyingCandidate: boolean

  candidateReviewError: string | null
  candidateReviewMessage: string | null
  candidateDuplicateTitles: string[]

  runFlash: { runId: string; kind: 'success' | 'error'; message: string } | null
  onDismissRunFlash: () => void

  providerOptions: PlanningProviderOptions | null

  onPersistReview: (nextStatus?: 'draft' | 'approved' | 'rejected') => void
  onApplyCandidate: () => void
  onResetCandidateForm: () => void
}

/**
 * Candidate review panel: header summarising the active run, candidate
 * list on the left, candidate detail with edit/approve/apply on the right.
 * Pure presentation — all state mutation is delegated to handlers on props.
 */
export function CandidateReviewPanel({
  selectedRun,
  candidates,
  candidatesLoading,
  candidatesError,
  selectedCandidate,
  selectedCandidateId,
  onSelectCandidate,
  candidateForm,
  onCandidateFormChange,
  candidateFormDirty,
  selectedCandidateApplied,
  canApplySelectedCandidate,
  savingCandidate,
  applyingCandidate,
  candidateReviewError,
  candidateReviewMessage,
  candidateDuplicateTitles,
  runFlash,
  onDismissRunFlash,
  providerOptions,
  onPersistReview,
  onApplyCandidate,
  onResetCandidateForm,
}: CandidateReviewPanelProps) {
  const providerLabel = makeProviderLabeler(providerOptions)
  const modelLabel = makeModelLabeler(providerOptions)

  return (
    <div className="planning-candidate-panel">
      <div className="planning-stage-header">
        <div>
          <h3 style={{ marginBottom: '0.25rem' }}>Suggested Backlog</h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            Review ranked backlog suggestions, inspect why each item was proposed, then approve and apply the ones worth materializing into tasks.
          </p>
          {selectedRun && (
            <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
              {providerLabel(selectedRun.provider_id)} / {modelLabel(selectedRun.provider_id, selectedRun.model_id)} via {planningBindingSourceLabel(selectedRun.binding_source).toLowerCase()}{selectedRun.binding_label ? ` (${selectedRun.binding_label})` : ''}, {planningSelectionSourceLabel(selectedRun.selection_source).toLowerCase()}.
            </p>
          )}
          {selectedRun && (
            <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
              {planningExecutionModeLabel(selectedRun.execution_mode)}. {planningDispatchStatusLabel(selectedRun.dispatch_status)}{selectedRun.connector_label ? ` on ${selectedRun.connector_label}` : ''}.
              {selectedRun.connector_cli_info && (
                <> CLI: <strong>{selectedRun.connector_cli_info.agent}</strong>{selectedRun.connector_cli_info.model ? <> / <strong>{selectedRun.connector_cli_info.model}</strong></> : null}{selectedRun.connector_cli_info.model_source ? ` (${selectedRun.connector_cli_info.model_source})` : ''}.</>
              )}
            </p>
          )}
        </div>
        {selectedRun && <span className="badge badge-todo">{candidates.length} candidate{candidates.length === 1 ? '' : 's'}</span>}
      </div>

      {candidatesError && <div className="error-banner" style={{ marginTop: '1rem' }}>{candidatesError}</div>}
      {candidateReviewError && <div className="error-banner" style={{ marginTop: '1rem' }}>{candidateReviewError}</div>}
      {candidateReviewMessage && <div className="alert alert-success" style={{ marginTop: '1rem' }}>{candidateReviewMessage}</div>}
      {runFlash && selectedRun && runFlash.runId === selectedRun.id && (
        <div
          className={runFlash.kind === 'success' ? 'alert alert-success' : 'error-banner'}
          style={{ marginTop: '1rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}
        >
          <span>{runFlash.message}</span>
          <button type="button" className="btn btn-secondary btn-small" onClick={onDismissRunFlash}>Dismiss</button>
        </div>
      )}

      {!selectedRun ? (
        <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
          <h3>No planning run selected</h3>
          <p>Select a planning run to inspect its ranked suggested backlog candidates.</p>
        </div>
      ) : candidatesLoading ? (
        <div className="loading" style={{ padding: '1rem 0 0.5rem' }}>Loading suggested backlog…</div>
      ) : candidates.length === 0 ? (
        <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
          <h3>No suggested backlog yet</h3>
          <p>
            {selectedRun.dispatch_status === 'queued' || selectedRun.dispatch_status === 'leased'
              ? 'This local connector run is still waiting for the paired machine to return ranked backlog suggestions.'
              : 'This run has not produced any ranked backlog suggestions.'}
          </p>
        </div>
      ) : (
        <div className="planning-candidate-review-layout">
          <div className="planning-candidate-list">
            {candidates.map(candidate => (
              <button
                key={candidate.id}
                type="button"
                className={`planning-candidate-card ${selectedCandidateId === candidate.id ? 'is-active' : ''}`}
                onClick={() => onSelectCandidate(candidate.id)}
              >
                <div className="requirement-card-top">
                  <strong>{candidate.title}</strong>
                  <span className={`badge ${backlogCandidateStatusBadgeClass(candidate.status)}`}>{candidate.status}</span>
                </div>
                <div className="planning-run-meta" style={{ marginTop: '0.4rem' }}>
                  <span>#{candidate.rank}</span>
                  <span>{backlogCandidateSuggestionLabel(candidate)}</span>
                  <span>Score {formatCandidateScore(candidate.priority_score)}</span>
                  <span>Confidence {formatCandidateScore(candidate.confidence)}%</span>
                </div>
                {candidate.description && <div className="requirement-description">{candidate.description}</div>}
                <div className="planning-run-meta">
                  <span>Created {formatRelativeTime(candidate.created_at)}</span>
                  <span>Updated {formatRelativeTime(candidate.updated_at)}</span>
                </div>
              </button>
            ))}
          </div>

          <div className="planning-candidate-detail-card">
            {selectedCandidate ? (
              <>
                <div className="planning-stage-header">
                  <div>
                    <h3 style={{ marginBottom: '0.25rem' }}>Candidate Review</h3>
                    <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
                      Inspect the ranked recommendation, persist copy changes, then approve and apply it into the task workflow.
                    </p>
                  </div>
                  <span className={`badge ${backlogCandidateStatusBadgeClass(selectedCandidate.status)}`}>{selectedCandidate.status}</span>
                </div>

                <div className="planning-run-meta" style={{ marginTop: '1rem' }}>
                  <span>Rank #{selectedCandidate.rank}</span>
                  <span>{backlogCandidateSuggestionLabel(selectedCandidate)}</span>
                  <span>Score {formatCandidateScore(selectedCandidate.priority_score)}</span>
                  <span>Confidence {formatCandidateScore(selectedCandidate.confidence)}%</span>
                  <span>{providerLabel(selectedRun.provider_id)} / {modelLabel(selectedRun.provider_id, selectedRun.model_id)}</span>
                </div>

                {selectedCandidateApplied && (
                  <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                    <strong>Applied candidate</strong>
                    <div>This candidate already materialized into a task. Review fields are now locked and apply is idempotent.</div>
                  </div>
                )}

                <div className="form-group" style={{ marginTop: '1rem' }}>
                  <label>Title</label>
                  <input
                    value={candidateForm.title}
                    onChange={e => onCandidateFormChange({ ...candidateForm, title: e.target.value })}
                    disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}
                  />
                </div>

                <div className="form-group">
                  <label>Description</label>
                  <textarea
                    rows={7}
                    value={candidateForm.description}
                    onChange={e => onCandidateFormChange({ ...candidateForm, description: e.target.value })}
                    disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}
                  />
                </div>

                <div className="form-group">
                  <label>Review Status</label>
                  <select
                    value={candidateForm.status}
                    onChange={e => onCandidateFormChange({ ...candidateForm, status: e.target.value as BacklogCandidate['status'] })}
                    disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}
                  >
                    <option value="draft">draft</option>
                    <option value="approved">approved</option>
                    <option value="rejected">rejected</option>
                  </select>
                </div>

                {selectedCandidate.rationale && <div className="planning-candidate-rationale">{selectedCandidate.rationale}</div>}

                {selectedCandidate.validation_criteria && (
                  <div className="planning-duplicate-note" style={{ marginTop: '1rem', borderLeft: '3px solid var(--accent-green, #22c55e)', paddingLeft: '0.75rem' }}>
                    <strong style={{ color: 'var(--accent-green, #22c55e)' }}>Validation criteria</strong>
                    <p style={{ margin: '0.35rem 0 0', lineHeight: '1.5' }}>{selectedCandidate.validation_criteria}</p>
                  </div>
                )}

                {selectedCandidate.po_decision && (
                  <div className="planning-duplicate-note" style={{ marginTop: '1rem', borderLeft: '3px solid var(--accent-orange, #f97316)', paddingLeft: '0.75rem' }}>
                    <strong style={{ color: 'var(--accent-orange, #f97316)' }}>PO decision needed</strong>
                    <p style={{ margin: '0.35rem 0 0', lineHeight: '1.5' }}>{selectedCandidate.po_decision}</p>
                  </div>
                )}

                {selectedCandidate.evidence.length > 0 && (
                  <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                    <strong>Why this was suggested</strong>
                    <div className="planning-run-meta" style={{ display: 'grid', gap: '0.35rem' }}>
                      {selectedCandidate.evidence.map(item => (
                        <span key={item}>{item}</span>
                      ))}
                    </div>
                  </div>
                )}

                {(selectedCandidate.evidence_detail.summary.length > 0 ||
                  selectedCandidate.evidence_detail.documents.length > 0 ||
                  selectedCandidate.evidence_detail.drift_signals.length > 0 ||
                  selectedCandidate.evidence_detail.sync_run ||
                  selectedCandidate.evidence_detail.agent_runs.length > 0 ||
                  selectedCandidate.evidence_detail.duplicates.length > 0) && (
                  <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
                    <strong>Context Evidence Breakdown</strong>
                    <div style={{ display: 'grid', gap: '0.85rem', marginTop: '0.75rem' }}>
                      <div className="planning-run-meta" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '0.5rem' }}>
                        <span>Impact {formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.impact)}%</span>
                        <span>Urgency {formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.urgency)}%</span>
                        <span>Dependency unlock {formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.dependency_unlock)}%</span>
                        <span>Risk reduction {formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.risk_reduction)}%</span>
                        <span>Effort {formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.effort)}%</span>
                        <span>Evidence bonus +{formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.evidence_bonus)}</span>
                        <span>Duplicate penalty -{formatCandidateScore(selectedCandidate.evidence_detail.score_breakdown.duplicate_penalty)}</span>
                      </div>

                      {selectedCandidate.evidence_detail.summary.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Evidence summary</strong>
                          <div className="planning-run-meta" style={{ display: 'grid', gap: '0.35rem' }}>
                            {selectedCandidate.evidence_detail.summary.map(item => (
                              <span key={item}>{item}</span>
                            ))}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.documents.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Documents</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.documents.map(document => (
                              <div key={document.document_id || `${document.title}-${document.file_path}`} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                <span>{document.title || document.file_path}</span>
                                <span>{document.doc_type || 'general'}{document.is_stale ? ` • stale ${document.staleness_days}d` : ''}</span>
                                {document.matched_keywords.length > 0 && <span>Matched keywords: {document.matched_keywords.join(', ')}</span>}
                                {document.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.drift_signals.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Drift signals</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.drift_signals.map(signal => (
                              <div key={signal.drift_signal_id} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                <span>{signal.document_title || signal.trigger_detail || signal.trigger_type}</span>
                                <span>Severity {signal.severity} • {signal.trigger_type}</span>
                                {signal.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.sync_run && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Latest sync</strong>
                          <div className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                            <span>Status {selectedCandidate.evidence_detail.sync_run.status}</span>
                            <span>{selectedCandidate.evidence_detail.sync_run.commits_scanned} commits • {selectedCandidate.evidence_detail.sync_run.files_changed} files</span>
                            {selectedCandidate.evidence_detail.sync_run.error_message && <span>{selectedCandidate.evidence_detail.sync_run.error_message}</span>}
                            {selectedCandidate.evidence_detail.sync_run.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.agent_runs.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Recent agent runs</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.agent_runs.map(agentRun => (
                              <div key={agentRun.agent_run_id} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                <span>{agentRun.agent_name || 'agent'} • {agentRun.action_type} • {agentRun.status}</span>
                                {agentRun.summary && <span>{agentRun.summary}</span>}
                                {agentRun.error_message && <span>{agentRun.error_message}</span>}
                                {agentRun.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.duplicates.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Duplicate overlap impact</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.duplicates.map(duplicate => (
                              <div key={duplicate.title} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                <span>{duplicate.title}</span>
                                {duplicate.contribution_reasons.map(reason => <span key={reason}>{reason}</span>)}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {candidateDuplicateTitles.length > 0 && (
                  <div className="planning-duplicate-note">
                    <strong>Possible duplicate open work</strong>
                    <div className="planning-run-meta">
                      {candidateDuplicateTitles.map(title => (
                        <span key={title}>{title}</span>
                      ))}
                    </div>
                  </div>
                )}

                <div className="planning-run-meta">
                  <span>Created {formatDateTime(selectedCandidate.created_at)}</span>
                  <span>Updated {formatDateTime(selectedCandidate.updated_at)}</span>
                  <span>Run {selectedRun.status}</span>
                  <span>{planningSelectionSourceLabel(selectedRun.selection_source)}</span>
                </div>

                <div className="planning-candidate-actions">
                  <button className="btn btn-primary" onClick={() => onPersistReview()} disabled={savingCandidate || applyingCandidate || !candidateFormDirty || selectedCandidateApplied}>
                    {savingCandidate ? 'Saving…' : 'Save Changes'}
                  </button>
                  <button className="btn btn-ghost" onClick={onResetCandidateForm} disabled={savingCandidate || applyingCandidate || !candidateFormDirty}>
                    Reset
                  </button>
                  <button className="btn btn-ghost" onClick={() => onPersistReview('draft')} disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}>
                    Return To Draft
                  </button>
                  <button className="btn btn-primary" onClick={() => onPersistReview('approved')} disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}>
                    Approve
                  </button>
                  <button className="btn btn-danger" onClick={() => onPersistReview('rejected')} disabled={savingCandidate || applyingCandidate || selectedCandidateApplied}>
                    Reject
                  </button>
                  <button className="btn btn-primary" onClick={onApplyCandidate} disabled={!canApplySelectedCandidate}>
                    {applyingCandidate ? 'Applying…' : selectedCandidateApplied ? 'Applied' : 'Apply To Tasks'}
                  </button>
                </div>
              </>
            ) : (
              <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
                <h3>No candidate selected</h3>
                <p>Select a candidate to review its draft content.</p>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
