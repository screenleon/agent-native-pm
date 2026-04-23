package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

// ---------------------------------------------------------------------------
// extractJSONFromOutput tests
// ---------------------------------------------------------------------------

func TestExtractJSONFromOutput_FencedBlock(t *testing.T) {
	input := "Some preamble text\n```json\n{\"candidates\":[{\"title\":\"Do X\"}]}\n```\nSome epilogue"
	result, err := extractJSONFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["candidates"]; !ok {
		t.Fatal("expected 'candidates' key in result")
	}
}

func TestExtractJSONFromOutput_FencedBlockUppercase(t *testing.T) {
	// Regex is case-insensitive for the fence language tag.
	input := "```JSON\n{\"candidates\":[]}\n```"
	result, err := extractJSONFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["candidates"]; !ok {
		t.Fatal("expected 'candidates' key")
	}
}

func TestExtractJSONFromOutput_RawBraces(t *testing.T) {
	// Strategy 2: no fence, just surrounding prose.
	input := `Here is the output: {"candidates":[{"title":"Foo","rank":1}]} end of output`
	result, err := extractJSONFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["candidates"]; !ok {
		t.Fatal("expected 'candidates' key")
	}
}

func TestExtractJSONFromOutput_DirectJSON(t *testing.T) {
	// Strategy 3: entire text is valid JSON.
	input := `{"candidates":[{"title":"Bar","rank":2}]}`
	result, err := extractJSONFromOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["candidates"]; !ok {
		t.Fatal("expected 'candidates' key")
	}
}

func TestExtractJSONFromOutput_ANSICodes(t *testing.T) {
	// ANSI codes should be stripped before extraction is attempted by the
	// caller; but the function itself should still parse raw JSON that happens
	// to contain ANSI in surrounding prose (stripped by stripANSI before this
	// call). Test that after stripping we get valid JSON.
	raw := "\x1b[32msome output\x1b[0m\n```json\n{\"candidates\":[]}\n```"
	cleaned := stripANSI(raw)
	result, err := extractJSONFromOutput(cleaned)
	if err != nil {
		t.Fatalf("unexpected error after ANSI strip: %v", err)
	}
	if _, ok := result["candidates"]; !ok {
		t.Fatal("expected 'candidates' key")
	}
}

func TestExtractJSONFromOutput_NoJSON(t *testing.T) {
	_, err := extractJSONFromOutput("this is not json at all")
	if err == nil {
		t.Fatal("expected error for non-JSON input")
	}
}

// ---------------------------------------------------------------------------
// buildBuiltinPrompt tests
// ---------------------------------------------------------------------------

func TestBuildBuiltinPrompt_Backlog(t *testing.T) {
	input := ExecJSONInput{
		Run:                    &models.PlanningRun{AdapterType: "backlog"},
		Requirement:            &models.Requirement{Title: "Add login"},
		Project:                &models.Project{Name: "MyApp"},
		RequestedMaxCandidates: 3,
	}
	prompt := buildBuiltinPrompt(adapterTypeBacklog, input)
	if !strings.Contains(prompt, "backlog planner") {
		t.Errorf("backlog prompt should contain 'backlog planner'; got:\n%s", prompt[:min(200, len(prompt))])
	}
	if !strings.Contains(prompt, "MyApp") {
		t.Error("prompt should contain project name")
	}
	if !strings.Contains(prompt, "Add login") {
		t.Error("prompt should contain requirement title")
	}
}

func TestBuildBuiltinPrompt_Whatsnext(t *testing.T) {
	input := ExecJSONInput{
		Run:                    &models.PlanningRun{AdapterType: "whatsnext"},
		Requirement:            &models.Requirement{Title: "Improve reliability"},
		Project:                &models.Project{Name: "PlatformX"},
		RequestedMaxCandidates: 5,
	}
	prompt := buildBuiltinPrompt(adapterTypeWhatsnext, input)
	if !strings.Contains(prompt, "strategic product advisor") {
		t.Errorf("whatsnext prompt should contain 'strategic product advisor'; got prefix:\n%s", prompt[:min(300, len(prompt))])
	}
	if !strings.Contains(prompt, "PlatformX") {
		t.Error("prompt should contain project name")
	}
}

func TestBuildBuiltinPrompt_EmptyAdapterType_DefaultsToBacklog(t *testing.T) {
	input := ExecJSONInput{
		Run:     &models.PlanningRun{AdapterType: ""},
		Project: &models.Project{Name: "TestProj"},
	}
	prompt := buildBuiltinPrompt(builtinAdapterType(input.Run), input)
	if !strings.Contains(prompt, "backlog planner") {
		t.Error("empty adapter type should default to backlog prompt")
	}
}

