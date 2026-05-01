import { useEffect, useMemo, useState, type FormEvent } from 'react'
import type { BacklogItem, CreateBacklogItemPayload, Task, UpdateBacklogItemPayload } from '../../types'
import { commitBacklogItemToTask, createBacklogItem, listBacklogItems, updateBacklogItem } from '../../api/client'

type BacklogFilterState = {
  status: '' | BacklogItem['status']
  priority: '' | BacklogItem['priority']
  label: string
  q: string
}

type BacklogFormState = {
  title: string
  description: string
  status: BacklogItem['status']
  priority: BacklogItem['priority']
  rank: string
  labels: string
  acceptance_criteria: string
  blocked_reason: string
}

const priorityOptions: Array<{ value: Task['priority']; label: string }> = [
  { value: 'urgent', label: 'Urgent' },
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
]

const statusOptions: Array<{ value: BacklogItem['status']; label: string }> = [
  { value: 'triage', label: 'Triage' },
  { value: 'ready', label: 'Ready' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'committed', label: 'Committed' },
  { value: 'archived', label: 'Archived' },
]

const emptyForm: BacklogFormState = {
  title: '',
  description: '',
  status: 'triage',
  priority: 'medium',
  rank: '0',
  labels: '',
  acceptance_criteria: '',
  blocked_reason: '',
}

interface BacklogTabProps {
  projectId: string
  onReload: () => void
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
  onNavigateToTasks: () => void
}

