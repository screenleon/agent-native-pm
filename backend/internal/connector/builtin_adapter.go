package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
	"github.com/screenleon/agent-native-pm/internal/prompts"
)

const (
	builtinDefaultTimeoutSec    = 300 // 5 min, matches DEFAULT_TIMEOUT_SEC in reference adapters
	builtinDefaultMaxCandidates = 3
	builtinDefaultClaudeModel   = "claude-sonnet-4-6"

	adapterTypeBacklog   = "backlog"
	adapterTypeWhatsnext = "whatsnext"
)

// ansiRE strips ANSI escape codes from PTY output (same pattern as Python adapters).
var ansiRE = regexp.MustCompile("\x1b(?:[@-Z\\\\-_]|\\[[0-?]*[ -/]*[@-~])")

// jsonBlockRE matches a fenced ```json ... ``` block (strategy 1 of _extract_json).
var jsonBlockRE = regexp.MustCompile("(?is)```(?:json)?\\s*\n(.*?)```")

// ExecuteBuiltin runs the built-in adapter logic directly in Go, bypassing the
// exec-json subprocess path. It is called when State.Adapter.Command == "".
// It respects the same CliSelection precedence as the Python reference adapter
// (D4): cli_selection > PATH lookup of claude, then codex.
func ExecuteBuiltin(ctx context.Context, input ExecJSONInput) models.LocalConnectorSubmitRunResultRequest {
	// Resolve the CLI to use.
	agent, binary, model, modelSource, resolveErr := resolveBuiltinCLI(input.CliSelection, input.Run)
	if resolveErr != "" {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: resolveErr}
	}

	// Pick adapter type.
	adapterType := builtinAdapterType(input.Run)

	// Build prompt.
	prompt := buildBuiltinPrompt(adapterType, input)

	// Respect ANPM_ADAPTER_TIMEOUT env var (same as Python reference adapters).
	timeoutSec := builtinDefaultTimeoutSec
	if v := strings.TrimSpace(os.Getenv("ANPM_ADAPTER_TIMEOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	}

	// Run CLI.
	output, runErr := invokeBuiltinCLI(ctx, agent, binary, model, prompt, timeoutSec)
	if runErr != "" {
		return models.LocalConnectorSubmitRunResultRequest{
			Success:      false,
			ErrorMessage: runErr,
			CliInfo:      &models.CliUsageInfo{Agent: agent, Model: model, ModelSource: modelSource},
		}
	}

	// Strip ANSI (Codex PTY output may contain escape codes even after drain).
	output = stripANSI(output)

	// Extract JSON from model output.
	parsed, extractErr := extractJSONFromOutput(output)
	if extractErr != nil {
		snippet := strings.TrimSpace(output)
		if len(snippet) > 240 {
			snippet = snippet[:240]
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		return models.LocalConnectorSubmitRunResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("could not parse agent output as JSON: %v; first 240 chars: %s", extractErr, snippet),
			CliInfo:      &models.CliUsageInfo{Agent: agent, Model: model, ModelSource: modelSource},
		}
	}

	// Normalize candidates.
	maxCandidates := input.RequestedMaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = builtinDefaultMaxCandidates
	}
	candidates := normalizeBuiltinCandidates(parsed, maxCandidates)
	if len(candidates) == 0 {
		label := "backlog candidates"
		if adapterType == adapterTypeWhatsnext {
			label = "whatsnext directions"
		}
		return models.LocalConnectorSubmitRunResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("agent returned no valid %s", label),
			CliInfo:      &models.CliUsageInfo{Agent: agent, Model: model, ModelSource: modelSource},
		}
	}

	return models.LocalConnectorSubmitRunResultRequest{
		Success:    true,
		Candidates: candidates,
		CliInfo:    &models.CliUsageInfo{Agent: agent, Model: model, ModelSource: modelSource},
	}
}