func TestBuildBuiltinPrompt_UnknownAdapterType_DefaultsToBacklog(t *testing.T) {
	input := ExecJSONInput{
		Run:     &models.PlanningRun{AdapterType: "somethingunknown"},
		Project: &models.Project{Name: "TestProj"},
	}
	prompt := buildBuiltinPrompt(builtinAdapterType(input.Run), input)
	if !strings.Contains(prompt, "backlog planner") {
		t.Error("unknown adapter type should default to backlog prompt")
	}
}

func TestBuildBuiltinPrompt_NilPlanningContext_NoPanic(t *testing.T) {
	input := ExecJSONInput{
		Run:             &models.PlanningRun{AdapterType: "backlog"},
		Requirement:     &models.Requirement{Title: "Test"},
		Project:         &models.Project{Name: "Proj"},
		PlanningContext: nil,
	}
	// Must not panic.
	prompt := buildBuiltinPrompt(adapterTypeBacklog, input)
	if !strings.Contains(prompt, "no planning context provided") {
		t.Error("nil context should produce '(no planning context provided)'")
	}
}

func TestBuildBuiltinPrompt_NilRequirement_NoPanic(t *testing.T) {
	input := ExecJSONInput{
		Run:         &models.PlanningRun{},
		Requirement: nil,
		Project:     &models.Project{Name: "Proj"},
	}
	// Must not panic.
	_ = buildBuiltinPrompt(adapterTypeBacklog, input)
}

func TestBuildBuiltinPrompt_WithPlanningContext(t *testing.T) {
	ctx := &wire.PlanningContextV1{
		SchemaVersion: "context.v1",
		Sources: wire.PlanningContextSources{
			OpenTasks: []wire.WireTask{
				{ID: "t1", Title: "Fix auth", Status: "open", Priority: "high"},
			},
			RecentDocuments: []wire.WireDocument{
				{ID: "d1", Title: "API spec", FilePath: "docs/api.md", DocType: "spec", IsStale: true},
			},
		},
	}
	input := ExecJSONInput{
		Run:             &models.PlanningRun{},
		Project:         &models.Project{Name: "P"},
		PlanningContext: ctx,
	}
	prompt := buildBuiltinPrompt(adapterTypeBacklog, input)
	if !strings.Contains(prompt, "Fix auth") {
		t.Error("prompt should include task title")
	}
	if !strings.Contains(prompt, "API spec") {
		t.Error("prompt should include document title")
	}
	if !strings.Contains(prompt, "STALE") {
		t.Error("prompt should mark stale documents")
	}
}

// ---------------------------------------------------------------------------
// normalizeBuiltinCandidates tests
// ---------------------------------------------------------------------------

func TestNormalizeBuiltinCandidates_Basic(t *testing.T) {
	raw := map[string]json.RawMessage{
		"candidates": json.RawMessage(`[
			{"title":"Task A","description":"desc","rationale":"why","priority_score":0.9,"confidence":0.8,"rank":1,"evidence":["task:t1"]},
			{"title":"Task B","priority_score":1.5,"confidence":-0.2,"rank":2}
		]`),
	}
	candidates := normalizeBuiltinCandidates(raw, 5)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Title != "Task A" {
		t.Errorf("unexpected title: %q", candidates[0].Title)
	}
	// priority_score 1.5 should be clamped to 1.0
	if candidates[1].PriorityScore != 1.0 {
		t.Errorf("priority_score should be clamped to 1.0, got %f", candidates[1].PriorityScore)
	}
	// confidence -0.2 should be clamped to 0.0
	if candidates[1].Confidence != 0.0 {
		t.Errorf("confidence should be clamped to 0.0, got %f", candidates[1].Confidence)
	}
}

func TestNormalizeBuiltinCandidates_TruncatesTitle(t *testing.T) {
	longTitle := strings.Repeat("A", 130)
	raw := map[string]json.RawMessage{
		"candidates": json.RawMessage(fmt.Sprintf(`[{"title":%q,"rank":1}]`, longTitle)),
	}
	candidates := normalizeBuiltinCandidates(raw, 5)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate")
	}
	if len([]rune(candidates[0].Title)) > 120 {
		t.Errorf("title should be truncated to 120 runes, got %d", len([]rune(candidates[0].Title)))
	}
}

func TestNormalizeBuiltinCandidates_FillsMissingRank(t *testing.T) {
	raw := map[string]json.RawMessage{
		"candidates": json.RawMessage(`[{"title":"A"},{"title":"B"}]`),
	}
	candidates := normalizeBuiltinCandidates(raw, 5)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates")
	}
	if candidates[0].Rank != 1 {
		t.Errorf("first candidate rank should be 1, got %d", candidates[0].Rank)
	}
	if candidates[1].Rank != 2 {
		t.Errorf("second candidate rank should be 2, got %d", candidates[1].Rank)
	}
}

