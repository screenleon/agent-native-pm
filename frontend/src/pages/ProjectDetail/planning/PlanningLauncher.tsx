import { Link } from 'react-router-dom'
import type { AccountBinding, PlanningExecutionMode, PlanningProviderOptions, Requirement } from '../../../types'
import { formatRelativeTime } from '../../../utils/formatters'
import {
  credentialModeLabel,
  planningBindingSourceLabel,
  planningExecutionModeLabel,
  requirementStatusBadgeClass,
} from './labels'

interface PlanningLauncherProps {
  selectedRequirement: Requirement
  providerOptions: PlanningProviderOptions | null
  providerOptionsLoading: boolean
  providerOptionsError: string | null

  executionMode: PlanningExecutionMode
  onExecutionModeChange: (mode: PlanningExecutionMode) => void

  cliBindings: AccountBinding[]
  cliBindingsLoading: boolean
  selectedCliBindingId: string | null
  onCliBindingChange: (id: string) => void

  planningModelOverride: string
  onPlanningModelOverrideChange: (value: string) => void

  creatingRun: boolean
  runningWhatsnext: boolean
  runsLoading: boolean
  runReady: boolean
  runBlockedReason: string | null

  onStartRun: () => void
  onRefreshRuns: () => void
  onRunWhatsnext: () => void
}

/**
 * The "launch a run" card: requirement detail header + decomposition
 * settings (execution mode, model override, connector presence) + the
 * Start Planning Run / Refresh Runs / What's Next Now buttons.
 *
 * Everything below this card (PlanningRunList, CandidateReviewPanel) is
 * rendered in separate siblings by the parent; this component only emits
 * the launcher surface.
 */
