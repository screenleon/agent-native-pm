import type { Project, SyncRun } from '../types'

export type SyncGuidance = {
  tone: 'neutral' | 'success' | 'warning' | 'danger'
  headline: string
  detail: string
  nextAction: string
}

type SyncFailureGuidance = {
  headline: string
  detail: string
  nextAction: string
  triageHint: string
}

type ProjectRiskSnapshot = {
  openDriftCount: number
  latestSync: SyncRun | null
}

function failedSyncGuidance(errorMessage: string): SyncFailureGuidance | null {
  const error = errorMessage.toLowerCase()

  if (error.includes('no repo_path or repo_url configured')) {
    return {
      headline: 'Missing repository source',
      detail: 'This project does not have a repo mapping, repo URL, or manual repo path configured.',
      nextAction: 'Add a primary mirror mapping, set a repo URL for managed clone mode, or provide a manual repo path, then run sync again.',
      triageHint: 'Sync failed: project has no repo source configured. Add a primary mirror mapping, repo URL, or manual repo path.',
    }
  }

  if (error.includes('git clone failed') || error.includes('git sync failed')) {
    return {
      headline: 'Managed repository update failed',
      detail: 'The service could not clone or refresh the managed repository checkout.',
      nextAction: 'Verify repo URL, branch, and repository access, then rerun sync.',
      triageHint: 'Sync failed: managed repository update failed. Verify repo URL, branch, and access.',
    }
  }

  if (error.includes('not a git repository')) {
    return {
      headline: 'Repo path is not a Git repository',
      detail: 'The configured path does not contain a valid .git repository.',
      nextAction: 'Check repo path and branch, then rerun sync.',
      triageHint: 'Sync failed: repo path is not a git repository. Verify path and branch.',
    }
  }

  if (error.includes('no commits yet') || error.includes('repository has no commits')) {
    return {
      headline: 'Repository has no commits yet',
      detail: 'The configured repository exists, but there is no commit history to scan yet.',
      nextAction: 'Create the first commit in the repository, then rerun sync.',
      triageHint: 'Sync failed: repository has no commits yet. Create the first commit, then rerun sync.',
    }
  }

  if (error.includes('unknown revision') || error.includes('ambiguous argument') || error.includes('needed a single revision')) {
    return {
      headline: 'Default branch could not be resolved',
      detail: 'Git could not resolve the configured branch or revision for history scanning.',
      nextAction: 'Verify the project default branch matches an existing branch in the repository, then rerun sync.',
      triageHint: 'Sync failed: branch could not be resolved. Verify the configured default branch.',
    }
  }

  if (error.includes('git rev-list count failed') || error.includes('git log failed')) {
    return {
      headline: 'Git scan command failed',
      detail: 'Git history scan failed, often due to an invalid branch or inaccessible repo state.',
      nextAction: 'Verify default branch and ensure the repo is accessible by the service.',
      triageHint: 'Sync failed: git scan command failed. Check branch and repository accessibility.',
    }
  }

  return null
}

export function syncTriageHint(project: Pick<Project, 'repo_path' | 'repo_url'>, snapshot: ProjectRiskSnapshot | undefined) {
  if (!project.repo_path && !project.repo_url) {
    return 'Add a primary mirror mapping first. Managed clone URL and manual path remain fallback options.'
  }
  if (!project.repo_path && project.repo_url) {
    return 'This project is using managed clone fallback mode. Mounted mirror mappings are preferred for local changes.'
  }
  if (!snapshot || !snapshot.latestSync) {
    return 'Run first sync to establish drift baseline.'
  }

  const latestSync = snapshot.latestSync
  if (latestSync.status === 'failed') {
    return failedSyncGuidance(latestSync.error_message)?.triageHint ?? 'Sync failed: inspect latest sync error and rerun after fixing repo setup.'
  }
  if (latestSync.status === 'running') {
    return 'Sync is still running. Refresh shortly to review drift impact.'
  }
  if (snapshot.openDriftCount > 0) {
    return `${snapshot.openDriftCount} open drift signal${snapshot.openDriftCount === 1 ? '' : 's'} need review.`
  }
  if (latestSync.files_changed === 0) {
    return 'Latest sync found no changed files in current scan window.'
  }
  return 'Latest sync changed files but no open drift remains.'
}

export function syncRunGuidance(run: SyncRun, openDrift: number): SyncGuidance {
  if (run.status === 'running') {
    return {
      tone: 'warning',
      headline: 'Sync currently running',
      detail: 'The scan has started but has not completed yet.',
      nextAction: 'Wait for completion, then review open drift signals.',
    }
  }

  if (run.status === 'failed') {
    const failedGuidance = failedSyncGuidance(run.error_message ?? '')
    if (failedGuidance) {
      return {
        tone: 'danger',
        headline: failedGuidance.headline,
        detail: failedGuidance.detail,
        nextAction: failedGuidance.nextAction,
      }
    }
    return {
      tone: 'danger',
      headline: 'Sync failed',
      detail: 'The sync run did not finish successfully.',
      nextAction: 'Read the error message and rerun after fixing the repository setup.',
    }
  }

  if (run.files_changed === 0 && run.commits_scanned === 0) {
    return {
      tone: 'success',
      headline: 'No new commits in scan window',
      detail: 'No commits were detected since the previous sync baseline.',
      nextAction: 'No action required now. Run sync again after code changes.',
    }
  }

  if (run.files_changed === 0) {
    return {
      tone: 'neutral',
      headline: 'No tracked file changes detected',
      detail: 'Commits exist, but no relevant file changes were found for drift detection.',
      nextAction: 'No immediate drift action needed. Keep syncing on each release cycle.',
    }
  }

  if (openDrift > 0) {
    return {
      tone: 'warning',
      headline: `${openDrift} open drift signal${openDrift === 1 ? '' : 's'} need triage`,
      detail: 'Code changes were detected and linked documents may now be stale.',
      nextAction: 'Open the Drift tab and resolve or dismiss each signal.',
    }
  }

  return {
    tone: 'success',
    headline: 'Changes scanned and drift is under control',
    detail: 'Sync found changed files, and no open drift signals remain.',
    nextAction: 'Continue regular sync checks to keep docs aligned.',
  }
}