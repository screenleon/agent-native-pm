package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func runGit(t *testing.T, repo string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func gitCommit(t *testing.T, repo, msg, date string) {
	t.Helper()
	env := []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
	}
	runGit(t, repo, env, "commit", "-m", msg)
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, nil, "init")
	runGit(t, repo, nil, "config", "user.email", "test@example.com")
	runGit(t, repo, nil, "config", "user.name", "test")
	return repo
}

func TestRecentChanges_RenameUsesNewPath(t *testing.T) {
	repo := setupGitRepo(t)
	now := time.Now().UTC()
	firstDate := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	secondDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	if err := os.WriteFile(filepath.Join(repo, "legacy.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	runGit(t, repo, nil, "add", "legacy.txt")
	gitCommit(t, repo, "initial", firstDate)

	runGit(t, repo, nil, "mv", "legacy.txt", "renamed.txt")
	runGit(t, repo, nil, "add", "-A")
	gitCommit(t, repo, "rename", secondDate)

	since := now.Add(-3 * 24 * time.Hour)
	files, commitCount, err := RecentChanges(repo, "HEAD", since)
	if err != nil {
		t.Fatalf("RecentChanges error: %v", err)
	}
	if commitCount != 1 {
		t.Fatalf("expected 1 commit, got %d", commitCount)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(files))
	}
	if files[0].Path != "renamed.txt" {
		t.Fatalf("expected renamed.txt, got %s", files[0].Path)
	}
	if files[0].ChangeType != "R" {
		t.Fatalf("expected change type R, got %s", files[0].ChangeType)
	}
}

func TestRecentChanges_DeduplicatesFilesAcrossCommits(t *testing.T) {
	repo := setupGitRepo(t)
	now := time.Now().UTC()
	firstDate := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	secondDate := now.Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	thirdDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	path := filepath.Join(repo, "service.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	runGit(t, repo, nil, "add", "service.go")
	gitCommit(t, repo, "initial", firstDate)

	if err := os.WriteFile(path, []byte("package main\n// v2\n"), 0o644); err != nil {
		t.Fatalf("write v2 file: %v", err)
	}
	runGit(t, repo, nil, "add", "service.go")
	gitCommit(t, repo, "update1", secondDate)

	if err := os.WriteFile(path, []byte("package main\n// v3\n"), 0o644); err != nil {
		t.Fatalf("write v3 file: %v", err)
	}
	runGit(t, repo, nil, "add", "service.go")
	gitCommit(t, repo, "update2", thirdDate)

	since := now.Add(-4 * 24 * time.Hour)
	files, commitCount, err := RecentChanges(repo, "HEAD", since)
	if err != nil {
		t.Fatalf("RecentChanges error: %v", err)
	}
	if commitCount != 2 {
		t.Fatalf("expected 2 commits, got %d", commitCount)
	}
	if len(files) != 1 {
		t.Fatalf("expected deduplicated 1 file, got %d", len(files))
	}
	if files[0].Path != "service.go" {
		t.Fatalf("expected service.go, got %s", files[0].Path)
	}
}

func TestRecentChanges_NoCommitsInRange(t *testing.T) {
	repo := setupGitRepo(t)

	if err := os.WriteFile(filepath.Join(repo, "readme.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, nil, "add", "readme.md")
	gitCommit(t, repo, "initial", "2026-01-01T00:00:00Z")

	since := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	files, commitCount, err := RecentChanges(repo, "HEAD", since)
	if err != nil {
		t.Fatalf("RecentChanges error: %v", err)
	}
	if commitCount != 0 {
		t.Fatalf("expected 0 commits, got %d", commitCount)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestRecentChanges_EmptyRepoReturnsNoChanges(t *testing.T) {
	repo := setupGitRepo(t)

	files, commitCount, err := RecentChanges(repo, "main", time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("RecentChanges error: %v", err)
	}
	if commitCount != 0 {
		t.Fatalf("expected 0 commits, got %d", commitCount)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestRecentChanges_InvalidBranchIncludesGitStderr(t *testing.T) {
	repo := setupGitRepo(t)
	runGit(t, repo, nil, "branch", "-M", "main")

	if err := os.WriteFile(filepath.Join(repo, "readme.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, nil, "add", "readme.md")
	gitCommit(t, repo, "initial", "2026-01-01T00:00:00Z")

	_, _, err := RecentChanges(repo, "does-not-exist", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected error for invalid branch")
	}
	if !strings.Contains(err.Error(), "detected default branch is \"main\"") {
		t.Fatalf("expected detected branch hint in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "unknown revision") && !strings.Contains(err.Error(), "ambiguous argument") {
		t.Fatalf("expected git stderr in error, got %v", err)
	}
}

func TestIsGitRepo(t *testing.T) {
	repo := setupGitRepo(t)
	if !IsGitRepo(repo) {
		t.Fatalf("expected true for git repo")
	}

	nonRepo := t.TempDir()
	if IsGitRepo(nonRepo) {
		t.Fatalf("expected false for non-git repo")
	}
}

func TestRecentChanges_ReturnsStableOrderForAssertions(t *testing.T) {
	repo := setupGitRepo(t)
	now := time.Now().UTC()

	for _, f := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(repo, f), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", now.Add(-2*24*time.Hour).Format(time.RFC3339))

	since := now.Add(-3 * 24 * time.Hour)
	files, _, err := RecentChanges(repo, "HEAD", since)
	if err != nil {
		t.Fatalf("RecentChanges error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	paths := []string{files[0].Path, files[1].Path}
	sort.Strings(paths)
	if paths[0] != "a.txt" || paths[1] != "b.txt" {
		t.Fatalf("unexpected changed paths: %v", paths)
	}
}