// resolveBuiltinCLI returns (agent, binary, model, modelSource, errorMessage).
// Mirrors D4 precedence (same as Python reference adapter _resolve_cli_selection):
//
//	Agent:  cli_selection.provider_id > ANPM_ADAPTER_AGENT > binary-filename inference > PATH lookup
//	Model:  run.ModelOverride > cli_selection.model_id > ANPM_ADAPTER_MODEL > built-in default
//
// modelSource values: "override" | "stdin" | "env" | "default" | "subscription"
func resolveBuiltinCLI(sel *AdapterCliSelection, run *models.PlanningRun) (agent, binary, model, modelSource, errMsg string) {
	// Per-run model override takes highest precedence.
	modelOverride := ""
	if run != nil {
		modelOverride = strings.TrimSpace(run.ModelOverride)
	}

	// Read env-var fallbacks (same names as Python adapters).
	envAgent := strings.TrimSpace(strings.ToLower(os.Getenv("ANPM_ADAPTER_AGENT")))
	envModel := strings.TrimSpace(os.Getenv("ANPM_ADAPTER_MODEL"))

	if sel != nil {
		// Derive agent from provider_id ("cli:claude" → "claude", "cli:codex" → "codex").
		providerID := strings.TrimSpace(sel.ProviderID)
		if strings.HasPrefix(providerID, "cli:") {
			agent = strings.ToLower(strings.TrimSpace(providerID[4:]))
		} else if providerID != "" {
			agent = strings.ToLower(strings.TrimSpace(providerID))
		}

		// Resolve binary: cli_command from selection wins.
		binary = strings.TrimSpace(sel.CliCommand)
		if binary == "" && agent != "" {
			binary, _ = exec.LookPath(agent)
		}

		// Resolve model: run override > selection > env > (default applied below).
		if modelOverride != "" {
			model = modelOverride
			modelSource = "override"
		} else if strings.TrimSpace(sel.ModelID) != "" {
			model = strings.TrimSpace(sel.ModelID)
			modelSource = "stdin"
		} else if envModel != "" {
			model = envModel
			modelSource = "env"
		}
	} else if modelOverride != "" {
		model = modelOverride
		modelSource = "override"
	} else if envModel != "" {
		model = envModel
		modelSource = "env"
	}

	// Resolve agent when still empty: infer from binary filename first (handles
	// the case where sel.CliCommand is set but sel.ProviderID is empty), then
	// env var, then PATH lookup.
	if agent == "" {
		if binary != "" {
			base := strings.ToLower(filepath.Base(binary))
			switch {
			case strings.HasPrefix(base, "claude"):
				agent = "claude"
			case strings.HasPrefix(base, "codex"):
				agent = "codex"
			default:
				agent = "claude" // safest fallback for unknown binaries
			}
		} else if envAgent != "" {
			agent = envAgent
		} else if p, err := exec.LookPath("claude"); err == nil {
			agent = "claude"
			binary = p
		} else if p, err := exec.LookPath("codex"); err == nil {
			agent = "codex"
			binary = p
		} else {
			errMsg = "no CLI found: neither claude nor codex is on PATH (install Claude Code or Codex CLI)"
			return
		}
	}

	// Ensure binary is resolved when we got agent from env var or filename.
	if binary == "" {
		p, err := exec.LookPath(agent)
		if err != nil {
			errMsg = fmt.Sprintf("%s CLI not found on PATH (install %s)", agent, agent)
			return
		}
		binary = p
	}

	// Apply model defaults when still unset.
	if model == "" {
		if agent == "claude" {
			model = builtinDefaultClaudeModel
			modelSource = "default"
		} else {
			// Codex: no fixed default; pass no --model flag and let the subscription decide.
			modelSource = "subscription"
		}
	}

	return
}

// builtinAdapterType normalises the adapter type string from the run.
func builtinAdapterType(run *models.PlanningRun) string {
	if run == nil {
		return adapterTypeBacklog
	}
	switch strings.TrimSpace(run.AdapterType) {
	case adapterTypeWhatsnext:
		return adapterTypeWhatsnext
	default:
		return adapterTypeBacklog
	}
}

