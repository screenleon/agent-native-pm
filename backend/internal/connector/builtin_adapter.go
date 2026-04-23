package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
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

	// Run CLI.
	output, runErr := invokeBuiltinCLI(ctx, agent, binary, model, prompt, builtinDefaultTimeoutSec)
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
// Mirrors D4 precedence: cli_selection > PATH lookup claude, then codex.
// modelSource values: "override" | "stdin" | "default" | "subscription"
func resolveBuiltinCLI(sel *AdapterCliSelection, run *models.PlanningRun) (agent, binary, model, modelSource, errMsg string) {
	// Per-run model override takes highest precedence.
	modelOverride := ""
	if run != nil {
		modelOverride = strings.TrimSpace(run.ModelOverride)
	}

	if sel != nil {
		// Derive agent from provider_id ("cli:claude" -> "claude", "cli:codex" -> "codex").
		providerID := strings.TrimSpace(sel.ProviderID)
		if strings.HasPrefix(providerID, "cli:") {
			agent = strings.ToLower(strings.TrimSpace(providerID[4:]))
		} else if providerID != "" {
			agent = strings.ToLower(strings.TrimSpace(providerID))
		}

		// Resolve binary: cli_command from selection wins; fall back to PATH.
		binary = strings.TrimSpace(sel.CliCommand)
		if binary == "" && agent != "" {
			binary, _ = exec.LookPath(agent)
		}

		// Resolve model: per-run override > selection model.
		if modelOverride != "" {
			model = modelOverride
			modelSource = "override"
		} else if strings.TrimSpace(sel.ModelID) != "" {
			model = strings.TrimSpace(sel.ModelID)
			modelSource = "stdin"
		}
	}

	// If agent is still empty, infer from binary filename first (handles the
	// case where sel.CliCommand is set but sel.ProviderID is empty), then fall
	// back to PATH lookup. This prevents PATH-found claude from overwriting a
	// caller-supplied binary path.
	if agent == "" {
		if binary != "" {
			// Infer agent from the base name of the provided binary.
			base := strings.ToLower(filepath.Base(binary))
			switch {
			case strings.HasPrefix(base, "claude"):
				agent = "claude"
			case strings.HasPrefix(base, "codex"):
				agent = "codex"
			default:
				// Unknown binary — accept it without a recognised agent name;
				// Claude invocation path will be used as the safest fallback.
				agent = "claude"
			}
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

	// Validate binary is resolvable.
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
		if modelOverride != "" {
			model = modelOverride
			modelSource = "override"
		} else if agent == "claude" {
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

// buildBuiltinPrompt builds the full prompt string for the chosen adapter type.
func buildBuiltinPrompt(adapterType string, input ExecJSONInput) string {
	switch adapterType {
	case adapterTypeWhatsnext:
		return buildWhatsnextPrompt(input)
	default:
		return buildBacklogPrompt(input)
	}
}

// jsonSchemaFence is the literal fenced JSON schema block included in prompts.
// Defined as a const to avoid backtick-in-format-string issues.
const backlogJSONSchema = "```json\n" +
	"{\n" +
	"  \"candidates\": [\n" +
	"    {\n" +
	"      \"title\": \"string (<= 120 chars)\",\n" +
	"      \"description\": \"string\",\n" +
	"      \"rationale\": \"string (why this is the right next step)\",\n" +
	"      \"priority_score\": \"number between 0 and 1\",\n" +
	"      \"confidence\": \"number between 0 and 1\",\n" +
	"      \"rank\": \"integer starting at 1 (lower = higher priority)\",\n" +
	"      \"evidence\": [\"string, ...\"],\n" +
	"      \"duplicate_titles\": [\"string, ...\"]\n" +
	"    }\n" +
	"  ]\n" +
	"}\n" +
	"```"

const whatsnextJSONSchema = "```json\n" +
	"{\n" +
	"  \"candidates\": [\n" +
	"    {\n" +
	"      \"title\": \"string (<= 120 chars, framed as a strategic direction)\",\n" +
	"      \"description\": \"string (scope, expected impact)\",\n" +
	"      \"rationale\": \"string (why NOW)\",\n" +
	"      \"validation_criteria\": \"string (how to know if this is working in 1-4 weeks)\",\n" +
	"      \"po_decision\": \"string (key decision or trade-off the PO must make)\",\n" +
	"      \"priority_score\": \"number between 0 and 1\",\n" +
	"      \"confidence\": \"number between 0 and 1\",\n" +
	"      \"rank\": \"integer starting at 1\",\n" +
	"      \"evidence\": [\"string, ...\"],\n" +
	"      \"duplicate_titles\": [\"string, ...\"]\n" +
	"    }\n" +
	"  ]\n" +
	"}\n" +
	"```"

// buildBacklogPrompt ports _build_prompt from backlog_adapter.py.
func buildBacklogPrompt(input ExecJSONInput) string {
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

	schemaVersion := "none"
	if input.PlanningContext != nil {
		schemaVersion = input.PlanningContext.SchemaVersion
	}

	var sb strings.Builder
	sb.WriteString("You are a backlog planner for the software project \"")
	sb.WriteString(projectName)
	sb.WriteString("\".\n\n")
	sb.WriteString(fmt.Sprintf("Decompose the requirement below into AT MOST %d concrete backlog\n", maxCandidates))
	sb.WriteString("candidates (tasks) scoped to THIS project. Each candidate must:\n")
	sb.WriteString("  1. Be independently implementable within \"")
	sb.WriteString(projectName)
	sb.WriteString("\".\n")
	sb.WriteString("  2. Reference specific evidence from the project context when relevant\n")
	sb.WriteString("     (open tasks, documents, drift signals, sync failures, recent agent runs).\n")
	sb.WriteString("     Evidence items MUST be strings of the form \"doc:<id>\", \"task:<id>\",\n")
	sb.WriteString("     \"drift:<id>\", \"sync:<id>\", or \"agent_run:<id>\" using the exact ids from\n")
	sb.WriteString("     the context below. Omit evidence if none applies.\n")
	sb.WriteString("  3. Not duplicate any existing open task. If you think a candidate is close\n")
	sb.WriteString("     to an existing task, add that task title to \"duplicate_titles\".\n\n")
	sb.WriteString("Return STRICT JSON inside a single ")
	sb.WriteString("```json")
	sb.WriteString(" fenced code block with this schema:\n")
	sb.WriteString(backlogJSONSchema)
	sb.WriteString("\n\nDo not include any prose outside the fenced JSON block. Do not invent ids\n")
	sb.WriteString("that are not in the context.\n\n")
	sb.WriteString("=== Project ===\n")
	sb.WriteString("Name: ")
	sb.WriteString(projectName)
	sb.WriteString("\n")
	if projectDescription != "" {
		sb.WriteString("Description: ")
		sb.WriteString(projectDescription)
		sb.WriteString("\n")
	}
	sb.WriteString("\n=== Requirement ===\n")
	sb.WriteString(buildRequirementSnippet(input.Requirement))
	sb.WriteString("\n\n=== Project context (schema=")
	sb.WriteString(schemaVersion)
	sb.WriteString(") ===\n")
	sb.WriteString(buildContextSnippet(input.PlanningContext))
	sb.WriteString("\n")
	return sb.String()
}

// buildWhatsnextPrompt ports _build_prompt from whatsnext_adapter.py.
func buildWhatsnextPrompt(input ExecJSONInput) string {
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

	schemaVersion := "none"
	if input.PlanningContext != nil {
		schemaVersion = input.PlanningContext.SchemaVersion
	}

	var sb strings.Builder
	sb.WriteString("You are a strategic product advisor for \"")
	sb.WriteString(projectName)
	sb.WriteString("\".\n\n")
	sb.WriteString(fmt.Sprintf("Your role is to identify the %d highest-leverage DIRECTIONS the team should pursue next ", maxCandidates))
	sb.WriteString("-- not individual bug fixes or task lists, but strategic bets that create meaningful, measurable product or quality improvements.\n\n")
	sb.WriteString("Think in terms of the Agent-era product cycle:\n")
	sb.WriteString("  1. Agent proposes a direction (that is your job here)\n")
	sb.WriteString("  2. PO decides and prioritises\n")
	sb.WriteString("  3. Agent / Dev executes quickly\n")
	sb.WriteString("  4. Team validates with data / UX / business signals\n")
	sb.WriteString("  5. Direction is adjusted\n\n")
	sb.WriteString("For each direction, answer three questions:\n")
	sb.WriteString("  A. What is the bet? -- a clear hypothesis (\"Investing in X will achieve Y\")\n")
	sb.WriteString("  B. How do we know it worked? -- concrete validation signals (metrics, observable outcomes, user feedback)\n")
	sb.WriteString("  C. What must the PO decide? -- the key judgement call or trade-off the PO must make to unlock this direction\n\n")
	sb.WriteString("Evidence signals to draw on (use these to ground each direction):\n")
	sb.WriteString("  - Repeated failures or blocked tasks -- signal of systemic risk\n")
	sb.WriteString("  - Stale / drifted documents -- signal of knowledge debt\n")
	sb.WriteString("  - Gaps in recent agent runs -- signal of missing capability or tooling\n")
	sb.WriteString("  - High-priority tasks with no owner -- signal of execution risk\n")
	sb.WriteString("  - Sync errors -- signal of reliability risk\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("  - Directions must be strategic, not tactical. \"Fix bug X\" is not a direction; \"Invest in connector reliability\" is.\n")
	sb.WriteString("  - Every direction must be grounded in specific evidence from the context. Evidence items MUST be strings of the form\n")
	sb.WriteString("    \"doc:<id>\", \"task:<id>\", \"drift:<id>\", \"sync:<id>\", or \"agent_run:<id>\" using exact ids from the context. Omit if none applies.\n")
	sb.WriteString("  - Do NOT invent ids.\n")
	sb.WriteString("  - Rank 1 = highest strategic leverage right now. Higher rank = lower urgency.\n")
	sb.WriteString("  - If a direction is very similar to an existing open task, note it in \"duplicate_titles\".\n\n")
	sb.WriteString("Return STRICT JSON inside a single ")
	sb.WriteString("```json")
	sb.WriteString(" fenced code block with this schema:\n")
	sb.WriteString(whatsnextJSONSchema)
	sb.WriteString("\n\nDo not include any prose outside the fenced JSON block.\n\n")
	sb.WriteString("=== Project ===\n")
	sb.WriteString("Name: ")
	sb.WriteString(projectName)
	sb.WriteString("\n")
	if projectDescription != "" {
		sb.WriteString("Description: ")
		sb.WriteString(projectDescription)
		sb.WriteString("\n")
	}
	if scope := buildScopeSnippet(input.Requirement); scope != "" {
		sb.WriteString("\n=== Focus scope ===\n")
		sb.WriteString(scope)
		sb.WriteString("\n")
	}
	sb.WriteString("\n=== Current project state (schema=")
	sb.WriteString(schemaVersion)
	sb.WriteString(") ===\n")
	sb.WriteString(buildContextSnapshotWhatsnext(input.PlanningContext))
	sb.WriteString("\n")
	return sb.String()
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

