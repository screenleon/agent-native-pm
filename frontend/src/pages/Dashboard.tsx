import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  listProjects,
  listLocalConnectors,
  listNotifications,
} from '../api/client'
import type { Project, LocalConnector, Notification, User } from '../types'

interface DashboardProps {
  me: User
}

type NextStep = {
  key: string
  title: string
  body: string
  cta: { to: string; label: string }
  tone: 'info' | 'warn' | 'success'
}

function relativeTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const ts = new Date(iso).getTime()
  if (Number.isNaN(ts)) return ''
  const diffSec = Math.round((Date.now() - ts) / 1000)
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.round(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.round(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.round(diffHr / 24)
  return `${diffDay}d ago`
}

function connectorIsLive(c: LocalConnector): boolean {
  if (c.status === 'revoked') return false
  if (!c.last_seen_at) return c.status === 'online'
  const ageMs = Date.now() - new Date(c.last_seen_at).getTime()
  return c.status !== 'offline' && ageMs < 5 * 60 * 1000
}

export default function Dashboard({ me }: DashboardProps) {
  const [projects, setProjects] = useState<Project[]>([])
  const [connectors, setConnectors] = useState<LocalConnector[]>([])
  const [notifications, setNotifications] = useState<Notification[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      setError(null)
      try {
        const [projResp, connResp, notifResp] = await Promise.all([
          listProjects(),
          listLocalConnectors().catch(() => ({ data: [] as LocalConnector[] })),
          listNotifications(false).catch(() => ({ data: [] as Notification[] })),
        ])
        if (cancelled) return
        setProjects(projResp.data ?? [])
        setConnectors(connResp.data ?? [])
        setNotifications(notifResp.data ?? [])
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load dashboard')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => {
      cancelled = true
    }
  }, [])

  const liveConnectors = useMemo(() => connectors.filter(connectorIsLive), [connectors])
  const unreadNotifications = useMemo(() => notifications.filter(n => !n.is_read), [notifications])
  const recentNotifications = useMemo(
    () => [...notifications].sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()).slice(0, 6),
    [notifications],
  )
  const recentProjects = useMemo(
    () => [...projects].sort((a, b) => new Date(b.updated_at ?? b.created_at).getTime() - new Date(a.updated_at ?? a.created_at).getTime()).slice(0, 6),
    [projects],
  )

  const nextStep: NextStep = useMemo(() => {
    if (connectors.length === 0) {
      return {
        key: 'pair-connector',
        title: 'Pair your local connector',
        body: 'Without a paired connector you cannot run agent-driven planning. Pairing takes about a minute.',
        cta: { to: '/settings/connector', label: 'Pair a connector' },
        tone: 'info',
      }
    }
    if (liveConnectors.length === 0) {
      return {
        key: 'connector-offline',
        title: 'Your connector looks offline',
        body: 'No connector has reported in for the last 5 minutes. Start `bin/anpm-connector serve` on your machine.',
        cta: { to: '/settings/connector', label: 'View connector status' },
        tone: 'warn',
      }
    }
    if (projects.length === 0) {
      return {
        key: 'create-project',
        title: 'Create your first project',
        body: 'Projects group requirements, tasks, documents, and planning runs together.',
        cta: { to: '/projects', label: 'Create project' },
        tone: 'info',
      }
    }
    if (unreadNotifications.length > 0) {
      return {
        key: 'review-notifications',
        title: `You have ${unreadNotifications.length} unread notification${unreadNotifications.length === 1 ? '' : 's'}`,
        body: 'Recent planning runs or drift signals are waiting for review.',
        cta: { to: '/projects', label: 'Open projects' },
        tone: 'info',
      }
    }
    return {
      key: 'all-clear',
      title: 'You are all caught up',
      body: 'Nothing requires your attention right now. Pick a project to keep working.',
      cta: { to: '/projects', label: 'Browse projects' },
      tone: 'success',
    }
  }, [connectors.length, liveConnectors.length, projects.length, unreadNotifications.length])

  if (loading) return <div className="loading">Loading dashboard…</div>

  return (
    <div className="dashboard-page">
      <div className="page-header">
        <div>
          <h2>Hello, {me.username}</h2>
          <p style={{ color: 'var(--text-muted)', marginTop: '0.25rem' }}>
            {projects.length} project{projects.length === 1 ? '' : 's'} · {connectors.length} connector{connectors.length === 1 ? '' : 's'} ·
            {' '}{unreadNotifications.length} unread
          </p>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Link className="btn btn-ghost" to="/projects">All projects</Link>
          <Link className="btn btn-primary" to="/projects">+ New project</Link>
        </div>
      </div>

      {error && <div className="error-message">{error}</div>}

      <OnboardingChecklist
        hasConnector={connectors.length > 0}
        hasLiveConnector={liveConnectors.length > 0}
        hasProject={projects.length > 0}
      />

      <div className={`callout callout-${nextStep.tone}`} style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <div>
            <strong style={{ display: 'block', marginBottom: '0.25rem' }}>{nextStep.title}</strong>
            <span style={{ color: 'var(--text-muted)' }}>{nextStep.body}</span>
          </div>
          <Link className="btn btn-primary" to={nextStep.cta.to}>{nextStep.cta.label}</Link>
        </div>
      </div>

      <div className="dashboard-grid">
        <section className="card">
          <h3>Connectors</h3>
          {connectors.length === 0 ? (
            <p style={{ color: 'var(--text-muted)' }}>No connectors paired yet.</p>
          ) : (
            <ul className="dashboard-list">
              {connectors.map(c => (
                <li key={c.id}>
                  <span className={`status-dot ${connectorIsLive(c) ? 'is-live' : 'is-offline'}`} aria-hidden="true" />
                  <span className="dashboard-list-main">{c.label || c.id.slice(0, 8)}</span>
                  <span className="dashboard-list-meta">
                    {c.platform || 'unknown'} · {c.last_seen_at ? `seen ${relativeTime(c.last_seen_at)}` : 'never seen'}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <div style={{ marginTop: '0.75rem' }}>
            <Link className="btn btn-ghost btn-sm" to="/settings/connector">Manage connectors</Link>
          </div>
        </section>

        <section className="card">
          <h3>Recent projects</h3>
          {recentProjects.length === 0 ? (
            <p style={{ color: 'var(--text-muted)' }}>You have no projects yet.</p>
          ) : (
            <ul className="dashboard-list">
              {recentProjects.map(p => (
                <li key={p.id}>
                  <Link className="dashboard-list-main" to={`/projects/${p.id}`}>{p.name}</Link>
                  <span className="dashboard-list-meta">
                    {p.description ? p.description.slice(0, 64) : 'No description'}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="card">
          <h3>Recent notifications {unreadNotifications.length > 0 && <span className="notification-badge">{unreadNotifications.length}</span>}</h3>
          {recentNotifications.length === 0 ? (
            <p style={{ color: 'var(--text-muted)' }}>No notifications yet.</p>
          ) : (
            <ul className="dashboard-list">
              {recentNotifications.map(n => {
                const link = n.link || (n.project_id ? `/projects/${n.project_id}` : '#')
                return (
                  <li key={n.id} className={n.is_read ? 'is-read' : ''}>
                    {!n.is_read && <span className="status-dot is-live" aria-hidden="true" />}
                    {n.is_read && <span className="status-dot is-offline" aria-hidden="true" />}
                    <Link className="dashboard-list-main" to={link}>{n.title}</Link>
                    <span className="dashboard-list-meta">
                      {n.kind} · {relativeTime(n.created_at)}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      </div>
    </div>
  )
}

interface OnboardingChecklistProps {
  hasConnector: boolean
  hasLiveConnector: boolean
  hasProject: boolean
}

const ONBOARDING_DISMISSED_KEY = 'anpm:onboardingDismissed'

function OnboardingChecklist({ hasConnector, hasLiveConnector, hasProject }: OnboardingChecklistProps) {
  const [dismissed, setDismissed] = useState<boolean>(() => {
    try { return localStorage.getItem(ONBOARDING_DISMISSED_KEY) === '1' } catch { return false }
  })
  const allDone = hasConnector && hasLiveConnector && hasProject
  if (dismissed && !allDone) return null
  if (allDone) return null

  function dismiss() {
    try { localStorage.setItem(ONBOARDING_DISMISSED_KEY, '1') } catch { /* ignore */ }
    setDismissed(true)
  }

  const steps: { key: string; label: string; done: boolean; cta: { to: string; label: string } | null; hint: string }[] = [
    {
      key: 'connector',
      label: 'Pair a local connector',
      done: hasConnector,
      cta: hasConnector ? null : { to: '/settings/connector', label: 'Pair now' },
      hint: 'Required for agent-driven planning runs.',
    },
    {
      key: 'live',
      label: 'Bring the connector online',
      done: hasLiveConnector,
      cta: hasLiveConnector ? null : { to: '/settings/connector', label: 'How to start' },
      hint: 'Run `bin/anpm-connector serve` so it polls for work.',
    },
    {
      key: 'project',
      label: 'Create a project',
      done: hasProject,
      cta: hasProject ? null : { to: '/projects', label: 'Create project' },
      hint: 'Projects hold requirements, tasks, and planning runs.',
    },
  ]

  const completedCount = steps.filter(s => s.done).length

  return (
    <section className="onboarding-card" aria-label="Getting started checklist">
      <div className="onboarding-card-header">
        <div>
          <strong>Getting started</strong>
          <span style={{ color: 'var(--text-muted)', marginLeft: '0.5rem', fontSize: '0.85rem' }}>
            {completedCount} / {steps.length} complete
          </span>
        </div>
        <button type="button" className="btn btn-ghost btn-sm" onClick={dismiss}>Hide</button>
      </div>
      <ol className="onboarding-list">
        {steps.map(step => (
          <li key={step.key} className={step.done ? 'is-done' : ''}>
            <span className="onboarding-check" aria-hidden="true">{step.done ? '✓' : '○'}</span>
            <div className="onboarding-text">
              <span className="onboarding-label">{step.label}</span>
              <span className="onboarding-hint">{step.hint}</span>
            </div>
            {step.cta && <Link className="btn btn-ghost btn-sm" to={step.cta.to}>{step.cta.label}</Link>}
          </li>
        ))}
      </ol>
    </section>
  )
}
