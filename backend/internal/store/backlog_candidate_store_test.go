package store

import (
	"errors"
	"sync"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupBacklogCandidateStore(t *testing.T) (*BacklogCandidateStore, *models.Requirement, *models.PlanningRun) {
	t.Helper()

	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	requirementStore := NewRequirementStore(db)
	planningRunStore := NewPlanningRunStore(db, testutil.TestDialect())
	candidateStore := NewBacklogCandidateStore(db, testutil.TestDialect())

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Candidate Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	requirement, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{
		Title:       "Improve sync failure recovery UX",
		Summary:     "Expose recovery options before creating tasks",
		Description: "Users should be able to see a saved draft candidate before deciding whether it deserves a task.",
	})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	run, err := planningRunStore.Create(project.ID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}

	return candidateStore, requirement, run
}

func sampleCandidateDrafts(requirement *models.Requirement) []models.BacklogCandidateDraft {
	return []models.BacklogCandidateDraft{
		{
			SuggestionType: "implementation",
			Title:          requirement.Title,
			Description:    "Deliver the core requirement as the first shippable slice.",
			Rationale:      "Primary recommendation for the requirement.",
			PriorityScore:  84.5,
			Confidence:     81.2,
			Rank:           1,
			Evidence:       []string{"Requirement summary: " + requirement.Summary},
			EvidenceDetail: models.PlanningEvidenceDetail{
				Summary: []string{"Requirement summary: " + requirement.Summary},
				Documents: []models.PlanningDocumentEvidence{{
					DocumentID:          "doc-1",
					Title:               "Recovery Guide",
					ContributionReasons: []string{"Grounded implementation recommendation."},
				}},
				ScoreBreakdown: models.PlanningScoreBreakdown{Impact: 92, FinalPriorityScore: 84.5, FinalConfidence: 81.2},
			},
			DuplicateTitles: []string{},
		},
		{
			SuggestionType: "validation",
			Title:          "Validate " + requirement.Title,
			Description:    "Protect review and apply behavior.",
			Rationale:      "Secondary recommendation for validation coverage.",
			PriorityScore:  66.1,
			Confidence:     73.4,
			Rank:           2,
			Evidence:       []string{"No exact-title overlap detected in current open tasks."},
			EvidenceDetail: models.PlanningEvidenceDetail{
				Summary:        []string{"No exact-title overlap detected in current open tasks."},
				ScoreBreakdown: models.PlanningScoreBreakdown{Impact: 61, FinalPriorityScore: 66.1, FinalConfidence: 73.4},
			},
			DuplicateTitles: []string{},
		},
	}
}

func TestBacklogCandidateStoreCreateDraftsForPlanningRun(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	candidates, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 draft candidates, got %d", len(candidates))
	}

	candidate := candidates[0]
	if candidate.ProjectID != requirement.ProjectID {
		t.Fatalf("expected project id %s, got %s", requirement.ProjectID, candidate.ProjectID)
	}
	if candidate.RequirementID != requirement.ID {
		t.Fatalf("expected requirement id %s, got %s", requirement.ID, candidate.RequirementID)
	}
	if candidate.PlanningRunID != run.ID {
		t.Fatalf("expected planning run id %s, got %s", run.ID, candidate.PlanningRunID)
	}
	if candidate.Status != models.BacklogCandidateStatusDraft {
		t.Fatalf("expected draft status, got %s", candidate.Status)
	}
	if candidate.Title != requirement.Title {
		t.Fatalf("expected candidate title %q, got %q", requirement.Title, candidate.Title)
	}
	if candidate.PriorityScore <= 0 {
		t.Fatalf("expected positive priority score, got %v", candidate.PriorityScore)
	}
	if candidate.Confidence <= 0 {
		t.Fatalf("expected positive confidence, got %v", candidate.Confidence)
	}
	if candidate.Rank != 1 {
		t.Fatalf("expected rank 1, got %d", candidate.Rank)
	}
	if len(candidate.Evidence) == 0 {
		t.Fatal("expected candidate evidence to be populated")
	}
	if len(candidate.EvidenceDetail.Documents) != 1 {
		t.Fatalf("expected evidence detail documents to persist, got %+v", candidate.EvidenceDetail.Documents)
	}

	stored, total, err := store.ListByPlanningRun(run.ID, 1, 20)
	if err != nil {
		t.Fatalf("list candidates by planning run: %v", err)
	}
	if total != 2 || len(stored) != 2 {
		t.Fatalf("expected 2 stored candidates, got total=%d len=%d", total, len(stored))
	}
	if stored[0].ID != candidate.ID {
		t.Fatalf("expected candidate id %s, got %s", candidate.ID, stored[0].ID)
	}
	if stored[0].EvidenceDetail.ScoreBreakdown.FinalPriorityScore <= 0 {
		t.Fatalf("expected score breakdown to persist, got %+v", stored[0].EvidenceDetail.ScoreBreakdown)
	}
}

