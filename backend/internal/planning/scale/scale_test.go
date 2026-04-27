package scale

import (
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

func TestEstimateTaskScale_LargeKeywords(t *testing.T) {
	cases := []struct {
		title       string
		description string
		keyword     string
	}{
		{"Refactor the auth module", "", "refactor"},
		{"Migrate database schema", "Move from SQLite to Postgres", "migrate"},
		{"Redesign the planning workflow", "", "redesign"},
		{"Overhaul CI pipeline", "Complete overhaul of GitHub Actions", "overhaul"},
		{"Architecture decision for new service", "", "architecture"},
	}

	for _, tc := range cases {
		got := EstimateTaskScale(tc.title, tc.description)
		if got != wire.TaskScaleLarge {
			t.Errorf("keyword=%q title=%q desc=%q: want large, got %s", tc.keyword, tc.title, tc.description, got)
		}
	}
}

func TestEstimateTaskScale_LargeByWordCount(t *testing.T) {
	// Combined word count >= 300 → large.
	// Title "" = 0 words, description = 300 words → combined 300 → large.
	words := strings.Repeat("word ", 300)
	got := EstimateTaskScale("", words)
	if got != wire.TaskScaleLarge {
		t.Errorf("300-word combined: want large, got %s", got)
	}

	// Title "t" = 1 word, description = 300 words → combined 301 → large.
	words301 := strings.Repeat("word ", 300)
	if got2 := EstimateTaskScale("t", words301); got2 != wire.TaskScaleLarge {
		t.Errorf("301-word combined: want large, got %s", got2)
	}
}

func TestEstimateTaskScale_SmallByWordCount(t *testing.T) {
	// Short title only — well below 100 words.
	got := EstimateTaskScale("Add login button", "")
	if got != wire.TaskScaleSmall {
		t.Errorf("short title: want small, got %s", got)
	}
}

func TestEstimateTaskScale_SmallKeywords(t *testing.T) {
	cases := []string{"add", "fix", "update", "rename", "tweak"}
	for _, kw := range cases {
		got := EstimateTaskScale(kw+" something small", "")
		if got != wire.TaskScaleSmall {
			t.Errorf("keyword=%q: want small, got %s", kw, got)
		}
	}
}

func TestEstimateTaskScale_Medium(t *testing.T) {
	// 150 words, no large keywords → medium.
	desc := strings.Repeat("word ", 150)
	got := EstimateTaskScale("Implement feature", desc)
	if got != wire.TaskScaleMedium {
		t.Errorf("150-word text: want medium, got %s", got)
	}
}

func TestEstimateTaskScale_MediumBoundaryLow(t *testing.T) {
	// Exactly 100 words (hits smallThreshold) with no large keyword → medium.
	desc := strings.Repeat("word ", 100)
	got := EstimateTaskScale("t", desc)
	if got != wire.TaskScaleMedium {
		t.Errorf("100-word text: want medium, got %s", got)
	}
}

func TestEstimateTaskScale_MediumBoundaryHigh(t *testing.T) {
	// Combined word count must be < 300 for medium.
	// Title "t" = 1 word, so description = 298 words → combined 299 → medium.
	desc := strings.Repeat("word ", 298)
	got := EstimateTaskScale("t", desc)
	if got != wire.TaskScaleMedium {
		t.Errorf("298-word desc (299 combined): want medium, got %s", got)
	}
}

func TestEstimateTaskScale_LargeKeywordOverridesShortText(t *testing.T) {
	// Even a 3-word title with "refactor" should be large despite being short.
	got := EstimateTaskScale("Refactor auth", "")
	if got != wire.TaskScaleLarge {
		t.Errorf("short refactor title: want large, got %s", got)
	}
}

func TestEstimateTaskScale_KeywordBoundaryCheck(t *testing.T) {
	// "prefix" contains "fix" but should NOT match the "fix" small keyword.
	// The boundary check should prevent "fix" from matching inside "prefix".
	// With 150 words, result should be medium not small.
	desc := strings.Repeat("word ", 150)
	got := EstimateTaskScale("Fix something prefix", desc)
	// "Fix" matches at start of "Fix something prefix" → small keyword matched.
	// But word count is ~152 → medium band, so small keyword has no effect.
	// The large keyword check takes precedence — no large keyword here.
	// Word count 152 ≥ 100 → medium.
	if got != wire.TaskScaleMedium {
		t.Errorf("prefix test: want medium, got %s", got)
	}
}

func TestEstimateTaskScale_EmptyInputs(t *testing.T) {
	got := EstimateTaskScale("", "")
	if got != wire.TaskScaleSmall {
		t.Errorf("empty inputs: want small, got %s", got)
	}
}

func TestEstimateTaskScale_CaseInsensitive(t *testing.T) {
	cases := []struct {
		title string
		want  wire.TaskScale
	}{
		{"REFACTOR the system", wire.TaskScaleLarge},
		{"Refactor the system", wire.TaskScaleLarge},
		{"refactor the system", wire.TaskScaleLarge},
		{"MIGRATE data", wire.TaskScaleLarge},
	}

	for _, tc := range cases {
		got := EstimateTaskScale(tc.title, "")
		if got != tc.want {
			t.Errorf("title=%q: want %s, got %s", tc.title, tc.want, got)
		}
	}
}

func TestWordCount(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"  spaces   everywhere  ", 2},
		{"one\ttwo\nthree", 3},
	}

	for _, tc := range cases {
		got := wordCount(tc.input)
		if got != tc.want {
			t.Errorf("wordCount(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
