// Package connector — Phase 6c PR-3: LLM-based role suggestion.
//
// SuggestRole runs the dispatcher meta-prompt on the server side (single-machine
// assumption, documented in docs/phase6c-plan.md §2.2 PR-3). It never persists
// the result to actor_audit; the caller (SuggestRole handler) returns the
// suggestion to the frontend so the operator can confirm or override before any
// actor_audit row is written.
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/prompts"
	"github.com/screenleon/agent-native-pm/internal/roles"
)

// SuggestRoleResult is the parsed output of the dispatcher meta-prompt.
// On success, RoleID is non-empty and ErrorKind is "".
// On failure, RoleID is empty and ErrorKind + ErrorMessage describe the failure.
type SuggestRoleResult struct {
	RoleID       string                   `json:"role_id"`
	Confidence   float64                  `json:"confidence"`
	Reasoning    string                   `json:"reasoning"`
	Alternatives []SuggestRoleAlternative `json:"alternatives"`
	// Error fields — non-empty on failure.
	ErrorKind    string `json:"error_kind,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// SuggestRoleAlternative is a secondary role suggestion from the dispatcher.
type SuggestRoleAlternative struct {
	RoleID string  `json:"role_id"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score"`
}

// rawDispatcherResult is the shape the dispatcher prompt emits.
type rawDispatcherResult struct {
	RoleID       string                   `json:"role_id"`
	Confidence   float64                  `json:"confidence"`
	Reasoning    string                   `json:"reasoning"`
	Alternatives []SuggestRoleAlternative `json:"alternatives"`
}

// SuggestRole runs the dispatcher meta-prompt against the given task information
// and returns a role suggestion. It does NOT persist the result — the operator
// must confirm before actor_audit is written (Phase 6c PR-3 suggest-only
// constraint; auto-apply is deferred to Phase 6d).
//
// CLI resolution follows the same PATH-first strategy as ExecuteBuiltin: the
// call runs server-side against the operator's local CLI (claude or codex on
// PATH, or overridden via ANPM_ADAPTER_AGENT / ANPM_ADAPTER_MODEL env vars).
// cliSel may be nil when the caller has no per-run binding.
func SuggestRole(ctx context.Context, taskTitle, taskDescription, requirement, projectContext string, cliSel *AdapterCliSelection) SuggestRoleResult {
	agent, binary, model, _, resolveErr := resolveBuiltinCLI(cliSel, nil)
	if resolveErr != "" {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindCliNotFound,
			ErrorMessage: resolveErr,
		}
	}

	// Build the role catalog list injected into the prompt.
	all := roles.All()
	lines := make([]string, 0, len(all))
	for _, r := range all {
		if r.Category == roles.CategoryRole {
			lines = append(lines, fmt.Sprintf("- %s: %s", r.ID, r.UseCase))
		}
	}

	rendered, renderErr := prompts.Render("meta/dispatcher", map[string]string{
		"TASK_TITLE":       strings.TrimSpace(taskTitle),
		"TASK_DESCRIPTION": strings.TrimSpace(taskDescription),
		"REQUIREMENT":      strings.TrimSpace(requirement),
		"PROJECT_CONTEXT":  strings.TrimSpace(projectContext),
		"ROLE_CATALOG":     strings.Join(lines, "\n"),
	})
	if renderErr != nil {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindAdapterProtocol,
			ErrorMessage: "prompt render: " + renderErr.Error(),
		}
	}

	// Timeout: use catalog value for the dispatcher role. TimeoutFor returns 0
	// when ANPM_DISPATCH_TIMEOUT=0 (disabled); invokeBuiltinCLI treats 0 as
	// "no timeout" so we forward it unchanged.
	d := roles.TimeoutFor("dispatcher")
	timeoutSec := int(d.Seconds())

	output, truncated, runErr := invokeBuiltinCLI(ctx, agent, binary, model, rendered, timeoutSec)
	if runErr != "" {
		return SuggestRoleResult{
			ErrorKind:    classifyDispatchRunError(runErr),
			ErrorMessage: runErr,
		}
	}
	if truncated {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindOutputTooLarge,
			ErrorMessage: "dispatcher output exceeded cap",
		}
	}

	output = stripANSI(output)
	parsed, parseErr := extractJSONFromOutput(output)
	if parseErr != nil {
		snippet := strings.TrimSpace(output)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindInvalidResultSchema,
			ErrorMessage: fmt.Sprintf("cannot parse dispatcher output: %v; first 200 chars: %s", parseErr, snippet),
		}
	}

	// Re-marshal the parsed map into the typed result.
	b, _ := json.Marshal(parsed)
	var raw rawDispatcherResult
	if err := json.Unmarshal(b, &raw); err != nil {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindInvalidResultSchema,
			ErrorMessage: "malformed dispatcher result: " + err.Error(),
		}
	}

	// Empty role_id = dispatcher could not classify.
	if raw.RoleID == "" {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindRouterNoMatch,
			ErrorMessage: "dispatcher could not match task to any known role",
			Reasoning:    raw.Reasoning,
		}
	}

	// role_id must be in the task-execution catalog (not meta).
	if !roles.IsKnown(raw.RoleID) {
		return SuggestRoleResult{
			ErrorKind:    models.ErrorKindRouterNoMatch,
			ErrorMessage: fmt.Sprintf("dispatcher returned unknown role_id %q", raw.RoleID),
			Reasoning:    raw.Reasoning,
		}
	}

	// Clamp scores to [0, 1].
	confidence := clampFloat(raw.Confidence, 0, 1)
	alts := make([]SuggestRoleAlternative, 0, len(raw.Alternatives))
	for _, a := range raw.Alternatives {
		if a.RoleID == "" {
			continue
		}
		alts = append(alts, SuggestRoleAlternative{
			RoleID: a.RoleID,
			Reason: a.Reason,
			Score:  clampFloat(a.Score, 0, 1),
		})
	}

	return SuggestRoleResult{
		RoleID:       raw.RoleID,
		Confidence:   confidence,
		Reasoning:    raw.Reasoning,
		Alternatives: alts,
	}
}
