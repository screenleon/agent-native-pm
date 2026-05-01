import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { BacklogItem } from '../../types'
import { BacklogTab } from './BacklogTab'

const listBacklogItems = vi.fn()
const createBacklogItem = vi.fn()
const updateBacklogItem = vi.fn()
const commitBacklogItemToTask = vi.fn()

vi.mock('../../api/client', () => ({
  listBacklogItems: (...args: unknown[]) => listBacklogItems(...args),
  createBacklogItem: (...args: unknown[]) => createBacklogItem(...args),
  updateBacklogItem: (...args: unknown[]) => updateBacklogItem(...args),
  commitBacklogItemToTask: (...args: unknown[]) => commitBacklogItemToTask(...args),
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
  createBacklogItem.mockResolvedValue({ data: null })
  updateBacklogItem.mockResolvedValue({ data: null })
  commitBacklogItemToTask.mockResolvedValue({ data: { already_applied: false } })
})

describe('<BacklogTab /> focus summary', () => {
  /**
   * Counts urgent focus as project-level open urgent with committed context.
   * Steps:
   * 1. Return urgent open, urgent committed, ready, and blocked backlog items.
   * 2. Render the backlog tab.
   * 3. Assert the focus strip separates open urgent from committed urgent.
   */
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

  /**
   * Keeps focus metrics project-wide while the visible list is filtered.
   * Steps:
   * 1. Return one filtered row for the visible list and three rows for summary.
   * 2. Render the backlog tab.
   * 3. Assert visible totals and project-level urgent focus do not collapse to the filter.
   */
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

describe('<BacklogTab /> workflow controls', () => {
  /**
   * Disables commit on blocked backlog rows.
   * Steps:
   * 1. Render one blocked backlog item.
   * 2. Inspect the row action exposed to the user.
   * 3. Assert the commit action is disabled and no commit API call can be triggered.
   */
  it('disables commit action for blocked backlog items', async () => {
    const user = userEvent.setup()
    const blockedItem = makeItem({
      id: 'blocked',
      title: 'Blocked work',
      priority: 'urgent',
      status: 'blocked',
      blocked_reason: 'Needs API contract',
    })
    listBacklogItems.mockResolvedValue({
      data: [blockedItem],
      meta: { page: 1, per_page: 100, total: 1 },
    })

    renderBacklog()

    const row = (await screen.findByText('Blocked work')).closest('.backlog-row')
    if (!(row instanceof HTMLElement)) throw new Error('blocked backlog row not found')
    const commitButton = within(row).getByRole('button', { name: 'Blocked' })
    expect(commitButton).toBeDisabled()

    await user.click(commitButton)
    expect(commitBacklogItemToTask).not.toHaveBeenCalled()
  })

  /**
   * Omits committed status from the create form.
   * Steps:
   * 1. Open the create backlog modal.
   * 2. Inspect the status select options.
   * 3. Assert users cannot create already-committed backlog items from the UI.
   */
  it('does not offer committed status while creating a backlog item', async () => {
    const user = userEvent.setup()
    listBacklogItems.mockResolvedValue({
      data: [],
      meta: { page: 1, per_page: 100, total: 0 },
    })

    renderBacklog()

    await screen.findByLabelText('Backlog current focus')
    await user.click(screen.getByRole('button', { name: '+ New Backlog' }))

    const modal = screen.getByRole('heading', { name: 'Create Backlog Item' }).closest('.modal')
    if (!(modal instanceof HTMLElement)) throw new Error('create backlog modal not found')
    const statusSelect = within(modal).getByDisplayValue('Triage') as HTMLSelectElement
    const optionLabels = Array.from(statusSelect.options).map(option => option.textContent)
    expect(optionLabels).toEqual(['Triage', 'Ready', 'Blocked', 'Archived'])
  })

  /**
   * Keeps task-owned fields read-only after a backlog item is committed.
   * Steps:
   * 1. Render one committed backlog item and open the edit modal.
   * 2. Change a non-task metadata field and save.
   * 3. Assert title/priority remain the original values in the update payload.
   */
  it('locks task-owned fields when editing committed backlog items', async () => {
    const user = userEvent.setup()
    const committedItem = makeItem({
      id: 'committed',
      title: 'Committed urgent',
      description: 'Original description',
      priority: 'urgent',
      status: 'committed',
      task_id: 'task-1',
      labels: ['api'],
    })
    listBacklogItems.mockResolvedValue({
      data: [committedItem],
      meta: { page: 1, per_page: 100, total: 1 },
    })

    renderBacklog()

    const row = (await screen.findByText('Committed urgent')).closest('.backlog-row')
    if (!(row instanceof HTMLElement)) throw new Error('committed backlog row not found')
    await user.click(within(row).getByRole('button', { name: 'Edit' }))

    const modal = screen.getByRole('heading', { name: 'Edit Backlog Item' }).closest('.modal')
    if (!(modal instanceof HTMLElement)) throw new Error('edit backlog modal not found')
    expect(within(modal).getByDisplayValue('Committed urgent')).toBeDisabled()
    expect(within(modal).getByDisplayValue('Original description')).toBeDisabled()
    expect(within(modal).getByDisplayValue('Urgent')).toBeDisabled()

    const labelsInput = within(modal).getByPlaceholderText('api, ui, urgent-path')
    await user.clear(labelsInput)
    await user.type(labelsInput, 'api, committed')
    await user.click(within(modal).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(updateBacklogItem).toHaveBeenCalledTimes(1))
    expect(updateBacklogItem).toHaveBeenCalledWith('committed', expect.objectContaining({
      title: 'Committed urgent',
      description: 'Original description',
      priority: 'urgent',
      status: 'committed',
      labels: ['api', 'committed'],
    }))
  })
})
