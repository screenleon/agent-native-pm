import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { AgentRun, SyncRun } from '../types'
import { AgentsTab } from './AgentsTab'

function makeAgentRun(overrides: Partial<AgentRun> = {}): AgentRun {
  return {
    id: 'ar-1',
    project_id: 'p1',
    agent_name: 'backend-architect',
    action_type: 'update',
    status: 'completed',
    summary: 'Refactored store layer',
    files_affected: ['backend/store/a.go', 'backend/store/b.go'],
    needs_human_review: false,
    started_at: '2026-04-22T10:00:00Z',
    completed_at: '2026-04-22T10:05:00Z',
    error_message: '',
    idempotency_key: null,
    created_at: '2026-04-22T10:00:00Z',
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
    commits_scanned: 12,
    files_changed: 4,
    error_message: '',
    ...overrides,
  }
}

describe('<AgentsTab />', () => {
  it('renders empty state when there is no agent activity', () => {
    render(<AgentsTab agentRuns={[]} syncRuns={[]} />)
    expect(screen.getByText(/No agent activity/i)).toBeInTheDocument()
    expect(screen.queryByText(/Recent Sync Runs/i)).not.toBeInTheDocument()
  })

  it('renders agent run metadata and limits file badges to 5', () => {
    const files = Array.from({ length: 8 }, (_, i) => `file${i}.ts`)
    render(<AgentsTab agentRuns={[makeAgentRun({ files_affected: files })]} syncRuns={[]} />)

    expect(screen.getByText('backend-architect')).toBeInTheDocument()
    expect(screen.getByText('update')).toBeInTheDocument()
    expect(screen.getByText('Refactored store layer')).toBeInTheDocument()
    expect(screen.getByText('file0.ts')).toBeInTheDocument()
    expect(screen.getByText('file4.ts')).toBeInTheDocument()
    expect(screen.queryByText('file5.ts')).not.toBeInTheDocument()
  })

  it('renders the recent-sync-runs section only when sync runs are present', () => {
    render(<AgentsTab agentRuns={[makeAgentRun()]} syncRuns={[makeSyncRun()]} />)
    expect(screen.getByText(/Recent Sync Runs/i)).toBeInTheDocument()
    expect(screen.getByText(/commits 12/i)).toBeInTheDocument()
  })
})