export function PlanningLauncher({
  selectedRequirement,
  providerOptions,
  providerOptionsLoading,
  providerOptionsError,
  executionMode,
  onExecutionModeChange,
  cliBindings,
  cliBindingsLoading,
  selectedCliBindingId,
  onCliBindingChange,
  planningModelOverride,
  onPlanningModelOverrideChange,
  creatingRun,
  runningWhatsnext,
  runsLoading,
  runReady,
  runBlockedReason,
  onStartRun,
  onRefreshRuns,
  onRunWhatsnext,
}: PlanningLauncherProps) {
  const usesLocalConnector = executionMode === 'local_connector'
  const defaultSelection = providerOptions?.default_selection ?? null
  const effectiveProvider = defaultSelection
    ? providerOptions?.providers.find(p => p.id === defaultSelection.provider_id) ?? null
    : null
  const effectiveModel = defaultSelection && effectiveProvider
    ? effectiveProvider.models.find(m => m.id === defaultSelection.model_id) ?? null
    : null
  const currentModelID = planningModelOverride.trim() || defaultSelection?.model_id || ''
  const currentModel = effectiveProvider?.models.find(m => m.id === currentModelID) ?? effectiveModel

  return (
    <div className="requirement-detail-panel">
      <div className="requirement-card-top">
        <strong>{selectedRequirement.title}</strong>
        <span className={`badge ${requirementStatusBadgeClass(selectedRequirement.status)}`}>{selectedRequirement.status}</span>
      </div>
      {selectedRequirement.summary && <p style={{ marginTop: '0.6rem', color: 'var(--text-muted)' }}>{selectedRequirement.summary}</p>}
      {selectedRequirement.description && (
        <div className="planning-placeholder-note" style={{ marginTop: '0.75rem' }}>
          {selectedRequirement.description}
        </div>
      )}

      <div className="requirement-detail-grid">
        <div className="requirement-detail-block">
          <span>Source</span>
          <strong>{selectedRequirement.source}</strong>
        </div>
        <div className="requirement-detail-block">
          <span>Created</span>
          <strong>{formatRelativeTime(selectedRequirement.created_at)}</strong>
        </div>
        <div className="requirement-detail-block">
          <span>Updated</span>
          <strong>{formatRelativeTime(selectedRequirement.updated_at)}</strong>
        </div>
      </div>

      <div className="planning-duplicate-note" style={{ marginTop: '1rem' }}>
        <strong>Decomposition Settings</strong>
        <div className="planning-run-meta" style={{ display: 'grid', gap: '0.75rem', marginTop: '0.75rem' }}>
          {providerOptionsLoading && <span>Loading provider options…</span>}
          {providerOptionsError && <span style={{ color: 'var(--danger)' }}>{providerOptionsError}</span>}

          {providerOptions && providerOptions.available_execution_modes.length > 1 && (
            <label style={{ display: 'grid', gap: '0.35rem' }}>
              <span>Execution source for this run</span>
              <select
                value={executionMode}
                onChange={e => onExecutionModeChange(e.target.value as PlanningExecutionMode)}
                disabled={creatingRun || providerOptionsLoading}
              >
                {providerOptions.available_execution_modes.map(mode => (
                  <option key={mode} value={mode}>{planningExecutionModeLabel(mode)}</option>
                ))}
              </select>
            </label>
          )}

          {usesLocalConnector ? (
            <>
              {providerOptions?.paired_connector_available ? (
                <div className="connector-run-info connector-run-info-ready">
                  <div className="connector-run-row">
                    <span className="connector-badge connector-badge-ready">● Online</span>
                    <strong>{providerOptions.active_connector_label ?? 'My Machine'}</strong>
                  </div>
                  <div className="connector-run-desc">
                    The run will be queued and your local connector will pick it up automatically.
                    The connector calls your local AI CLI (<code>claude</code> or <code>codex</code>) using the binding selected below.
                  </div>

                  <div style={{ marginTop: '0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.65rem 0.9rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                    <div>
                      <strong style={{ fontSize: '0.88rem' }}>Project Health Check</strong>
                      <p style={{ margin: '0.15rem 0 0', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                        No specific requirement? Run a full project analysis — surfaces urgent tasks, drift signals, and stale docs.
                      </p>
                    </div>
                    <button
                      type="button"
                      className="btn btn-secondary btn-small"
                      onClick={onRunWhatsnext}
                      disabled={runningWhatsnext || creatingRun || !runReady}
                      style={{ whiteSpace: 'nowrap' }}
                    >
                      {runningWhatsnext ? 'Starting…' : "Run What's Next"}
                    </button>
                  </div>

                  <div style={{ marginTop: '0.75rem', display: 'grid', gap: '0.5rem' }}>
                    {/* CLI Binding Selector */}
                    {cliBindingsLoading ? (
                      <span style={{ fontSize: '0.88rem', color: 'var(--text-muted)' }}>Loading CLI bindings…</span>
                    ) : cliBindings.length === 0 ? (
                      <div style={{ fontSize: '0.88rem', color: 'var(--text-muted)' }}>
                        No CLI binding configured.{' '}
                        <Link to="/account-bindings">Set up a CLI binding</Link> to use your subscription.
                      </div>
                    ) : (
                      <label style={{ display: 'grid', gap: '0.3rem' }}>
                        <span style={{ fontSize: '0.88rem' }}>CLI binding for this run</span>
                        <select
                          value={selectedCliBindingId ?? ''}
                          onChange={e => onCliBindingChange(e.target.value)}
                          disabled={creatingRun}
                          style={{ padding: '0.4rem 0.6rem', fontSize: '0.88rem', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
                        >
                          {cliBindings.map(b => (
                            <option key={b.id} value={b.id}>
                              {b.label}{b.model_id ? ` [${b.model_id}]` : ''}{b.is_primary ? ' (primary)' : ''}
                            </option>
                          ))}
                        </select>
                        <small style={{ color: 'var(--text-muted)' }}>
                          Manage bindings in <Link to="/account-bindings">My Bindings</Link>.
                        </small>
                      </label>
                    )}
                    <small style={{ color: 'var(--text-muted)' }}>
                      Adapter type is selected automatically: backlog planner for requirement runs, what's next for health checks.
                    </small>
                  </div>
                </div>
              ) : (
                <div className="connector-run-info connector-run-info-offline">
                  <div className="connector-run-row">
                    <span className="connector-badge connector-badge-offline">○ No live connector</span>
                  </div>
                  <div className="connector-run-desc">
                    No connector is currently online. Start the connector binary on this machine first, then return here to queue a run.
                    <br />
                    <Link to="/connector" style={{ fontSize: '0.88rem' }}>Go to My Connector →</Link>
                  </div>
                </div>
              )}
            </>
          ) : (
            <>
              {effectiveProvider && <span>Provider: {effectiveProvider.label}</span>}
              {providerOptions && <span>Credential mode: {credentialModeLabel(providerOptions.credential_mode)}</span>}
              {providerOptions?.resolved_binding_source && (
                <span>
                  Resolved from: {planningBindingSourceLabel(providerOptions.resolved_binding_source)}
                  {providerOptions.resolved_binding_label ? ` (${providerOptions.resolved_binding_label})` : ''}
                </span>
              )}
              {currentModel && <span>Execution model: {currentModel.label}</span>}
              {effectiveProvider && <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>{effectiveProvider.description}</span>}
              {providerOptions?.allow_model_override && effectiveProvider && (
                <label style={{ display: 'grid', gap: '0.35rem' }}>
                  <span>Model override for this run</span>
                  <select value={planningModelOverride} onChange={e => onPlanningModelOverrideChange(e.target.value)} disabled={creatingRun || !runReady}>
                    {effectiveProvider.models.filter(m => m.enabled).map(m => (
                      <option key={m.id} value={m.id}>{m.label}</option>
                    ))}
                  </select>
                  <small>Changing this only overrides the model for this one run.</small>
                </label>
              )}
              {runBlockedReason && (
                <span style={{ color: 'var(--danger)' }}>
                  {runBlockedReason}
                  {runBlockedReason.toLowerCase().includes('connector') && (
                    <> — <Link to="/connector">Set up connector</Link></>
                  )}
                </span>
              )}
              <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                Workspace policy is managed in <Link to="/settings/models">Model Settings</Link>. Personal credentials are managed in <Link to="/account-bindings">My Bindings</Link>.
              </span>
            </>
          )}
        </div>
      </div>

      <div className="planning-run-actions">
        <button
          className="btn btn-primary"
          onClick={onStartRun}
          disabled={creatingRun || !runReady || providerOptionsLoading || (usesLocalConnector && cliBindingsLoading)}
        >
          {creatingRun ? 'Starting…' : 'Start Planning Run'}
        </button>
        <button
          className="btn btn-ghost"
          onClick={onRefreshRuns}
          disabled={runsLoading || creatingRun}
        >
          {runsLoading ? 'Refreshing…' : 'Refresh Runs'}
        </button>
      </div>

      <div className="planning-placeholder-note">
        Requirement review, candidate review, and candidate-to-task apply are now all available in-place. Use this workspace to move from draft planning to a traceable task set.
      </div>
    </div>
  )
}
