import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Project, SyncRun } from '../types'
import { SyncStatusPanel } from './SyncStatusPanel'

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: 'agent-native-pm',
    description: '',
    repo_url: '',
    repo_path: '/mirrors/agent-native-pm',
    default_branch: 'main',
    last_sync_at: null,
    created_at: '2026-04-14T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

function makeSyncRun(overrides: Partial<SyncRun> = {}): SyncRun {
  return {
    id: 'sr-1',
    project_id: 'p1',
    started_at: '2026-04-22T10:00:00Z',
    completed_at: '2026-04-22T10:00:30Z',
    status: 'completed',
    commits_scanned: 2,
    files_changed: 0,
    error_message: '',
    ...overrides,
  }
}

const baseProps = {
  project: makeProject(),
  latestSyncRun: makeSyncRun(),
  recentSyncRuns: [] as SyncRun[],
  openDriftCount: 0,
  hasRepoSource: true,
  syncing: false,
  latestSyncGuidance: null,
  canApplyDetectedBranchAndRerun: false,
  detectedSyncBranch: '',
  quickFixBranchTarget: { type: 'project' as const },
  savingProjectBranch: false,
  projectBranchForm: 'main',
  branchFormChanged: false,
  detectedProjectBranch: 'main',
  onSync: vi.fn(),
  onApplyDetectedBranchAndRerunSync: vi.fn(),
  onNavigateToDrift: vi.fn(),
  onProjectBranchFormChange: vi.fn(),
  onSaveProjectBranch: vi.fn(),
  onClearProjectBranch: vi.fn(),
  onUseDetectedBranch: vi.fn(),
}

describe('<SyncStatusPanel />', () => {
  it('starts collapsed when the last run completed successfully', () => {
    render(<SyncStatusPanel {...baseProps} />)
    expect(screen.queryByText(/Sync Status/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Details/i })).toBeInTheDocument()
  })

  it('auto-expands when there is no repo source configured', () => {
    render(<SyncStatusPanel {...baseProps} hasRepoSource={false} latestSyncRun={null} />)
    expect(screen.getByText(/Sync Status/i)).toBeInTheDocument()
    expect(screen.getByText(/no repository source configured/i)).toBeInTheDocument()
  })

  it('auto-expands on failed sync and disables Sync Now without a repo source', () => {
    render(
      <SyncStatusPanel
        {...baseProps}
        hasRepoSource={false}
        latestSyncRun={makeSyncRun({ status: 'failed', error_message: 'git fetch failed' })}
      />,
    )
    expect(screen.getByText(/git fetch failed/i)).toBeInTheDocument()
    const syncButtons = screen.getAllByRole('button', { name: /Sync/i })
    expect(syncButtons.every(btn => btn.hasAttribute('disabled'))).toBe(true)
  })

  it('toggles from collapsed to expanded via the Details button', async () => {
    render(<SyncStatusPanel {...baseProps} />)
    await userEvent.click(screen.getByRole('button', { name: /Details/i }))
    expect(screen.getByText(/Sync Status/i)).toBeInTheDocument()
  })
})
