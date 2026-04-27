// Package scale provides a heuristic for estimating task complexity from
// free-form title and description text.
//
// The estimator is intentionally simple: it counts words and checks for
// complexity-indicator keywords. It is not a classifier; callers should treat
// the result as an advisory hint for context budget allocation, not as a
// guarantee of actual task effort.
package scale

import (
	"strings"
	"unicode"

	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

// largeKeywords triggers a "large" scale classification regardless of word
// count. These indicate multi-component, architectural, or restructuring work.
var largeKeywords = []string{
	"refactor",
	"migrate",
	"redesign",
	"overhaul",
	"architecture",
}

// smallKeywords lower the scale to "small" when matched AND word count is
// below the medium threshold. They indicate focused, well-scoped tasks.
var smallKeywords = []string{
	"add",
	"fix",
	"update",
	"rename",
	"tweak",
}

// Word count thresholds. These are applied to the combined title + description
// word count after splitting on whitespace.
const (
	smallThreshold  = 100 // < 100 words → candidate for small
	mediumThreshold = 300 // < 300 words → candidate for medium; ≥ 300 → large
)

// EstimateTaskScale returns a scale estimate based on title+description word
// count and the presence of complexity-indicator keywords.
//
// Classification rules (evaluated in order):
//  1. If any largeKeyword appears in the lowercased combined text → large.
//  2. Word count ≥ mediumThreshold → large.
//  3. Word count < smallThreshold AND any smallKeyword appears → small.
//  4. Word count < smallThreshold AND no smallKeyword → small (short text with
//     no complexity signals is still small).
//  5. Otherwise (smallThreshold ≤ count < mediumThreshold) → medium.
func EstimateTaskScale(title, description string) wire.TaskScale {
	combined := strings.ToLower(title + " " + description)

	// Rule 1: large keyword takes priority.
	for _, kw := range largeKeywords {
		if containsWord(combined, kw) {
			return wire.TaskScaleLarge
		}
	}

	// Rule 2: word count ceiling.
	count := wordCount(combined)
	if count >= mediumThreshold {
		return wire.TaskScaleLarge
	}

	// Rules 3–4: short text → small regardless of small keyword presence.
	if count < smallThreshold {
		return wire.TaskScaleSmall
	}

	// Rule 5: medium band (smallThreshold ≤ count < mediumThreshold).
	return wire.TaskScaleMedium
}

// wordCount counts whitespace-delimited tokens in s, skipping empty tokens.
func wordCount(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}

// containsWord reports whether word appears as a whole word (bounded by
// non-letter runes or string boundaries) in the lowercased text s. This
// avoids matching "refactoring" when searching for "refactor" would
// admittedly also match, but avoids matching "fix" inside "prefix".
//
// Implementation: simple substring scan with boundary check on both sides.
func containsWord(s, word string) bool {
	idx := 0
	for {
		pos := strings.Index(s[idx:], word)
		if pos < 0 {
			return false
		}
		abs := idx + pos
		// Check left boundary.
		leftOK := abs == 0 || !unicode.IsLetter(rune(s[abs-1]))
		// Check right boundary.
		end := abs + len(word)
		rightOK := end >= len(s) || !unicode.IsLetter(rune(s[end]))
		if leftOK && rightOK {
			return true
		}
		idx = abs + 1
		if idx >= len(s) {
			return false
		}
	}
}
