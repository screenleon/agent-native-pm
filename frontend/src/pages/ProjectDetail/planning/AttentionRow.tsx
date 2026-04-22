interface AttentionRowProps {
  requirementsAwaitingPlanning: number
  candidatesAwaitingReview: number
  appliedOpenTasks: number
  openDriftCount: number
  onJumpToRequirements: () => void
  onJumpToCandidates: () => void
  onJumpToTasks: () => void
  onJumpToDrift: () => void
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
        border: `1px solid ${applyAttention ? 'var(--danger, #ef4444)' : 'var(--border)'}`,
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
 * All counts are derived from project state already loaded by
 * ProjectDetail / PlanningTab. S2 intentionally adds no new API call.
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
}: AttentionRowProps) {
  return (
    <div
      className="attention-row"
      role="region"
      aria-label="Planning attention row"
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
        gap: '0.55rem',
        marginBottom: '1rem',
      }}
    >
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
  )
}
