import { useState } from 'react'
import type { PlanningProviderOptions, PlanningRun } from '../../../types'
import { formatDateTime, formatRelativeTime } from '../../../utils/formatters'
import { ConnectorActivityBadge } from './ConnectorActivityBadge'
import { PlanningRunContextDrawer } from './PlanningRunContextDrawer'
import {
  makeModelLabeler,
  makeProviderLabeler,
  planningBindingSourceLabel,
  planningDispatchStatusLabel,
  planningExecutionModeLabel,
  planningRunStatusBadgeClass,
  planningSelectionSourceLabel,
} from './labels'

interface PlanningRunListProps {
  runs: PlanningRun[]
  loading: boolean
  errorMessage: string | null
  selectedRunId: string | null
  cancellingRunId: string | null
  providerOptions: PlanningProviderOptions | null
  onSelectRun: (id: string) => void
  onCancelRun: (id: string) => void
}

/**
 * Planning-run history for the currently-selected requirement. Each card
 * shows dispatch mode, timing, provider/model, and binding source; active
 * runs surface a cancel affordance.
 */
export function PlanningRunList({
  runs,
  loading,
  errorMessage,
  selectedRunId,
  cancellingRunId,
  providerOptions,
  onSelectRun,
  onCancelRun,
}: PlanningRunListProps) {
  const providerLabel = makeProviderLabeler(providerOptions)
  const modelLabel = makeModelLabeler(providerOptions)
  const [openContextRunId, setOpenContextRunId] = useState<string | null>(null)

  if (errorMessage) {
    return <div className="error-banner" style={{ marginTop: '1rem' }}>{errorMessage}</div>
  }
  if (loading) {
    return <div className="loading" style={{ padding: '1rem 0 0.5rem' }}>Loading planning runs…</div>
  }
  if (runs.length === 0) {
    return (
      <div className="empty-state" style={{ padding: '1.5rem 0.5rem 0.5rem' }}>
        <h3>No planning runs yet</h3>
        <p>Start the first run for this requirement to establish orchestration history.</p>
      </div>
    )
  }

  return (
    <div className="planning-run-list">
      {runs.map(run => {
        const isActiveRun = run.status === 'queued' || run.status === 'running'
        const queuedFor = run.status === 'queued' ? formatRelativeTime(run.created_at) : null
        const isLocalConnectorWaiting =
          isActiveRun &&
          run.execution_mode === 'local_connector' &&
          (run.dispatch_status === 'queued' || run.dispatch_status === 'leased')
        return (
          <div key={run.id} className="planning-run-card-wrapper">
            <button
              type="button"
              className={`planning-run-card ${selectedRunId === run.id ? 'is-active' : ''}`}
              onClick={() => onSelectRun(run.id)}
            >
              <div className="requirement-card-top">
                <strong>{run.trigger_source || 'manual'}</strong>
                <span className={`badge ${planningRunStatusBadgeClass(run.status)}`}>{run.status}</span>
              </div>
              <div className="planning-run-meta">
                <span>Created {formatRelativeTime(run.created_at)}</span>
                <span>Started {formatDateTime(run.started_at)}</span>
                <span>Completed {formatDateTime(run.completed_at)}</span>
                <span>{planningExecutionModeLabel(run.execution_mode)}</span>
                <span>{planningDispatchStatusLabel(run.dispatch_status)}</span>
                {run.connector_label && <span>Connector {run.connector_label}</span>}
                {run.connector_cli_info && (() => {
                  const info = run.connector_cli_info
                  const inv = info.cli_invocation ?? (info.agent ? { agent: info.agent, model: info.model, model_source: info.model_source } : null)
                  if (!inv) return null
                  return (
                    <span title={`model_source: ${inv.model_source ?? '—'}`}>
                      CLI: {inv.agent}{inv.model ? ` / ${inv.model}` : ''}
                    </span>
                  )
                })()}
                <span>{providerLabel(run.provider_id)} / {modelLabel(run.provider_id, run.model_id)}</span>
                <span>{planningBindingSourceLabel(run.binding_source)}{run.binding_label ? ` (${run.binding_label})` : ''}</span>
                <span>{planningSelectionSourceLabel(run.selection_source)}</span>
              </div>
              {run.dispatch_error && <div className="error-banner" style={{ marginTop: '0.75rem', marginBottom: 0 }}>{run.dispatch_error}</div>}
              {run.error_message && <div className="error-banner" style={{ marginTop: '0.75rem', marginBottom: 0 }}>{run.error_message}</div>}
              {isActiveRun && run.connector_id && (
                <div style={{ marginTop: '0.5rem' }}>
                  <ConnectorActivityBadge
                    connectorId={run.connector_id}
                    label={run.connector_label ?? run.connector_id}
                    variant="standard"
                  />
                </div>
              )}
              {/* Phase 3B PR-3: quality summary — shown only when all
                  candidates have been reviewed (pending===0) and the
                  run has at least one candidate. */}
              {run.quality_summary && run.quality_summary.total > 0 && run.quality_summary.pending === 0 && (
                <div className="quality-summary-row" style={{ marginTop: '0.5rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                  Acceptance: {Math.round(run.quality_summary.acceptance_rate * 100)}%
                  ({run.quality_summary.approved}/{run.quality_summary.total})
                </div>
              )}
            </button>
            {/* Phase 3B PR-2: context snapshot drawer — available on all
                completed/failed runs; lazy-loads on first toggle. */}
            {(run.status === 'completed' || run.status === 'failed') && (
              <PlanningRunContextDrawer
                runId={run.id}
                open={openContextRunId === run.id}
                onToggle={() => setOpenContextRunId(prev => prev === run.id ? null : run.id)}
              />
            )}
            {isActiveRun && (
              <div className="planning-run-actions-row">
                {isLocalConnectorWaiting && (
                  <span className="planning-run-hint">
                    Waiting for local connector{run.connector_label ? ` "${run.connector_label}"` : ''} to claim this run.
                    {queuedFor && ` Queued ${queuedFor}.`}
                  </span>
                )}
                <button
                  type="button"
                  className="btn btn-ghost btn-small"
                  onClick={() => onCancelRun(run.id)}
                  disabled={cancellingRunId === run.id}
                >
                  {cancellingRunId === run.id ? 'Cancelling…' : 'Cancel run'}
                </button>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
