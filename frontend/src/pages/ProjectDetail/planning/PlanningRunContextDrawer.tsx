import { useCallback, useEffect, useRef, useState } from 'react'
import { getContextSnapshot } from '../../../api/client'
import type { ContextSnapshot } from '../../../types'

interface PlanningRunContextDrawerProps {
  runId: string
  /** Controlled open state managed by the parent (PlanningRunList). */
  open: boolean
  onToggle: () => void
}

/**
 * Lazy-loaded collapsible drawer showing the context-pack v2 snapshot for a
 * planning run.  Fetches from GET /api/planning-runs/:id/context-snapshot on
 * first open; subsequent toggles reuse the cached result.
 */
export function PlanningRunContextDrawer({ runId, open, onToggle }: PlanningRunContextDrawerProps) {
  const [snapshot, setSnapshot] = useState<ContextSnapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fetched = useRef(false)

  const load = useCallback(async () => {
    if (fetched.current) return
    setLoading(true)
    try {
      const res = await getContextSnapshot(runId)
      fetched.current = true  // only after success so transient errors can be retried
      setSnapshot(res.data)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load context snapshot')
    } finally {
      setLoading(false)
    }
  }, [runId])

  useEffect(() => {
    if (open) load()
  }, [open, load])

  const hasDropped = snapshot?.available && Object.values(snapshot.dropped_counts ?? {}).some(n => n > 0)

  return (
    <div className="planning-run-context-drawer" style={{ marginTop: '0.5rem' }}>
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        onClick={onToggle}
        style={{ fontSize: '0.78rem', padding: '0.1rem 0.4rem' }}
        aria-expanded={open}
      >
        {open ? '▾ Context' : '▸ Context'}
        {hasDropped && (
          <span
            title="Some context sources were truncated due to byte cap"
            style={{ marginLeft: '0.3rem', color: 'var(--warning, #f59e0b)', fontWeight: 700 }}
          >
            ⚠
          </span>
        )}
      </button>

      {open && (
        <div
          className="planning-run-context-content"
          style={{
            marginTop: '0.4rem',
            padding: '0.6rem 0.75rem',
            background: 'var(--bg-muted, #1a1a2e)',
            borderRadius: '6px',
            fontSize: '0.8rem',
            color: 'var(--text-muted)',
          }}
        >
          {loading && <span>Loading context…</span>}
          {error && <span style={{ color: 'var(--error, #f87171)' }}>{error}</span>}

          {!loading && !error && snapshot && !snapshot.available && (
            <span style={{ fontStyle: 'italic' }}>
              Context data not available — this run predates snapshot saving.
            </span>
          )}

          {!loading && !error && snapshot?.available && (
            <>
              {/* Source counts */}
              <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap', marginBottom: '0.5rem' }}>
                <SourceCount label="tasks" count={snapshot.open_task_count} />
                <SourceCount label="docs" count={snapshot.document_count} />
                <SourceCount label="drift signals" count={snapshot.drift_count} />
                <SourceCount label="agent runs" count={snapshot.agent_run_count} />
                {snapshot.has_sync_run && <SourceCount label="sync run" count={1} />}
              </div>

              {/* V2 envelope: role / intent / scale */}
              {(snapshot.role || snapshot.intent_mode || snapshot.task_scale) && (
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.4rem' }}>
                  {snapshot.role && <MetaChip label="role" value={snapshot.role} />}
                  {snapshot.intent_mode && <MetaChip label="intent" value={snapshot.intent_mode} />}
                  {snapshot.task_scale && <MetaChip label="scale" value={snapshot.task_scale} />}
                </div>
              )}

              {/* Byte budget */}
              <div style={{ marginBottom: '0.4rem' }}>
                <span title={`${snapshot.sources_bytes.toLocaleString()} bytes consumed`}>
                  {formatKB(snapshot.sources_bytes)} KB context
                </span>
                <span style={{ marginLeft: '0.4rem', opacity: 0.6 }}>
                  pack {snapshot.pack_id.slice(0, 8)}
                </span>
              </div>

              {/* Source-of-truth files */}
              {snapshot.source_of_truth && snapshot.source_of_truth.length > 0 && (
                <div style={{ marginBottom: '0.4rem' }}>
                  <span style={{ opacity: 0.7 }}>Source of truth: </span>
                  {snapshot.source_of_truth.map((ref, i) => (
                    <span key={i} title={ref.path || ref.name} style={{ marginRight: '0.4rem' }}>
                      <code style={{ fontSize: '0.78rem' }}>{ref.name}</code>
                      {ref.role && <span style={{ opacity: 0.6 }}> ({ref.role})</span>}
                      {i < snapshot.source_of_truth!.length - 1 && ', '}
                    </span>
                  ))}
                </div>
              )}

              {/* Truncation warnings */}
              {hasDropped && (
                <div
                  style={{
                    marginTop: '0.4rem',
                    padding: '0.3rem 0.5rem',
                    background: 'rgba(245,158,11,0.12)',
                    borderRadius: '4px',
                    color: 'var(--warning, #f59e0b)',
                  }}
                >
                  <strong>Context truncated</strong> — byte cap reached:
                  {Object.entries(snapshot.dropped_counts)
                    .filter(([, n]) => n > 0)
                    .map(([k, n]) => (
                      <span key={k} style={{ marginLeft: '0.5rem' }}>
                        {n} {k}
                      </span>
                    ))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

function SourceCount({ label, count }: { label: string; count: number }) {
  return (
    <span>
      <strong>{count}</strong> {label}
    </span>
  )
}

function MetaChip({ label, value }: { label: string; value: string }) {
  return (
    <span
      style={{
        background: 'var(--bg-subtle, rgba(255,255,255,0.06))',
        borderRadius: '4px',
        padding: '0.1rem 0.35rem',
        fontSize: '0.76rem',
      }}
    >
      {label}: <strong>{value}</strong>
    </span>
  )
}

function formatKB(bytes: number): string {
  return (bytes / 1024).toFixed(1)
}
