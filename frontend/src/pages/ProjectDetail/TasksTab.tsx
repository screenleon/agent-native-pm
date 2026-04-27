import { useState, useEffect } from 'react'
import type { Task, ProjectSummary } from '../../types'
import { createTask, updateTask, deleteTask, batchUpdateTasks, listProjectTaskLineage, requeueDispatchTask, type AppliedLineageEntry } from '../../api/client'

// ---------------------------------------------------------------------------
// DispatchStatusBadge — inline indicator for role_dispatch execution lifecycle
// ---------------------------------------------------------------------------
interface DispatchStatusBadgeProps { task: Task; onRequeue?: () => void }

function DispatchStatusBadge({ task, onRequeue }: DispatchStatusBadgeProps) {
  const [expanded, setExpanded] = useState(false)
  const ds = task.dispatch_status

  if (!ds || ds === 'none') return null

  const labelMap: Record<string, string> = {
    queued: 'Queued',
    running: 'Running…',
    completed: 'Completed',
    failed: 'Failed',
  }
  const colorMap: Record<string, string> = {
    queued: 'var(--text-muted)',
    running: 'var(--color-info, #6366f1)',
    completed: 'var(--color-success, #22c55e)',
    failed: 'var(--color-danger, #ef4444)',
  }
  const label = labelMap[ds] ?? ds
  const color = colorMap[ds] ?? 'var(--text-muted)'

  if (ds === 'completed' && task.execution_result) {
    const files: string[] = []
    try {
      const raw = task.execution_result as Record<string, unknown>
      if (Array.isArray(raw['files'])) {
        for (const f of raw['files'] as unknown[]) {
          if (typeof f === 'string') files.push(f)
        }
      }
    } catch { /* ignore */ }
    return (
      <span style={{ marginLeft: '0.5rem', verticalAlign: 'middle' }}>
        <button
          onClick={e => { e.stopPropagation(); setExpanded(v => !v) }}
          style={{
            fontSize: '0.7rem',
            padding: '1px 6px',
            borderRadius: '4px',
            border: `1px solid ${color}`,
            background: 'transparent',
            color,
            cursor: 'pointer',
          }}
          aria-expanded={expanded}
          data-testid="dispatch-badge-completed"
        >
          {label} {expanded ? '▲' : '▼'}
        </button>
        {expanded && (
          <span
            onClick={e => e.stopPropagation()}
            style={{
              display: 'block',
              marginTop: '4px',
              fontSize: '0.72rem',
              color: 'var(--text-muted)',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
            }}
            data-testid="dispatch-result-block"
          >
            {files.length > 0
              ? files.map(f => <span key={f} style={{ display: 'block' }}>{f}</span>)
              : JSON.stringify(task.execution_result, null, 2)}
          </span>
        )}
      </span>
    )
  }

  if (ds === 'failed') {
    const errMsg: string = (() => {
      try {
        const raw = task.execution_result as Record<string, unknown> | null | undefined
        if (raw && typeof raw['error_message'] === 'string') return raw['error_message']
        if (raw && typeof raw['error'] === 'string') return raw['error']
      } catch { /* ignore */ }
      return ''
    })()
    const isRoleDispatch = task.source?.startsWith('role_dispatch')
    return (
      <span style={{ marginLeft: '0.5rem', verticalAlign: 'middle' }}>
        <span
          style={{ fontSize: '0.7rem', padding: '1px 6px', borderRadius: '4px', border: `1px solid ${color}`, color }}
          data-testid="dispatch-badge-failed"
        >
          {label}
        </span>
        {errMsg && (
          <span
            style={{ marginLeft: '4px', fontSize: '0.7rem', color }}
            data-testid="dispatch-error-message"
          >
            {errMsg}
          </span>
        )}
        {isRoleDispatch && onRequeue && (
          <button
            onClick={e => { e.stopPropagation(); onRequeue() }}
            style={{
              marginLeft: '6px',
              fontSize: '0.7rem',
              padding: '1px 6px',
              borderRadius: '4px',
              border: '1px solid var(--text-muted)',
              background: 'transparent',
              color: 'var(--text-muted)',
              cursor: 'pointer',
            }}
            data-testid="dispatch-retry-btn"
          >
            Retry
          </button>
        )}
      </span>
    )
  }

  return (
    <span
      style={{
        marginLeft: '0.5rem',
        fontSize: '0.7rem',
        padding: '1px 6px',
        borderRadius: '4px',
        border: `1px solid ${color}`,
        color,
        verticalAlign: 'middle',
      }}
      data-testid={`dispatch-badge-${ds}`}
    >
      {label}
    </span>
  )
}

