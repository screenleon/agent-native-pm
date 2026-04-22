import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Requirement } from '../../../types'
import { RequirementQueue } from './RequirementQueue'

function makeRequirement(overrides: Partial<Requirement> = {}): Requirement {
  return {
    id: 'r1',
    project_id: 'p1',
    title: 'Improve sync failure UX',
    summary: '',
    description: '',
    status: 'draft',
    source: 'human',
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

describe('<RequirementQueue />', () => {
  it('renders the empty state with zero requirements', () => {
    render(
      <RequirementQueue
        requirements={[]}
        selectedRequirementId={null}
        onSelectRequirement={() => {}}
        planningLoadError={null}
      />,
    )
    expect(screen.getByText(/No requirements yet/i)).toBeInTheDocument()
  })

  it('renders status counters and a card per requirement', () => {
    render(
      <RequirementQueue
        requirements={[
          makeRequirement({ id: 'r1', status: 'draft' }),
          makeRequirement({ id: 'r2', title: 'Another', status: 'planned' }),
          makeRequirement({ id: 'r3', title: 'Old', status: 'archived' }),
        ]}
        selectedRequirementId="r1"
        onSelectRequirement={() => {}}
        planningLoadError={null}
      />,
    )
    expect(screen.getByText(/1 draft/i)).toBeInTheDocument()
    expect(screen.getByText(/1 planned/i)).toBeInTheDocument()
    expect(screen.getByText(/1 archived/i)).toBeInTheDocument()
    // Title appears as rendered text (original is in <strong>; the badge is a separate element)
    expect(screen.getByText('Improve sync failure UX')).toBeInTheDocument()
    expect(screen.getByText('Another')).toBeInTheDocument()
  })

  it('surfaces the planningLoadError banner when provided', () => {
    render(
      <RequirementQueue
        requirements={[]}
        selectedRequirementId={null}
        onSelectRequirement={() => {}}
        planningLoadError="planning index unavailable"
      />,
    )
    expect(screen.getByText(/planning index unavailable/i)).toBeInTheDocument()
  })

  it('fires onSelectRequirement with the clicked id', async () => {
    const onSelectRequirement = vi.fn()
    render(
      <RequirementQueue
        requirements={[makeRequirement({ id: 'r1' })]}
        selectedRequirementId={null}
        onSelectRequirement={onSelectRequirement}
        planningLoadError={null}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /Improve sync failure UX/i }))
    expect(onSelectRequirement).toHaveBeenCalledWith('r1')
  })
})
