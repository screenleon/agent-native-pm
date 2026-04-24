package connector

import (
	"context"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

// probePromptText is the fixed one-turn prompt sent to the CLI when the
// operator clicks "Test on connector" in the UI (P4-4). Kept short so the
// round-trip stays inside a single heartbeat interval on a healthy CLI.
// Hardcoded in-process (never sent over the wire from the server) to satisfy
// the D7 constraint that the server never injects free-text instructions
// into adapter stdin.
const probePromptText = "Respond with exactly the single word: ok"

// probeTimeoutSeconds caps a single probe at 60s. Longer than the S5b
// --version probe (10s) because we expect a real completion here, but
// short enough that a stuck CLI does not starve the heartbeat worker.
const probeTimeoutSeconds = 60

// ExecuteProbe runs the configured CLI with a minimal prompt and returns a
// CliProbeResult suitable for submission on the next heartbeat. Unlike
// ExecuteBuiltin this does NOT expect the CLI to emit structured JSON — any
// non-empty stdout counts as "ok". The goal is to verify the CLI + model
// combination is live end-to-end; candidate generation is out of scope.
func ExecuteProbe(ctx context.Context, req models.PendingCliProbeRequest) models.CliProbeResult {
	sel := &AdapterCliSelection{
		ProviderID: req.ProviderID,
		ModelID:    req.ModelID,
		CliCommand: req.CliCommand,
	}
	// resolveBuiltinCLI shares the D4 precedence logic; passing run=nil so
	// per-run model overrides do not apply here (the probe explicitly uses
	// the binding's configured model_id).
	agent, binary, model, _, resolveErr := resolveBuiltinCLI(sel, nil)
	if resolveErr != "" {
		return models.CliProbeResult{
			ProbeID:      req.ProbeID,
			BindingID:    req.BindingID,
			OK:           false,
			ErrorKind:    classifyProbeResolveError(resolveErr),
			ErrorMessage: resolveErr,
			CompletedAt:  time.Now().UTC(),
		}
	}

	start := time.Now()
	output, runErr := invokeBuiltinCLI(ctx, agent, binary, model, probePromptText, probeTimeoutSeconds)
	latency := time.Since(start).Milliseconds()

	if runErr != "" {
		return models.CliProbeResult{
			ProbeID:      req.ProbeID,
			BindingID:    req.BindingID,
			OK:           false,
			LatencyMS:    latency,
			ErrorKind:    classifyProbeRunError(runErr),
			ErrorMessage: runErr,
			CompletedAt:  time.Now().UTC(),
		}
	}

	content := stripANSI(output)
	content = strings.TrimSpace(content)
	if len(content) > 320 {
		content = content[:320] + "…"
	}
	if content == "" {
		return models.CliProbeResult{
			ProbeID:      req.ProbeID,
			BindingID:    req.BindingID,
			OK:           false,
			LatencyMS:    latency,
			ErrorKind:    models.ErrorKindUnknown,
			ErrorMessage: "CLI returned empty output",
			CompletedAt:  time.Now().UTC(),
		}
	}
	return models.CliProbeResult{
		ProbeID:     req.ProbeID,
		BindingID:   req.BindingID,
		OK:          true,
		LatencyMS:   latency,
		Content:     content,
		CompletedAt: time.Now().UTC(),
	}
}

// classifyProbeResolveError maps the free-text resolveBuiltinCLI error into
// the allowlisted error_kind enum. Every CLI-resolution failure currently
// lands as ErrorKindUnknown — the error_message field carries the specific
// reason (missing binary, unsupported agent, etc.) and the S5a remediation
// catalog has no entry for "unknown" on purpose.
func classifyProbeResolveError(msg string) string {
	_ = msg
	return models.ErrorKindUnknown
}

// classifyProbeRunError maps invokeBuiltinCLI errors to the same enum.
// Timeouts and rate-limit shapes get their dedicated kinds so the remediation
// catalog in models.ErrorKindRemediations renders a useful hint.
func classifyProbeRunError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "timed out"):
		return models.ErrorKindAdapterTimeout
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "429"):
		return models.ErrorKindRateLimited
	case strings.Contains(lower, "session"), strings.Contains(lower, "unauthor"), strings.Contains(lower, "401"):
		return models.ErrorKindSessionExpired
	case strings.Contains(lower, "context"):
		return models.ErrorKindContextOverflow
	}
	return models.ErrorKindUnknown
}
