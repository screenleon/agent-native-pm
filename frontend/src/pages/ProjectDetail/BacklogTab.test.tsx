import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import type { BacklogItem } from '../../types'
import { BacklogTab } from './BacklogTab'

const listBacklogItems = vi.fn()

vi.mock('../../api/client', () => ({
  listBacklogItems: (...args: unknown[]) => listBacklogItems(...args),
  createBacklogItem: vi.fn(),
  updateBacklogItem: vi.fn(),
  commitBacklogItemToTask: vi.fn(),
}))

function makeItem(overrides: Partial<BacklogItem>): BacklogItem {
  return {
    id: overrides.id ?? 'backlog-1',
    project_id: 'project-1',
    title: overrides.title ?? 'Backlog item',
    description: overrides.description ?? '',
    status: overrides.status ?? 'triage',
    priority: overrides.priority ?? 'medium',
    source: overrides.source ?? 'human',
    rank: overrides.rank ?? 0,
    labels: overrides.labels ?? [],
    acceptance_criteria: overrides.acceptance_criteria ?? '',
    blocked_reason: overrides.blocked_reason ?? '',
    created_at: overrides.created_at ?? '2026-05-01T00:00:00Z',
    updated_at: overrides.updated_at ?? '2026-05-01T00:00:00Z',
    ...overrides,
  }
}

function renderBacklog() {
  return render(
    <BacklogTab
      projectId="project-1"
      onReload={vi.fn()}
      onError={vi.fn()}
      onSuccess={vi.fn()}
      onNavigateToTasks={vi.fn()}
    />,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('<BacklogTab /> focus summary', () => {
  it('counts urgent focus as open urgent while showing total and committed context', async () => {
    const backlogItems = [
      makeItem({ id: 'urgent-open', title: 'Open urgent', priority: 'urgent', status: 'triage' }),
      makeItem({ id: 'urgent-committed', title: 'Committed urgent', priority: 'urgent', status: 'committed', task_id: 'task-1' }),
      makeItem({ id: 'ready', title: 'Ready work', priority: 'high', status: 'ready' }),
      makeItem({ id: 'blocked', title: 'Blocked work', priority: 'medium', status: 'blocked' }),
    ]
    listBacklogItems.mockResolvedValue({
      data: backlogItems,
      meta: { page: 1, per_page: 100, total: backlogItems.length },
    })

    renderBacklog()

    const summary = await screen.findByLabelText('Backlog current focus')
    await waitFor(() => expect(within(summary).getByText('Urgent open')).toBeInTheDocument())

    expect(within(summary).getByText('2 total · 1 committed')).toBeInTheDocument()
    expect(within(summary).getByText('Blocked open')).toBeInTheDocument()
    expect(within(summary).getByText('Ready open')).toBeInTheDocument()
    expect(within(summary).getByText('Already converted to tasks')).toBeInTheDocument()
  })

  it('keeps focus summary project-level when the visible list is filtered', async () => {
    const filteredItems = [
      makeItem({ id: 'ready', title: 'Ready work', priority: 'high', status: 'ready', labels: ['ui'] }),
    ]
    const projectItems = [
      ...filteredItems,
      makeItem({ id: 'urgent-open', title: 'Open urgent', priority: 'urgent', status: 'triage', labels: ['api'] }),
      makeItem({ id: 'blocked', title: 'Blocked work', priority: 'medium', status: 'blocked', labels: ['api'] }),
    ]
    listBacklogItems
      .mockResolvedValueOnce({ data: filteredItems, meta: { page: 1, per_page: 100, total: filteredItems.length } })
      .mockResolvedValueOnce({ data: projectItems, meta: { page: 1, per_page: 500, total: projectItems.length } })

    renderBacklog()

    const summary = await screen.findByLabelText('Backlog current focus')
    expect(within(summary).getByText('Urgent open')).toBeInTheDocument()
    expect(within(summary).getByText('1 total · 0 committed')).toBeInTheDocument()
    expect(screen.getByText('Showing 1 of 1 matching · 3 project total')).toBeInTheDocument()
  })
})
