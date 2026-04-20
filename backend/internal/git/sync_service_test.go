package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupSyncTestDB(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	oldDocDate := now.Add(-14 * 24 * time.Hour)

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	codePath := filepath.Join(repo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", initialDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "sync project",
		Description:   "for sync test",
		RepoPath:      repo,
		DefaultBranch: "HEAD",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Service Doc",
		FilePath: "docs/service.md",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	if _, err := db.Exec(`UPDATE documents SET last_updated_at=$1 WHERE id=$2`, oldDocDate, doc.ID); err != nil {
		t.Fatalf("set document last_updated_at: %v", err)
	}

	if _, err := docLinkStore.Create(doc.ID, models.CreateDocumentLinkRequest{
		CodePath: "src/service.go",
		LinkType: "covers",
	}); err != nil {
		t.Fatalf("create document link: %v", err)
	}

	if err := os.WriteFile(codePath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "change service", changedDate)

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}

	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}
	if run.CommitsScanned < 1 {
		t.Fatalf("expected commits_scanned >= 1, got %d", run.CommitsScanned)
	}
	if run.FilesChanged < 1 {
		t.Fatalf("expected files_changed >= 1, got %d", run.FilesChanged)
	}

	updatedDoc, err := docStore.GetByID(doc.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if updatedDoc == nil || !updatedDoc.IsStale {
		t.Fatalf("expected document marked stale")
	}
	if updatedDoc.StalenessDays < 1 {
		t.Fatalf("expected staleness_days >= 1, got %d", updatedDoc.StalenessDays)
	}

	signals, total, err := driftStore.ListByProject(project.ID, "open", "created_at", 1, 20)
	if err != nil {
		t.Fatalf("list drift signals: %v", err)
	}
	if total != 1 || len(signals) != 1 {
		t.Fatalf("expected exactly 1 open drift signal, got total=%d len=%d", total, len(signals))
	}
	if signals[0].DocumentID != doc.ID {
		t.Fatalf("expected drift for doc %s, got %s", doc.ID, signals[0].DocumentID)
	}
	if signals[0].TriggerType != "code_change" {
		t.Fatalf("expected trigger_type code_change, got %s", signals[0].TriggerType)
	}

	updatedProject, err := projectStore.GetByID(project.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if updatedProject.LastSyncAt == nil {
		t.Fatalf("expected project last_sync_at to be set")
	}
}

func TestSyncServiceRun_CreatesSyncRunAndDriftSignals(t *testing.T) {
	setupSyncTestDB(t)
}

func TestSyncServiceRun_EmptyRepoCompletesWithZeroBaseline(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "empty repo project",
		RepoPath:      repo,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}
	if run.CommitsScanned != 0 {
		t.Fatalf("expected 0 commits_scanned, got %d", run.CommitsScanned)
	}
	if run.FilesChanged != 0 {
		t.Fatalf("expected 0 files_changed, got %d", run.FilesChanged)
	}
	if run.ErrorMessage != "" {
		t.Fatalf("expected empty error message, got %q", run.ErrorMessage)
	}

	updatedProject, err := projectStore.GetByID(project.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if updatedProject == nil || updatedProject.LastSyncAt == nil {
		t.Fatalf("expected project last_sync_at to be set")
	}
}

func TestSyncServiceRun_AutoDetectsEmptyProjectBranch(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	runGit(t, repo, nil, "branch", "-M", "main")
	codePath := filepath.Join(repo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339))

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "auto detect branch project",
		RepoPath:      repo,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	emptyBranch := ""
	project, err = projectStore.Update(project.ID, models.UpdateProjectRequest{DefaultBranch: &emptyBranch})
	if err != nil {
		t.Fatalf("clear project branch: %v", err)
	}

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}
}