// buildBuiltinPrompt renders the prompt for the chosen adapter type. The
// prompt body itself lives as markdown in backend/internal/prompts/ (see
// docs/phase5-plan.md §3 A1). This function only computes the variable
// map the template needs and delegates rendering — both this Go path and
// the Python reference adapters share the same canonical source files.
func buildBuiltinPrompt(adapterType string, input ExecJSONInput) string {
	vars := builtinPromptVars(input)
	name := "backlog"
	if adapterType == adapterTypeWhatsnext {
		name = "whatsnext"
		vars["SCOPE_SECTION"] = buildWhatsnextScopeSection(input.Requirement)
		vars["CONTEXT"] = buildContextSnapshotWhatsnext(input.PlanningContext)
	}
	out, err := prompts.Render(name, vars)
	if err != nil {
		// The prompt files are embedded at build time so a missing file
		// here would only fire on a broken build. Degrading to a terse
		// fallback keeps the connector loop running rather than crashing.
		return fmt.Sprintf("prompt render error for %q: %v", name, err)
	}
	return out
}

// builtinPromptVars computes the shared variable map used by both the
// backlog and whatsnext prompts. whatsnext overrides CONTEXT + adds
// SCOPE_SECTION in buildBuiltinPrompt itself.
func builtinPromptVars(input ExecJSONInput) map[string]string {
	maxCandidates := input.RequestedMaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = builtinDefaultMaxCandidates
	}

	projectName := "(unnamed project)"
	projectDescription := ""
	if input.Project != nil {
		if n := strings.TrimSpace(input.Project.Name); n != "" {
			projectName = n
		}
		projectDescription = strings.TrimSpace(input.Project.Description)
	}

	// Pre-compute the Description line so the template stays branch-free.
	descLine := ""
	if projectDescription != "" {
		descLine = "Description: " + projectDescription
	}

	schemaVersion := "none"
	if input.PlanningContext != nil {
		schemaVersion = input.PlanningContext.SchemaVersion
	}

	return map[string]string{
		"PROJECT_NAME":             projectName,
		"PROJECT_DESCRIPTION_LINE": descLine,
		"REQUIREMENT":              buildRequirementSnippet(input.Requirement),
		"MAX_CANDIDATES":           fmt.Sprintf("%d", maxCandidates),
		"CONTEXT":                  buildContextSnippet(input.PlanningContext),
		"SCHEMA_VERSION":           schemaVersion,
		"SCOPE_SECTION":            "", // backlog does not use this; whatsnext overrides
	}
}

// buildWhatsnextScopeSection wraps the requirement scope block with the
// === Focus scope === header when a scope is present, or returns "" so
// the markdown template's {{SCOPE_SECTION}} substitutes to an empty line
// that the Python f-string version would also produce.
func buildWhatsnextScopeSection(req *models.Requirement) string {
	scope := buildScopeSnippet(req)
	if scope == "" {
		return ""
	}
	return "\n=== Focus scope ===\n" + scope + "\n"
}

// buildRequirementSnippet ports _requirement_snippet from backlog_adapter.py.
func buildRequirementSnippet(req *models.Requirement) string {
	if req == nil {
		return "(no requirement provided)"
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "(untitled)"
	}
	parts := []string{"Title: " + title}
	if s := strings.TrimSpace(req.Summary); s != "" {
		parts = append(parts, "Summary: "+s)
	}
	if d := strings.TrimSpace(req.Description); d != "" {
		parts = append(parts, "Description: "+d)
	}
	return strings.Join(parts, "\n")
}