func TestNormalizeBuiltinCandidates_SkipsEmptyTitle(t *testing.T) {
	raw := map[string]json.RawMessage{
		"candidates": json.RawMessage(`[{"title":""},{"title":"Valid"}]`),
	}
	candidates := normalizeBuiltinCandidates(raw, 5)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (empty title skipped), got %d", len(candidates))
	}
	if candidates[0].Title != "Valid" {
		t.Errorf("unexpected title %q", candidates[0].Title)
	}
}

func TestNormalizeBuiltinCandidates_RespectsMaxCandidates(t *testing.T) {
	raw := map[string]json.RawMessage{
		"candidates": json.RawMessage(`[{"title":"A"},{"title":"B"},{"title":"C"},{"title":"D"}]`),
	}
	candidates := normalizeBuiltinCandidates(raw, 2)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (maxCandidates cap), got %d", len(candidates))
	}
}

// ---------------------------------------------------------------------------
// ExecuteBuiltin integration tests with stub CLI
// ---------------------------------------------------------------------------

// makeStubCLI creates a temporary executable Go-based stub binary that writes
// the given output to stdout and exits with the given code. Using a Go binary
// avoids POSIX shell quoting issues with special characters in the JSON output.
func makeStubCLI(t *testing.T, output string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub binaries require Unix fork/exec semantics for PTY tests")
	}
	// Write the JSON output to a temp file that the script will cat.
	dir := t.TempDir()
	outputFile := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(outputFile, []byte(output), 0o600); err != nil {
		t.Fatalf("write output file: %v", err)
	}
	scriptPath := filepath.Join(dir, "stub-cli")
	exitStmt := ""
	if exitCode != 0 {
		exitStmt = fmt.Sprintf("echo 'stub failure' >&2\nexit %d", exitCode)
	}
	script := fmt.Sprintf("#!/bin/sh\ncat %s\n%s\n", outputFile, exitStmt)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return scriptPath
}

func TestExecuteBuiltin_SuccessWithStubCLI(t *testing.T) {
	// Stub script echoes valid fenced JSON to stdout.
	jsonOut := "```json\n{\"candidates\":[{\"title\":\"Do the thing\",\"description\":\"desc\",\"rationale\":\"why\",\"priority_score\":0.8,\"confidence\":0.7,\"rank\":1}]}\n```"
	stubPath := makeStubCLI(t, jsonOut, 0)

	sel := &AdapterCliSelection{
		ProviderID: "cli:claude",
		ModelID:    "test-model",
		CliCommand: stubPath,
	}
	input := ExecJSONInput{
		Run:                    &models.PlanningRun{AdapterType: "backlog"},
		Requirement:            &models.Requirement{Title: "Add tests"},
		Project:                &models.Project{Name: "TestProject"},
		RequestedMaxCandidates: 3,
		CliSelection:           sel,
	}

	result := ExecuteBuiltin(context.Background(), input)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.ErrorMessage)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if result.Candidates[0].Title != "Do the thing" {
		t.Errorf("unexpected title: %q", result.Candidates[0].Title)
	}
	if result.CliInfo == nil {
		t.Fatal("expected CliInfo to be populated")
	}
	if result.CliInfo.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", result.CliInfo.Agent)
	}
}

func TestExecuteBuiltin_FailureWhenStubExitsNonZero(t *testing.T) {
	stubPath := makeStubCLI(t, "", 1)

	sel := &AdapterCliSelection{
		ProviderID: "cli:claude",
		CliCommand: stubPath,
	}
	input := ExecJSONInput{
		Run:          &models.PlanningRun{},
		Requirement:  &models.Requirement{Title: "Fail test"},
		Project:      &models.Project{Name: "P"},
		CliSelection: sel,
	}

	result := ExecuteBuiltin(context.Background(), input)
	if result.Success {
		t.Fatal("expected failure when stub exits non-zero")
	}
	if result.ErrorMessage == "" {
		t.Error("expected a non-empty error message")
	}
}

func TestExecuteBuiltin_NilCliSelectionFallsBackToPath(t *testing.T) {
	// When CliSelection is nil and neither claude nor codex is on PATH,
	// ExecuteBuiltin must return a failure with a diagnostic message.
	// We cannot inject a fake PATH easily in a unit test, so we just verify
	// the failure path is reached when nothing is available and PATH is empty.
	origPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	input := ExecJSONInput{
		Run:          &models.PlanningRun{},
		Requirement:  &models.Requirement{Title: "Test"},
		Project:      &models.Project{Name: "P"},
		CliSelection: nil,
	}

	result := ExecuteBuiltin(context.Background(), input)
	if result.Success {
		t.Fatal("expected failure when no CLI is on PATH")
	}
	if !strings.Contains(result.ErrorMessage, "neither claude nor codex") {
		t.Errorf("error message should mention PATH fallback, got: %q", result.ErrorMessage)
	}
}

