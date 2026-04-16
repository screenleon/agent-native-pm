package git

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// SyncService orchestrates a repo scan and drift signal generation.
type SyncService struct {
	syncStore          *store.SyncRunStore
	docLinkStore       *store.DocumentLinkStore
	driftStore         *store.DriftSignalStore
	docStore           *store.DocumentStore
	projectStore       *store.ProjectStore
	repoMappingStore   *store.ProjectRepoMappingStore
	staleDaysThreshold int
	repoRoot           string
}

func NewSyncService(
	syncStore *store.SyncRunStore,
	docLinkStore *store.DocumentLinkStore,
	driftStore *store.DriftSignalStore,
	docStore *store.DocumentStore,
	projectStore *store.ProjectStore,
	repoMappingStore *store.ProjectRepoMappingStore,
	staleDaysThreshold int,
	repoRoot string,
) *SyncService {
	if staleDaysThreshold < 0 {
		staleDaysThreshold = 30
	}
	return &SyncService{
		syncStore:          syncStore,
		docLinkStore:       docLinkStore,
		driftStore:         driftStore,
		docStore:           docStore,
		projectStore:       projectStore,
		repoMappingStore:   repoMappingStore,
		staleDaysThreshold: staleDaysThreshold,
		repoRoot:           repoRoot,
	}
}

// ── False-positive filter ─────────────────────────────────────────────────────

// isNoisyPath returns true for auto-generated, test, vendored, or lock files
// whose changes rarely require doc updates.
func isNoisyPath(path string) bool {
	lower := strings.ToLower(path)
	noisyDirs := []string{
		"vendor/", "node_modules/", ".git/", "dist/", "build/",
		"__pycache__/", ".next/", "coverage/",
	}
	for _, dir := range noisyDirs {
		if strings.HasPrefix(lower, dir) || strings.Contains(lower, "/"+dir) {
			return true
		}
	}
	noisySuffixes := []string{
		"_test.go", "_test.ts", "_test.tsx", ".test.ts", ".test.tsx",
		".test.js", ".spec.ts", ".spec.js",
		".pb.go", ".pb.gw.go", // protobuf generated
		"_gen.go", ".generated.", // code gen
		"package-lock.json", "yarn.lock", "go.sum",
		".min.js", ".min.css",
	}
	base := filepath.Base(lower)
	for _, suf := range noisySuffixes {
		if strings.HasSuffix(base, suf) || strings.Contains(base, suf) {
			return true
		}
	}
	return false
}

// ── Severity scoring ──────────────────────────────────────────────────────────

// codeChangeSeverity computes a 1-3 severity score and a confidence label for a
// code_change signal.
//
//	severity 3 (high)   — destructive change (D/R) or many files + linked doc
//	severity 2 (medium) — linked doc or multiple files changed
//	severity 1 (low)    — only registry match, single file modified
func codeChangeSeverity(files []models.ChangedFileMeta, hasDocumentLink bool) (severity int, confidence string) {
	hasDelete := false
	for _, f := range files {
		if f.ChangeType == "D" || f.ChangeType == "R" {
			hasDelete = true
			break
		}
	}

	// Confidence: high when the document has an explicit link to the changed file.
	if hasDocumentLink {
		confidence = "high"
	} else {
		confidence = "medium" // registry path match — less precise
	}

	switch {
	case hasDelete && hasDocumentLink:
		severity = 3
	case hasDelete || (len(files) >= 3 && hasDocumentLink):
		severity = 3
	case hasDocumentLink || len(files) >= 3:
		severity = 2
	default:
		severity = 1
	}
	return
}

// ── Main Run ──────────────────────────────────────────────────────────────────