// buildContextSnippet ports _context_snippet from backlog_adapter.py.
func buildContextSnippet(ctx *wire.PlanningContextV1) string {
	if ctx == nil {
		return "(no planning context provided)"
	}
	sources := ctx.Sources
	var out []string

	tasks := sources.OpenTasks
	if len(tasks) > 20 {
		tasks = tasks[:20]
	}
	if len(tasks) > 0 {
		out = append(out, "Open tasks:")
		for _, t := range tasks {
			out = append(out, fmt.Sprintf("  - [%s] %s (id=%s, priority=%s)", t.Status, t.Title, t.ID, t.Priority))
		}
	}

	docs := sources.RecentDocuments
	if len(docs) > 12 {
		docs = docs[:12]
	}
	if len(docs) > 0 {
		out = append(out, "Recent documents:")
		for _, d := range docs {
			stale := ""
			if d.IsStale {
				stale = " STALE"
			}
			out = append(out, fmt.Sprintf("  - %s (%s, type=%s%s, id=%s)", d.Title, d.FilePath, d.DocType, stale, d.ID))
		}
	}

	drift := sources.OpenDriftSignals
	if len(drift) > 8 {
		drift = drift[:8]
	}
	if len(drift) > 0 {
		out = append(out, "Open drift signals:")
		for _, ds := range drift {
			out = append(out, fmt.Sprintf("  - [%s] %s on %s: %s (id=%s)", ds.Severity, ds.TriggerType, ds.DocumentTitle, ds.TriggerDetail, ds.ID))
		}
	}

	if sr := sources.LatestSyncRun; sr != nil {
		line := fmt.Sprintf("Latest sync run: status=%s", sr.Status)
		if e := strings.TrimSpace(sr.ErrorMessage); e != "" {
			line += ", error=" + e
		}
		out = append(out, line)
	}

	agentRuns := sources.RecentAgentRuns
	if len(agentRuns) > 6 {
		agentRuns = agentRuns[:6]
	}
	if len(agentRuns) > 0 {
		out = append(out, "Recent agent runs:")
		for _, ar := range agentRuns {
			out = append(out, fmt.Sprintf("  - %s / %s (%s): %s", ar.AgentName, ar.ActionType, ar.Status, ar.Summary))
		}
	}

	if len(ctx.Meta.DroppedCounts) > 0 {
		anyDropped := false
		for _, v := range ctx.Meta.DroppedCounts {
			if v > 0 {
				anyDropped = true
				break
			}
		}
		if anyDropped {
			out = append(out, fmt.Sprintf("(note: some context entries were dropped under byte cap: %v)", ctx.Meta.DroppedCounts))
		}
	}

	if len(out) == 0 {
		return "(context is empty)"
	}
	return strings.Join(out, "\n")
}

// buildContextSnapshotWhatsnext ports _context_snapshot from whatsnext_adapter.py.
// Differences from buildContextSnippet: task limit 25 (not 20), drift limit 10 (not 8),
// and stale/fresh document split.
func buildContextSnapshotWhatsnext(ctx *wire.PlanningContextV1) string {
	if ctx == nil {
		return "(no planning context provided)"
	}
	sources := ctx.Sources
	var out []string

	tasks := sources.OpenTasks
	if len(tasks) > 25 {
		tasks = tasks[:25]
	}
	if len(tasks) > 0 {
		out = append(out, fmt.Sprintf("Open tasks (%d shown):", len(tasks)))
		for _, t := range tasks {
			out = append(out, fmt.Sprintf("  - [%s][%s] %s (id=%s)", t.Status, t.Priority, t.Title, t.ID))
		}
	}

	var staleDocs, freshDocs []wire.WireDocument
	for _, d := range sources.RecentDocuments {
		if d.IsStale {
			staleDocs = append(staleDocs, d)
		} else {
			freshDocs = append(freshDocs, d)
		}
	}
	if len(staleDocs) > 8 {
		staleDocs = staleDocs[:8]
	}
	if len(freshDocs) > 8 {
		freshDocs = freshDocs[:8]
	}
	if len(staleDocs) > 0 {
		out = append(out, fmt.Sprintf("STALE documents (%d):", len(staleDocs)))
		for _, d := range staleDocs {
			out = append(out, fmt.Sprintf("  - [STALE] %s (%s, type=%s, id=%s)", d.Title, d.FilePath, d.DocType, d.ID))
		}
	}
	if len(freshDocs) > 0 {
		out = append(out, fmt.Sprintf("Recent documents (%d fresh):", len(freshDocs)))
		for _, d := range freshDocs {
			out = append(out, fmt.Sprintf("  - %s (%s, type=%s, id=%s)", d.Title, d.FilePath, d.DocType, d.ID))
		}
	}

	drift := sources.OpenDriftSignals
	if len(drift) > 10 {
		drift = drift[:10]
	}
	if len(drift) > 0 {
		out = append(out, fmt.Sprintf("Open drift signals (%d):", len(drift)))
		for _, ds := range drift {
			out = append(out, fmt.Sprintf("  - [%s] %s on %s: %s (id=%s)", ds.Severity, ds.TriggerType, ds.DocumentTitle, ds.TriggerDetail, ds.ID))
		}
	}

	if sr := sources.LatestSyncRun; sr != nil {
		line := fmt.Sprintf("Latest sync run: status=%s", sr.Status)
		if e := strings.TrimSpace(sr.ErrorMessage); e != "" {
			if len(e) > 200 {
				e = e[:200]
			}
			line += ", error=" + e
		}
		out = append(out, line)
	}

	agentRuns := sources.RecentAgentRuns
	if len(agentRuns) > 6 {
		agentRuns = agentRuns[:6]
	}
	if len(agentRuns) > 0 {
		out = append(out, fmt.Sprintf("Recent agent runs (%d):", len(agentRuns)))
		for _, ar := range agentRuns {
			out = append(out, fmt.Sprintf("  - %s / %s (%s): %s", ar.AgentName, ar.ActionType, ar.Status, ar.Summary))
		}
	}

	if len(ctx.Meta.DroppedCounts) > 0 {
		anyDropped := false
		for _, v := range ctx.Meta.DroppedCounts {
			if v > 0 {
				anyDropped = true
				break
			}
		}
		if anyDropped {
			out = append(out, fmt.Sprintf("(note: some context entries were truncated: %v)", ctx.Meta.DroppedCounts))
		}
	}

	if len(out) == 0 {
		return "(project context is empty)"
	}
	return strings.Join(out, "\n")
}

