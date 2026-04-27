package wire

import (
	"testing"
	"time"
)

func TestContextV2Constants(t *testing.T) {
	if ContextSchemaV2 != "context.v2" {
		t.Fatalf("ContextSchemaV2 = %q, want %q", ContextSchemaV2, "context.v2")
	}
	if IntentModeAnalyze != "analyze" {
		t.Fatalf("IntentModeAnalyze = %q, want %q", IntentModeAnalyze, "analyze")
	}
	if IntentModeImplement != "implement" {
		t.Fatalf("IntentModeImplement = %q, want %q", IntentModeImplement, "implement")
	}
	if IntentModeReview != "review" {
		t.Fatalf("IntentModeReview = %q, want %q", IntentModeReview, "review")
	}
	if IntentModeDocument != "document" {
		t.Fatalf("IntentModeDocument = %q, want %q", IntentModeDocument, "document")
	}
	if TaskScaleSmall != "small" {
		t.Fatalf("TaskScaleSmall = %q, want %q", TaskScaleSmall, "small")
	}
	if TaskScaleMedium != "medium" {
		t.Fatalf("TaskScaleMedium = %q, want %q", TaskScaleMedium, "medium")
	}
	if TaskScaleLarge != "large" {
		t.Fatalf("TaskScaleLarge = %q, want %q", TaskScaleLarge, "large")
	}
}

func TestUpgradeV1ToV2_CopiesV1Fields(t *testing.T) {
	now := time.Now().UTC()
	v1 := PlanningContextV1{
		SchemaVersion:    ContextSchemaV1,
		GeneratedAt:      now,
		GeneratedBy:      GeneratedByServer,
		SanitizerVersion: SanitizerVersion,
		Limits:           DefaultLimits(),
		Sources: PlanningContextSources{
			OpenTasks:        []WireTask{{ID: "t1", Title: "task one", Status: "todo", Priority: "medium", UpdatedAt: now}},
			RecentDocuments:  []WireDocument{},
			OpenDriftSignals: []WireDriftSignal{},
			RecentAgentRuns:  []WireAgentRun{},
		},
		Meta: PlanningContextMeta{
			Ranking:       DefaultRanking(),
			DroppedCounts: map[string]int{},
			SourcesBytes:  512,
			Warnings:      []string{},
		},
	}

	packID := "aaaabbbb-cccc-dddd-eeee-ffffaaaabbbb"
	role := "backend-architect"
	sot := []SourceRef{{Name: "DECISIONS.md", Path: "DECISIONS.md", Role: "safety-rules"}}

	v2 := UpgradeV1ToV2(v1, packID, role, IntentModeImplement, TaskScaleMedium, sot)

	if v2.SchemaVersion != ContextSchemaV2 {
		t.Errorf("SchemaVersion = %q, want %q", v2.SchemaVersion, ContextSchemaV2)
	}
	if v2.PackID != packID {
		t.Errorf("PackID = %q, want %q", v2.PackID, packID)
	}
	if v2.Role != role {
		t.Errorf("Role = %q, want %q", v2.Role, role)
	}
	if v2.IntentMode != IntentModeImplement {
		t.Errorf("IntentMode = %q, want %q", v2.IntentMode, IntentModeImplement)
	}
	if v2.TaskScale != TaskScaleMedium {
		t.Errorf("TaskScale = %q, want %q", v2.TaskScale, TaskScaleMedium)
	}
	if !v2.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt not preserved")
	}
	if v2.GeneratedBy != GeneratedByServer {
		t.Errorf("GeneratedBy = %q, want %q", v2.GeneratedBy, GeneratedByServer)
	}
	if v2.SanitizerVersion != SanitizerVersion {
		t.Errorf("SanitizerVersion = %q, want %q", v2.SanitizerVersion, SanitizerVersion)
	}
	if v2.Limits.MaxOpenTasks != DefaultMaxOpenTasks {
		t.Errorf("Limits not preserved")
	}
	if v2.Meta.SourcesBytes != 512 {
		t.Errorf("Meta.SourcesBytes = %d, want 512", v2.Meta.SourcesBytes)
	}
	if len(v2.Sources.OpenTasks) != 1 || v2.Sources.OpenTasks[0].ID != "t1" {
		t.Errorf("Sources.OpenTasks not preserved")
	}
	if len(v2.SourceOfTruth) != 1 || v2.SourceOfTruth[0].Name != "DECISIONS.md" {
		t.Errorf("SourceOfTruth not set correctly")
	}
}

func TestUpgradeV1ToV2_NilSourceOfTruthBecomesEmptySlice(t *testing.T) {
	v1 := PlanningContextV1{
		SchemaVersion:    ContextSchemaV1,
		GeneratedAt:      time.Now().UTC(),
		GeneratedBy:      GeneratedByServer,
		SanitizerVersion: SanitizerVersion,
		Limits:           DefaultLimits(),
		Meta: PlanningContextMeta{
			Ranking:       DefaultRanking(),
			DroppedCounts: map[string]int{},
			Warnings:      []string{},
		},
	}

	v2 := UpgradeV1ToV2(v1, "pack-1", "ui-scaffolder", IntentModeAnalyze, TaskScaleSmall, nil)

	if v2.SourceOfTruth == nil {
		t.Error("SourceOfTruth should be non-nil empty slice, got nil")
	}
	if len(v2.SourceOfTruth) != 0 {
		t.Errorf("SourceOfTruth len = %d, want 0", len(v2.SourceOfTruth))
	}
}

func TestUpgradeV1ToV2_SchemaVersionOverwritten(t *testing.T) {
	// Even if v1.SchemaVersion is somehow wrong, v2 must carry ContextSchemaV2.
	v1 := PlanningContextV1{
		SchemaVersion: "context.v1",
		GeneratedAt:   time.Now().UTC(),
		GeneratedBy:   GeneratedByServer,
		Meta: PlanningContextMeta{
			Ranking:       DefaultRanking(),
			DroppedCounts: map[string]int{},
			Warnings:      []string{},
		},
	}

	v2 := UpgradeV1ToV2(v1, "pack-2", "", IntentModeReview, TaskScaleLarge, nil)

	if v2.SchemaVersion != ContextSchemaV2 {
		t.Errorf("expected SchemaVersion=%q, got %q", ContextSchemaV2, v2.SchemaVersion)
	}
}
