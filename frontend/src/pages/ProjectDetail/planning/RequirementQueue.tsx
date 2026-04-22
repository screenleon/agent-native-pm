import type { Requirement } from '../../../types'
import { formatRelativeTime } from '../../../utils/formatters'
import { requirementStatusBadgeClass } from './labels'

interface RequirementQueueProps {
  requirements: Requirement[]
  selectedRequirementId: string | null
  onSelectRequirement: (id: string) => void
  planningLoadError: string | null
}

/**
 * Requirement queue listing all draft / planned / archived requirements for
 * the project. Clicking a card selects it; the selected requirement drives
 * the downstream PlanningLauncher + PlanningRunList + CandidateReviewPanel
 * surfaces.
 */
export function RequirementQueue({
  requirements,
  selectedRequirementId,
  onSelectRequirement,
  planningLoadError,
}: RequirementQueueProps) {
  const draftCount = requirements.filter(r => r.status === 'draft').length
  const plannedCount = requirements.filter(r => r.status === 'planned').length
  const archivedCount = requirements.filter(r => r.status === 'archived').length

  return (
    <div className="card">
      <div className="planning-stage-header">
        <div>
          <h3 style={{ marginBottom: '0.25rem' }}>Requirement Queue</h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
            Draft requirements stay here until they move through planning runs, candidate review, and apply-to-task flow.
          </p>
        </div>
        <div className="planning-stats">
          <span className="badge badge-todo">{draftCount} draft</span>
          <span className="badge badge-fresh">{plannedCount} planned</span>
          <span className="badge badge-low">{archivedCount} archived</span>
        </div>
      </div>

      {planningLoadError && <div className="error-banner" style={{ marginTop: '1rem' }}>{planningLoadError}</div>}

      {requirements.length === 0 ? (
        <div className="empty-state">
          <h3>No requirements yet</h3>
          <p>Use the intake form to capture the first planning requirement for this project.</p>
        </div>
      ) : (
        <div className="requirement-list">
          {requirements.map(requirement => (
            <button
              key={requirement.id}
              type="button"
              className={`requirement-card ${selectedRequirementId === requirement.id ? 'is-active' : ''}`}
              onClick={() => onSelectRequirement(requirement.id)}
            >
              <div className="requirement-card-top">
                <strong>{requirement.title}</strong>
                <span className={`badge ${requirementStatusBadgeClass(requirement.status)}`}>{requirement.status}</span>
              </div>
              {requirement.summary && <p>{requirement.summary}</p>}
              {requirement.description && <div className="requirement-description">{requirement.description}</div>}
              <div className="requirement-meta">
                <span>{requirement.source}</span>
                <span>Updated {formatRelativeTime(requirement.updated_at)}</span>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
