import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import ProjectDetail from './ProjectDetail'

// ProjectDetail loads ~10 project-scoped APIs on mount. We stub them all
// so the page lands in the "loaded" branch and we can exercise the P4-1
// rail / More ▾ popover / Settings gear icon.

const mockProject = {
  id: 'p1',
  name: 'Test Project',
  description: 'A test project',
  default_branch: 'main',
  repo_path: '',
  repo_url: '',
  created_at: '2026-04-01T00:00:00Z',
  updated_at: '2026-04-20T00:00:00Z',
}

const mockSummary = {
  project_id: 'p1',
  summary: {
    total_tasks: 0,
    tasks_in_progress: 0,
    tasks_done: 0,
    total_documents: 0,
    stale_documents: 0,
    health_score: 0.9,
  },
  latest_sync_run: null,
  open_drift_count: 0,
  recent_agent_runs: [],
}

vi.mock('../api/client', () => ({
  getProject: vi.fn(() => Promise.resolve({ data: mockProject })),
  getProjectDashboardSummary: vi.fn(() => Promise.resolve({ data: mockSummary })),
  getProjectSummary: vi.fn(() => Promise.resolve({ data: mockSummary.summary })),
  updateProject: vi.fn(() => Promise.resolve({ data: mockProject })),
  listRequirements: vi.fn(() => Promise.resolve({ data: [] })),
  listTasksFiltered: vi.fn(() => Promise.resolve({ data: [] })),
  deleteDocument: vi.fn(() => Promise.resolve({ data: null })),
  getDocumentContent: vi.fn(() => Promise.resolve({ data: { content: '', truncated: false } })),
  triggerSync: vi.fn(() => Promise.resolve({ data: null })),
  listSyncRuns: vi.fn(() => Promise.resolve({ data: [] })),
  listAgentRuns: vi.fn(() => Promise.resolve({ data: [] })),
  listDriftSignals: vi.fn(() => Promise.resolve({ data: [] })),
  updateDriftSignal: vi.fn(() => Promise.resolve({ data: null })),
  bulkResolveDriftSignals: vi.fn(() => Promise.resolve({ data: null })),
  listDocumentLinks: vi.fn(() => Promise.resolve({ data: [] })),
  createDocumentLink: vi.fn(() => Promise.resolve({ data: null })),
  deleteDocumentLink: vi.fn(() => Promise.resolve({ data: null })),
  listProjectRepoMappings: vi.fn(() => Promise.resolve({ data: [] })),
  updateProjectRepoMapping: vi.fn(() => Promise.resolve({ data: null })),
  listDocuments: vi.fn(() => Promise.resolve({ data: [] })),
  refreshDocumentSummary: vi.fn(() => Promise.resolve({ data: null })),
  discoverMirrorRepos: vi.fn(() => Promise.resolve({ data: { repos: [] } })),
  // PlanningTab + planning/ siblings call these; stub so they don't error.
  listPlanningRuns: vi.fn(() => Promise.resolve({ data: [] })),
  listBacklogCandidates: vi.fn(() => Promise.resolve({ data: [] })),
  listBacklogCandidatesByEvidence: vi.fn(() => Promise.resolve({ data: [] })),
  listProjectTaskLineage: vi.fn(() => Promise.resolve({ data: [] })),
  getProjectPendingReviewCount: vi.fn(() => Promise.resolve({ data: { count: 0 } })),
}))

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/projects/:id" element={<ProjectDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

async function waitLoaded() {
  await waitFor(() => {
    expect(screen.getByRole('navigation', { name: /Project sections/i })).toBeInTheDocument()
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('<ProjectDetail /> P4-1 IA', () => {
  // T-P4-1-1: primary rail contains exactly Workspace, Overview, Tasks,
  // Documents — plus exactly one More ▾ button. This test codifies the
  // 2026-04-24 DECISIONS.md entry "the primary rail's four tabs are a stable
  // set" (N-2): a future PR that adds a fifth primary tab without amending
  // DECISIONS will fail this count assertion in CI.
  it('T-P4-1-1: primary rail exposes exactly 4 primary tabs + More ▾', async () => {
    renderAt('/projects/p1')
    await waitLoaded()
    const rail = screen.getByRole('navigation', { name: /Project sections/i })

    // Exactly 5 buttons: 4 primary tabs + 1 More ▾ trigger. A new rail entry
    // forces either updating this number (and DECISIONS.md) or routing it
    // through the More popover instead.
    const railButtons = within(rail).getAllByRole('button')
    expect(railButtons).toHaveLength(5)

    expect(within(rail).getByRole('button', { name: /Workspace/ })).toBeInTheDocument()
    expect(within(rail).getByRole('button', { name: /^Overview$/i })).toBeInTheDocument()
    expect(within(rail).getByRole('button', { name: /^Tasks/ })).toBeInTheDocument()
    expect(within(rail).getByRole('button', { name: /^Documents/ })).toBeInTheDocument()
    expect(within(rail).getByRole('button', { name: /More/i })).toBeInTheDocument()
    // Drift + Activity must NOT be directly visible — they live in More ▾.
    expect(within(rail).queryByRole('button', { name: /^Drift$/i })).not.toBeInTheDocument()
    expect(within(rail).queryByRole('button', { name: /^Activity$/i })).not.toBeInTheDocument()
    // Settings has moved to the gear icon in the page header.
    expect(within(rail).queryByRole('button', { name: /^Settings/i })).not.toBeInTheDocument()
  })

  // T-P4-1-3: click "More ▾" → popover reveals Drift + Activity.
  it('T-P4-1-3: More popover reveals Drift + Activity', async () => {
    renderAt('/projects/p1')
    await waitLoaded()
    await userEvent.click(screen.getByRole('button', { name: /More/i }))
    const menu = screen.getByRole('menu')
    expect(within(menu).getByRole('menuitem', { name: /Drift/i })).toBeInTheDocument()
    expect(within(menu).getByRole('menuitem', { name: /Activity/i })).toBeInTheDocument()
  })

  // T-P4-1-5: click gear icon → Settings tab activates; URL reflects it.
  it('T-P4-1-5: gear icon activates Settings tab', async () => {
    renderAt('/projects/p1')
    await waitLoaded()
    await userEvent.click(screen.getByRole('button', { name: /Project settings/i }))
    // The Settings tab body renders something containing "Repository" or the
    // tab heading — we only assert that the gear is now styled as active.
    expect(screen.getByRole('button', { name: /Project settings/i })).toHaveClass('is-active')
  })

  // T-P4-1-6: deep-link ?tab=settings renders with the gear active.
  it('T-P4-1-6: deep-link ?tab=settings lands on Settings with the gear active', async () => {
    renderAt('/projects/p1?tab=settings')
    await waitLoaded()
    expect(screen.getByRole('button', { name: /Project settings/i })).toHaveClass('is-active')
  })

  // T-P4-1-7: deep-link ?tab=drift lands on Drift with More ▾ rendered active.
  it('T-P4-1-7: deep-link ?tab=drift renders More as active', async () => {
    renderAt('/projects/p1?tab=drift')
    await waitLoaded()
    expect(screen.getByRole('button', { name: /More/i })).toHaveClass('is-active')
  })
})
