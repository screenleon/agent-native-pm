import { useState, useEffect } from 'react'
import type { Project, ProjectRepoMapping, MirrorRepoDiscovery } from '../../types'
import { createProjectRepoMapping, deleteProjectRepoMapping, updateProjectRepoMapping } from '../../api/client'

interface SettingsTabProps {
  projectId: string
  project: Project
  primaryRepoMapping: ProjectRepoMapping | null
  repoMappings: ProjectRepoMapping[]
  repoMirrorDiscovery: MirrorRepoDiscovery | null
  repoMirrorLoading: boolean
  repoMirrorLoadError: string | null
  detectedPrimaryRepoMappingBranch: string
  onLoadRepoMirrorDiscovery: () => void
  onReload: () => void
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
}

export function SettingsTab({
  projectId,
  project,
  primaryRepoMapping,
  repoMappings,
  repoMirrorDiscovery,
  repoMirrorLoading,
  repoMirrorLoadError,
  detectedPrimaryRepoMappingBranch,
  onLoadRepoMirrorDiscovery,
  onReload,
  onError,
  onSuccess,
}: SettingsTabProps) {
  const [showRepoMappingForm, setShowRepoMappingForm] = useState(false)
  const [repoMappingForm, setRepoMappingForm] = useState({ alias: '', repo_path: '', default_branch: '', is_primary: false })
  const [primaryRepoMappingBranchForm, setPrimaryRepoMappingBranchForm] = useState(primaryRepoMapping?.default_branch ?? '')
  const [savingRepoMappingBranch, setSavingRepoMappingBranch] = useState(false)

  useEffect(() => {
    setPrimaryRepoMappingBranchForm(primaryRepoMapping?.default_branch ?? '')
  }, [primaryRepoMapping])

  const primaryRepoMappingBranchChanged = primaryRepoMapping !== null &&
    primaryRepoMappingBranchForm.trim() !== (primaryRepoMapping.default_branch || '').trim()

  async function handleCreateRepoMapping(e: React.FormEvent) {
    e.preventDefault()
    if (!repoMappingForm.alias.trim() || !repoMappingForm.repo_path.trim()) return
    try {
      await createProjectRepoMapping(projectId, {
        alias: repoMappingForm.alias.trim(),
        repo_path: repoMappingForm.repo_path.trim(),
        default_branch: repoMappingForm.default_branch.trim(),
        is_primary: repoMappingForm.is_primary,
      })
      setRepoMappingForm({ alias: '', repo_path: '', default_branch: '', is_primary: false })
      setShowRepoMappingForm(false)
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to create repo mapping')
    }
  }

  async function handleDeleteRepoMapping(mappingId: string) {
    if (!confirm('Delete this repo mapping?')) return
    try {
      await deleteProjectRepoMapping(mappingId)
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to delete repo mapping')
    }
  }

  function handleUseDiscoveredMirror(repo: MirrorRepoDiscovery['repos'][number]) {
    setRepoMappingForm({
      alias: repo.suggested_alias,
      repo_path: repo.repo_path,
      default_branch: repo.detected_default_branch || project?.default_branch || '',
      is_primary: repoMappings.length === 0,
    })
    setShowRepoMappingForm(true)
  }

  async function handleSavePrimaryRepoMappingBranch() {
    if (!primaryRepoMapping) return
    const normalizedBranch = primaryRepoMappingBranchForm.trim()
    const currentBranch = (primaryRepoMapping.default_branch || '').trim()
    if (normalizedBranch === currentBranch) return

    try {
      setSavingRepoMappingBranch(true)
      await updateProjectRepoMapping(primaryRepoMapping.id, { default_branch: normalizedBranch })
      onSuccess(
        normalizedBranch === ''
          ? 'Primary repo mapping branch cleared. Sync will fall back to the project branch or auto-detect.'
          : `Primary repo mapping branch updated to ${normalizedBranch}.`,
      )
      onReload()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Failed to update primary repo mapping branch')
    } finally {
      setSavingRepoMappingBranch(false)
    }
  }

  return (
    <div className="card" style={{ marginBottom: '1.5rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
        <div>
          <h3 style={{ marginBottom: '0.2rem' }}>Repo Mappings</h3>
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
            Bind one or more mirror repositories to this project. Use alias-prefixed paths like <strong>docs-repo/path/to/file.md</strong> for secondary repos.
          </p>
        </div>
        <button className="btn btn-primary" onClick={() => setShowRepoMappingForm(true)}>+ Add Repo Mapping</button>
      </div>

      {primaryRepoMapping && (
        <div style={{ marginTop: '1rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', background: 'var(--bg)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <div>
              <strong>Primary Repo Mapping Branch</strong>
              <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem', marginTop: '0.25rem' }}>
                This override wins over the project default branch for sync. Leave blank to inherit the project fallback branch and auto-detect path.
              </div>
            </div>
            <span className="badge badge-low">Current {primaryRepoMapping.default_branch || 'inherit project fallback'}</span>
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.75rem', alignItems: 'center' }}>
            <input
              value={primaryRepoMappingBranchForm}
              onChange={e => setPrimaryRepoMappingBranchForm(e.target.value)}
              placeholder="leave blank to inherit project branch"
              style={{ padding: '0.5rem 0.75rem', minWidth: '240px', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: '0.375rem', color: 'var(--text)' }}
            />
            <button className="btn btn-primary btn-sm" onClick={handleSavePrimaryRepoMappingBranch} disabled={!primaryRepoMappingBranchChanged || savingRepoMappingBranch}>
              {savingRepoMappingBranch ? 'Saving…' : 'Save Mapping Branch'}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => setPrimaryRepoMappingBranchForm('')} disabled={savingRepoMappingBranch || primaryRepoMappingBranchForm === ''}>
              Clear to Fallback
            </button>
            {detectedPrimaryRepoMappingBranch && detectedPrimaryRepoMappingBranch !== primaryRepoMappingBranchForm.trim() && (
              <button className="btn btn-ghost btn-sm" onClick={() => setPrimaryRepoMappingBranchForm(detectedPrimaryRepoMappingBranch)} disabled={savingRepoMappingBranch}>
                Use Detected {detectedPrimaryRepoMappingBranch}
              </button>
            )}
          </div>
          <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginTop: '0.45rem' }}>
            Precedence: primary repo mapping branch → project default branch → auto-detect.
          </div>
        </div>
      )}

      <div style={{ marginTop: '1rem', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.85rem', background: 'var(--bg)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap', marginBottom: '0.5rem' }}>
          <div>
            <strong>Mounted Mirrors</strong>
            <div style={{ color: 'var(--text-muted)', fontSize: '0.82rem' }}>
              Auto-load mirrors already visible inside the container and prefill the repo mapping form.
            </div>
          </div>
          <button type="button" className="btn btn-ghost btn-sm" onClick={onLoadRepoMirrorDiscovery} disabled={repoMirrorLoading}>
            {repoMirrorLoading ? 'Loading…' : 'Reload'}
          </button>
        </div>

        {repoMirrorLoadError && <div className="error-banner">{repoMirrorLoadError}</div>}

        {repoMirrorLoading ? (
          <div className="loading" style={{ padding: '0.75rem 0' }}>Loading mounted mirrors…</div>
        ) : !repoMirrorDiscovery || repoMirrorDiscovery.repos.length === 0 ? (
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
            No mounted mirrors were discovered for this project context.
          </p>
        ) : (
          <div style={{ display: 'grid', gap: '0.65rem' }}>
            {repoMirrorDiscovery.repos.map(repo => (
              <div key={repo.repo_path} style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', flexWrap: 'wrap' }}>
                <div>
                  <strong>{repo.repo_name}</strong>
                  <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.82rem' }}>{repo.repo_path}</div>
                  <div style={{ marginTop: '0.35rem', display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
                    <span className="badge badge-low">alias {repo.suggested_alias}</span>
                    <span className="badge badge-low">branch {repo.detected_default_branch || project?.default_branch || 'main'}</span>
                    {repo.is_primary_for_project && <span className="badge badge-fresh">primary</span>}
                    {repo.is_mapped_to_project && !repo.is_primary_for_project && <span className="badge badge-low">already mapped</span>}
                  </div>
                </div>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  disabled={repo.is_mapped_to_project}
                  onClick={() => handleUseDiscoveredMirror(repo)}
                >
                  {repo.is_mapped_to_project ? 'Added' : 'Use This Mirror'}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {showRepoMappingForm && (
        <form onSubmit={handleCreateRepoMapping} style={{ marginTop: '1rem', display: 'grid', gap: '0.75rem' }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '0.75rem' }}>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label>Alias</label>
              <input value={repoMappingForm.alias} onChange={e => setRepoMappingForm({ ...repoMappingForm, alias: e.target.value })} placeholder="app, docs-repo, shared-lib" />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label>Repo Path</label>
              <input value={repoMappingForm.repo_path} onChange={e => setRepoMappingForm({ ...repoMappingForm, repo_path: e.target.value })} placeholder="/mirrors/agent-native-pm" />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label>Default Branch</label>
              <input value={repoMappingForm.default_branch} onChange={e => setRepoMappingForm({ ...repoMappingForm, default_branch: e.target.value })} placeholder="main" />
            </div>
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
            <input type="checkbox" checked={repoMappingForm.is_primary} onChange={e => setRepoMappingForm({ ...repoMappingForm, is_primary: e.target.checked })} />
            Set as primary repo
          </label>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button type="submit" className="btn btn-primary">Save Mapping</button>
            <button type="button" className="btn btn-ghost" onClick={() => setShowRepoMappingForm(false)}>Cancel</button>
          </div>
        </form>
      )}

      {repoMappings.length === 0 ? (
        <p style={{ marginTop: '1rem', color: 'var(--text-muted)' }}>No repo mappings yet. Add a primary mapping like <strong>/mirrors/agent-native-pm</strong> to enable mirror-based scanning.</p>
      ) : (
        <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
          {repoMappings.map(mapping => (
            <div key={mapping.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', flexWrap: 'wrap', border: '1px solid var(--border)', borderRadius: '0.5rem', padding: '0.75rem 0.9rem', background: 'var(--bg)' }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                  <strong>{mapping.alias}</strong>
                  {mapping.is_primary && <span className="badge badge-fresh">primary</span>}
                  <span className="badge badge-low">{mapping.default_branch || `inherits ${project?.default_branch || 'auto-detect'}`}</span>
                </div>
                <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>{mapping.repo_path}</div>
              </div>
              <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteRepoMapping(mapping.id)}>Delete</button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
