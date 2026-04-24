import { useState } from 'react'
import { Link } from 'react-router-dom'
import { createRequirement, getPlanningProviderOptions, createPlanningRun, demoSeed } from '../../../api/client'
import { RequirementWizardModal } from './RequirementWizardModal'

interface Props {
  projectId: string
  onRunCreated: (requirementId: string, runId: string) => void
  onWhatsnext: () => void
  planningRunsCount: number
}

export function WorkspaceOnboardingPanel({ projectId, onRunCreated, onWhatsnext, planningRunsCount }: Props) {
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
    if (!title.trim()) return
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
    <div className="workspace-onboarding-panel">
      {showDemoBanner && (
        <div className="helper-note" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', marginBottom: '1rem' }}>
          <span>
            New here? Try the demo: we&apos;ll drop a sample requirement + approved backlog into this project so you can see the full loop.
          </span>
          <div style={{ display: 'flex', gap: '0.5rem', whiteSpace: 'nowrap' }}>
            <button className="btn btn-primary btn-sm" onClick={handleDemoSeed} disabled={demoBusy}>
              {demoBusy ? 'Loading…' : 'Show me'}
            </button>
            <button className="btn btn-secondary btn-sm" onClick={dismissDemo}>
              Not now
            </button>
          </div>
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <div style={{ flex: 1, minWidth: '16rem' }}>
          <label htmlFor="onboarding-input" style={{ display: 'block', marginBottom: '0.35rem', fontWeight: 500 }}>
            What are you working on?
          </label>
          <input
            id="onboarding-input"
            type="text"
            value={input}
            onChange={e => setInput(e.target.value)}
            placeholder="Describe your goal or feature…"
            style={{ width: '100%', boxSizing: 'border-box' }}
            onKeyDown={e => { if (e.key === 'Enter') void startPlanning(input) }}
          />
        </div>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
          <button
            className="btn btn-primary"
            onClick={() => startPlanning(input)}
            disabled={busy || !input.trim() || noProvider}
          >
            {busy ? 'Starting…' : 'Start planning →'}
          </button>
          <button
            className="btn btn-secondary"
            onClick={() => setShowWizard(true)}
            disabled={busy}
            type="button"
          >
            Refine (audience + success)
          </button>
          <button
            className="btn btn-ghost"
            onClick={onWhatsnext}
            disabled={busy}
            type="button"
          >
            What should I focus on next?
          </button>
        </div>
      </div>

      {noProvider && (
        <div style={{ marginTop: '0.75rem', color: 'var(--danger)', fontSize: '0.88rem' }}>
          No planning provider configured.{' '}
          <Link to="/settings/models-hub">Set one up →</Link>
        </div>
      )}

      {error && (
        <div className="error-banner" style={{ marginTop: '0.75rem' }}>{error}</div>
      )}

      {showWizard && (
        <RequirementWizardModal
          initialTitle={input}
          onSave={handleWizardSave}
          onClose={() => setShowWizard(false)}
        />
      )}
    </div>
  )
}
