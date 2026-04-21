package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ChangedFile represents a single file changed in git history.
type ChangedFile struct {
	Path       string
	ChangeType string // M | A | D | R
}

// RecentChanges returns files changed in the repo since `since` on the given branch.
// It shells out to git so git must be available in PATH.
func RecentChanges(repoPath, branch string, since time.Time) ([]ChangedFile, int, error) {
	sinceStr := since.UTC().Format("2006-01-02T15:04:05")
	configuredBranch := strings.TrimSpace(branch)
	resolvedBranch := configuredBranch
	if resolvedBranch == "" {
		resolvedBranch = detectDefaultBranch(repoPath)
		if resolvedBranch == "" {
			resolvedBranch = "HEAD"
		}
	}

	hasHistory, err := repoHasHistory(repoPath)
	if err != nil {
		return nil, 0, fmt.Errorf("git history check failed: %w", err)
	}
	if !hasHistory {
		return []ChangedFile{}, 0, nil
	}

	// Count commits in the range
	countOut, err := gitCombinedOutput(repoPath,
		"rev-list", "--count",
		fmt.Sprintf("--since=%s", sinceStr),
		resolvedBranch,
	)
	if err != nil {
		return nil, 0, wrapBranchResolutionError(repoPath, resolvedBranch, configuredBranch != "", "git rev-list count", err)
	}
	commitCount := 0
	fmt.Sscanf(countOut, "%d", &commitCount)

	if commitCount == 0 {
		return []ChangedFile{}, 0, nil
	}

	// Get changed files
	out, err := gitCombinedOutput(repoPath,
		"log",
		fmt.Sprintf("--since=%s", sinceStr),
		"--name-status",
		"--diff-filter=ADMR",
		"--format=",
		resolvedBranch,
	)
	if err != nil {
		return nil, 0, wrapBranchResolutionError(repoPath, resolvedBranch, configuredBranch != "", "git log", err)
	}

	seen := make(map[string]bool)
	var files []ChangedFile

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		changeType := string(parts[0][0]) // first char: M/A/D/R
		// Rename lines are usually: "R100 old/path new/path".
		// Use the last token so we track the current file path.
		path := parts[len(parts)-1]
		if !seen[path] {
			seen[path] = true
			files = append(files, ChangedFile{Path: path, ChangeType: changeType})
		}
	}
	if files == nil {
		files = []ChangedFile{}
	}
	return files, commitCount, scanner.Err()
}

func wrapBranchResolutionError(repoPath, branch string, explicitlyConfigured bool, command string, err error) error {
	if explicitlyConfigured {
		detectedBranch := detectDefaultBranch(repoPath)
		if detectedBranch != "" && detectedBranch != branch {
			return fmt.Errorf("%s failed for branch %q: configured branch could not be resolved; detected default branch is %q: %w", command, branch, detectedBranch, err)
		}
	}
	return fmt.Errorf("%s failed for branch %q: %w", command, branch, err)
}

// IsGitRepo returns true if the given path is a git repository.
func IsGitRepo(repoPath string) bool {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func repoHasHistory(repoPath string) (bool, error) {
	output, err := gitCombinedOutput(repoPath, "rev-list", "--count", "--all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "0", nil
}

func gitCombinedOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	cmd.Env = append(os.Environ(),
		"LC_ALL=C",
		"LANG=C",
	)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			trimmed = err.Error()
		}
		return "", fmt.Errorf("%s", trimmed)
	}
	return trimmed, nil
}