func TestExecuteBuiltin_ModelOverrideApplied(t *testing.T) {
	// The stub outputs model-independent JSON; verify that model_override on
	// the run is forwarded into CliInfo.ModelSource == "override".
	jsonOut := "```json\n{\"candidates\":[{\"title\":\"T\",\"rank\":1}]}\n```"
	stubPath := makeStubCLI(t, jsonOut, 0)

	sel := &AdapterCliSelection{
		ProviderID: "cli:claude",
		CliCommand: stubPath,
	}
	input := ExecJSONInput{
		Run:          &models.PlanningRun{ModelOverride: "claude-opus-4"},
		Requirement:  &models.Requirement{Title: "T"},
		Project:      &models.Project{Name: "P"},
		CliSelection: sel,
	}

	result := ExecuteBuiltin(context.Background(), input)
	if !result.Success {
		t.Fatalf("expected success: %s", result.ErrorMessage)
	}
	if result.CliInfo == nil {
		t.Fatal("expected CliInfo")
	}
	if result.CliInfo.ModelSource != "override" {
		t.Errorf("expected ModelSource 'override', got %q", result.CliInfo.ModelSource)
	}
	if result.CliInfo.Model != "claude-opus-4" {
		t.Errorf("expected Model 'claude-opus-4', got %q", result.CliInfo.Model)
	}
}

// ---------------------------------------------------------------------------
// resolveBuiltinCLI unit tests
// ---------------------------------------------------------------------------

func TestResolveBuiltinCLI_NilSelectionNoCLI(t *testing.T) {
	// Empty PATH so neither claude nor codex is found.
	origPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	agent, binary, model, _, errMsg := resolveBuiltinCLI(nil, nil)
	if errMsg == "" {
		t.Errorf("expected error message, got agent=%q binary=%q model=%q", agent, binary, model)
	}
}

func TestResolveBuiltinCLI_SelectionWithCliCommand(t *testing.T) {
	sel := &AdapterCliSelection{
		ProviderID: "cli:claude",
		ModelID:    "claude-haiku",
		CliCommand: "/usr/local/bin/claude",
	}
	agent, binary, model, modelSource, errMsg := resolveBuiltinCLI(sel, nil)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", agent)
	}
	if binary != "/usr/local/bin/claude" {
		t.Errorf("expected binary '/usr/local/bin/claude', got %q", binary)
	}
	if model != "claude-haiku" {
		t.Errorf("expected model 'claude-haiku', got %q", model)
	}
	if modelSource != "stdin" {
		t.Errorf("expected modelSource 'stdin', got %q", modelSource)
	}
}

func TestResolveBuiltinCLI_ProviderIDWithoutCliPrefix(t *testing.T) {
	sel := &AdapterCliSelection{
		ProviderID: "codex",
		CliCommand: "/usr/bin/codex",
	}
	agent, _, _, _, errMsg := resolveBuiltinCLI(sel, nil)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if agent != "codex" {
		t.Errorf("expected agent 'codex', got %q", agent)
	}
}

func TestResolveBuiltinCLI_ModelOverrideTakesPrecedence(t *testing.T) {
	sel := &AdapterCliSelection{
		ProviderID: "cli:claude",
		ModelID:    "claude-haiku",
		CliCommand: "/fake/claude",
	}
	run := &models.PlanningRun{ModelOverride: "claude-opus-4"}
	_, _, model, modelSource, errMsg := resolveBuiltinCLI(sel, run)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if model != "claude-opus-4" {
		t.Errorf("model override should win, got %q", model)
	}
	if modelSource != "override" {
		t.Errorf("expected modelSource 'override', got %q", modelSource)
	}
}

// ---------------------------------------------------------------------------
// stripANSI unit test
// ---------------------------------------------------------------------------

func TestStripANSI(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"\x1b[32mgreen text\x1b[0m", "green text"},
		{"\x1b[1;31mbold red\x1b[0m and normal", "bold red and normal"},
		{"no ansi here", "no ansi here"},
		{"\x1b[?25l\x1b[2J\x1b[H", ""}, // cursor and clear codes
	}
	for _, tc := range cases {
		got := stripANSI(tc.input)
		if got != tc.expected {
			t.Errorf("stripANSI(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
