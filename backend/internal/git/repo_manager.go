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
	if branch == "" {
		branch = "main"
	}

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return "", fmt.Errorf("create repo root: %w", err)
	}

	targetPath := managedRepoPath(repoRoot, projectID)
	if !IsGitRepo(targetPath) {
		if err := os.RemoveAll(targetPath); err != nil {
			return "", fmt.Errorf("clean managed repo path: %w", err)
		}
		cmd := exec.Command("git", "clone", "--branch", branch, "--single-branch", repoURL, targetPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(output)))
		}
		return targetPath, nil
	}

	commands := [][]string{
		{"-C", targetPath, "fetch", "origin", branch, "--prune"},
		{"-C", targetPath, "checkout", "-B", branch, "origin/" + branch},
		{"-C", targetPath, "reset", "--hard", "origin/" + branch},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git sync failed: %s", strings.TrimSpace(string(output)))
		}
	}

	return targetPath, nil
}
