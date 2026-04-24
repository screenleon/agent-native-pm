package prompts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// T-P5-A1-1: missing prompt surfaces an error.
func TestRender_UnknownPromptErrors(t *testing.T) {
	_, err := Render("this-does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

// T-P5-A1-2 + T-P5-A1-6: substitution works and JSON schema single-braces survive.
func TestRender_SubstitutionReplacesVarsButLeavesSingleBraces(t *testing.T) {
	out, err := Render("backlog", map[string]string{
		"PROJECT_NAME":             "ACME",
		"PROJECT_DESCRIPTION_LINE": "Description: The canonical test project.",
		"REQUIREMENT":              "Ship login flow.",
		"MAX_CANDIDATES":           "3",
		"CONTEXT":                  "(no context)",
		"SCHEMA_VERSION":           "v1",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "ACME") {
		t.Fatalf("expected PROJECT_NAME substitution; body = %q", out[:120])
	}
	if strings.Contains(out, "{{PROJECT_NAME}}") {
		t.Fatal("placeholder was not substituted")
	}
	// JSON schema in the prompt body uses single braces that must survive.
	if !strings.Contains(out, `"candidates":`) {
		t.Fatal("expected JSON schema example in rendered body")
	}
}

// T-P5-A1-3: unknown variable left as-is so the mismatch is visible at
// review time rather than silently producing an empty string in the
// rendered prompt.
func TestRender_UnknownVariableLeftAsIs(t *testing.T) {
	out, err := Render("backlog", map[string]string{
		"PROJECT_NAME":   "ACME",
		"MAX_CANDIDATES": "5",
		// Intentionally omit REQUIREMENT etc.
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "{{REQUIREMENT}}") {
		t.Fatal("expected unsubstituted {{REQUIREMENT}} to remain visible")
	}
}

// T-P5-A1-4: if a variable's VALUE contains {{OTHER}}, that MUST NOT be
// re-substituted. Single-pass safety. Prevents infinite loops + prompt
// injection via user-provided data.
func TestRender_ValuesAreNotReSubstituted(t *testing.T) {
	out, err := Render("backlog", map[string]string{
		"PROJECT_NAME":             "{{REQUIREMENT}}", // malicious value
		"PROJECT_DESCRIPTION_LINE": "",
		"REQUIREMENT":              "REAL-REQ",
		"MAX_CANDIDATES":           "1",
		"CONTEXT":                  "ctx",
		"SCHEMA_VERSION":           "v1",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// The first {{PROJECT_NAME}} in the prompt body is now the literal
	// string "{{REQUIREMENT}}" — NOT the value of REQUIREMENT.
	// And subsequent real {{REQUIREMENT}} placeholders ARE substituted.
	//
	// Confirm both invariants:
	if !strings.Contains(out, "{{REQUIREMENT}}") {
		t.Fatal("expected the literal {{REQUIREMENT}} injected via PROJECT_NAME to survive as-is")
	}
	if !strings.Contains(out, "REAL-REQ") {
		t.Fatal("expected real REQUIREMENT placeholders to be substituted")
	}
}

// T-P5-A1-5: frontmatter is stripped before the body is exposed to the model.
func TestRender_FrontmatterStripped(t *testing.T) {
	out, err := Render("backlog", map[string]string{
		"PROJECT_NAME":             "X",
		"PROJECT_DESCRIPTION_LINE": "",
		"REQUIREMENT":              "r",
		"MAX_CANDIDATES":           "1",
		"CONTEXT":                  "c",
		"SCHEMA_VERSION":           "v1",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.HasPrefix(out, "---") {
		t.Fatal("frontmatter delimiter leaked into rendered body")
	}
	if strings.Contains(out[:200], "title:") {
		t.Fatal("frontmatter metadata leaked into rendered body")
	}
}

func TestLoad_ReturnsFullSourceIncludingFrontmatter(t *testing.T) {
	raw, err := Load("backlog")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "---") {
		t.Fatal("Load should return the full source with frontmatter intact")
	}
}

func TestExists(t *testing.T) {
	if !Exists("backlog") {
		t.Fatal("backlog should exist")
	}
	if Exists("no-such-prompt") {
		t.Fatal("non-existent prompt should not exist")
	}
}

// TestRender_MatchesPythonParityGolden_Whatsnext mirrors the backlog
// golden test for the whatsnext prompt. Both prompts are active
// production surfaces; pinning only the backlog side leaves whatsnext
// drift undetected. See docs/phase5-plan.md §7 R1 / risk-reviewer S2.
func TestRender_MatchesPythonParityGolden_Whatsnext(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	goldenPath := filepath.Join(
		filepath.Dir(thisFile), "..", "..", "..",
		"adapters", "testdata", "whatsnext_render_golden.txt",
	)
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := Render("whatsnext", map[string]string{
		"PROJECT_NAME":             "CrossLang",
		"PROJECT_DESCRIPTION_LINE": "Description: Parity-check project.",
		"SCOPE_SECTION":            "\n=== Focus scope ===\nReliability push Q2\n",
		"MAX_CANDIDATES":           "3",
		"CONTEXT":                  "=== Current state ===\n(no items)",
		"SCHEMA_VERSION":           "context.v1",
		"REQUIREMENT":              "",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != string(expected) {
		t.Fatalf("Go-rendered whatsnext prompt drifted from the pinned golden.\n"+
			"if intentional, regenerate via UPDATE_PROMPT_GOLDEN=1 on the Python side.\n\n"+
			"=== Go (%d bytes) ===\n%s\n\n=== Golden (%d bytes) ===\n%s",
			len(got), got, len(expected), string(expected))
	}
}

// TestRender_MatchesPythonParityGolden enforces byte-identical output
// with the Python reference adapter. The companion test in
// adapters/test_prompt_loader.py renders the same fixture and asserts
// against the same golden. If either language drifts, the mismatching
// side fails. Regenerate with `UPDATE_PROMPT_GOLDEN=1 python3 -m
// unittest adapters.test_prompt_loader.PromptLoaderTests.
// test_python_render_matches_golden` — NEVER edit the golden by hand.
func TestRender_MatchesPythonParityGolden(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/prompts/render_test.go → repo-root/adapters/testdata/
	goldenPath := filepath.Join(
		filepath.Dir(thisFile), "..", "..", "..",
		"adapters", "testdata", "backlog_render_golden.txt",
	)
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := Render("backlog", map[string]string{
		"PROJECT_NAME":             "CrossLang",
		"PROJECT_DESCRIPTION_LINE": "Description: Parity-check project.",
		"REQUIREMENT":              "Requirement title: do X.\nSummary: do X better.",
		"MAX_CANDIDATES":           "4",
		"CONTEXT":                  "=== Context ===\n(no items)",
		"SCHEMA_VERSION":           "context.v1",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != string(expected) {
		t.Fatalf("Go-rendered backlog prompt drifted from the pinned golden.\n"+
			"if intentional, regenerate via UPDATE_PROMPT_GOLDEN=1 on the Python side.\n\n"+
			"=== Go (%d bytes) ===\n%s\n\n=== Golden (%d bytes) ===\n%s",
			len(got), got, len(expected), string(expected))
	}
}

// T-P5-B1-1: role prompts load via the nested "roles/<id>" name.
func TestRender_LoadsRolePrompts(t *testing.T) {
	roles, err := ListRoles()
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		t.Fatal("expected at least one role prompt")
	}
	for _, r := range roles {
		out, err := Render("roles/"+r, map[string]string{
			"TASK_TITLE":       "demo",
			"TASK_DESCRIPTION": "demo",
			"PROJECT_CONTEXT":  "demo",
			"REQUIREMENT":      "demo",
		})
		if err != nil {
			t.Fatalf("render role %s: %v", r, err)
		}
		if strings.HasPrefix(out, "---") {
			t.Fatalf("role %s frontmatter leaked", r)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("role %s rendered empty", r)
		}
	}
}