type TaskFilterState = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string }
type BatchTaskFormState = { status: '' | Task['status']; priority: '' | Task['priority']; assignee: string; clearAssignee: boolean }

interface TasksTabProps {
  projectId: string
  tasks: Task[]
  summary: ProjectSummary | null
  taskSort: string
  taskOrder: string
  taskFilters: TaskFilterState
  onSortChange: (sort: string) => void
  onOrderChange: (order: string) => void
  onFilterChange: (filters: TaskFilterState) => void
  onReload: () => void
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
}

export function TasksTab({
  projectId,
  tasks,
  taskSort,
  taskOrder,
  taskFilters,
  onSortChange,
  onOrderChange,
  onFilterChange,
  onReload,
  onError,
  onSuccess,
}: TasksTabProps) {
  const [showTaskForm, setShowTaskForm] = useState(false)
  const [taskForm, setTaskForm] = useState({ title: '', description: '', priority: 'medium' as Task['priority'], assignee: '', source: 'human' })
  const [editingTask, setEditingTask] = useState<Task | null>(null)
  const [editTaskForm, setEditTaskForm] = useState<{ title: string; description: string; status: Task['status']; priority: Task['priority']; assignee: string }>({ title: '', description: '', status: 'todo', priority: 'medium', assignee: '' })
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const [batchTaskForm, setBatchTaskForm] = useState<BatchTaskFormState>({ status: '', priority: '', assignee: '', clearAssignee: false })
  const [lineageByTask, setLineageByTask] = useState<Record<string, AppliedLineageEntry>>({})

  useEffect(() => {
    setSelectedTaskIds(prev => prev.filter(taskId => tasks.some(task => task.id === taskId)))
  }, [tasks])

  useEffect(() => {
    let active = true
    listProjectTaskLineage(projectId)
      .then(resp => {
        if (!active) return
        const map: Record<string, AppliedLineageEntry> = {}
        for (const entry of resp.data ?? []) map[entry.task_id] = entry
        setLineageByTask(map)
      })
      .catch(() => {})
    return () => { active = false }
  }, [projectId, tasks.length])

  const hasActiveTaskFilters = Boolean(taskFilters.status || taskFilters.priority || taskFilters.assignee.trim())
  const allVisibleTasksSelected = tasks.length > 0 && selectedTaskIds.length === tasks.length

  function resetTaskFilters() {
    onFilterChange({ status: '', priority: '', assignee: '' })
  }

  function toggleTaskSelection(taskId: string) {
    setSelectedTaskIds(prev => prev.includes(taskId) ? prev.filter(id => id !== taskId) : [...prev, taskId])
  }

  function toggleAllVisibleTasks() {
    if (tasks.length === 0) return
    setSelectedTaskIds(prev => prev.length === tasks.length ? [] : tasks.map(task => task.id))
  }

  function openEditTask(task: Task) {
    setEditingTask(task)
    setEditTaskForm({ title: task.title, description: task.description, status: task.status, priority: task.priority, assignee: task.assignee })
  }

  function closeEditTask() {
    setEditingTask(null)
  }

  async function handleCreateTask(e: React.FormEvent) {
    e.preventDefault()
    if (!taskForm.title.trim()) return
    try {
      await createTask(projectId, taskForm)
      setTaskForm({ title: '', description: '', priority: 'medium', assignee: '', source: 'human' })
      setShowTaskForm(false)
      onSuccess('Task created.')
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to create task')
    }
  }

  async function handleSaveTask(e: React.FormEvent) {
    e.preventDefault()
    if (!editingTask) return
    try {
      await updateTask(editingTask.id, editTaskForm)
      setEditingTask(null)
      onSuccess('Task updated.')
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to update task')
    }
  }

  async function handleDeleteEditingTask() {
    if (!editingTask || !confirm('Delete this task?')) return
    try {
      await deleteTask(editingTask.id)
      setEditingTask(null)
      setSelectedTaskIds(prev => prev.filter(taskId => taskId !== editingTask.id))
      onSuccess('Task deleted.')
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to delete task')
    }
  }

  async function handleApplyBatchUpdate() {
    if (selectedTaskIds.length === 0) return

    const changes: Parameters<typeof batchUpdateTasks>[2] = {}
    if (batchTaskForm.status) changes.status = batchTaskForm.status
    if (batchTaskForm.priority) changes.priority = batchTaskForm.priority
    if (batchTaskForm.clearAssignee) {
      changes.assignee = ''
    } else if (batchTaskForm.assignee.trim()) {
      changes.assignee = batchTaskForm.assignee.trim()
    }

    if (Object.keys(changes).length === 0) {
      onError('Select at least one batch change before applying.')
      return
    }

    try {
      const response = await batchUpdateTasks(projectId, selectedTaskIds, changes)
      setSelectedTaskIds([])
      setBatchTaskForm({ status: '', priority: '', assignee: '', clearAssignee: false })
      onSuccess(`Updated ${response.data.updated_count} tasks.`)
      await onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to update selected tasks')
    }
  }

  return (
    <div>
      <div style={{ display: 'grid', gap: '0.75rem', marginBottom: '1rem' }}>
        <div className="task-toolbar">
          <div className="task-toolbar-group">
            <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Status</label>
            <select className="toolbar-control toolbar-control--compact" value={taskFilters.status} onChange={e => onFilterChange({ ...taskFilters, status: e.target.value as TaskFilterState['status'] })}>
              <option value="">All</option>
              <option value="todo">To Do</option>
              <option value="in_progress">In Progress</option>
              <option value="done">Done</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Priority</label>
            <select className="toolbar-control toolbar-control--compact" value={taskFilters.priority} onChange={e => onFilterChange({ ...taskFilters, priority: e.target.value as TaskFilterState['priority'] })}>
              <option value="">All</option>
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
            </select>
            <input
              className="toolbar-control toolbar-control--wide"
              value={taskFilters.assignee}
              onChange={e => onFilterChange({ ...taskFilters, assignee: e.target.value })}
              placeholder="Filter by assignee"
            />
            <button className="btn btn-ghost btn-sm" onClick={resetTaskFilters} disabled={!hasActiveTaskFilters}>Reset Filters</button>
          </div>
          <div className="task-toolbar-group">
            <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Sort by:</label>
            <select className="toolbar-control toolbar-control--compact" value={taskSort} onChange={e => onSortChange(e.target.value)}>
              <option value="created_at">Created Date</option>
              <option value="updated_at">Updated Date</option>
              <option value="priority">Priority</option>
              <option value="status">Status</option>
              <option value="title">Title</option>
            </select>
            <select className="toolbar-control toolbar-control--compact" value={taskOrder} onChange={e => onOrderChange(e.target.value)}>
              <option value="desc">Descending</option>
              <option value="asc">Ascending</option>
            </select>
            <button className="btn btn-primary" onClick={() => setShowTaskForm(true)}>+ New Task</button>
          </div>
        </div>

        {selectedTaskIds.length > 0 && (
          <div className="task-batch-bar">
            <strong>{selectedTaskIds.length} selected</strong>
            <select className="toolbar-control toolbar-control--compact" value={batchTaskForm.status} onChange={e => setBatchTaskForm(prev => ({ ...prev, status: e.target.value as BatchTaskFormState['status'] }))}>
              <option value="">Keep status</option>
              <option value="todo">To Do</option>
              <option value="in_progress">In Progress</option>
              <option value="done">Done</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <select className="toolbar-control toolbar-control--compact" value={batchTaskForm.priority} onChange={e => setBatchTaskForm(prev => ({ ...prev, priority: e.target.value as BatchTaskFormState['priority'] }))}>
              <option value="">Keep priority</option>
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
            </select>
            <input
              className="toolbar-control toolbar-control--wide"
              value={batchTaskForm.assignee}
              onChange={e => setBatchTaskForm(prev => ({ ...prev, assignee: e.target.value, clearAssignee: false }))}
              placeholder="Set assignee"
            />
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
              <input
                type="checkbox"
                checked={batchTaskForm.clearAssignee}
                onChange={e => setBatchTaskForm(prev => ({ ...prev, clearAssignee: e.target.checked, assignee: e.target.checked ? '' : prev.assignee }))}
              />
              Clear assignee
            </label>
            <button className="btn btn-primary btn-sm" onClick={handleApplyBatchUpdate}>Apply to Selected</button>
            <button className="btn btn-ghost btn-sm" onClick={() => setSelectedTaskIds([])}>Clear Selection</button>
          </div>
        )}
      </div>

      {showTaskForm && (
        <div className="modal-overlay" onClick={() => setShowTaskForm(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Create Task</h3>
            <form onSubmit={handleCreateTask}>
              <div className="form-group">
                <label>Title *</label>
                <input value={taskForm.title} onChange={e => setTaskForm({ ...taskForm, title: e.target.value })} autoFocus />
              </div>
              <div className="form-group">
                <label>Description</label>
                <textarea value={taskForm.description} onChange={e => setTaskForm({ ...taskForm, description: e.target.value })} />
              </div>
              <div className="form-group">
                <label>Priority</label>
                <select value={taskForm.priority} onChange={e => setTaskForm({ ...taskForm, priority: e.target.value as Task['priority'] })}>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
              </div>
              <div className="form-group">
                <label>Assignee</label>
                <input value={taskForm.assignee} onChange={e => setTaskForm({ ...taskForm, assignee: e.target.value })} placeholder="human or agent:name" />
              </div>
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setShowTaskForm(false)}>Cancel</button>
                <button type="submit" className="btn btn-primary">Create</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {editingTask && (
        <div className="modal-overlay" onClick={closeEditTask}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Edit Task</h3>
            <form onSubmit={handleSaveTask}>
              <div className="form-group">
                <label>Title *</label>
                <input value={editTaskForm.title} onChange={e => setEditTaskForm({ ...editTaskForm, title: e.target.value })} autoFocus />
              </div>
              <div className="form-group">
                <label>Description</label>
                <textarea value={editTaskForm.description} onChange={e => setEditTaskForm({ ...editTaskForm, description: e.target.value })} rows={4} />
              </div>
              <div className="form-group">
                <label>Status</label>
                <select value={editTaskForm.status} onChange={e => setEditTaskForm({ ...editTaskForm, status: e.target.value as Task['status'] })}>
                  <option value="todo">To Do</option>
                  <option value="in_progress">In Progress</option>
                  <option value="done">Done</option>
                  <option value="cancelled">Cancelled</option>
                </select>
              </div>
              <div className="form-group">
                <label>Priority</label>
                <select value={editTaskForm.priority} onChange={e => setEditTaskForm({ ...editTaskForm, priority: e.target.value as Task['priority'] })}>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
              </div>
              <div className="form-group">
                <label>Assignee</label>
                <input value={editTaskForm.assignee} onChange={e => setEditTaskForm({ ...editTaskForm, assignee: e.target.value })} placeholder="human or agent:name" />
              </div>
              <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginBottom: '0.5rem' }}>
                Source: {editingTask.source} &nbsp;·&nbsp; Created: {new Date(editingTask.created_at).toLocaleString()}
              </div>
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" style={{ color: 'var(--danger)', marginRight: 'auto' }} onClick={handleDeleteEditingTask}>Delete</button>
                <button type="button" className="btn btn-ghost" onClick={closeEditTask}>Cancel</button>
                <button type="submit" className="btn btn-primary">Save</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {tasks.length === 0 ? (
        <div className="empty-state">
          <h3>{hasActiveTaskFilters ? 'No tasks match the current filters' : 'No tasks yet'}</h3>
          <p>{hasActiveTaskFilters ? 'Adjust or reset filters to see more tasks.' : 'Create your first task to get started.'}</p>
        </div>
      ) : (
        <div className="table-wrap table-wrap--wide">
          <table className="table">
            <thead>
              <tr>
                <th style={{ width: '44px' }}>
                  <input type="checkbox" checked={allVisibleTasksSelected} onChange={toggleAllVisibleTasks} />
                </th>
                <th>Title</th>
                <th>Status</th>
                <th>Priority</th>
                <th>Assignee</th>
                <th>Updated</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map(task => (
                <tr key={task.id} style={{ cursor: 'pointer', background: selectedTaskIds.includes(task.id) ? 'rgba(99, 102, 241, 0.08)' : undefined }} onClick={() => openEditTask(task)}>
                  <td onClick={e => e.stopPropagation()}>
                    <input type="checkbox" checked={selectedTaskIds.includes(task.id)} onChange={() => toggleTaskSelection(task.id)} />
                  </td>
                  <td>
                    {task.title}
                    {lineageByTask[task.id] && (
                      <span
                        className="badge badge-low"
                        title={`Lineage: ${lineageByTask[task.id].lineage_kind}`}
                        style={{ fontSize: '0.7rem', marginLeft: '0.5rem', verticalAlign: 'middle' }}
                      >
                        {lineageByTask[task.id].lineage_kind === 'applied_candidate' ? '← Plan' : '← Req'}
                      </span>
                    )}
                    {task.dispatch_status && task.dispatch_status !== 'none' && (
                      <DispatchStatusBadge
                        task={task}
                        onRequeue={task.dispatch_status === 'failed' ? async () => {
                          try {
                            await requeueDispatchTask(task.id)
                            onReload()
                          } catch (e) {
                            onError(e instanceof Error ? e.message : 'Failed to requeue task')
                          }
                        } : undefined}
                      />
                    )}
                  </td>
                  <td><span className={`badge badge-${task.status === 'done' ? 'fresh' : task.status === 'in_progress' ? 'low' : task.status === 'cancelled' ? 'stale' : 'todo'}`}>{task.status.replace('_', ' ')}</span></td>
                  <td><span className={`badge badge-${task.priority}`}>{task.priority}</span></td>
                  <td style={{ color: 'var(--text-muted)' }}>{task.assignee || '—'}</td>
                  <td style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{new Date(task.updated_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