func TestBacklogCandidateStoreDeleteByPlanningRun(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	if _, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement)); err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	if err := store.DeleteByPlanningRun(run.ID); err != nil {
		t.Fatalf("delete by planning run: %v", err)
	}

	candidates, total, err := store.ListByPlanningRun(run.ID, 1, 20)
	if err != nil {
		t.Fatalf("list candidates after delete: %v", err)
	}
	if total != 0 || len(candidates) != 0 {
		t.Fatalf("expected 0 candidates after delete, got total=%d len=%d", total, len(candidates))
	}
}

func TestBacklogCandidateStoreUpdate(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]

	title := "Improved recovery UX"
	description := "Persist draft review details before creating tasks."
	status := models.BacklogCandidateStatusApproved
	updated, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{
		Title:       &title,
		Description: &description,
		Status:      &status,
	})
	if err != nil {
		t.Fatalf("update candidate: %v", err)
	}
	if updated.Title != title {
		t.Fatalf("expected title %q, got %q", title, updated.Title)
	}
	if updated.Description != description {
		t.Fatalf("expected description %q, got %q", description, updated.Description)
	}
	if updated.Status != status {
		t.Fatalf("expected status %q, got %q", status, updated.Status)
	}
	if !updated.UpdatedAt.After(candidate.UpdatedAt) {
		t.Fatalf("expected updated_at to advance, got before=%v after=%v", candidate.UpdatedAt, updated.UpdatedAt)
	}

	persisted, err := store.GetByID(candidate.ID)
	if err != nil {
		t.Fatalf("get updated candidate: %v", err)
	}
	if persisted.Title != title || persisted.Description != description || persisted.Status != status {
		t.Fatalf("expected persisted values to match update, got %+v", persisted)
	}
}

func TestBacklogCandidateStoreUpdateRejectsAppliedCandidate(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]
	status := models.BacklogCandidateStatusApplied
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Status: &status}); !errors.Is(err, ErrBacklogCandidateBadStatus) {
		t.Fatalf("expected invalid status for applied transition, got %v", err)
	}

	_, err = store.db.Exec(`UPDATE backlog_candidates SET status = $1 WHERE id = $2`, models.BacklogCandidateStatusApplied, candidate.ID)
	if err != nil {
		t.Fatalf("force candidate to applied: %v", err)
	}
	newTitle := "Should fail"
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Title: &newTitle}); !errors.Is(err, ErrBacklogCandidateNotMutable) {
		t.Fatalf("expected ErrBacklogCandidateNotMutable, got %v", err)
	}
}

func TestBacklogCandidateStoreUpdateRejectsBlankTitleAndNoChanges(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]
	blankTitle := "   "
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Title: &blankTitle}); !errors.Is(err, ErrBacklogCandidateBlankTitle) {
		t.Fatalf("expected ErrBacklogCandidateBlankTitle, got %v", err)
	}
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{}); !errors.Is(err, ErrBacklogCandidateNoChanges) {
		t.Fatalf("expected ErrBacklogCandidateNoChanges, got %v", err)
	}
}