// Run performs a synchronous repo scan for a project, creates a SyncRun record,
// generates drift signals for any linked documents whose files changed, and marks
// those documents as stale.
func (s *SyncService) Run(projectID string) (*models.SyncRun, error) {
	project, err := s.projectStore.GetByID(projectID)
	if err != nil || project == nil {
		return nil, fmt.Errorf("project not found")
	}

	mappings := []models.ProjectRepoMapping{}
	if s.repoMappingStore != nil {
		mappings, err = s.repoMappingStore.ListByProject(projectID)
		if err != nil {
			return nil, fmt.Errorf("failed to load repo mappings: %w", err)
		}
	}

	if len(mappings) == 0 && project.RepoPath == "" {
		if project.RepoURL == "" {
			return nil, fmt.Errorf("project has no repo_path or repo_url configured")
		}
		managedPath, ensureErr := EnsureManagedRepo(s.repoRoot, projectID, project.RepoURL, project.DefaultBranch)
		if ensureErr != nil {
			return nil, ensureErr
		}
		project.RepoPath = managedPath
		_, _ = s.projectStore.Update(projectID, models.UpdateProjectRequest{RepoPath: &managedPath})
	} else if len(mappings) == 0 && project.RepoURL != "" && strings.HasPrefix(project.RepoPath, managedRepoPath(s.repoRoot, projectID)) {
		managedPath, ensureErr := EnsureManagedRepo(s.repoRoot, projectID, project.RepoURL, project.DefaultBranch)
		if ensureErr != nil {
			return nil, ensureErr
		}
		project.RepoPath = managedPath
	}
	if len(mappings) == 0 && !IsGitRepo(project.RepoPath) {
		return nil, fmt.Errorf("repo_path is not a git repository: %s", project.RepoPath)
	}

	// Create sync run record
	syncRun, err := s.syncStore.Create(projectID)
	if err != nil {
		return nil, err
	}

	// Determine scan window: from last_sync_at or 30 days ago
	var since time.Time
	if project.LastSyncAt != nil {
		since = *project.LastSyncAt
	} else {
		since = time.Now().UTC().Add(-30 * 24 * time.Hour)
	}

	type logicalChangedFile struct {
		Path       string
		ChangeType string
	}
	var changedFiles []logicalChangedFile
	commitCount := 0
	seenChangedFiles := map[string]bool{}

	appendLogicalChange := func(path, changeType string) {
		key := path + "|" + changeType
		if seenChangedFiles[key] {
			return
		}
		seenChangedFiles[key] = true
		changedFiles = append(changedFiles, logicalChangedFile{Path: path, ChangeType: changeType})
	}

	if len(mappings) > 0 {
		for _, mapping := range mappings {
			if !IsGitRepo(mapping.RepoPath) {
				if ferr := s.syncStore.Fail(syncRun.ID, fmt.Sprintf("mapped repo is not a git repository: %s", mapping.RepoPath)); ferr != nil {
					log.Printf("failed to record sync failure: %v", ferr)
				}
				return nil, fmt.Errorf("mapped repo is not a git repository: %s", mapping.RepoPath)
			}
			branch := mapping.DefaultBranch
			if branch == "" {
				branch = project.DefaultBranch
			}
			if branch == "" {
				branch = "main"
			}
			mappingChanges, mappingCommits, mappingErr := RecentChanges(mapping.RepoPath, branch, since)
			if mappingErr != nil {
				if ferr := s.syncStore.Fail(syncRun.ID, mappingErr.Error()); ferr != nil {
					log.Printf("failed to record sync failure: %v", ferr)
				}
				return nil, mappingErr
			}
			commitCount += mappingCommits
			for _, cf := range mappingChanges {
				logicalPath := cf.Path
				if !mapping.IsPrimary && strings.TrimSpace(mapping.Alias) != "" {
					logicalPath = strings.TrimSpace(mapping.Alias) + "/" + cf.Path
				}
				appendLogicalChange(logicalPath, cf.ChangeType)
			}
		}
	} else {
		branch := project.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		projectChanges, projectCommits, projectErr := RecentChanges(project.RepoPath, branch, since)
		if projectErr != nil {
			if ferr := s.syncStore.Fail(syncRun.ID, projectErr.Error()); ferr != nil {
				log.Printf("failed to record sync failure: %v", ferr)
			}
			return nil, projectErr
		}
		commitCount = projectCommits
		for _, cf := range projectChanges {
			appendLogicalChange(cf.Path, cf.ChangeType)
		}
	}

	openDocIDs, err := s.driftStore.ListOpenDocumentIDsByProject(projectID)
	if err != nil {
		log.Printf("drift: failed to load open signals for project %s: %v", projectID, err)
		openDocIDs = map[string]bool{}
	}

	// ── Phase 1: build per-document affected file lists ────────────────────

	// affectedDocs maps docID → list of changed file metas (deduplicated by path).
	// docHasLink tracks whether at least one of the matched files came from a
	// document_link (vs registry path match), used for confidence scoring.
	type docAffect struct {
		files      []models.ChangedFileMeta
		hasDocLink bool
	}
	affectedMap := map[string]*docAffect{}

	addFile := func(docID string, cf logicalChangedFile, fromDocLink bool) {
		da, ok := affectedMap[docID]
		if !ok {
			da = &docAffect{}
			affectedMap[docID] = da
		}
		// deduplicate by path
		for _, existing := range da.files {
			if existing.Path == cf.Path {
				if fromDocLink {
					da.hasDocLink = true
				}
				return
			}
		}
		da.files = append(da.files, models.ChangedFileMeta{
			Path:       cf.Path,
			ChangeType: cf.ChangeType,
		})
		if fromDocLink {
			da.hasDocLink = true
		}
	}

	for _, cf := range changedFiles {
		if isNoisyPath(cf.Path) {
			continue
		}

		linkedDocIDs, err := s.docLinkStore.FindDocumentsForFile(cf.Path)
		if err != nil {
			log.Printf("drift: error finding linked docs for %s: %v", cf.Path, err)
		} else {
			for _, docID := range linkedDocIDs {
				addFile(docID, cf, true)
			}
		}

		registryDocIDs, err := s.docStore.FindIDsByProjectAndFilePath(projectID, cf.Path)
		if err != nil {
			log.Printf("drift: error matching registry docs for %s: %v", cf.Path, err)
		} else {
			for _, docID := range registryDocIDs {
				addFile(docID, cf, false)
			}
		}
	}

	// ── Phase 2: generate code_change signals ─────────────────────────────

	newDriftCount := 0
	for docID, da := range affectedMap {
		// Always mark stale regardless of existing open signal.
		if err := s.docStore.MarkStale(docID); err != nil {
			log.Printf("drift: failed to mark doc %s stale: %v", docID, err)
		}

		// Skip if already has an open signal for this document.
		if openDocIDs[docID] {
			continue
		}

		severity, confidence := codeChangeSeverity(da.files, da.hasDocLink)

		// Human-readable summary (kept for backward compat with clients that
		// still display trigger_detail directly).
		var triggerDetail string
		paths := make([]string, len(da.files))
		for i, f := range da.files {
			paths[i] = fmt.Sprintf("%s (%s)", f.Path, f.ChangeType)
		}
		if len(paths) == 1 {
			triggerDetail = fmt.Sprintf("File changed: %s", paths[0])
		} else {
			triggerDetail = fmt.Sprintf("Files changed: %s", strings.Join(paths, ", "))
		}

		meta := &models.TriggerMeta{
			ChangedFiles: da.files,
			Confidence:   confidence,
		}

		_, err := s.driftStore.Create(projectID, models.CreateDriftSignalRequest{
			DocumentID:    docID,
			TriggerType:   "code_change",
			TriggerDetail: triggerDetail,
			TriggerMeta:   meta,
			Severity:      severity,
			SyncRunID:     syncRun.ID,
		})
		if err != nil {
			log.Printf("drift: failed to create signal for doc %s: %v", docID, err)
			continue
		}

		openDocIDs[docID] = true
		newDriftCount++
	}

	// ── Phase 3: time_decay signals ───────────────────────────────────────

	decayedDocIDs, err := s.docStore.FindTimeDecayedDocumentIDs(projectID, s.staleDaysThreshold)
	if err != nil {
		log.Printf("drift: failed to query time-decayed documents for project %s: %v", projectID, err)
		decayedDocIDs = []string{}
	}

	for _, docID := range decayedDocIDs {
		if err := s.docStore.MarkStale(docID); err != nil {
			log.Printf("drift: failed to mark time-decayed doc %s stale: %v", docID, err)
		}

		// Skip if already has an open signal (code_change takes priority over time_decay).
		if openDocIDs[docID] {
			continue
		}

		meta := &models.TriggerMeta{
			DaysStale:  s.staleDaysThreshold,
			Confidence: "medium",
		}

		_, err := s.driftStore.Create(projectID, models.CreateDriftSignalRequest{
			DocumentID:    docID,
			TriggerType:   "time_decay",
			TriggerDetail: fmt.Sprintf("Document stale for over %d days", s.staleDaysThreshold),
			TriggerMeta:   meta,
			Severity:      2, // medium — important but not as urgent as a direct code change
			SyncRunID:     syncRun.ID,
		})
		if err != nil {
			log.Printf("drift: failed to create time-decay signal for doc %s: %v", docID, err)
			continue
		}

		openDocIDs[docID] = true
		newDriftCount++
	}

	// Update project last_sync_at
	_ = s.projectStore.UpdateLastSyncAt(projectID)

	// Complete sync run
	if err := s.syncStore.Complete(syncRun.ID, commitCount, len(changedFiles)); err != nil {
		log.Printf("failed to complete sync run: %v", err)
	}

	log.Printf("sync[%s]: %d commits, %d files changed, %d new drift signals",
		projectID, commitCount, len(changedFiles), newDriftCount)

	return s.syncStore.GetByID(syncRun.ID)
}
