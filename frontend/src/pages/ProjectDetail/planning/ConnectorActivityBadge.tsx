import type { ConnectorPhase } from '../../../types';
import { useConnectorActivity, type ActivitySource } from '../../../hooks/useConnectorActivity';

const PHASE_LABELS: Record<ConnectorPhase, string> = {
  idle: 'idle',
  claiming_run: 'claiming',
  planning: 'planning',
  claiming_task: 'claiming task',
  dispatching: 'dispatching',
  submitting: 'submitting',
};

function phaseColor(phase: ConnectorPhase): string {
  switch (phase) {
    case 'idle': return '#888';
    case 'claiming_run':
    case 'claiming_task': return '#f0a500';
    case 'planning': return '#3b82f6';
    case 'dispatching':
    case 'submitting': return '#10b981';
    default: return '#888';
  }
}

function sourceSuffix(source: ActivitySource): string {
  if (source === 'stale') return ' (stale)';
  return '';
}

interface Props {
  connectorId: string;
  variant?: 'compact' | 'standard' | 'full';
  label?: string;
}

export function ConnectorActivityBadge({ connectorId, variant = 'standard', label }: Props) {
  const { activity, online, source } = useConnectorActivity(connectorId);
  const phase: ConnectorPhase = activity?.phase ?? 'idle';
  const color = online ? phaseColor(phase) : '#888';
  const stale = source === 'stale';

  if (variant === 'compact') {
    return (
      <span
        className="connector-activity-badge compact"
        style={{ color, opacity: stale ? 0.6 : 1, fontSize: '0.75rem' }}
        title={`${label ?? connectorId}: ${PHASE_LABELS[phase]}${sourceSuffix(source)}`}
      >
        ● {PHASE_LABELS[phase]}
      </span>
    );
  }

  if (variant === 'full') {
    return (
      <div className="connector-activity-badge full" style={{ opacity: stale ? 0.6 : 1 }}>
        <span className="connector-activity-dot" style={{ color }}>●</span>
        <span className="connector-activity-label">{label ?? connectorId}</span>
        <span className="connector-activity-phase" style={{ color }}>{PHASE_LABELS[phase]}</span>
        {activity?.subject_title && (
          <span className="connector-activity-subject" title={activity.subject_id}>
            {activity.subject_title}
          </span>
        )}
        {activity?.step && (
          <span className="connector-activity-step">{activity.step}</span>
        )}
        {stale && <span className="connector-activity-stale">stale</span>}
      </div>
    );
  }

  // standard
  return (
    <span
      className="connector-activity-badge standard"
      style={{ color, opacity: stale ? 0.6 : 1, fontSize: '0.8rem' }}
    >
      <span style={{ marginRight: '0.25em' }}>●</span>
      {PHASE_LABELS[phase]}
      {activity?.subject_title && ` — ${activity.subject_title}`}
      {stale && <span style={{ color: '#888', marginLeft: '0.25em' }}>(stale)</span>}
    </span>
  );
}