// genericTitles mirrors _GENERIC_TITLES from whatsnext_adapter.py.
var genericTitles = map[string]bool{
	"":                 true,
	"what's next":      true,
	"whats next":       true,
	"next steps":       true,
	"next step":        true,
	"what to do next":  true,
	"what should i do": true,
	"analysis":         true,
	"review":           true,
	"backlog review":   true,
	"project review":   true,
	"triage":           true,
}

// buildScopeSnippet ports _scope_snippet from whatsnext_adapter.py.
func buildScopeSnippet(req *models.Requirement) string {
	if req == nil {
		return ""
	}
	title := strings.TrimSpace(req.Title)
	if genericTitles[strings.ToLower(title)] {
		return ""
	}
	parts := []string{"Focus scope: " + title}
	if s := strings.TrimSpace(req.Summary); s != "" {
		parts = append(parts, "Scope detail: "+s)
	} else if d := strings.TrimSpace(req.Description); d != "" {
		parts = append(parts, "Scope detail: "+d)
	}
	return strings.Join(parts, "\n")
}

// invokeBuiltinCLI runs the CLI and returns (output, errorMessage).
// For Claude: uses exec.CommandContext with -p flag.
// For Codex: uses creack/pty because Codex checks stdin.isTTY.
func invokeBuiltinCLI(ctx context.Context, agent, binary, model, prompt string, timeoutSec int) (string, string) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	switch agent {
	case "claude":
		return invokeClaudeCLI(runCtx, binary, model, prompt, timeoutSec)
	case "codex":
		return invokeCodexCLI(runCtx, binary, model, prompt, timeoutSec)
	default:
		return "", fmt.Sprintf("unsupported agent %q (expected 'claude' or 'codex')", agent)
	}
}

// invokeClaudeCLI runs: claude -p <prompt> [--model <model>]
func invokeClaudeCLI(ctx context.Context, binary, model, prompt string, timeoutSec int) (string, string) {
	args := []string{"-p", prompt}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = nil // disconnected

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Sprintf("claude CLI timed out after %ds", timeoutSec)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return "", fmt.Sprintf("claude CLI failed: %s", detail)
	}
	return stdout.String(), ""
}