func TestSyncServiceRun_InvalidConfiguredBranchHintsDetectedBranch(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	runGit(t, repo, nil, "branch", "-M", "main")
	codePath := filepath.Join(repo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339))

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "invalid branch project",
		RepoPath:      repo,
		DefaultBranch: "master",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	_, err = svc.Run(project.ID)
	if err == nil {
		t.Fatal("expected sync to fail for invalid configured branch")
	}
	if !strings.Contains(err.Error(), "detected default branch is \"main\"") {
		t.Fatalf("expected detected branch hint, got %v", err)
	}
}

func TestSyncServiceRun_DedupesSignalsForSameDocumentAcrossMultipleChangedFiles(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	servicePath := filepath.Join(repo, "src", "service.go")
	helperPath := filepath.Join(repo, "src", "helper.go")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(servicePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write service code: %v", err)
	}
	if err := os.WriteFile(helperPath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write helper code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", initialDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "sync project",
		RepoPath:      repo,
		DefaultBranch: "HEAD",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Service Doc",
		FilePath: "docs/service.md",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	if _, err := docLinkStore.Create(doc.ID, models.CreateDocumentLinkRequest{CodePath: "src/service.go", LinkType: "covers"}); err != nil {
		t.Fatalf("create link service: %v", err)
	}
	if _, err := docLinkStore.Create(doc.ID, models.CreateDocumentLinkRequest{CodePath: "src/helper.go", LinkType: "covers"}); err != nil {
		t.Fatalf("create link helper: %v", err)
	}

	if err := os.WriteFile(servicePath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed service: %v", err)
	}
	if err := os.WriteFile(helperPath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed helper: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "change both files", changedDate)

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}
	if run.FilesChanged < 2 {
		t.Fatalf("expected files_changed >= 2, got %d", run.FilesChanged)
	}

	signals, total, err := driftStore.ListByProject(project.ID, "open", "created_at", 1, 20)
	if err != nil {
		t.Fatalf("list drift signals: %v", err)
	}
	if total != 1 || len(signals) != 1 {
		t.Fatalf("expected exactly 1 open drift signal after dedupe, got total=%d len=%d", total, len(signals))
	}
}

