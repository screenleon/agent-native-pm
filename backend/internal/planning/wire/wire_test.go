package wire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSanitizerRedactsKnownSecretPatterns(t *testing.T) {
	mustRedact := []struct {
		name  string
		input string
	}{
		{"openai key", "error: sk-abcdefghijklmnopqrstuvwxyz1234 is invalid"},
		{"aws key id", "cred AKIAIOSFODNN7EXAMPLE leaked"},
		{"pem header", "-----BEGIN RSA PRIVATE KEY-----\nMIIE..."},
		{"bearer token", "got Authorization: Bearer eyJhbGciOiJIUzI1NiJ9xyz"},
		{"basic auth url", "dial https://admin:hunter2@example.com/api"},
		{"password equals", "password=hunter2 still wrong"},
		{"token colon", "token: abc123-xyz"},
		{"api_key assign", "api_key=xyz-xyz-xyz"},
		{"sha256 label", "digest sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"},
		{"authorization header", "Authorization: Bearer abcdef123456"},
	}
	for _, tc := range mustRedact {
		t.Run("must_redact/"+tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if !strings.Contains(got, redactedPlaceholder) {
				t.Fatalf("expected redaction placeholder in output; got %q", got)
			}
		})
	}
}

func TestSanitizerPreservesNonSecretLookAlikes(t *testing.T) {
	mustNotRedact := []string{
		// 40-char git commit SHA in prose.
		"failed at commit 5e3a1b8c7d2e9f0a6b4c5d8e7f1a2b3c4d5e6f70 during cherry-pick",
		// 64-char unlabeled hex hash.
		"blob f3c4a1b2c3d4e5f60718293a4b5c6d7e8f9011121314151617181920212223242 rejected",
		// UUID.
		"run 3f2504e0-4f89-11d3-9a0c-0305e82c3301 finished",
		// English prose including "bearer token missing".
		"authentication failed: bearer token missing in request",
		// Numeric IDs.
		"resource 12345678901234567890 not found",
		// Markdown code block with short hex.
		"see `abcdef0123` for details",
	}
	for _, input := range mustNotRedact {
		t.Run(input, func(t *testing.T) {
			got := redactSecrets(input)
			if strings.Contains(got, redactedPlaceholder) {
				t.Fatalf("unexpected redaction of prose input %q -> %q", input, got)
			}
		})
	}
}

func TestSanitizerDeepCopyDoesNotMutateInput(t *testing.T) {
	now := time.Now().UTC()
	summary := "Authorization: Bearer secrettoken-longenough-123456"
	errMsg := "token=sk-abcdefghijklmnopqrstuvwxyz1234"
	input := PlanningContextV1{
		SchemaVersion: ContextSchemaV1,
		GeneratedAt:   now,
		Sources: PlanningContextSources{
			OpenTasks: []WireTask{{ID: "t1", Title: "task", Status: "open", Priority: "high", UpdatedAt: now}},
			RecentAgentRuns: []WireAgentRun{
				{ID: "a1", AgentName: "planner", ActionType: "plan", Status: "succeeded", StartedAt: now, Summary: summary},
			},
			LatestSyncRun: &WireSyncRun{ID: "s1", Status: "success", StartedAt: now, ErrorMessage: errMsg},
		},
		Meta: PlanningContextMeta{
			Ranking: map[string]string{"tasks": RankingTasks},
		},
	}

	out := SanitizePlanningContextV1(input)

	// Sanitized output must differ in redacted fields.
	if !strings.Contains(out.Sources.RecentAgentRuns[0].Summary, redactedPlaceholder) {
		t.Fatalf("expected sanitized agent run summary to contain redaction; got %q", out.Sources.RecentAgentRuns[0].Summary)
	}
	if !strings.Contains(out.Sources.LatestSyncRun.ErrorMessage, redactedPlaceholder) {
		t.Fatalf("expected sanitized sync run error to contain redaction; got %q", out.Sources.LatestSyncRun.ErrorMessage)
	}

	// Input must be unchanged.
	if input.Sources.RecentAgentRuns[0].Summary != summary {
		t.Fatalf("input agent run summary was mutated: %q", input.Sources.RecentAgentRuns[0].Summary)
	}
	if input.Sources.LatestSyncRun.ErrorMessage != errMsg {
		t.Fatalf("input sync run error was mutated: %q", input.Sources.LatestSyncRun.ErrorMessage)
	}

	// Mutating output must not mutate input.
	out.Sources.OpenTasks[0].Title = "mutated"
	if input.Sources.OpenTasks[0].Title == "mutated" {
		t.Fatalf("mutation of output task propagated to input")
	}
	out.Meta.Ranking["tasks"] = "overwritten"
	if input.Meta.Ranking["tasks"] == "overwritten" {
		t.Fatalf("mutation of output ranking map propagated to input")
	}
}

func TestSanitizerTruncatesFreeFormFieldsToCap(t *testing.T) {
	longSummary := strings.Repeat("a", MaxAgentRunSummaryChars+100)
	longError := strings.Repeat("b", MaxSyncRunErrorChars+100)
	input := PlanningContextV1{
		Sources: PlanningContextSources{
			RecentAgentRuns: []WireAgentRun{{Summary: longSummary}},
			LatestSyncRun:   &WireSyncRun{ErrorMessage: longError},
		},
	}
	out := SanitizePlanningContextV1(input)
	if got := len([]rune(out.Sources.RecentAgentRuns[0].Summary)); got > MaxAgentRunSummaryChars {
		t.Fatalf("summary not truncated: len=%d", got)
	}
	if got := len([]rune(out.Sources.LatestSyncRun.ErrorMessage)); got > MaxSyncRunErrorChars {
		t.Fatalf("error not truncated: len=%d", got)
	}
}

