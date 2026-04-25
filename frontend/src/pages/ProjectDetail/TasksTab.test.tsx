import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import type { Task } from '../../types'
import { TasksTab } from './TasksTab'

type TaskFilters = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string }

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 't1',
    project_id: 'p1',
    title: 'Wire up SSE fallback',
    description: '',
    status: 'todo',
    priority: 'medium',
    assignee: '',
    source: 'human',
    created_at: '2026-04-22T10:00:00Z',
    updated_at: '2026-04-22T10:00:00Z',
    ...overrides,
  }
}

const emptyFilters: TaskFilters = { status: '', priority: '', assignee: '' }

const baseProps = {
  projectId: 'p1',
  summary: null,
  taskSort: 'created_at',
  taskOrder: 'desc',
  onSortChange: vi.fn(),
  onOrderChange: vi.fn(),
  onFilterChange: vi.fn(),
  onReload: vi.fn(),
  onError: vi.fn(),
  onSuccess: vi.fn(),
}

describe('<TasksTab />', () => {
  it('renders the default empty state when no filters are active', () => {
    render(<TasksTab {...baseProps} tasks={[]} taskFilters={emptyFilters} />)
    expect(screen.getByText(/No tasks yet/i)).toBeInTheDocument()
    expect(screen.getByText(/Create your first task/i)).toBeInTheDocument()
  })

  it('renders the filtered-empty state when filters are active but produce no matches', () => {
    const activeFilters: TaskFilters = { status: 'done', priority: '', assignee: '' }
    render(<TasksTab {...baseProps} tasks={[]} taskFilters={activeFilters} />)
    expect(screen.getByText(/No tasks match the current filters/i)).toBeInTheDocument()
  })

  it('renders a task row for each task in the list', () => {
    const tasks = [
      makeTask({ id: 'a', title: 'First' }),
      makeTask({ id: 'b', title: 'Second', status: 'in_progress', priority: 'high' }),
    ]
    render(<TasksTab {...baseProps} tasks={tasks} taskFilters={emptyFilters} />)
    expect(screen.getByText('First')).toBeInTheDocument()
    expect(screen.getByText('Second')).toBeInTheDocument()
    // Status badge cell (not the filter <option>)
    const [badge] = screen.getAllByText('in progress', { selector: 'span.badge' })
    expect(badge).toBeInTheDocument()
  })

  // --- Phase 6b: dispatch_status display ---

  it('T-6b-UI-1: renders queued badge for dispatch_status=queued', () => {
    const tasks = [makeTask({ id: 'q1', title: 'Queued task', dispatch_status: 'queued', source: 'role_dispatch:backend-architect' })]
    render(<TasksTab {...baseProps} tasks={tasks} taskFilters={emptyFilters} />)
    expect(screen.getByTestId('dispatch-badge-queued')).toBeInTheDocument()
    expect(screen.getByTestId('dispatch-badge-queued').textContent).toBe('待執行')
  })

  it('T-6b-UI-2: renders running badge for dispatch_status=running', () => {
    const tasks = [makeTask({ id: 'r1', title: 'Running task', dispatch_status: 'running', source: 'role_dispatch:backend-architect' })]
    render(<TasksTab {...baseProps} tasks={tasks} taskFilters={emptyFilters} />)
    expect(screen.getByTestId('dispatch-badge-running')).toBeInTheDocument()
    expect(screen.getByTestId('dispatch-badge-running').textContent).toBe('執行中…')
  })

  it('T-6b-UI-3: renders completed badge with expandable result block; clicking shows file paths', () => {
    const result = { files: ['src/api.go', 'src/store.go'] }
    const tasks = [
      makeTask({
        id: 'c1',
        title: 'Completed task',
        dispatch_status: 'completed',
        execution_result: result as Record<string, unknown>,
        source: 'role_dispatch:backend-architect',
      }),
    ]
    render(<TasksTab {...baseProps} tasks={tasks} taskFilters={emptyFilters} />)
    const toggle = screen.getByTestId('dispatch-badge-completed')
    expect(toggle).toBeInTheDocument()
    // Result block not visible before click
    expect(screen.queryByTestId('dispatch-result-block')).toBeNull()
    fireEvent.click(toggle)
    // Result block visible after click
    const block = screen.getByTestId('dispatch-result-block')
    expect(block).toBeInTheDocument()
    expect(block.textContent).toContain('src/api.go')
    expect(block.textContent).toContain('src/store.go')
  })
})
