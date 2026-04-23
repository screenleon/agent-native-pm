import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AgentRun, DriftSignal, ProjectSummary, Requirement } from '../../types'
import { ProjectOverviewTab } from './ProjectOverviewTab'

function makeSummary(overrides: Partial<ProjectSummary> = {}): ProjectSummary {
  return {
    project_id: 'p1',
    snapshot_date: '2026-04-22',
    total_tasks: 10,
    tasks_todo: 3,
    tasks_in_progress: 4,
    tasks_done: 3,
    tasks_cancelled: 0,
    total_documents: 5,
    stale_documents: 1,
    health_score: 82,
    ...overrides,
  }
}

function makeRequirement(overrides: Partial<Requirement> = {}): Requirement {
  return {
    id: 'req-1',
    project_id: 'p1',
    title: 'Requirement A',
    summary: '',
    description: '',
    status: 'draft',
    source: 'user',
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

describe('<ProjectOverviewTab />', () => {
  it('renders empty-state copy when nothing is populated yet', () => {
    render(
      <ProjectOverviewTab
        requirements={[]}
        openDriftCount={0}
        driftSignals={[]}
        agentRuns={[]}
        summary={null}
        onSetTab={() => {}}
      />,
    )

    expect(screen.getByText(/No requirements submitted yet/i)).toBeInTheDocument()
    expect(screen.getByText(/No open drift signals/i)).toBeInTheDocument()
    expect(screen.getByText(/Run sync to populate task counts/i)).toBeInTheDocument()
    expect(screen.getByText(/No agent runs recorded yet/i)).toBeInTheDocument()
  })

  it('summarises populated counts and pluralises correctly', () => {
    render(
      <ProjectOverviewTab
        requirements={[makeRequirement({ id: 'r1' }), makeRequirement({ id: 'r2' })]}
        openDriftCount={3}
        driftSignals={[{ id: 'd1' } as DriftSignal]}
        agentRuns={[{ id: 'a1' } as AgentRun]}
        summary={makeSummary()}
        onSetTab={() => {}}
      />,
    )

    expect(screen.getByText(/2 requirements on file/i)).toBeInTheDocument()
    expect(screen.getByText(/3 open drift signals need triage/i)).toBeInTheDocument()
    expect(screen.getByText(/4 in progress · 3 done · 10 total/i)).toBeInTheDocument()
    expect(screen.getByText(/1 agent run on record/i)).toBeInTheDocument()
  })

  it('disables the Drift / Activity buttons when their lists are empty', () => {
    render(
      <ProjectOverviewTab
        requirements={[]}
        openDriftCount={0}
        driftSignals={[]}
        agentRuns={[]}
        summary={null}
        onSetTab={() => {}}
      />,
    )

    expect(screen.getByRole('button', { name: /Open Drift/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Open Activity/i })).toBeDisabled()
  })

  it('invokes onSetTab with the target tab when a rail button is clicked', async () => {
    const onSetTab = vi.fn()
    render(
      <ProjectOverviewTab
        requirements={[makeRequirement()]}
        openDriftCount={0}
        driftSignals={[]}
        agentRuns={[]}
        summary={makeSummary()}
        onSetTab={onSetTab}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /Open Planning/i }))
    expect(onSetTab).toHaveBeenCalledWith('planning')
    await userEvent.click(screen.getByRole('button', { name: /Open Tasks/i }))
    expect(onSetTab).toHaveBeenCalledWith('tasks')
  })
})