export function BacklogTab({ projectId, onReload, onError, onSuccess, onNavigateToTasks }: BacklogTabProps) {
  const [items, setItems] = useState<BacklogItem[]>([])
  const [summaryItems, setSummaryItems] = useState<BacklogItem[]>([])
  const [filteredTotal, setFilteredTotal] = useState(0)
  const [projectBacklogTotal, setProjectBacklogTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState<BacklogFilterState>({ status: '', priority: '', label: '', q: '' })
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState<BacklogFormState>(emptyForm)
  const [editingItem, setEditingItem] = useState<BacklogItem | null>(null)
  const [editForm, setEditForm] = useState<BacklogFormState>(emptyForm)
  const [committingId, setCommittingId] = useState<string | null>(null)

  const loadBacklog = async () => {
    try {
      setLoading(true)
      const [response, summaryResponse] = await Promise.all([
        listBacklogItems(projectId, 1, 100, 'rank', 'asc', {
          status: filters.status || undefined,
          priority: filters.priority || undefined,
          label: filters.label.trim() || undefined,
          q: filters.q.trim() || undefined,
        }),
        listBacklogItems(projectId, 1, 500, 'rank', 'asc'),
      ])
      setItems(response.data)
      setSummaryItems(summaryResponse.data)
      setFilteredTotal(response.meta?.total ?? response.data.length)
      setProjectBacklogTotal(summaryResponse.meta?.total ?? summaryResponse.data.length)
    } catch (err) {
      setItems([])
      setSummaryItems([])
      setFilteredTotal(0)
      setProjectBacklogTotal(0)
      onError(err instanceof Error ? err.message : 'Failed to load backlog')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadBacklog()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, filters.status, filters.priority, filters.label, filters.q])

  const openCount = useMemo(() => items.filter(item => item.status !== 'committed' && item.status !== 'archived').length, [items])
  const visibleItems = useMemo(
    () => filters.status ? items : items.filter(item => item.status !== 'archived'),
    [filters.status, items],
  )
  const allLabels = useMemo(() => {
    const labels = new Set<string>()
    for (const item of items) {
      for (const label of item.labels ?? []) labels.add(label)
    }
    return [...labels].sort()
  }, [items])
  const focusCounts = useMemo(() => ({
    urgentOpen: summaryItems.filter(item => item.priority === 'urgent' && item.status !== 'committed' && item.status !== 'archived').length,
    urgentTotal: summaryItems.filter(item => item.priority === 'urgent').length,
    urgentCommitted: summaryItems.filter(item => item.priority === 'urgent' && item.status === 'committed').length,
    blockedOpen: summaryItems.filter(item => item.status === 'blocked').length,
    readyOpen: summaryItems.filter(item => item.status === 'ready').length,
    committedTotal: summaryItems.filter(item => item.status === 'committed').length,
  }), [summaryItems])

  function resetFilters() {
    setFilters({ status: '', priority: '', label: '', q: '' })
  }

  function itemToForm(item: BacklogItem): BacklogFormState {
    return {
      title: item.title,
      description: item.description,
      status: item.status,
      priority: item.priority,
      rank: String(item.rank),
      labels: (item.labels ?? []).join(', '),
      acceptance_criteria: item.acceptance_criteria,
      blocked_reason: item.blocked_reason,
    }
  }

  function parseLabels(raw: string): string[] {
    const seen = new Set<string>()
    return raw
      .split(',')
      .map(label => label.trim())
      .filter(label => {
        if (!label || seen.has(label)) return false
        seen.add(label)
        return true
      })
  }

function formToCreatePayload(state: BacklogFormState): CreateBacklogItemPayload {
    return {
      title: state.title.trim(),
      description: state.description,
      status: state.status,
      priority: state.priority,
      source: 'human',
      rank: Number.parseInt(state.rank, 10) || 0,
      labels: parseLabels(state.labels),
      acceptance_criteria: state.acceptance_criteria,
      blocked_reason: state.blocked_reason,
    }
  }

  function formToUpdatePayload(state: BacklogFormState, original: BacklogItem): UpdateBacklogItemPayload {
    return {
      title: original.status === 'committed' ? original.title : state.title.trim(),
      description: original.status === 'committed' ? original.description : state.description,
      status: state.status,
      priority: original.status === 'committed' ? original.priority : state.priority,
      rank: Number.parseInt(state.rank, 10) || 0,
      labels: parseLabels(state.labels),
      acceptance_criteria: state.acceptance_criteria,
      blocked_reason: state.blocked_reason,
    }
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!form.title.trim()) return
    try {
      await createBacklogItem(projectId, formToCreatePayload(form))
      setForm(emptyForm)
      setShowCreate(false)
      onSuccess('Backlog item created.')
      await loadBacklog()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to create backlog item')
    }
  }

  async function handleSave(e: FormEvent) {
    e.preventDefault()
    if (!editingItem || !editForm.title.trim()) return
    try {
      await updateBacklogItem(editingItem.id, formToUpdatePayload(editForm, editingItem))
      setEditingItem(null)
      onSuccess('Backlog item updated.')
      await loadBacklog()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to update backlog item')
    }
  }

  async function handleCommit(item: BacklogItem) {
    try {
      setCommittingId(item.id)
      const response = await commitBacklogItemToTask(item.id)
      onSuccess(response.data.already_applied ? 'Backlog item already has a task.' : 'Backlog item committed to task.')
      await loadBacklog()
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to commit backlog item')
    } finally {
      setCommittingId(null)
    }
  }

  function openEdit(item: BacklogItem) {
    setEditingItem(item)
    setEditForm(itemToForm(item))
  }

  const hasActiveFilters = Boolean(filters.status || filters.priority || filters.label.trim() || filters.q.trim())
  const statusBadgeClass = (status: BacklogItem['status']) => {
    if (status === 'committed') return 'badge-fresh'
    if (status === 'blocked') return 'badge-stale'
    if (status === 'ready') return 'badge-low'
    if (status === 'archived') return 'badge-cancelled'
    return 'badge-todo'
  }

  return (
    <div>
      <div className="backlog-focus-strip" aria-label="Backlog current focus">
        <div className="backlog-focus-item">
          <span>Urgent open</span>
          <strong>{focusCounts.urgentOpen}</strong>
          <small>{focusCounts.urgentTotal} total · {focusCounts.urgentCommitted} committed</small>
        </div>
        <div className="backlog-focus-item">
          <span>Blocked open</span>
          <strong>{focusCounts.blockedOpen}</strong>
          <small>Needs unblock before task commit</small>
        </div>
        <div className="backlog-focus-item">
          <span>Ready open</span>
          <strong>{focusCounts.readyOpen}</strong>
          <small>Can become tasks next</small>
        </div>
        <div className="backlog-focus-item">
          <span>Committed</span>
          <strong>{focusCounts.committedTotal}</strong>
          <small>Already converted to tasks</small>
        </div>
      </div>

      <div className="task-toolbar" style={{ marginBottom: '1rem' }}>
        <div className="task-toolbar-group">
          <strong>{openCount} open</strong>
          <span className="backlog-muted">
            Showing {visibleItems.length} of {filteredTotal}
            {projectBacklogTotal !== filteredTotal ? ` matching · ${projectBacklogTotal} project total` : ''}
          </span>
          <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Status</label>
          <select className="toolbar-control toolbar-control--compact" value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value as BacklogFilterState['status'] })}>
            <option value="">All</option>
            {statusOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
          <label style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>Priority</label>
          <select className="toolbar-control toolbar-control--compact" value={filters.priority} onChange={e => setFilters({ ...filters, priority: e.target.value as BacklogFilterState['priority'] })}>
            <option value="">All</option>
            {priorityOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
          <input
            className="toolbar-control toolbar-control--compact"
            value={filters.label}
            onChange={e => setFilters({ ...filters, label: e.target.value })}
            placeholder="Label"
            list="backlog-labels"
          />
          <datalist id="backlog-labels">
            {allLabels.map(label => <option key={label} value={label} />)}
          </datalist>
          <input
            className="toolbar-control toolbar-control--wide"
            value={filters.q}
            onChange={e => setFilters({ ...filters, q: e.target.value })}
            placeholder="Search backlog"
          />
          <button className="btn btn-ghost btn-sm" onClick={resetFilters} disabled={!hasActiveFilters}>Reset</button>
        </div>
        <div className="task-toolbar-group">
          <button className="btn btn-ghost" onClick={onNavigateToTasks}>Open Tasks</button>
          <button className="btn btn-primary" onClick={() => setShowCreate(true)}>+ New Backlog</button>
        </div>
      </div>

      {loading ? (
        <div className="empty-state">
          <p>Loading backlog...</p>
        </div>
      ) : visibleItems.length === 0 ? (
        <div className="empty-state">
          <h3>{hasActiveFilters ? 'No backlog items match the current filters' : 'No backlog items yet'}</h3>
          <p>{hasActiveFilters ? 'Adjust or reset filters to see more items.' : 'Create backlog items first, then commit only the ones that should become tasks.'}</p>
        </div>
      ) : (
        <div className="backlog-list" aria-label="Backlog items">
          {visibleItems.map(item => (
            <div key={item.id} className="backlog-row">
              <div className="backlog-row-main">
                <div className="backlog-row-title">
                  <span className={`badge badge-${item.priority}`}>{item.priority}</span>
                  <strong>{item.title}</strong>
                </div>
                {item.description && <p>{item.description}</p>}
                <div className="backlog-row-meta">
                  <span className={`badge ${statusBadgeClass(item.status)}`}>{item.status}</span>
                  {(item.labels ?? []).length === 0
                    ? <span className="backlog-muted">No labels</span>
                    : item.labels.map(label => <span key={label} className="badge badge-low">{label}</span>)}
                  <span className="backlog-muted">Source {item.source}</span>
                  <span className="backlog-muted">Updated {new Date(item.updated_at).toLocaleDateString()}</span>
                </div>
              </div>
              <div className="backlog-row-action">
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => openEdit(item)}
                >
                  Edit
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  disabled={item.status === 'committed' || item.status === 'archived' || item.status === 'blocked' || committingId === item.id}
                  onClick={() => handleCommit(item)}
                >
                  {item.status === 'committed' ? 'Committed' : item.status === 'blocked' ? 'Blocked' : committingId === item.id ? 'Committing' : 'Commit to task'}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showCreate && (
        <BacklogFormModal
          title="Create Backlog Item"
          form={form}
          onChange={setForm}
          onClose={() => setShowCreate(false)}
          onSubmit={handleCreate}
          includeCommittedStatus={false}
        />
      )}

      {editingItem && (
        <BacklogFormModal
          title="Edit Backlog Item"
          form={editForm}
          onChange={setEditForm}
          onClose={() => setEditingItem(null)}
          onSubmit={handleSave}
          includeCommittedStatus={editingItem.status === 'committed'}
          taskFieldsReadOnly={editingItem.status === 'committed'}
        />
      )}
    </div>
  )
}

function BacklogFormModal({
  title,
  form,
  onChange,
  onClose,
  onSubmit,
  includeCommittedStatus,
  taskFieldsReadOnly,
}: {
  title: string
  form: BacklogFormState
  onChange: (next: BacklogFormState) => void
  onClose: () => void
  onSubmit: (e: FormEvent) => void
  includeCommittedStatus?: boolean
  taskFieldsReadOnly?: boolean
}) {
  const modalStatusOptions = includeCommittedStatus
    ? statusOptions
    : statusOptions.filter(option => option.value !== 'committed')

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <h3>{title}</h3>
        <form onSubmit={onSubmit}>
          <div className="form-group">
            <label>Title *</label>
            <input value={form.title} onChange={e => onChange({ ...form, title: e.target.value })} disabled={taskFieldsReadOnly} autoFocus={!taskFieldsReadOnly} />
          </div>
          <div className="form-group">
            <label>Description</label>
            <textarea value={form.description} onChange={e => onChange({ ...form, description: e.target.value })} rows={3} disabled={taskFieldsReadOnly} />
          </div>
          <div className="grid-2">
            <div className="form-group">
              <label>Status</label>
              <select value={form.status} onChange={e => onChange({ ...form, status: e.target.value as BacklogItem['status'] })}>
                {modalStatusOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </div>
            <div className="form-group">
              <label>Priority</label>
              <select value={form.priority} onChange={e => onChange({ ...form, priority: e.target.value as BacklogItem['priority'] })} disabled={taskFieldsReadOnly}>
                {priorityOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </div>
          </div>
          {taskFieldsReadOnly && (
            <p className="helper-note" style={{ marginBottom: '1rem' }}>
              Title, description, and priority are locked after commit because the linked task owns those fields.
            </p>
          )}
          <div className="grid-2">
            <div className="form-group">
              <label>Rank</label>
              <input type="number" value={form.rank} onChange={e => onChange({ ...form, rank: e.target.value })} />
            </div>
            <div className="form-group">
              <label>Labels</label>
              <input value={form.labels} onChange={e => onChange({ ...form, labels: e.target.value })} placeholder="api, ui, urgent-path" />
            </div>
          </div>
          <div className="form-group">
            <label>Acceptance Criteria</label>
            <textarea value={form.acceptance_criteria} onChange={e => onChange({ ...form, acceptance_criteria: e.target.value })} rows={3} />
          </div>
          <div className="form-group">
            <label>Blocked Reason</label>
            <input value={form.blocked_reason} onChange={e => onChange({ ...form, blocked_reason: e.target.value })} />
          </div>
          <div className="modal-actions">
            <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
            <button type="submit" className="btn btn-primary">Save</button>
          </div>
        </form>
      </div>
    </div>
  )
}
