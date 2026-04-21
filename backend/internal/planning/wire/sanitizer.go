package wire

import (
	"regexp"
	"strings"
)

// Redaction placeholder.
const redactedPlaceholder = "[REDACTED]"

// Redaction patterns for sanitizer v1. These expressions match secret
// shapes anywhere in the input string. See docs/local-connector-context.md
// §7 for rationale. Bare hex sequences (commit SHAs, generic digests) are
// deliberately NOT in this set.
var sanitizerPatterns = []*regexp.Regexp{
	// OpenAI-style API keys.
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9]{20,}`),
	// AWS access key IDs.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// PEM private key headers.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	// Bearer tokens (min 16 chars — avoids matching "bearer token" prose).
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{16,}`),
	// Basic-auth credentials embedded in URLs.
	regexp.MustCompile(`https?://[^\s/@:]+:[^\s/@]+@`),
	// Labeled secrets: password=..., token:..., api_key=..., secret: ...
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key)\s*[:=]\s*\S+`),
	// Labeled SHA-256 hashes.
	regexp.MustCompile(`(?i)sha256:[A-Fa-f0-9]{32,}`),
	// Literal Authorization header dumps.
	regexp.MustCompile(`(?i)Authorization:\s*\S+`),
}

func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	out := s
	for _, pattern := range sanitizerPatterns {
		out = pattern.ReplaceAllString(out, redactedPlaceholder)
	}
	return out
}

// RedactSecrets exposes the v1 redaction pipeline so other packages (e.g.
// server-side prompt builders) can apply the same secret-shape filter
// without depending on the rest of the wire DTO machinery. The result is
// safe to forward to external services.
func RedactSecrets(s string) string {
	return redactSecrets(s)
}

// TruncateRunes exposes the rune-aware truncation helper used by the
// sanitizer so other packages can apply the same per-field caps when
// assembling free-form prompt fragments.
func TruncateRunes(s string, max int) string {
	return truncateRunes(s, max)
}

// truncateRunes truncates s to at most max runes. If truncation occurs, an
// ellipsis character is NOT appended — callers that need an ellipsis are
// responsible for it (matches compactAgentRunsForPrompt behavior).
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max]))
}

// SanitizePlanningContextV1 returns a deep copy of ctx with secret-shaped
// substrings redacted in free-form fields (AgentRun.Summary and
// SyncRun.ErrorMessage), and those fields truncated to Phase A caps.
// The input is never mutated.
func SanitizePlanningContextV1(ctx PlanningContextV1) PlanningContextV1 {
	out := ctx

	// Clone per-source slices and sanitize fields as needed.
	out.Sources.OpenTasks = append([]WireTask(nil), ctx.Sources.OpenTasks...)
	out.Sources.RecentDocuments = append([]WireDocument(nil), ctx.Sources.RecentDocuments...)
	out.Sources.OpenDriftSignals = append([]WireDriftSignal(nil), ctx.Sources.OpenDriftSignals...)

	if ctx.Sources.LatestSyncRun != nil {
		syncCopy := *ctx.Sources.LatestSyncRun
		syncCopy.ErrorMessage = truncateRunes(redactSecrets(syncCopy.ErrorMessage), MaxSyncRunErrorChars)
		if ctx.Sources.LatestSyncRun.CompletedAt != nil {
			completedCopy := *ctx.Sources.LatestSyncRun.CompletedAt
			syncCopy.CompletedAt = &completedCopy
		}
		out.Sources.LatestSyncRun = &syncCopy
	}

	out.Sources.RecentAgentRuns = make([]WireAgentRun, len(ctx.Sources.RecentAgentRuns))
	for i, run := range ctx.Sources.RecentAgentRuns {
		sanitized := run
		sanitized.Summary = truncateRunes(redactSecrets(run.Summary), MaxAgentRunSummaryChars)
		out.Sources.RecentAgentRuns[i] = sanitized
	}

	// Clone maps in meta to prevent aliasing the caller's state.
	if ctx.Meta.Ranking != nil {
		cloned := make(map[string]string, len(ctx.Meta.Ranking))
		for k, v := range ctx.Meta.Ranking {
			cloned[k] = v
		}
		out.Meta.Ranking = cloned
	}
	if ctx.Meta.DroppedCounts != nil {
		cloned := make(map[string]int, len(ctx.Meta.DroppedCounts))
		for k, v := range ctx.Meta.DroppedCounts {
			cloned[k] = v
		}
		out.Meta.DroppedCounts = cloned
	}
	if ctx.Meta.Warnings != nil {
		out.Meta.Warnings = append([]string(nil), ctx.Meta.Warnings...)
	} else {
		out.Meta.Warnings = []string{}
	}

	out.SanitizerVersion = SanitizerVersion
	return out
}
