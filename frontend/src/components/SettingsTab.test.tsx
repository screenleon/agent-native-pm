import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { Project, ProjectRepoMapping } from '../types'
import { SettingsTab } from './SettingsTab'

vi.mock('../api/client', () => ({
  createProjectRepoMapping: vi.fn().mockResolvedValue({ data: null }),
  deleteProjectRepoMapping: vi.fn().mockResolvedValue({ data: null }),
  updateProjectRepoMapping: vi.fn().mockResolvedValue({ data: null }),
}))

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: 'agent-native-pm',
    description: '',
    repo_url: '',
    repo_path: '',
    default_branch: 'main',
    last_sync_at: null,
    created_at: '2026-04-14T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  }
}

function makeMapping(overrides: Partial<ProjectRepoMapping> = {}): ProjectRepoMapping {
  return {
    id: 'm1',
    project_id: 'p1',
    alias: 'app',
    repo_path: '/mirrors/agent-native-pm',
    default_branch: 'main',
    is_primary: true,
    created_at: '2026-04-14T00:00:00Z',
    updated_at: '2026-04-14T00:00:00Z',
    ...overrides,
  }
}

const baseProps = {
  projectId: 'p1',
  project: makeProject(),
  primaryRepoMapping: null as ProjectRepoMapping | null,
  repoMappings: [] as ProjectRepoMapping[],
  repoMirrorDiscovery: null,
  repoMirrorLoading: false,
  repoMirrorLoadError: null,
  detectedPrimaryRepoMappingBranch: '',
  onLoadRepoMirrorDiscovery: vi.fn(),
  onReload: vi.fn(),
  onError: vi.fn(),
  onSuccess: vi.fn(),
}

describe('<SettingsTab />', () => {
  it('renders the empty-state hint when no repo mappings exist', () => {
    render(<SettingsTab {...baseProps} />)
    expect(screen.getByText(/No repo mappings yet/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /\+ Add Repo Mapping/i })).toBeInTheDocument()
  })

  it('renders the primary-branch editor and existing mapping when one is primary', () => {
    const primary = makeMapping()
    render(
      <SettingsTab
        {...baseProps}
        primaryRepoMapping={primary}
        repoMappings={[primary]}
      />,
    )
    // The phrase appears both in the editor heading and in the precedence note
    expect(screen.getAllByText(/Primary Repo Mapping Branch/i).length).toBeGreaterThan(0)
    expect(screen.getByText('/mirrors/agent-native-pm')).toBeInTheDocument()
    expect(screen.getAllByText(/primary/i).length).toBeGreaterThan(0)
  })

  it('surfaces the mirror discovery error banner when loading fails', () => {
    render(<SettingsTab {...baseProps} repoMirrorLoadError="permission denied" />)
    expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
  })
})
