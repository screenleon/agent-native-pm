import { useState } from 'react'
import { Link } from 'react-router-dom'
import { createRequirement, getPlanningProviderOptions, createPlanningRun, demoSeed } from '../../../api/client'
import { RequirementWizardModal } from './RequirementWizardModal'

interface Props {
  projectId: string
  onRunCreated: (requirementId: string, runId: string) => void
  planningRunsCount: number
  planningRunReady?: boolean
  onRunWhatsnext?: () => void
  runningWhatsnext?: boolean
}

export function WorkspaceOnboardingPanel({ projectId, onRunCreated, planningRunsCount, planningRunReady, onRunWhatsnext, runningWhatsnext }: Props) {
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [noProvider, setNoProvider] = useState(false)
  const [showWizard, setShowWizard] = useState(false)
  const [demoBusy, setDemoBusy] = useState(false)
  const [demoDismissed, setDemoDismissed] = useState(
    () => localStorage.getItem(`anpm_demo_dismissed_${projectId}`) === '1'
  )

  const showDemoBanner = planningRunsCount === 0 && !demoDismissed

  async function startPlanning(title: string, audience?: string, successCriteria?: string) {
    if (!title.trim() || busy) return
    setBusy(true)
    setError(null)
    setNoProvider(false)
    try {
      const reqResp = await createRequirement(projectId, {
        title: title.trim(),
        source: 'onboarding',
        ...(audience ? { audience } : {}),
        ...(successCriteria ? { success_criteria: successCriteria } : {}),
      })
      const requirementId = reqResp.data.id

      const provResp = await getPlanningProviderOptions(projectId)
      const opts = provResp.data
      if (!opts.can_run || !opts.default_selection) {
        setNoProvider(true)
        setBusy(false)
        return
      }

      const runResp = await createPlanningRun(requirementId, {
        adapter_type: 'backlog',
        execution_mode: opts.default_selection ? opts.available_execution_modes[0] : 'deterministic',
      })
      onRunCreated(requirementId, runResp.data.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  function handleWizardSave(title: string, audience: string, successCriteria: string) {
    setShowWizard(false)
    void startPlanning(title, audience, successCriteria)
  }

  async function handleDemoSeed() {
    setDemoBusy(true)
    setError(null)
    try {
      const resp = await demoSeed(projectId)
      onRunCreated(resp.data.requirement_id, resp.data.planning_run_id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Demo seed failed')
    } finally {
      setDemoBusy(false)
    }
  }

  function dismissDemo() {
    localStorage.setItem(`anpm_demo_dismissed_${projectId}`, '1')
    setDemoDismissed(true)
  }

  return (
    <div className="planning-welcome-view">
      <div className="planning-welcome-how">
        <h3 style={{ margin: '0 0 0.35rem' }}>How it works</h3>
        <ol className="planning-welcome-steps">
          <li>Describe a feature or goal — this becomes a <strong>requirement</strong>.</li>
          <li>The AI breaks it down into a prioritized <strong>backlog</strong> of draft tasks.</li>
          <li>You review the draft, approve what fits, and apply them as real tasks.</li>
        </ol>
      </div>

      {showDemoBanner && (
        <div className="planning-welcome-demo">
          <span>Want to skip ahead? Load a sample requirement + backlog to see the full flow.</span>
          <div style={{ display: 'flex', gap: '0.5rem', whiteSpace: 'nowrap' }}>
            <button className="btn btn-primary btn-sm" onClick={handleDemoSeed} disabled={demoBusy}>
              {demoBusy ? 'Loading…' : 'Load demo'}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={dismissDemo}>Dismiss</button>
          </div>
        </div>
      )}

      <div className="planning-welcome-input-area">
        <label htmlFor="onboarding-input" style={{ fontWeight: 600, fontSize: '1rem' }}>
          What are you building?
        </label>
        <div className="planning-welcome-input-row">
          <input
            id="onboarding-input"
            type="text"
            value={input}
            onChange={e => setInput(e.target.value)}
            placeholder="e.g. Add Google SSO login for enterprise customers"
            style={{ flex: 1, minWidth: '14rem' }}
            onKeyDown={e => { if (e.key === 'Enter' && !busy) void startPlanning(input) }}
            autoFocus
          />
          <button className="btn btn-primary" onClick={() => startPlanning(input)} disabled={busy || !input.trim() || noProvider}>
            {busy ? 'Starting…' : 'Generate backlog →'}
          </button>
          <button className="btn btn-secondary" type="button" onClick={() => setShowWizard(true)} disabled={busy} title="Add audience and success criteria for better results">
            Add context…
          </button>
        </div>

        {noProvider && (
          <div style={{ color: 'var(--danger)', fontSize: '0.88rem' }}>
            No AI provider configured. <Link to="/settings/models-hub">Set one up in Model Settings →</Link>
          </div>
        )}

        {!noProvider && planningRunReady === false && (
          <div style={{ color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            No planning provider configured yet. <Link to="/settings/models-hub">Set up Model Settings →</Link> to run the AI step. You can still capture requirements now.
          </div>
        )}

        {error && <div className="error-banner">{error}</div>}
      </div>

      {planningRunReady && onRunWhatsnext && (
        <div className="planning-welcome-whatsnext">
          <span>Already have tasks and want a health check?</span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={onRunWhatsnext}
            disabled={runningWhatsnext || busy}
          >
            {runningWhatsnext ? 'Starting…' : "Run What's Next →"}
          </button>
        </div>
      )}

      {showWizard && (
        <RequirementWizardModal initialTitle={input} onSave={handleWizardSave} onClose={() => setShowWizard(false)} />
      )}
    </div>
  )
}
