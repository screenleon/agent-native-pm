import type { SyncRun } from '../types'
import type { SyncGuidance } from './syncGuidance'

export function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

export function formatRelativeTime(value: string | null | undefined): string {
  if (!value) return '—'
  const diffMs = Date.now() - new Date(value).getTime()
  if (diffMs < 60 * 1000) return 'just now'
  const minutes = Math.floor(diffMs / (60 * 1000))
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export function formatSyncDuration(run: SyncRun): string {
  if (!run.completed_at) return 'In progress'
  const started = new Date(run.started_at).getTime()
  const completed = new Date(run.completed_at).getTime()
  const diffMs = Math.max(0, completed - started)
  const seconds = Math.round(diffMs / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  return remainingSeconds === 0 ? `${minutes}m` : `${minutes}m ${remainingSeconds}s`
}

export function syncBadgeClass(status: SyncRun['status']): string {
  if (status === 'completed') return 'badge-fresh'
  if (status === 'failed') return 'badge-stale'
  return 'badge-low'
}

export function guidanceBadgeClass(tone: SyncGuidance['tone']): string {
  if (tone === 'success') return 'badge-fresh'
  if (tone === 'warning') return 'badge-low'
  if (tone === 'danger') return 'badge-stale'
  return 'badge-todo'
}
