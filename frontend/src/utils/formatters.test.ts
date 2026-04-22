import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest'
import type { SyncRun } from '../types'
import {
  formatDateTime,
  formatRelativeTime,
  formatSyncDuration,
  guidanceBadgeClass,
  syncBadgeClass,
} from './formatters'

function makeSyncRun(overrides: Partial<SyncRun> = {}): SyncRun {
  return {
    id: 'sync-1',
    project_id: 'proj-1',
    started_at: '2026-04-22T10:00:00.000Z',
    completed_at: '2026-04-22T10:00:45.000Z',
    status: 'completed',
    commits_scanned: 0,
    files_changed: 0,
    error_message: '',
    ...overrides,
  }
}

describe('formatDateTime', () => {
  it('returns em-dash for null/undefined', () => {
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDateTime('')).toBe('—')
  })

  it('renders a parseable locale string for valid input', () => {
    const formatted = formatDateTime('2026-04-22T10:00:00.000Z')
    expect(formatted).not.toBe('—')
    expect(formatted.length).toBeGreaterThan(0)
  })
})

describe('formatRelativeTime', () => {
  const fixedNow = new Date('2026-04-22T12:00:00.000Z').getTime()

  beforeAll(() => {
    vi.useFakeTimers()
    vi.setSystemTime(fixedNow)
  })

  afterAll(() => {
    vi.useRealTimers()
  })

  it('returns em-dash for empty input', () => {
    expect(formatRelativeTime(null)).toBe('—')
  })

  it('returns "just now" under one minute', () => {
    expect(formatRelativeTime(new Date(fixedNow - 30 * 1000).toISOString())).toBe('just now')
  })

  it('uses minute / hour / day buckets', () => {
    expect(formatRelativeTime(new Date(fixedNow - 5 * 60 * 1000).toISOString())).toBe('5m ago')
    expect(formatRelativeTime(new Date(fixedNow - 3 * 60 * 60 * 1000).toISOString())).toBe('3h ago')
    expect(formatRelativeTime(new Date(fixedNow - 2 * 24 * 60 * 60 * 1000).toISOString())).toBe('2d ago')
  })
})

describe('formatSyncDuration', () => {
  it('reports "In progress" when not completed', () => {
    expect(formatSyncDuration(makeSyncRun({ completed_at: null, status: 'running' }))).toBe('In progress')
  })

  it('formats sub-minute durations as seconds', () => {
    expect(formatSyncDuration(makeSyncRun())).toBe('45s')
  })

  it('formats round-minute durations without a trailing seconds segment', () => {
    expect(
      formatSyncDuration(
        makeSyncRun({
          started_at: '2026-04-22T10:00:00.000Z',
          completed_at: '2026-04-22T10:02:00.000Z',
        }),
      ),
    ).toBe('2m')
  })

  it('formats mixed minute+second durations', () => {
    expect(
      formatSyncDuration(
        makeSyncRun({
          started_at: '2026-04-22T10:00:00.000Z',
          completed_at: '2026-04-22T10:01:07.000Z',
        }),
      ),
    ).toBe('1m 7s')
  })
})

describe('badge class helpers', () => {
  it('maps sync status to badge class', () => {
    expect(syncBadgeClass('completed')).toBe('badge-fresh')
    expect(syncBadgeClass('failed')).toBe('badge-stale')
    expect(syncBadgeClass('running')).toBe('badge-low')
  })

  it('maps guidance tone to badge class', () => {
    expect(guidanceBadgeClass('success')).toBe('badge-fresh')
    expect(guidanceBadgeClass('warning')).toBe('badge-low')
    expect(guidanceBadgeClass('danger')).toBe('badge-stale')
    expect(guidanceBadgeClass('neutral')).toBe('badge-todo')
  })
})
