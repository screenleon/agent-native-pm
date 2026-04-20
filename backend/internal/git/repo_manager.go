package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func managedRepoPath(repoRoot, projectID string) string {
	return filepath.Join(repoRoot, projectID)
}

func EnsureManagedRepo(repoRoot, projectID, repoURL, branch string) (string, error) {
	if strings.TrimSpace(repoURL) == "" {
		return "", fmt.Errorf("project has no repo_url configured")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return "", fmt.Errorf("repo root is not configured")
	}
	configuredBranch := strings.TrimSpace(branch)

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return "", fmt.Errorf("create repo root: %w", err)
	}

	targetPath := managedRepoPath(repoRoot, projectID)
	resolvedBranch := configuredBranch
	if resolvedBranch == "" && IsGitRepo(targetPath) {
		resolvedBranch = detectDefaultBranch(targetPath)
	}
	if resolvedBranch == "" {
		resolvedBranch = detectRemoteDefaultBranch(repoURL)
	}

	if !IsGitRepo(targetPath) {
		if err := os.RemoveAll(targetPath); err != nil {
			return "", fmt.Errorf("clean managed repo path: %w", err)
		}
		cloneArgs := []string{"clone"}
		if resolvedBranch != "" {
			cloneArgs = append(cloneArgs, "--branch", resolvedBranch, "--single-branch")
		}
		cloneArgs = append(cloneArgs, repoURL, targetPath)
		cmd := exec.Command("git", cloneArgs...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", formatManagedRepoBranchError("git clone failed", configuredBranch, resolvedBranch, detectRemoteDefaultBranch(repoURL), output)
		}
		return targetPath, nil
	}

	if resolvedBranch == "" {
		resolvedBranch = detectDefaultBranch(targetPath)
	}
	if resolvedBranch == "" {
		return "", fmt.Errorf("managed repo default branch could not be resolved")
	}

	commands := [][]string{
		{"-C", targetPath, "fetch", "origin", resolvedBranch, "--prune"},
		{"-C", targetPath, "checkout", "-B", resolvedBranch, "origin/" + resolvedBranch},
		{"-C", targetPath, "reset", "--hard", "origin/" + resolvedBranch},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", formatManagedRepoBranchError("git sync failed", configuredBranch, resolvedBranch, detectDefaultBranch(targetPath), output)
		}
	}

	return targetPath, nil
}

func formatManagedRepoBranchError(prefix, configuredBranch, attemptedBranch, detectedBranch string, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = prefix
	}
	if configuredBranch != "" && detectedBranch != "" && detectedBranch != configuredBranch {
		return fmt.Errorf("%s for configured branch %q: detected default branch is %q: %s", prefix, configuredBranch, detectedBranch, detail)
	}
	if attemptedBranch != "" {
		return fmt.Errorf("%s for branch %q: %s", prefix, attemptedBranch, detail)
	}
	return fmt.Errorf("%s: %s", prefix, detail)
}
