package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
)

var mirrorAliasSanitizer = regexp.MustCompile(`[^a-z0-9._-]+`)

func DiscoverMirrorRepos(root string) (*models.MirrorRepoDiscovery, error) {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if cleanRoot == "" {
		cleanRoot = "/mirrors"
	}

	entries, err := os.ReadDir(cleanRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.MirrorRepoDiscovery{MirrorRoot: cleanRoot, Repos: []models.DiscoveredMirrorRepo{}}, nil
		}
		return nil, fmt.Errorf("read mirror root: %w", err)
	}

	repos := make([]models.DiscoveredMirrorRepo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoPath := filepath.Join(cleanRoot, entry.Name())
		if !IsGitRepo(repoPath) {
			continue
		}

		repos = append(repos, models.DiscoveredMirrorRepo{
			RepoName:              entry.Name(),
			RepoPath:              repoPath,
			SuggestedAlias:        suggestMirrorAlias(entry.Name()),
			DetectedDefaultBranch: detectDefaultBranch(repoPath),
		})
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].RepoName < repos[j].RepoName
	})

	return &models.MirrorRepoDiscovery{MirrorRoot: cleanRoot, Repos: repos}, nil
}

func detectDefaultBranch(repoPath string) string {
	commands := [][]string{
		{"-C", repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"},
		{"-C", repoPath, "symbolic-ref", "--quiet", "--short", "HEAD"},
		{"-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		branch := normalizeDetectedBranch(string(output))
		if branch != "" {
			return branch
		}
	}
	return ""
}

func detectRemoteDefaultBranch(repoURL string) string {
	if strings.TrimSpace(repoURL) == "" {
		return ""
	}

	cmd := exec.Command("git", "ls-remote", "--symref", repoURL, "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref:") || !strings.HasSuffix(line, "HEAD") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		branch := normalizeDetectedBranch(parts[1])
		if branch != "" {
			return branch
		}
	}

	return ""
}

func normalizeDetectedBranch(raw string) string {
	branch := strings.TrimSpace(raw)
	branch = strings.TrimPrefix(branch, "origin/")
	branch = strings.TrimPrefix(branch, "refs/heads/")
	branch = strings.TrimPrefix(branch, "refs/remotes/origin/")
	if branch == "HEAD" {
		return ""
	}
	return branch
}

func suggestMirrorAlias(name string) string {
	alias := strings.ToLower(strings.TrimSpace(name))
	alias = mirrorAliasSanitizer.ReplaceAllString(alias, "-")
	alias = strings.Trim(alias, "-._")
	if alias == "" {
		return "repo"
	}
	return alias
}
