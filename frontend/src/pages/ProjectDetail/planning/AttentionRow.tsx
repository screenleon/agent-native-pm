interface AttentionRowProps {
  requirementsAwaitingPlanning: number
  candidatesAwaitingReview: number
  appliedOpenTasks: number
  openDriftCount: number
  onJumpToRequirements: () => void
  onJumpToCandidates: () => void
  onJumpToTasks: () => void
  onJumpToDrift: () => void
  // What's Next action — optional so existing callers without a connector don't break
  onRunWhatsnext?: () => void
  runningWhatsnext?: boolean
  whatsnextReady?: boolean
}

interface TileProps {
  label: string
  count: number
  tone: 'attention' | 'info' | 'muted'
  disabled?: boolean
  onClick: () => void
}

function AttentionTile({ label, count, tone, disabled, onClick }: TileProps) {
  const toneBadge =
    tone === 'attention'
      ? 'badge-stale'
      : tone === 'info'
        ? 'badge-medium'
        : 'badge-low'
  const applyAttention = tone === 'attention' && count > 0
  return (
    <button
      type="button"
      className="attention-tile"
      onClick={onClick}
      disabled={disabled ?? count === 0}
      aria-label={`${label}: ${count}`}
      style={{
        display: 'grid',
        gridTemplateColumns: 'auto 1fr',
        alignItems: 'center',
        gap: '0.55rem',
        padding: '0.55rem 0.85rem',
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        borderLeft: applyAttention ? '3px solid var(--danger, #ef4444)' : '1px solid var(--border)',
        borderRadius: '0.55rem',
        cursor: (disabled ?? count === 0) ? 'default' : 'pointer',
        opacity: (disabled ?? count === 0) ? 0.6 : 1,
        color: 'inherit',
        textAlign: 'left',
      }}
    >
      <span className={`badge ${toneBadge}`} style={{ minWidth: '1.8rem', justifyContent: 'center' }}>
        {count}
      </span>
      <span style={{ fontSize: '0.85rem' }}>{label}</span>
    </button>
  )
}

/**
 * Pending-decision summary at the top of the Planning workspace. Surfaces
 * four counts with click-through so the operator can answer the "what's
 * blocked on me right now?" question without scanning every tab.
 *
 * The optional What's Next button gives a one-click path to a full project
 * health analysis without requiring a pre-selected requirement.
 */
export function AttentionRow({
  requirementsAwaitingPlanning,
  candidatesAwaitingReview,
  appliedOpenTasks,
  openDriftCount,
  onJumpToRequirements,
  onJumpToCandidates,
  onJumpToTasks,
  onJumpToDrift,
  onRunWhatsnext,
  runningWhatsnext = false,
  whatsnextReady = false,
}: AttentionRowProps) {
  return (
    <div
      className="attention-row"
      role="region"
      aria-label="Planning attention row"
      style={{
        display: 'flex',
        gap: '0.55rem',
        alignItems: 'stretch',
        flexWrap: 'wrap',
        marginBottom: '1rem',
      }}
    >
      <div style={{
        flex: 1,
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
        gap: '0.55rem',
        minWidth: 0,
      }}>
        <AttentionTile
          label="Requirements awaiting planning"
          count={requirementsAwaitingPlanning}
          tone="attention"
          onClick={onJumpToRequirements}
        />
        <AttentionTile
          label="Candidates awaiting review"
          count={candidatesAwaitingReview}
          tone="attention"
          onClick={onJumpToCandidates}
        />
        <AttentionTile
          label="Applied tasks still open"
          count={appliedOpenTasks}
          tone="info"
          onClick={onJumpToTasks}
        />
        <AttentionTile
          label="Open drift signals"
          count={openDriftCount}
          tone="attention"
          onClick={onJumpToDrift}
        />
      </div>

      {onRunWhatsnext && (
        <button
          type="button"
          className="btn btn-secondary"
          onClick={onRunWhatsnext}
          disabled={runningWhatsnext || !whatsnextReady}
          aria-label="Run What's Next project health analysis"
          title={whatsnextReady ? "Analyse the full project state and surface the most urgent work" : "Configure a planning provider or connect a local connector to enable this"}
          style={{
            alignSelf: 'stretch',
            padding: '0.55rem 1rem',
            whiteSpace: 'nowrap',
            fontSize: '0.875rem',
          }}
        >
          {runningWhatsnext ? 'Analysing…' : "What's Next"}
        </button>
      )}
    </div>
  )
}
