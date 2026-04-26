import { useState } from 'react'
import type { BacklogCandidate, PlanningProviderOptions, PlanningRun } from '../../../types'
import { formatDateTime, formatRelativeTime } from '../../../utils/formatters'
import Jargon from '../../../components/Jargon'
import { CandidateRoleEditor } from './CandidateRoleEditor'
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

/**
 * Shared inline-link affordance used by document + drift evidence rows.
 * Centralised here because both evidence kinds need the same look and feel
 * (underlined link-coloured text, left-aligned, button-element semantics
 * for keyboard + screen-reader activation) and the inline styles were
 * duplicated across both call sites before this extraction.
 *
 * The `ariaLabel` is required so blank headings cannot render a screen-
 * reader-invisible link — callers must always supply a descriptive label.
 */
interface EvidenceLinkProps {
  label: string
  ariaLabel: string
  onClick: () => void
}

function EvidenceLink({ label, ariaLabel, onClick }: EvidenceLinkProps) {
  return (
    <button
      type="button"
      className="evidence-link"
      onClick={onClick}
      aria-label={ariaLabel}
      style={{
        alignSelf: 'start',
        padding: 0,
        background: 'none',
        border: 'none',
        color: 'var(--link, #60a5fa)',
        textDecoration: 'underline',
        cursor: 'pointer',
        textAlign: 'left',
      }}
    >
      {label}
    </button>
  )
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
  onSkipCandidate: () => void
  onResetCandidateForm?: () => void

  // Phase 5 B3 + Phase 6c PR-2: execution mode radio + role dropdown.
  // `manual` is the Phase 4 behaviour; `role_dispatch` is now always
  // selectable (catch-22 resolved). When mode === 'role_dispatch'
  // the operator picks a role from `availableRoles`; the chosen value
  // travels in the apply payload. All four props are optional —
  // callers that do not wire them get the pre-Phase-5 UI with no
  // visible mode selector.
  selectedExecutionMode?: 'manual' | 'role_dispatch'
  onSelectedExecutionModeChange?: (mode: 'manual' | 'role_dispatch') => void
  chosenExecutionRole?: string
  onChosenExecutionRoleChange?: (role: string) => void
  // null = catalog still loading (suppresses stale-warning on mount);
  // [] = catalog loaded but empty (server returned no roles, also
  // safe — operators see no options). Critic round 1 finding #5.
  availableRoles?: ReadonlyArray<{
    id: string
    title: string
    version: number
    use_case: string
    default_timeout_sec: number
    category: 'role' | 'meta'
  }> | null
  // Phase 6c PR-2 (Copilot review #3): when /api/roles fetch fails,
  // availableRoles stays null (== loading sentinel) AND this string is
  // populated. The panel renders the message in the dropdown / chip
  // area so operators understand the catalog never loaded; otherwise
  // an empty dropdown with no roles would silently look like "the
  // server has no roles configured", which is a different failure.
  availableRolesError?: string | null
  // Phase 6c PR-2: optional callback to PATCH the candidate's
  // execution_role outside the apply flow. When provided, the
  // candidate row renders an inline CandidateRoleEditor (chip + edit
  // popover) so operators can pre-tag candidates with a role before
  // applying. When undefined, the chip is rendered read-only.
  onUpdateCandidateExecutionRole?: (candidateId: string, roleId: string) => Promise<void>

  /**
   * Optional evidence-link callbacks. When provided, the matching
   * evidence row becomes a clickable affordance:
   *   - onViewDocumentById → opens the existing document-preview modal
   *     (wired upstream in ProjectDetail.handleViewDoc).
   *   - onViewDriftSignal → navigates to the Drift tab. Preselecting the
   *     signal inside Drift is a post-Phase-2 enhancement.
   * When undefined, the rows render as plain text (no regression for
   * callers that have not opted in yet).
   */
  onViewDocumentById?: (documentId: string) => void
  onViewDriftSignal?: (driftSignalId: string) => void
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
  onSkipCandidate,
  selectedExecutionMode,
  onSelectedExecutionModeChange,
  chosenExecutionRole,
  onChosenExecutionRoleChange,
  availableRoles,
  availableRolesError,
  onUpdateCandidateExecutionRole,
  onViewDocumentById,
  onViewDriftSignal,
}: CandidateReviewPanelProps) {
  const [showSkipped, setShowSkipped] = useState(false)
  const providerLabel = makeProviderLabeler(providerOptions)
  const modelLabel = makeModelLabeler(providerOptions)

  const isWhatsnextRun = selectedRun?.adapter_type === 'whatsnext'

  return (
    <div className="planning-candidate-panel">
      <div className="planning-stage-header">
        <div>
          <h3 style={{ marginBottom: '0.25rem' }}>
            {isWhatsnextRun ? 'Suggested Focus Areas' : <><Jargon term="backlog candidate">Suggested Backlog</Jargon></>}
          </h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            {isWhatsnextRun
              ? 'Prioritised list of the most urgent open work across tasks, drift signals, and stale docs. Approve items worth scheduling and apply them as tasks.'
              : 'Review ranked backlog suggestions, inspect why each item was proposed, then approve and apply the ones worth materializing into tasks.'}
          </p>
          {selectedRun && (
            <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
              {providerLabel(selectedRun.provider_id)} / {modelLabel(selectedRun.provider_id, selectedRun.model_id)} via {planningBindingSourceLabel(selectedRun.binding_source).toLowerCase()}{selectedRun.binding_label ? ` (${selectedRun.binding_label})` : ''}, {planningSelectionSourceLabel(selectedRun.selection_source).toLowerCase()}.
            </p>
          )}
          {selectedRun && (
            <p style={{ margin: '0.45rem 0 0', color: 'var(--text-muted)', fontSize: '0.82rem' }}>
              {planningExecutionModeLabel(selectedRun.execution_mode)}. {planningDispatchStatusLabel(selectedRun.dispatch_status)}{selectedRun.connector_label ? ` on ${selectedRun.connector_label}` : ''}.
              {(() => {
                const info = selectedRun.connector_cli_info
                if (!info) return null
                const inv = info.cli_invocation ?? (info.agent ? { agent: info.agent, model: info.model, model_source: info.model_source } : null)
                if (!inv) return null
                return (
                  <> CLI: <strong>{inv.agent}</strong>{inv.model ? <> / <strong>{inv.model}</strong></> : null}{inv.model_source ? ` (${inv.model_source})` : ''}.</>
                )
              })()}
            </p>
          )}
        </div>
        {selectedRun && (() => {
          const activeCount = candidates.filter(c => c.status !== 'rejected').length
          return <span className="badge badge-todo">{activeCount} candidate{activeCount === 1 ? '' : 's'}</span>
        })()}
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

      {selectedRun &&
        selectedRun.dispatch_status === 'returned' &&
        selectedRun.status === 'failed' &&
        selectedRun.connector_cli_info?.error_kind &&
        selectedRun.connector_cli_info.error_kind !== 'unknown' &&
        selectedRun.connector_cli_info.remediation_hint && (
          <div className="helper-note" style={{ marginTop: '1rem', borderLeft: '3px solid var(--warning)', paddingLeft: '0.75rem' }}>
            <strong>Suggested next step:</strong>
            <p style={{ margin: '0.25rem 0 0' }}>{selectedRun.connector_cli_info.remediation_hint}</p>
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
            {candidates.filter(c => c.status !== 'rejected').map(candidate => (
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

            {(() => {
              const skipped = candidates.filter(c => c.status === 'rejected')
              if (skipped.length === 0) return null
              return (
                <div style={{ marginTop: '0.5rem', borderTop: '1px solid var(--border)', paddingTop: '0.5rem' }}>
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    style={{ width: '100%', textAlign: 'left', fontSize: '0.82rem', color: 'var(--text-muted)' }}
                    onClick={() => setShowSkipped(s => !s)}
                  >
                    {skipped.length} skipped {showSkipped ? '▴' : '▾'}
                  </button>
                  {showSkipped && skipped.map(c => (
                    <button
                      key={c.id}
                      type="button"
                      className="planning-candidate-card"
                      style={{ opacity: 0.5 }}
                      onClick={() => onSelectCandidate(c.id)}
                    >
                      <span style={{ fontSize: '0.88rem' }}>#{c.rank} {c.title}</span>
                    </button>
                  ))}
                </div>
              )
            })()}
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
                      {selectedCandidate.evidence.map((item, idx) => (
                        <span key={`${item}-${idx}`}>{item}</span>
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
                            {selectedCandidate.evidence_detail.summary.map((item, idx) => (
                              <span key={`${item}-${idx}`}>{item}</span>
                            ))}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.documents.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Documents</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.documents.map(document => {
                              // Fallback chain guarantees the row is never rendered blank:
                              // title → file_path → "Document <id>" → "(untitled document)".
                              const fallbackId = document.document_id ? `Document ${document.document_id}` : '(untitled document)'
                              const heading = document.title || document.file_path || fallbackId
                              const canOpen = Boolean(onViewDocumentById && document.document_id)
                              return (
                                <div key={document.document_id || `${document.title}-${document.file_path}`} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                  {canOpen ? (
                                    <EvidenceLink
                                      label={heading}
                                      ariaLabel={`Open document preview for ${heading}`}
                                      onClick={() => onViewDocumentById!(document.document_id!)}
                                    />
                                  ) : (
                                    <span>{heading}</span>
                                  )}
                                  <span>{document.doc_type || 'general'}{document.is_stale ? ` • stale ${document.staleness_days}d` : ''}</span>
                                  {document.matched_keywords.length > 0 && <span>Matched keywords: {document.matched_keywords.join(', ')}</span>}
                                  {document.contribution_reasons.map((reason, idx) => <span key={`${reason}-${idx}`}>{reason}</span>)}
                                </div>
                              )
                            })}
                          </div>
                        </div>
                      )}

                      {selectedCandidate.evidence_detail.drift_signals.length > 0 && (
                        <div>
                          <strong style={{ display: 'block', marginBottom: '0.35rem' }}>Drift signals</strong>
                          <div style={{ display: 'grid', gap: '0.5rem' }}>
                            {selectedCandidate.evidence_detail.drift_signals.map(signal => {
                              // Fallback chain: document_title → trigger_detail →
                              // trigger_type → "Drift signal <id>". The signal always
                              // has an id, so the row can never render empty.
                              const heading = signal.document_title || signal.trigger_detail || signal.trigger_type || `Drift signal ${signal.drift_signal_id}`
                              const canOpen = Boolean(onViewDriftSignal)
                              return (
                                <div key={signal.drift_signal_id} className="planning-run-meta" style={{ display: 'grid', gap: '0.2rem' }}>
                                  {canOpen ? (
                                    <EvidenceLink
                                      label={heading}
                                      ariaLabel={`Jump to drift signal ${heading}`}
                                      onClick={() => onViewDriftSignal!(signal.drift_signal_id)}
                                    />
                                  ) : (
                                    <span>{heading}</span>
                                  )}
                                  <span>Severity {signal.severity} • {signal.trigger_type}</span>
                                  {signal.contribution_reasons.map((reason, idx) => <span key={`${reason}-${idx}`}>{reason}</span>)}
                                </div>
                              )
                            })}
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
                            {selectedCandidate.evidence_detail.sync_run.contribution_reasons.map((reason, idx) => <span key={`${reason}-${idx}`}>{reason}</span>)}
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
                                {agentRun.contribution_reasons.map((reason, idx) => <span key={`${reason}-${idx}`}>{reason}</span>)}
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
                                {duplicate.contribution_reasons.map((reason, idx) => <span key={`${reason}-${idx}`}>{reason}</span>)}
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
                      {candidateDuplicateTitles.map((title, idx) => (
                        <span key={`${title}-${idx}`}>{title}</span>
                      ))}
                    </div>
                  </div>
                )}

                <div className="planning-run-meta">
                  <span>Created {formatDateTime(selectedCandidate.created_at)}</span>
                  <span>Updated {formatDateTime(selectedCandidate.updated_at)}</span>
                  <span>Run {selectedRun.status}</span>
                  <span>{planningSelectionSourceLabel(selectedRun.selection_source)}</span>
                  {/* Phase 6c PR-2: replace static chip with the
                      CandidateRoleEditor component, which adds an
                      inline edit popover so operators can pre-tag
                      candidates with a role outside the apply flow. */}
                  {onUpdateCandidateExecutionRole ? (
                    <CandidateRoleEditor
                      candidate={selectedCandidate}
                      availableRoles={availableRoles ?? null}
                      availableRolesError={availableRolesError ?? null}
                      onUpdateRole={role => onUpdateCandidateExecutionRole(selectedCandidate.id, role)}
                      disabled={selectedCandidateApplied || savingCandidate || applyingCandidate}
                    />
                  ) : (
                    selectedCandidate.execution_role && (
                      <span
                        title="Execution specialist earmarked for Phase 6 auto-dispatch"
                        style={{
                          background: 'var(--bg-hover, rgba(255, 255, 255, 0.05))',
                          border: '1px solid var(--border)',
                          borderRadius: '999px',
                          padding: '0.1rem 0.5rem',
                          fontSize: '0.78rem',
                        }}
                      >
                        Role: {selectedCandidate.execution_role}
                      </span>
                    )
                  )}
                </div>

                {onSelectedExecutionModeChange && (
                  <div className="planning-execution-mode-block" style={{ marginTop: '0.5rem' }}>
                    <div
                      className="planning-execution-mode"
                      role="radiogroup"
                      aria-labelledby="execution-mode-label"
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '0.75rem',
                        fontSize: '0.88rem',
                      }}
                    >
                      <span id="execution-mode-label" style={{ color: 'var(--text-muted)' }}>Execution:</span>
                      <label style={{ display: 'flex', alignItems: 'center', gap: '0.3rem', cursor: 'pointer' }}>
                        <input
                          type="radio"
                          name="execution-mode"
                          checked={selectedExecutionMode !== 'role_dispatch'}
                          onChange={() => onSelectedExecutionModeChange('manual')}
                        />
                        Manual
                      </label>
                      <label
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: '0.3rem',
                          cursor: 'pointer',
                        }}
                        title="Auto-dispatch the role you choose below"
                      >
                        <input
                          type="radio"
                          name="execution-mode"
                          checked={selectedExecutionMode === 'role_dispatch'}
                          onChange={() => onSelectedExecutionModeChange('role_dispatch')}
                        />
                        <Jargon term="dispatch">Auto-dispatch</Jargon>
                      </label>
                    </div>

                    {selectedExecutionMode === 'role_dispatch' && onChosenExecutionRoleChange && (
                      <div className="planning-execution-role-row" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.4rem', fontSize: '0.88rem' }}>
                        <span style={{ color: 'var(--text-muted)' }}>Role:</span>
                        <select
                          aria-label="Select execution role"
                          value={chosenExecutionRole ?? ''}
                          onChange={e => onChosenExecutionRoleChange(e.target.value)}
                          style={{ minWidth: '20rem' }}
                        >
                          <option value="">— select role —</option>
                          {(availableRoles ?? []).map(r => (
                            <option key={r.id} value={r.id} title={r.use_case}>
                              {r.title} (v{r.version}) — 預估 {Math.round(r.default_timeout_sec / 60)} min
                            </option>
                          ))}
                          {availableRoles === null && !availableRolesError && (
                            <option disabled>Loading roles…</option>
                          )}
                          {availableRoles === null && availableRolesError && (
                            <option disabled>Failed to load roles</option>
                          )}
                        </select>
                        {availableRolesError && (
                          <div
                            role="alert"
                            style={{
                              marginTop: '0.35rem',
                              fontSize: '0.78rem',
                              color: 'var(--danger, #ef4444)',
                            }}
                          >
                            Failed to load roles: {availableRolesError}
                          </div>
                        )}
                      </div>
                    )}

                    {selectedExecutionMode === 'role_dispatch' &&
                      selectedCandidate?.execution_role &&
                      // Phase 6c PR-2 critic #5 / risk-reviewer L1: only
                      // surface the stale-warning AFTER the catalog
                      // fetch resolves. Treating `null` (still loading)
                      // as "stale" produced a false positive flash on
                      // every mount until /api/roles returned.
                      availableRoles !== null &&
                      availableRoles !== undefined &&
                      !availableRoles.some(r => r.id === selectedCandidate.execution_role) && (
                        <div
                          role="status"
                          aria-live="polite"
                          style={{
                            marginTop: '0.4rem',
                            padding: '0.4rem 0.6rem',
                            borderLeft: '3px solid var(--warning, #f59e0b)',
                            background: 'var(--warning-bg, rgba(245, 158, 11, 0.08))',
                            fontSize: '0.82rem',
                            color: 'var(--text-muted)',
                          }}
                        >
                          ⚠ Previously suggested role <code>{selectedCandidate.execution_role}</code> is no longer in the catalog. Pick a current role above.
                        </div>
                      )}
                  </div>
                )}

                <div className="planning-candidate-actions">
                  {candidateFormDirty && !selectedCandidateApplied && (
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => onPersistReview()}
                      disabled={savingCandidate || applyingCandidate}
                    >
                      {savingCandidate ? 'Saving…' : 'Save edits'}
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={onSkipCandidate}
                    disabled={savingCandidate || applyingCandidate || selectedCandidateApplied || selectedCandidate.status === 'rejected'}
                  >
                    {selectedCandidate.status === 'rejected' ? 'Skipped' : 'Skip'}
                  </button>
                  <button
                    type="button"
                    className="btn btn-primary"
                    onClick={onApplyCandidate}
                    disabled={
                      !canApplySelectedCandidate ||
                      applyingCandidate ||
                      // Phase 6c PR-2: when mode=role_dispatch the
                      // chosen role is required. Disabling Apply
                      // surfaces the missing-role state without
                      // bouncing the operator off a 400 from the
                      // server.
                      (selectedExecutionMode === 'role_dispatch' &&
                        !(chosenExecutionRole && chosenExecutionRole.trim() !== ''))
                    }
                  >
                    {applyingCandidate ? 'Applying…' : selectedCandidateApplied ? 'Applied ✓' : 'Apply'}
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