func TestBacklogCandidateStoreApplyToTask(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]
	approved := models.BacklogCandidateStatusApproved
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Status: &approved}); err != nil {
		t.Fatalf("approve candidate: %v", err)
	}

	result, err := store.ApplyToTask(candidate.ID)
	if err != nil {
		t.Fatalf("apply candidate to task: %v", err)
	}
	if result.AlreadyApplied {
		t.Fatal("expected first apply to create a task")
	}
	if result.Task.Title != candidate.Title {
		t.Fatalf("expected task title %q, got %q", candidate.Title, result.Task.Title)
	}
	if result.Task.Status != "todo" {
		t.Fatalf("expected task status todo, got %s", result.Task.Status)
	}
	if result.Task.Source != appliedCandidateTaskSource {
		t.Fatalf("expected task source %q, got %q", appliedCandidateTaskSource, result.Task.Source)
	}
	if result.Candidate.Status != models.BacklogCandidateStatusApplied {
		t.Fatalf("expected applied candidate status, got %s", result.Candidate.Status)
	}
	if result.Lineage.TaskID != result.Task.ID {
		t.Fatalf("expected lineage task id %s, got %s", result.Task.ID, result.Lineage.TaskID)
	}
	if result.Lineage.BacklogCandidateID != candidate.ID {
		t.Fatalf("expected lineage candidate id %s, got %s", candidate.ID, result.Lineage.BacklogCandidateID)
	}

	taskStore := NewTaskStore(store.db)
	tasks, total, err := taskStore.ListByProject(requirement.ProjectID, 1, 20, "created_at", "desc", models.TaskListFilters{})
	if err != nil {
		t.Fatalf("list tasks after apply: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("expected 1 created task, got total=%d len=%d", total, len(tasks))
	}

	replay, err := store.ApplyToTask(candidate.ID)
	if err != nil {
		t.Fatalf("replay apply candidate: %v", err)
	}
	if !replay.AlreadyApplied {
		t.Fatal("expected replay apply to report already_applied")
	}
	if replay.Task.ID != result.Task.ID {
		t.Fatalf("expected replay task id %s, got %s", result.Task.ID, replay.Task.ID)
	}
	if replay.Lineage.ID != result.Lineage.ID {
		t.Fatalf("expected replay lineage id %s, got %s", result.Lineage.ID, replay.Lineage.ID)
	}

	var lineageCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM task_lineage WHERE backlog_candidate_id = $1`, candidate.ID).Scan(&lineageCount); err != nil {
		t.Fatalf("count lineage rows: %v", err)
	}
	if lineageCount != 1 {
		t.Fatalf("expected 1 lineage row, got %d", lineageCount)
	}
}

func TestBacklogCandidateStoreApplyToTaskRejectsDraftAndDuplicate(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]

	if _, err := store.ApplyToTask(candidate.ID); !errors.Is(err, ErrBacklogCandidateNotApproved) {
		t.Fatalf("expected ErrBacklogCandidateNotApproved, got %v", err)
	}

	approved := models.BacklogCandidateStatusApproved
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Status: &approved}); err != nil {
		t.Fatalf("approve candidate: %v", err)
	}

	taskStore := NewTaskStore(store.db)
	if _, err := taskStore.Create(requirement.ProjectID, models.CreateTaskRequest{
		Title:    candidate.Title,
		Status:   "todo",
		Priority: "medium",
		Source:   "human",
	}); err != nil {
		t.Fatalf("seed duplicate task: %v", err)
	}

	var conflictErr *BacklogCandidateTaskConflictError
	if _, err := store.ApplyToTask(candidate.ID); !errors.As(err, &conflictErr) {
		t.Fatalf("expected BacklogCandidateTaskConflictError, got %v", err)
	}
	if conflictErr.Task == nil || conflictErr.Task.Title != candidate.Title {
		t.Fatalf("expected duplicate task title %q, got %+v", candidate.Title, conflictErr.Task)
	}
}

func TestBacklogCandidateStoreApplyToTaskIsIdempotentUnderConcurrency(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	candidate := created[0]
	approved := models.BacklogCandidateStatusApproved
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Status: &approved}); err != nil {
		t.Fatalf("approve candidate: %v", err)
	}

	const workers = 2
	results := make([]*models.ApplyBacklogCandidateResponse, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index], errs[index] = store.ApplyToTask(candidate.ID)
		}(i)
	}

	close(start)
	wg.Wait()

	createdCount := 0
	replayedCount := 0
	var taskID string
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent apply %d failed: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("concurrent apply %d returned nil result", i)
		}
		if taskID == "" {
			taskID = results[i].Task.ID
		} else if results[i].Task.ID != taskID {
			t.Fatalf("expected both apply results to reference task %s, got %s", taskID, results[i].Task.ID)
		}
		if results[i].AlreadyApplied {
			replayedCount++
		} else {
			createdCount++
		}
	}
	if createdCount != 1 || replayedCount != 1 {
		t.Fatalf("expected one create and one replay, got created=%d replayed=%d", createdCount, replayedCount)
	}

	var taskCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE project_id = $1`, requirement.ProjectID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks after concurrent apply: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected exactly 1 task after concurrent apply, got %d", taskCount)
	}

	var lineageCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM task_lineage WHERE backlog_candidate_id = $1`, candidate.ID).Scan(&lineageCount); err != nil {
		t.Fatalf("count lineage after concurrent apply: %v", err)
	}
	if lineageCount != 1 {
		t.Fatalf("expected exactly 1 lineage row after concurrent apply, got %d", lineageCount)
	}
}

// TestBacklogCandidateStoreListAppliedLineageByProject covers the new
// endpoint query: filtering to lineage_kind='applied_candidate', ordering
// by created_at DESC, and the LEFT JOIN + COALESCE fallback when joined
// requirement / run / candidate rows are deleted (SET NULL on FK).
func TestBacklogCandidateStoreListAppliedLineageByProject(t *testing.T) {
	store, requirement, run := setupBacklogCandidateStore(t)

	created, err := store.CreateDraftsForPlanningRun(requirement, run.ID, sampleCandidateDrafts(requirement))
	if err != nil {
		t.Fatalf("create draft candidates: %v", err)
	}
	approved := models.BacklogCandidateStatusApproved
	candidate := created[0]
	if _, err := store.Update(candidate.ID, models.UpdateBacklogCandidateRequest{Status: &approved}); err != nil {
		t.Fatalf("approve candidate: %v", err)
	}
	applyResult, err := store.ApplyToTask(candidate.ID)
	if err != nil {
		t.Fatalf("apply candidate: %v", err)
	}

	entries, err := store.ListAppliedLineageByProject(requirement.ProjectID)
	if err != nil {
		t.Fatalf("list applied lineage: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 applied lineage entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.LineageID != applyResult.Lineage.ID {
		t.Fatalf("expected lineage id %s, got %s", applyResult.Lineage.ID, entry.LineageID)
	}
	if entry.TaskTitle != applyResult.Task.Title {
		t.Fatalf("expected task title %q, got %q", applyResult.Task.Title, entry.TaskTitle)
	}
	if entry.RequirementID != requirement.ID || entry.RequirementTitle != requirement.Title {
		t.Fatalf("expected requirement %s/%q, got %s/%q", requirement.ID, requirement.Title, entry.RequirementID, entry.RequirementTitle)
	}
	if entry.PlanningRunID != run.ID || entry.PlanningRunStatus == "" {
		t.Fatalf("expected run %s with non-empty status, got %s/%q", run.ID, entry.PlanningRunID, entry.PlanningRunStatus)
	}
	if entry.BacklogCandidateID != candidate.ID || entry.BacklogCandidateTitle != applyResult.Candidate.Title {
		t.Fatalf("expected candidate %s/%q, got %s/%q", candidate.ID, applyResult.Candidate.Title, entry.BacklogCandidateID, entry.BacklogCandidateTitle)
	}
	if entry.LineageKind != models.TaskLineageKindAppliedCandidate {
		t.Fatalf("expected lineage_kind %q, got %q", models.TaskLineageKindAppliedCandidate, entry.LineageKind)
	}

	// Deleting the requirement should keep the lineage row visible with
	// empty requirement fields (FK is ON DELETE SET NULL).
	if _, err := store.db.Exec(`DELETE FROM requirements WHERE id = $1`, requirement.ID); err != nil {
		t.Fatalf("delete requirement: %v", err)
	}
	entries, err = store.ListAppliedLineageByProject(applyResult.Task.ProjectID)
	if err != nil {
		t.Fatalf("list applied lineage after requirement delete: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected lineage row to survive requirement delete, got %d entries", len(entries))
	}
	if entries[0].RequirementID != "" || entries[0].RequirementTitle != "" {
		t.Fatalf("expected empty requirement fallback, got %q/%q", entries[0].RequirementID, entries[0].RequirementTitle)
	}
	if entries[0].TaskTitle != applyResult.Task.Title {
		t.Fatalf("expected task title to still render, got %q", entries[0].TaskTitle)
	}

	// Manual-kind lineage rows (not applied_candidate) must be filtered out.
	manualLineageID := "manual-" + applyResult.Task.ID
	if _, err := store.db.Exec(
		`INSERT INTO task_lineage (id, project_id, task_id, lineage_kind, created_at) VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)`,
		manualLineageID, applyResult.Task.ProjectID, applyResult.Task.ID, models.TaskLineageKindManualRequirement,
	); err != nil {
		t.Fatalf("insert manual lineage row: %v", err)
	}
	entries, err = store.ListAppliedLineageByProject(applyResult.Task.ProjectID)
	if err != nil {
		t.Fatalf("list applied lineage after manual insert: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected manual lineage to be filtered out, got %d entries", len(entries))
	}
}