func TestReduceSourcesUnderCapNoDrops(t *testing.T) {
	now := time.Now().UTC()
	sources := PlanningContextSources{
		OpenTasks: []WireTask{{ID: "t1", Title: "short", Status: "open", Priority: "high", UpdatedAt: now}},
	}
	_, dropped, sourcesBytes := ReduceSources(sources, 64*1024)
	for key, count := range dropped {
		if count != 0 {
			t.Fatalf("expected no drops; got %d for %s", count, key)
		}
	}
	if sourcesBytes <= 0 {
		t.Fatalf("expected non-zero sources_bytes; got %d", sourcesBytes)
	}
}

func TestReduceSourcesOverCapDropsFromLargest(t *testing.T) {
	now := time.Now().UTC()
	tasks := make([]WireTask, 200)
	for i := range tasks {
		tasks[i] = WireTask{
			ID:        "task-" + strings.Repeat("x", 30),
			Title:     strings.Repeat("T", 120),
			Status:    "open",
			Priority:  "medium",
			UpdatedAt: now,
		}
	}
	docs := []WireDocument{{ID: "d1", Title: "doc", FilePath: "docs/a.md", DocType: "spec"}}
	sources := PlanningContextSources{OpenTasks: tasks, RecentDocuments: docs}

	cap := 4 * 1024
	reduced, dropped, sourcesBytes := ReduceSources(sources, cap)

	if sourcesBytes > cap {
		t.Fatalf("expected sources under cap; got %d > %d", sourcesBytes, cap)
	}
	if dropped["open_tasks"] == 0 {
		t.Fatalf("expected drops from open_tasks; got %v", dropped)
	}
	// Documents were smaller; ideally none dropped.
	if len(reduced.RecentDocuments) == 0 {
		t.Fatalf("expected at least one document preserved")
	}
}

func TestReduceSourcesAllEmptyFloor(t *testing.T) {
	sources := PlanningContextSources{
		OpenTasks: []WireTask{{Title: strings.Repeat("a", 5000)}},
	}
	_, dropped, sourcesBytes := ReduceSources(sources, 10)
	if dropped["open_tasks"] != 1 {
		t.Fatalf("expected 1 task drop; got %v", dropped)
	}
	if sourcesBytes > 100 {
		t.Fatalf("expected tiny empty-sources payload; got %d", sourcesBytes)
	}
}

func TestReduceSourcesDropsLatestSyncRun(t *testing.T) {
	huge := strings.Repeat("x", 8000)
	sources := PlanningContextSources{
		LatestSyncRun: &WireSyncRun{ID: "s", Status: "failed", ErrorMessage: huge},
	}
	reduced, dropped, _ := ReduceSources(sources, 1024)
	if reduced.LatestSyncRun != nil {
		t.Fatalf("expected latest_sync_run to be dropped")
	}
	if dropped["latest_sync_run"] != 1 {
		t.Fatalf("expected latest_sync_run drop count 1; got %d", dropped["latest_sync_run"])
	}
}

func TestContextV1JSONShapeIsStable(t *testing.T) {
	input := PlanningContextV1{
		SchemaVersion:    ContextSchemaV1,
		GeneratedAt:      time.Date(2026, 4, 20, 12, 34, 56, 0, time.UTC),
		GeneratedBy:      GeneratedByServer,
		SanitizerVersion: SanitizerVersion,
		Limits:           DefaultLimits(),
		Sources: PlanningContextSources{
			OpenTasks:        []WireTask{},
			RecentDocuments:  []WireDocument{},
			OpenDriftSignals: []WireDriftSignal{},
			RecentAgentRuns:  []WireAgentRun{},
		},
		Meta: PlanningContextMeta{
			Ranking:       DefaultRanking(),
			DroppedCounts: map[string]int{},
			SourcesBytes:  0,
			Warnings:      []string{"documents: store unavailable"},
		},
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	required := []string{
		`"schema_version":"context.v1"`,
		`"generated_by":"server"`,
		`"sanitizer_version":"v1"`,
		`"max_open_tasks":100`,
		`"max_recent_documents":8`,
		`"max_open_drift_signals":6`,
		`"max_recent_agent_runs":6`,
		`"include_latest_sync_run":true`,
		`"max_sources_bytes":262144`,
		`"documents":"relevance_v1"`,
		`"tasks":"updated_at_desc"`,
		`"warnings":["documents: store unavailable"]`,
	}
	for _, want := range required {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("marshaled payload missing %q; got %s", want, payload)
		}
	}
}

func TestContextV1IgnoresUnknownFieldsOnDecode(t *testing.T) {
	body := []byte(`{
		"schema_version": "context.v1",
		"generated_by": "server",
		"sanitizer_version": "v1",
		"sources": {"open_tasks": [], "recent_documents": [], "open_drift_signals": [], "recent_agent_runs": []},
		"limits": {"max_open_tasks": 10},
		"meta": {"ranking": {}, "dropped_counts": {}, "sources_bytes": 0},
		"unknown_future_field": {"nested": 42}
	}`)
	var ctx PlanningContextV1
	if err := json.Unmarshal(body, &ctx); err != nil {
		t.Fatalf("decode with unknown field should succeed; got %v", err)
	}
	if ctx.SchemaVersion != ContextSchemaV1 {
		t.Fatalf("schema_version lost during decode: %q", ctx.SchemaVersion)
	}
}
