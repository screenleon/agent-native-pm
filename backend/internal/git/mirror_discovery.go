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
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		branch := strings.TrimSpace(string(output))
		branch = strings.TrimPrefix(branch, "origin/")
		if branch != "" {
			return branch
		}
	}
	return "main"
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