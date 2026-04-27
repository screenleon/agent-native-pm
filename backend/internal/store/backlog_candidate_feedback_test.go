package store

// Phase 3B PR-3: tests for feedback_kind / feedback_note on backlog candidates.

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/audit"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// TestCandidateFeedback_ValidKindPersisted verifies that a valid feedback_kind
// (and optional feedback_note) round-trips through PATCH → GetByID.
func TestCandidateFeedback_ValidKindPersisted(t *testing.T) {
	cs, req, run := setupBacklogCandidateStore(t)

	candidates, err := cs.CreateDraftsForPlanningRun(req, run.ID, sampleCandidateDrafts(req))
	if err != nil {
		t.Fatalf("create drafts: %v", err)
	}
	c := candidates[0]

	// Approve first so we can attach an approved feedback kind.
	approved := "approved"
	if _, err := cs.Update(c.ID, models.UpdateBacklogCandidateRequest{Status: &approved}, audit.ActorInfo{}); err != nil {
		t.Fatalf("approve: %v", err)
	}

	kind := "good_fit"
	note := "fits the sprint goal nicely"
	updated, err := cs.Update(c.ID, models.UpdateBacklogCandidateRequest{
		FeedbackKind: &kind,
		FeedbackNote: &note,
	}, audit.ActorInfo{})
	if err != nil {
		t.Fatalf("patch feedback: %v", err)
	}
	if updated == nil {
		t.Fatal("expected updated candidate, got nil")
	}
	if updated.FeedbackKind != kind {
		t.Errorf("want feedback_kind %q, got %q", kind, updated.FeedbackKind)
	}
	if updated.FeedbackNote != note {
		t.Errorf("want feedback_note %q, got %q", note, updated.FeedbackNote)
	}

	// Confirm persistence via GetByID.
	fetched, err := cs.GetByID(c.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched.FeedbackKind != kind {
		t.Errorf("persisted feedback_kind: want %q, got %q", kind, fetched.FeedbackKind)
	}
}

// TestCandidateFeedback_EmptyKindAllowed confirms that patching with
// feedback_kind="" is accepted (feedback is entirely optional).
func TestCandidateFeedback_EmptyKindAllowed(t *testing.T) {
	cs, req, run := setupBacklogCandidateStore(t)

	candidates, err := cs.CreateDraftsForPlanningRun(req, run.ID, sampleCandidateDrafts(req))
	if err != nil {
		t.Fatalf("create drafts: %v", err)
	}
	c := candidates[0]

	empty := ""
	note := "a note without a kind"
	updated, err := cs.Update(c.ID, models.UpdateBacklogCandidateRequest{
		FeedbackKind: &empty,
		FeedbackNote: &note,
	}, audit.ActorInfo{})
	if err != nil {
		t.Fatalf("patch with empty kind: %v", err)
	}
	if updated.FeedbackKind != "" {
		t.Errorf("want empty feedback_kind, got %q", updated.FeedbackKind)
	}
}

// TestCandidateFeedback_InvalidKindReturnsError confirms that an unknown
// feedback_kind is rejected by the store layer with a typed error.
func TestCandidateFeedback_InvalidKindReturnsError(t *testing.T) {
	cs, req, run := setupBacklogCandidateStore(t)

	candidates, err := cs.CreateDraftsForPlanningRun(req, run.ID, sampleCandidateDrafts(req))
	if err != nil {
		t.Fatalf("create drafts: %v", err)
	}
	c := candidates[0]

	bad := "not_a_real_kind"
	_, err = cs.Update(c.ID, models.UpdateBacklogCandidateRequest{
		FeedbackKind: &bad,
	}, audit.ActorInfo{})
	if err == nil {
		t.Fatal("expected error for invalid feedback_kind, got nil")
	}
	if !IsInvalidFeedbackKindError(err) {
		t.Errorf("expected IsInvalidFeedbackKindError, got: %v", err)
	}
}

// TestQualitySummary_ComputedOnGetByID confirms that PlanningRunStore.GetByID
// populates quality_summary from its backlog_candidates.
func TestQualitySummary_ComputedOnGetByID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	requirementStore := NewRequirementStore(db)
	planningRunStore := NewPlanningRunStore(db, testutil.TestDialect())
	candidateStore := NewBacklogCandidateStore(db, testutil.TestDialect())

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "QS Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	req, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{
		Title: "QS requirement", Source: "human",
	})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	run, err := planningRunStore.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID:      "deterministic",
		ModelID:         "deterministic",
		SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	drafts := []models.BacklogCandidateDraft{
		{Title: "A", Rank: 1, PriorityScore: 80, Confidence: 80},
		{Title: "B", Rank: 2, PriorityScore: 70, Confidence: 70},
		{Title: "C", Rank: 3, PriorityScore: 60, Confidence: 60},
	}
	candidates, err := candidateStore.CreateDraftsForPlanningRun(req, run.ID, drafts)
	if err != nil {
		t.Fatalf("create drafts: %v", err)
	}

	// Approve candidate[0], reject candidate[1], leave candidate[2] pending.
	approved := "approved"
	if _, err := candidateStore.Update(candidates[0].ID, models.UpdateBacklogCandidateRequest{Status: &approved}, audit.ActorInfo{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	rejected := "rejected"
	if _, err := candidateStore.Update(candidates[1].ID, models.UpdateBacklogCandidateRequest{Status: &rejected}, audit.ActorInfo{}); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// Set a feedback_kind on the approved candidate.
	kind := "good_fit"
	if _, err := candidateStore.Update(candidates[0].ID, models.UpdateBacklogCandidateRequest{FeedbackKind: &kind}, audit.ActorInfo{}); err != nil {
		t.Fatalf("feedback: %v", err)
	}

	// GetByID should now return quality_summary.
	fetched, err := planningRunStore.GetByID(run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if fetched.QualitySummary == nil {
		t.Fatal("expected quality_summary to be populated")
	}
	qs := fetched.QualitySummary
	if qs.Total != 3 {
		t.Errorf("want total=3, got %d", qs.Total)
	}
	if qs.Approved != 1 {
		t.Errorf("want approved=1, got %d", qs.Approved)
	}
	if qs.Rejected != 1 {
		t.Errorf("want rejected=1, got %d", qs.Rejected)
	}
	if qs.Pending != 1 {
		t.Errorf("want pending=1, got %d", qs.Pending)
	}
	// acceptance rate = 1 / (1+1) = 0.5
	if qs.AcceptanceRate < 0.49 || qs.AcceptanceRate > 0.51 {
		t.Errorf("want acceptance_rate ~0.5, got %v", qs.AcceptanceRate)
	}
	if qs.FeedbackDistrib["good_fit"] != 1 {
		t.Errorf("want feedback_distribution[good_fit]=1, got %v", qs.FeedbackDistrib)
	}
}