func TestSyncServiceRun_MatchesDocumentsRegistryFilePathWithoutDocumentLink(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	codePath := filepath.Join(repo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", initialDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "registry project",
		RepoPath:      repo,
		DefaultBranch: "HEAD",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Service Source Doc",
		FilePath: "src/service.go",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	if err := os.WriteFile(codePath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "change service", changedDate)

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, t.TempDir())
	if _, err := svc.Run(project.ID); err != nil {
		t.Fatalf("run sync: %v", err)
	}

	updatedDoc, err := docStore.GetByID(doc.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if updatedDoc == nil || !updatedDoc.IsStale {
		t.Fatalf("expected registry-matched document to be marked stale")
	}

	signals, total, err := driftStore.ListByProject(project.ID, "open", "created_at", 1, 20)
	if err != nil {
		t.Fatalf("list drift signals: %v", err)
	}
	if total != 1 || len(signals) != 1 {
		t.Fatalf("expected 1 open drift signal for registry-matched document, got total=%d len=%d", total, len(signals))
	}
	if signals[0].DocumentID != doc.ID {
		t.Fatalf("expected drift signal for doc %s, got %s", doc.ID, signals[0].DocumentID)
	}
}

func TestSyncServiceRun_CreatesTimeDecaySignalUsingThreshold(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	repo := setupGitRepo(t)
	codePath := filepath.Join(repo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, repo, nil, "add", ".")
	gitCommit(t, repo, "initial", initialDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "time-decay project",
		RepoPath:      repo,
		DefaultBranch: "HEAD",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Old Doc",
		FilePath: "docs/old.md",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	oldDocDate := now.Add(-3 * 24 * time.Hour)
	if _, err := db.Exec(`UPDATE documents SET last_updated_at=$1 WHERE id=$2`, oldDocDate, doc.ID); err != nil {
		t.Fatalf("set document last_updated_at: %v", err)
	}

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 0, t.TempDir())
	if _, err := svc.Run(project.ID); err != nil {
		t.Fatalf("run sync: %v", err)
	}

	signals, total, err := driftStore.ListByProject(project.ID, "open", "created_at", 1, 20)
	if err != nil {
		t.Fatalf("list drift signals: %v", err)
	}
	if total != 1 || len(signals) != 1 {
		t.Fatalf("expected 1 open drift signal for time decay, got total=%d len=%d", total, len(signals))
	}
	if signals[0].TriggerType != "time_decay" {
		t.Fatalf("expected trigger_type time_decay, got %s", signals[0].TriggerType)
	}
	if signals[0].DocumentID != doc.ID {
		t.Fatalf("expected signal for doc %s, got %s", doc.ID, signals[0].DocumentID)
	}
}

func TestSyncServiceRun_ClonesManagedRepoFromRepoURL(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)
	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	remoteRepo := setupGitRepo(t)
	runGit(t, remoteRepo, nil, "branch", "-M", "main")
	codePath := filepath.Join(remoteRepo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, remoteRepo, nil, "add", ".")
	gitCommit(t, remoteRepo, "initial", initialDate)

	if err := os.WriteFile(codePath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed code: %v", err)
	}
	runGit(t, remoteRepo, nil, "add", ".")
	gitCommit(t, remoteRepo, "changed", changedDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "managed repo project",
		RepoURL:       "file://" + remoteRepo,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Service Source Doc",
		FilePath: "src/service.go",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	repoRoot := t.TempDir()
	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, repoRoot)
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}

	updatedProject, err := projectStore.GetByID(project.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if updatedProject == nil || updatedProject.RepoPath == "" {
		t.Fatalf("expected managed repo path to be stored")
	}
	if !strings.HasPrefix(updatedProject.RepoPath, repoRoot) {
		t.Fatalf("expected managed repo path under repo root, got %s", updatedProject.RepoPath)
	}

	updatedDoc, err := docStore.GetByID(doc.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if updatedDoc == nil || !updatedDoc.IsStale {
		t.Fatalf("expected document marked stale after managed clone sync")
	}
}

func TestSyncServiceRun_ManagedCloneAutoDetectsRemoteDefaultBranch(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)
	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)

	remoteRepo := setupGitRepo(t)
	runGit(t, remoteRepo, nil, "branch", "-M", "main")
	codePath := filepath.Join(remoteRepo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(codePath), 0o755); err != nil {
		t.Fatalf("mkdir code dir: %v", err)
	}
	if err := os.WriteFile(codePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write initial code: %v", err)
	}
	runGit(t, remoteRepo, nil, "add", ".")
	gitCommit(t, remoteRepo, "initial", initialDate)

	if err := os.WriteFile(codePath, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed code: %v", err)
	}
	runGit(t, remoteRepo, nil, "add", ".")
	gitCommit(t, remoteRepo, "changed", changedDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:    "managed repo auto detect project",
		RepoURL: "file://" + remoteRepo,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	emptyBranch := ""
	project, err = projectStore.Update(project.ID, models.UpdateProjectRequest{DefaultBranch: &emptyBranch})
	if err != nil {
		t.Fatalf("clear project branch: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Service Source Doc",
		FilePath: "src/service.go",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	repoRoot := t.TempDir()
	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, nil, 30, repoRoot)
	run, err := svc.Run(project.ID)
	if err != nil {
		t.Fatalf("run sync: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("expected completed run, got %s", run.Status)
	}

	updatedDoc, err := docStore.GetByID(doc.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if updatedDoc == nil || !updatedDoc.IsStale {
		t.Fatalf("expected document marked stale after managed clone sync")
	}
}

func TestSyncServiceRun_PrefixesSecondaryRepoChangesWithAlias(t *testing.T) {
	now := time.Now().UTC()
	initialDate := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	changedDate := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	db := testutil.OpenTestDB(t)
	projectStore := store.NewProjectStore(db)
	docStore := store.NewDocumentStore(db)
	docLinkStore := store.NewDocumentLinkStore(db)
	driftStore := store.NewDriftSignalStore(db)
	syncStore := store.NewSyncRunStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)

	primaryRepo := setupGitRepo(t)
	runGit(t, primaryRepo, nil, "branch", "-M", "main")
	primaryCodePath := filepath.Join(primaryRepo, "src", "service.go")
	if err := os.MkdirAll(filepath.Dir(primaryCodePath), 0o755); err != nil {
		t.Fatalf("mkdir primary repo dir: %v", err)
	}
	if err := os.WriteFile(primaryCodePath, []byte("package service\n"), 0o644); err != nil {
		t.Fatalf("write primary code: %v", err)
	}
	runGit(t, primaryRepo, nil, "add", ".")
	gitCommit(t, primaryRepo, "initial primary", initialDate)

	secondaryRepo := setupGitRepo(t)
	runGit(t, secondaryRepo, nil, "branch", "-M", "main")
	secondaryCodePath := filepath.Join(secondaryRepo, "pkg", "helper.go")
	if err := os.MkdirAll(filepath.Dir(secondaryCodePath), 0o755); err != nil {
		t.Fatalf("mkdir secondary repo dir: %v", err)
	}
	if err := os.WriteFile(secondaryCodePath, []byte("package helper\n"), 0o644); err != nil {
		t.Fatalf("write secondary code: %v", err)
	}
	runGit(t, secondaryRepo, nil, "add", ".")
	gitCommit(t, secondaryRepo, "initial secondary", initialDate)

	project, err := projectStore.Create(models.CreateProjectRequest{
		Name:          "multi repo project",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if _, err := repoMappingStore.Create(project.ID, models.CreateProjectRepoMappingRequest{
		Alias:         "app",
		RepoPath:      primaryRepo,
		DefaultBranch: "main",
		IsPrimary:     true,
	}); err != nil {
		t.Fatalf("create primary mapping: %v", err)
	}
	if _, err := repoMappingStore.Create(project.ID, models.CreateProjectRepoMappingRequest{
		Alias:         "shared",
		RepoPath:      secondaryRepo,
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("create secondary mapping: %v", err)
	}

	doc, err := docStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "Shared Helper Doc",
		FilePath: "docs/shared-helper.md",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if _, err := docLinkStore.Create(doc.ID, models.CreateDocumentLinkRequest{CodePath: "shared/pkg/helper.go", LinkType: "covers"}); err != nil {
		t.Fatalf("create alias-prefixed link: %v", err)
	}

	if err := os.WriteFile(secondaryCodePath, []byte("package helper\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write changed secondary code: %v", err)
	}
	runGit(t, secondaryRepo, nil, "add", ".")
	gitCommit(t, secondaryRepo, "change helper", changedDate)

	svc := NewSyncService(syncStore, docLinkStore, driftStore, docStore, projectStore, repoMappingStore, 30, t.TempDir())
	if _, err := svc.Run(project.ID); err != nil {
		t.Fatalf("run sync: %v", err)
	}

	updatedDoc, err := docStore.GetByID(doc.ID)
	if err != nil {
		t.Fatalf("get updated doc: %v", err)
	}
	if updatedDoc == nil || !updatedDoc.IsStale {
		t.Fatalf("expected alias-matched document to be marked stale")
	}

	signals, total, err := driftStore.ListByProject(project.ID, "open", "created_at", 1, 20)
	if err != nil {
		t.Fatalf("list drift signals: %v", err)
	}
	if total != 1 || len(signals) != 1 {
		t.Fatalf("expected 1 open drift signal for alias-matched change, got total=%d len=%d", total, len(signals))
	}
	if !strings.Contains(signals[0].TriggerDetail, "shared/pkg/helper.go") {
		t.Fatalf("expected trigger detail to include alias-prefixed path, got %s", signals[0].TriggerDetail)
	}
}