// invokeCodexCLI runs codex inside a PTY because Codex checks stdin.isTTY.
func invokeCodexCLI(ctx context.Context, binary, model, prompt string, timeoutSec int) (string, string) {
	args := []string{prompt}
	if model != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(ctx, binary, args...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Sprintf("pty unavailable for codex: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Drain all output from the PTY master into a buffer.
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, ptmx)

	// Wait for the command to finish.
	if waitErr := cmd.Wait(); waitErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Sprintf("codex CLI timed out after %ds", timeoutSec)
		}
		raw := stripANSI(buf.String())
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\r", "\n")
		tail := raw
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		return "", fmt.Sprintf("codex CLI failed (%v): %s", waitErr, tail)
	}

	raw := buf.String()
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return raw, ""
}

// stripANSI removes ANSI escape codes from s.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// rawJSONCandidate is the unmarshalled shape produced by the model.
type rawJSONCandidate struct {
	SuggestionType     string   `json:"suggestion_type"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Rationale          string   `json:"rationale"`
	ValidationCriteria string   `json:"validation_criteria"`
	PODecision         string   `json:"po_decision"`
	PriorityScore      float64  `json:"priority_score"`
	Confidence         float64  `json:"confidence"`
	Rank               int      `json:"rank"`
	Evidence           []string `json:"evidence"`
	DuplicateTitles    []string `json:"duplicate_titles"`
	// ExecutionRole is optional (Phase 5 B2). The current planner prompts
	// do NOT ask the model to emit this field, so it will be empty in
	// almost all cases. Kept in the wire shape now so Phase 6 can enable
	// role-aware planners without a connector-protocol bump.
	ExecutionRole string `json:"execution_role"`
}

// extractJSONFromOutput implements the three-strategy extraction from _extract_json.
func extractJSONFromOutput(text string) (map[string]json.RawMessage, error) {
	text = strings.TrimSpace(text)

	// Strategy 1: fenced ```json ... ``` block.
	if m := jsonBlockRE.FindStringSubmatch(text); m != nil {
		candidate := strings.TrimSpace(m[1])
		var result map[string]json.RawMessage
		if err := json.Unmarshal([]byte(candidate), &result); err == nil {
			return result, nil
		}
	}

	// Strategy 2: first { ... last }.
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		fragment := text[start : end+1]
		var result map[string]json.RawMessage
		if err := json.Unmarshal([]byte(fragment), &result); err == nil {
			return result, nil
		}
	}

	// Strategy 3: direct parse.
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("no valid JSON found in agent output")
	}
	return result, nil
}

// normalizeBuiltinCandidates parses and normalises raw model candidates,
// clamping scores and truncating titles. Mirrors Python's _normalize_candidates.
func normalizeBuiltinCandidates(parsed map[string]json.RawMessage, maxCandidates int) []models.ConnectorBacklogCandidateDraft {
	rawList, ok := parsed["candidates"]
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawList, &items); err != nil {
		return nil
	}

	var out []models.ConnectorBacklogCandidateDraft
	for idx, raw := range items {
		if len(out) >= maxCandidates {
			break
		}
		var c rawJSONCandidate
		if err := json.Unmarshal(raw, &c); err != nil {
			continue
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			continue
		}
		// Truncate title to 120 runes.
		title = truncateRunes(title, 120)

		// Clamp scores to [0, 1].
		priority := clampFloat(c.PriorityScore, 0, 1)
		confidence := clampFloat(c.Confidence, 0, 1)

		// Fill missing rank sequentially.
		rank := c.Rank
		if rank <= 0 {
			rank = idx + 1
		}

		suggestionType := strings.TrimSpace(c.SuggestionType)
		if suggestionType == "" {
			suggestionType = "new_task"
		}

		out = append(out, models.ConnectorBacklogCandidateDraft{
			SuggestionType:     suggestionType,
			Title:              title,
			Description:        strings.TrimSpace(c.Description),
			Rationale:          strings.TrimSpace(c.Rationale),
			ValidationCriteria: strings.TrimSpace(c.ValidationCriteria),
			PODecision:         strings.TrimSpace(c.PODecision),
			PriorityScore:      priority,
			Confidence:         confidence,
			Rank:               rank,
			Evidence:           coerceStringList(c.Evidence),
			DuplicateTitles:    coerceStringList(c.DuplicateTitles),
			ExecutionRole:      strings.TrimSpace(c.ExecutionRole),
		})
	}
	return out
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes])
}

func coerceStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

