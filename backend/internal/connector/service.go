package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/prompts"
	"github.com/screenleon/agent-native-pm/internal/roles"
)

// cliInterpreterBlocklist is the set of bare command names that must NOT be
// probed with `<cmd> --version`. These are generic language runtimes whose
// --version output says nothing about whether the intended CLI agent works
// (T-S5b-5 / interpreter blocklist).
var cliInterpreterBlocklist = map[string]bool{
	"python":  true,
	"python3": true,
	"python2": true,
	"node":    true,
	"nodejs":  true,
	"ruby":    true,
	"perl":    true,
	"php":     true,
	"bash":    true,
	"sh":      true,
	"zsh":     true,
}

type Service struct {
	Client            *Client
	State             *State
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
	// CliHealthInterval controls how often CLI bindings are re-probed.
	// Zero or a negative value means use the default interval (5 minutes).
	// Enable CliHealthDisabled to disable probing entirely.
	CliHealthInterval time.Duration
	// CliHealthDisabled suppresses the health probe loop entirely.
	CliHealthDisabled bool
	Stdout            io.Writer
	Stderr            io.Writer
	// ActivityReporter is optional. When set, the service reports its current
	// execution phase to the server via POST /api/connector/activity.
	// Phase 6c PR-4.
	ActivityReporter *ActivityReporter

	mu               sync.Mutex
	knownCliBindings map[string]knownCliBinding
	lastCliHealthyAt *time.Time // most recent successful probe across all bindings
	// P4-4 probe state. Pending probes received from the last heartbeat
	// response are dispatched one at a time by a worker goroutine so they
	// neither block heartbeats nor run concurrently (keeps load predictable
	// on a shared subscription CLI). Completed results are buffered here
	// until the next heartbeat picks them up.
	pendingProbeIDs  map[string]bool
	probeResultsOut  []models.CliProbeResult
	probeWorkerOnce  sync.Once
	probeWorkerWake  chan struct{}
	probeWorkerMu    sync.Mutex
	probeQueue       []models.PendingCliProbeRequest
}

type knownCliBinding struct {
	command      string
	lastProbedAt time.Time
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil || s.Client == nil || s.State == nil {
		return fmt.Errorf("connector service requires client and state")
	}
	heartbeatInterval := s.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}
	pollInterval := s.PollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	cliHealthInterval := s.CliHealthInterval
	if cliHealthInterval <= 0 {
		cliHealthInterval = 5 * time.Minute
	}

	// P4-4 probe worker starts at Run() time and blocks on probeWorkerWake /
	// a 5s tick — effectively idle until a heartbeat response delivers a
	// pending probe. Starting here (rather than lazily on first probe)
	// keeps the heartbeat goroutine off the start-worker path and avoids a
	// subtle race where the first heartbeat could miss the wake signal.
	s.ensureProbeWorker(ctx)

	lastError := ""
	nextHeartbeat := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(nextHeartbeat) {
			// Run due health probes before building the heartbeat request so
			// the timestamp is current.
			if !s.CliHealthDisabled {
				s.runDueProbes(ctx, cliHealthInterval)
			}
			s.mu.Lock()
			healthyAt := s.lastCliHealthyAt
			// Drain any probe results produced since the previous heartbeat.
			probeResults := s.probeResultsOut
			s.probeResultsOut = nil
			s.mu.Unlock()
			connector, err := s.Client.Heartbeat(ctx, models.LocalConnectorHeartbeatRequest{
				Capabilities:     buildCapabilities(s.State.Adapter),
				LastError:        lastError,
				LastCliHealthyAt: healthyAt,
				CliProbeResults:  probeResults,
			})
			if err != nil {
				lastError = err.Error()
				fmt.Fprintf(s.Stderr, "heartbeat failed: %s\n", lastError)
				// On failure re-queue the results so they are sent next time.
				if len(probeResults) > 0 {
					s.mu.Lock()
					s.probeResultsOut = append(probeResults, s.probeResultsOut...)
					s.mu.Unlock()
				}
			} else {
				lastError = ""
				fmt.Fprintf(s.Stdout, "connector online: %s (%s)\n", connector.Label, connector.Status)
				// Pick up any pending probes the server announced via metadata.
				s.enqueuePendingProbesFromMetadata(connector)
			}
			nextHeartbeat = time.Now().Add(heartbeatInterval)
		}
		worked, err := s.RunOnce(ctx)
		if err != nil {
			lastError = err.Error()
			fmt.Fprintf(s.Stderr, "run loop error: %s\n", lastError)
		} else {
			lastError = ""
		}
		// Phase 6b: when the planning-run queue is idle, also try the task
		// dispatch queue. Both loops share the same poll/sleep cadence.
		if !worked {
			workedTask, taskErr := s.RunOnceTask(ctx)
			if taskErr != nil {
				lastError = taskErr.Error()
				fmt.Fprintf(s.Stderr, "task dispatch loop error: %s\n", lastError)
			}
			worked = workedTask
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// RunOnceTask implements the Phase 6b role_dispatch execution loop. It claims
// one queued task, renders the role prompt, invokes the CLI, and submits the
// result. Returns (true, nil) when a task was processed (regardless of
// success/failure of the task itself), (false, nil) when the queue is empty,
// and (false, err) on infrastructure errors.
func (s *Service) RunOnceTask(ctx context.Context) (bool, error) {
	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:     models.ConnectorPhaseClaimingTask,
			StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	resp, err := s.Client.ClaimNextTask(ctx)
	if err != nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return false, err
	}
	if resp == nil || resp.Task == nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return false, nil
	}
	task := resp.Task

	// Parse role_id from source ("role_dispatch:backend-architect" →
	// "backend-architect"). Mirrors the server-side parser in
	// store.parseRoleIDFromSource — same input → same error_kind, so
	// operators see consistent diagnostics whether the stale-role
	// check fired in the server claim path or made it through to the
	// connector. Two distinct kinds (Phase 6c PR-2 Copilot review #4):
	//   - source missing role suffix → role_dispatch_malformed
	//   - role id absent from catalog → role_not_found
	roleID := ""
	hasSuffix := false
	if after, ok := strings.CutPrefix(task.Source, "role_dispatch:"); ok {
		trimmed := strings.TrimSpace(after)
		if trimmed != "" {
			roleID = trimmed
			hasSuffix = true
		}
	}
	if !hasSuffix {
		fmt.Fprintf(s.Stderr, "task %s has invalid source %q — missing role_id\n", task.ID, task.Source)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("task source %q is missing a role suffix (expected role_dispatch:<role>)", task.Source),
			ErrorKind:    models.ErrorKindRoleDispatchMalformed,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	// Catalog enforcement: role must exist in the embedded prompt library.
	if !prompts.Exists("roles/" + roleID) {
		fmt.Fprintf(s.Stderr, "task %s: role %q not found in catalog\n", task.ID, roleID)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("role %q is not in the current catalog", roleID),
			ErrorKind:    models.ErrorKindRoleNotFound,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	fmt.Fprintf(s.Stdout, "claimed task %s (role=%s title=%q)\n", task.ID, roleID, task.Title)

	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:        models.ConnectorPhaseDispatching,
			SubjectKind:  "task",
			SubjectID:    task.ID,
			SubjectTitle: task.Title,
			RoleID:       roleID,
			Step:         "rendering prompt",
			StartedAt:    time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}

	// Resolve CLI. Use the connector's primary CLI config if available (the
	// connector state does not carry a binding for task dispatch, so we
	// construct a nil selection and let resolveBuiltinCLI fall back to env /
	// PATH lookup, which is the correct behaviour for role_dispatch).
	// The model is read from task or env; there is no per-task ModelID field
	// in Phase 6b so we pass nil run.
	agent, cliPath, cliModel, _, resolveErr := resolveBuiltinCLI(nil, nil)
	if resolveErr != "" {
		fmt.Fprintf(s.Stderr, "task %s: CLI resolve failed: %s\n", task.ID, resolveErr)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: resolveErr,
			ErrorKind:    "adapter_timeout",
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return true, nil
	}

	// Build template vars.
	vars := map[string]string{
		"TASK_TITLE":       strings.TrimSpace(task.Title),
		"TASK_DESCRIPTION": strings.TrimSpace(task.Description),
		"REQUIREMENT":      buildConnectorRequirementContext(resp.Requirement),
		"PROJECT_CONTEXT":  strings.TrimSpace(resp.ProjectContext),
	}

	// Render role prompt.
	rendered, renderErr := prompts.Render("roles/"+roleID, vars)
	if renderErr != nil {
		fmt.Fprintf(s.Stderr, "task %s: prompt render failed: %v\n", task.ID, renderErr)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("prompt render error: %v", renderErr),
			ErrorKind:    "unknown",
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	// Read per-role timeout. The role catalog is the source of truth;
	// ANPM_DISPATCH_TIMEOUT env can override (operators use this when
	// they pre-know a task is unusually long). 0 = disabled. See
	// docs/phase6c-plan.md §3 C2 for the resolution order.
	timeoutSec := int(roles.TimeoutFor(roleID).Seconds())

	// Invoke CLI. invokeBuiltinCLI applies signal escalation
	// (SIGTERM → 5s → SIGKILL) and the bounded-writer output cap.
	// Reuse the agent inferred by resolveBuiltinCLI rather than
	// re-deriving from the binary path.
	//
	// Progress ticker: prints a heartbeat line every 30 s so the operator
	// can see the connector is still working and hasn't stalled. The ticker
	// goroutine exits as soon as invokeBuiltinCLI returns.
	startedAt := time.Now()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				elapsed := t.Sub(startedAt).Truncate(time.Second)
				fmt.Fprintf(s.Stdout, "task %s: still running (role=%s, elapsed=%s)\n", task.ID, roleID, elapsed)
				if s.ActivityReporter != nil {
					s.ActivityReporter.Report(models.ConnectorActivity{
						Phase:        models.ConnectorPhaseDispatching,
						SubjectKind:  "task",
						SubjectID:    task.ID,
						SubjectTitle: task.Title,
						RoleID:       roleID,
						Step:         fmt.Sprintf("running (%s)", elapsed),
						StartedAt:    startedAt.UTC(),
						UpdatedAt:    time.Now().UTC(),
					})
				}
			}
		}
	}()
	output, truncated, runErrMsg := invokeBuiltinCLI(ctx, agent, cliPath, cliModel, rendered, timeoutSec)
	close(done)
	// Precedence: when the CLI both timed out / errored AND tripped
	// the output cap, the runErrMsg is more informative (the user
	// needs to know it timed out, not just that the cap fired). The
	// cap-only path applies only when runErrMsg is empty.
	if runErrMsg != "" {
		errKind := classifyDispatchRunError(runErrMsg)
		fmt.Fprintf(s.Stderr, "task %s: CLI failed: %s (truncated=%v)\n", task.ID, runErrMsg, truncated)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: runErrMsg,
			ErrorKind:    errKind,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}
	if truncated {
		fmt.Fprintf(s.Stderr, "task %s: CLI stdout exceeded dispatch output cap\n", task.ID)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("CLI stdout exceeded the dispatch output cap (%d bytes)", dispatchOutputMaxBytes()),
			ErrorKind:    models.ErrorKindOutputTooLarge,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	output = stripANSI(output)

	// Extract JSON from output.
	parsed, extractErr := extractJSONFromOutput(output)
	if extractErr != nil {
		snippet := strings.TrimSpace(output)
		if len(snippet) > 240 {
			snippet = snippet[:240]
		}
		// Normalize newlines so the error message renders cleanly in
		// stderr logs and the task result panel — matches the
		// builtin_adapter.go pattern for the same snippet shape.
		snippet = strings.ReplaceAll(snippet, "\r\n", " ")
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = strings.ReplaceAll(snippet, "\r", " ")
		errMsg := fmt.Sprintf("could not parse output as JSON: %v; first 240 chars: %s", extractErr, snippet)
		fmt.Fprintf(s.Stderr, "task %s: %s\n", task.ID, errMsg)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: errMsg,
			ErrorKind:    models.ErrorKindInvalidResultSchema,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	// Schema minimum validation (Phase 6c C2(c)).
	if schemaErr := validateExecutionResult(parsed); schemaErr != nil {
		fmt.Fprintf(s.Stderr, "task %s: result schema invalid: %v\n", task.ID, schemaErr)
		if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
			Success:      false,
			ErrorMessage: schemaErr.Error(),
			ErrorKind:    models.ErrorKindInvalidResultSchema,
		}); err != nil {
			fmt.Fprintf(s.Stderr, "task %s: submit result failed: %v\n", task.ID, err)
		}
		return true, nil
	}

	// Apply files to local repo if repo_path was provided in the claim response.
	filesApplied := 0
	if resp.RepoPath != "" {
		filesApplied = applyExecutionResultFiles(resp.RepoPath, parsed, s.Stderr)
		if filesApplied > 0 {
			fmt.Fprintf(s.Stdout, "task %s: applied %d file(s) to %s\n", task.ID, filesApplied, resp.RepoPath)
		}
	}
	// Annotate the result with how many files were applied so the UI can show it.
	if filesApplied > 0 {
		parsed["files_applied"] = json.RawMessage(fmt.Sprintf("%d", filesApplied))
	}

	// Re-marshal the full parsed map as the result payload.
	resultBytes, _ := json.Marshal(parsed)

	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:        models.ConnectorPhaseSubmitting,
			SubjectKind:  "task",
			SubjectID:    task.ID,
			SubjectTitle: task.Title,
			RoleID:       roleID,
			StartedAt:    time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}

	if err := s.Client.SubmitTaskResult(ctx, task.ID, SubmitTaskResultRequest{
		Success: true,
		Result:  json.RawMessage(resultBytes),
	}); err != nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return true, fmt.Errorf("submit task result for %s: %w", task.ID, err)
	}
	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:     models.ConnectorPhaseIdle,
			StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	fmt.Fprintf(s.Stdout, "completed task %s (role=%s)\n", task.ID, roleID)
	return true, nil
}

// classifyDispatchRunError maps invokeBuiltinCLI error strings to the
// dispatch-specific error_kind enum. Differs from classifyRunError
// (used by planning runs) in that timeouts map to dispatch_timeout
// rather than the generic adapter_timeout.
func classifyDispatchRunError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "timed out"):
		return models.ErrorKindDispatchTimeout
	case strings.Contains(lower, "session") && strings.Contains(lower, "expired"):
		return models.ErrorKindSessionExpired
	case strings.Contains(lower, "rate limit"):
		return models.ErrorKindRateLimited
	case strings.Contains(lower, "context") && strings.Contains(lower, "overflow"):
		return models.ErrorKindContextOverflow
	default:
		return models.ErrorKindUnknown
	}
}

// buildConnectorRequirementContext formats the requirement summary for prompt injection.
func buildConnectorRequirementContext(req *ConnectorRequirementSummary) string {
	if req == nil {
		return ""
	}
	var parts []string
	if t := strings.TrimSpace(req.Title); t != "" {
		parts = append(parts, "Title: "+t)
	}
	if s := strings.TrimSpace(req.Summary); s != "" {
		parts = append(parts, "Summary: "+s)
	}
	return strings.Join(parts, "\n")
}

// applyExecutionResultFiles writes the files[] array from a validated
// execution result to disk under repoPath. Errors are logged to stderr but do
// NOT fail the task — the JSON result is still submitted. This matches the
// "best-effort apply" principle: the server gets the result regardless, and
// the UI shows whether files were applied via the files_applied count field.
//
// Safety: any path that is absolute or that resolves to a location outside the
// repo root (starts with "..") is rejected.
func applyExecutionResultFiles(repoPath string, payload map[string]json.RawMessage, stderr io.Writer) (applied int) {
	if strings.TrimSpace(repoPath) == "" {
		return 0
	}
	rawFiles, ok := payload["files"]
	if !ok {
		return 0
	}
	var files []struct {
		Path     string `json:"path"`
		Contents string `json:"contents"`
		Mode     string `json:"mode"`
	}
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		fmt.Fprintf(stderr, "apply files: unmarshal error: %v\n", err)
		return 0
	}
	for _, f := range files {
		relPath := filepath.Clean(f.Path)
		// Safety: reject paths that escape the repo root or are absolute.
		if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "..") {
			fmt.Fprintf(stderr, "apply files: rejected unsafe path %q\n", f.Path)
			continue
		}
		dest := filepath.Join(repoPath, relPath)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			fmt.Fprintf(stderr, "apply files: mkdir %q: %v\n", filepath.Dir(dest), err)
			continue
		}
		if err := os.WriteFile(dest, []byte(f.Contents), 0644); err != nil {
			fmt.Fprintf(stderr, "apply files: write %q: %v\n", dest, err)
			continue
		}
		applied++
	}
	return applied
}

// resolveAgentFromBinary infers the agent name from the binary path.
func resolveAgentFromBinary(binary string) string {
	base := strings.ToLower(filepath.Base(binary))
	switch {
	case strings.HasPrefix(base, "claude"):
		return "claude"
	case strings.HasPrefix(base, "codex"):
		return "codex"
	default:
		return "claude"
	}
}

// classifyRunError maps common error substrings to error_kind values.
func classifyRunError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "session") && strings.Contains(lower, "expired"):
		return "session_expired"
	case strings.Contains(lower, "rate limit"):
		return "rate_limited"
	case strings.Contains(lower, "context") && strings.Contains(lower, "overflow"):
		return "context_overflow"
	case strings.Contains(lower, "timed out"):
		return "adapter_timeout"
	default:
		return "unknown"
	}
}

func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:     models.ConnectorPhaseClaimingRun,
			StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	claim, err := s.Client.ClaimNextRun(ctx)
	if err != nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return false, err
	}
	if claim == nil || claim.Run == nil || claim.Requirement == nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return false, nil
	}
	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:        models.ConnectorPhasePlanning,
			SubjectKind:  "planning_run",
			SubjectID:    claim.Run.ID,
			SubjectTitle: claim.Requirement.Title,
			StartedAt:    time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	projectLabel := claim.Run.ProjectID
	if claim.Project != nil && strings.TrimSpace(claim.Project.Name) != "" {
		projectLabel = claim.Project.Name
	}
	fmt.Fprintf(s.Stdout, "claimed planning run %s for project %q / requirement %q\n", claim.Run.ID, projectLabel, claim.Requirement.Title)
	var cliSelection *AdapterCliSelection
	if claim.CliBinding != nil {
		cliSelection = &AdapterCliSelection{
			ProviderID: claim.CliBinding.ProviderID,
			ModelID:    claim.CliBinding.ModelID,
			CliCommand: claim.CliBinding.CliCommand,
		}
		if !s.CliHealthDisabled {
			s.trackCliBinding(claim.CliBinding.ID, claim.CliBinding.CliCommand)
		}
	}
	execInput := ExecJSONInput{
		Run:                    claim.Run,
		Requirement:            claim.Requirement,
		Project:                claim.Project,
		RequestedMaxCandidates: defaultRequestedCandidates,
		PlanningContext:        claim.PlanningContext,
		CliSelection:           cliSelection,
	}
	// Progress ticker for planning runs — same 30-second cadence as task dispatch.
	runStartedAt := time.Now()
	runDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runDone:
				return
			case t := <-ticker.C:
				elapsed := t.Sub(runStartedAt).Truncate(time.Second)
				fmt.Fprintf(s.Stdout, "planning run %s: still running (elapsed=%s)\n", claim.Run.ID, elapsed)
				if s.ActivityReporter != nil {
					s.ActivityReporter.Report(models.ConnectorActivity{
						Phase:        models.ConnectorPhasePlanning,
						SubjectKind:  "planning_run",
						SubjectID:    claim.Run.ID,
						SubjectTitle: claim.Requirement.Title,
						Step:         fmt.Sprintf("running (%s)", elapsed),
						StartedAt:    runStartedAt.UTC(),
						UpdatedAt:    time.Now().UTC(),
					})
				}
			}
		}
	}()
	var result models.LocalConnectorSubmitRunResultRequest
	if strings.TrimSpace(s.State.Adapter.Command) == "" {
		result = ExecuteBuiltin(ctx, execInput)
	} else {
		result = ExecuteExecJSON(ctx, s.State.Adapter, execInput)
	}
	close(runDone)
	if _, err := s.Client.SubmitRunResult(ctx, claim.Run.ID, result); err != nil {
		if s.ActivityReporter != nil {
			s.ActivityReporter.Report(models.ConnectorActivity{
				Phase:     models.ConnectorPhaseIdle,
				StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		return true, fmt.Errorf("submit run result for %s: %w", claim.Run.ID, err)
	}
	if s.ActivityReporter != nil {
		s.ActivityReporter.Report(models.ConnectorActivity{
			Phase:     models.ConnectorPhaseIdle,
			StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	if result.Success {
		fmt.Fprintf(s.Stdout, "completed planning run %s (project %q) with %d candidates\n", claim.Run.ID, projectLabel, len(result.Candidates))
		return true, nil
	}
	fmt.Fprintf(s.Stderr, "planning run %s (project %q) failed locally: %s\n", claim.Run.ID, projectLabel, strings.TrimSpace(result.ErrorMessage))
	return true, nil
}

func (s *Service) trackCliBinding(bindingID, command string) {
	if strings.TrimSpace(bindingID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.knownCliBindings == nil {
		s.knownCliBindings = make(map[string]knownCliBinding)
	}
	existing, ok := s.knownCliBindings[bindingID]
	if !ok {
		s.knownCliBindings[bindingID] = knownCliBinding{command: command}
		return
	}
	if existing.command != command {
		s.knownCliBindings[bindingID] = knownCliBinding{command: command, lastProbedAt: existing.lastProbedAt}
	}
}

// runDueProbes probes each known CLI binding that hasn't been checked recently.
// Results are written to the connector log only — not sent to the server in
// per-binding detail. When any probe succeeds, lastCliHealthyAt is updated so
// the next heartbeat carries the single "last healthy" timestamp.
func (s *Service) runDueProbes(ctx context.Context, interval time.Duration) {
	s.mu.Lock()
	due := make(map[string]knownCliBinding)
	for id, b := range s.knownCliBindings {
		if time.Since(b.lastProbedAt) >= interval {
			due[id] = b
		}
	}
	s.mu.Unlock()

	for id, b := range due {
		healthy, versionString, errMsg, cancelled := s.probeCliCommand(ctx, b.command)
		if cancelled {
			// Connector is shutting down; don't advance lastProbedAt so the
			// probe runs again on the next startup rather than being silently
			// suppressed for the full cliHealthInterval.
			continue
		}
		if healthy {
			now := time.Now().UTC()
			fmt.Fprintf(s.Stdout, "cli health probe [%s] %s: healthy (%s)\n", id, b.command, versionString)
			s.mu.Lock()
			s.lastCliHealthyAt = &now
			s.mu.Unlock()
		} else {
			fmt.Fprintf(s.Stderr, "cli health probe [%s] %s: %s\n", id, b.command, errMsg)
		}
		s.mu.Lock()
		if existing, ok := s.knownCliBindings[id]; ok {
			s.knownCliBindings[id] = knownCliBinding{command: existing.command, lastProbedAt: time.Now()}
		}
		s.mu.Unlock()
	}
}

// probeCliCommand runs `<command> --version` and returns (healthy, versionString, errMsg, cancelled).
// cancelled is true only when the parent context was cancelled (connector shutting down) — the
// caller must not treat this as a probe failure or advance lastProbedAt.
// Commands on the interpreter blocklist are treated as unknown and skipped.
func (s *Service) probeCliCommand(ctx context.Context, command string) (healthy bool, versionString string, errMsg string, cancelled bool) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false, "", "cli_command not configured", false
	}
	base := strings.ToLower(filepath.Base(cmd))
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	if cliInterpreterBlocklist[base] {
		return false, "", "skipped: interpreter command", false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, cmd, "--version").CombinedOutput()
	if err != nil {
		if ctx.Err() == context.Canceled {
			return false, "", "", true
		}
		if probeCtx.Err() != nil {
			return false, "", "timed out", false
		}
		var exitErr *exec.ExitError
		if errors.Is(err, exec.ErrNotFound) {
			return false, "", "not found on PATH", false
		} else if errors.As(err, &exitErr) {
			return false, "", fmt.Sprintf("exited with status %d", exitErr.ExitCode()), false
		}
		return false, "", err.Error(), false
	}
	return true, strings.TrimSpace(string(out)), "", false
}

func buildCapabilities(config ExecJSONAdapterConfig) map[string]interface{} {
	adapterLabel := "builtin"
	if strings.TrimSpace(config.Command) != "" {
		adapterLabel = "exec-json"
	}
	capabilities := map[string]interface{}{
		"adapter":           adapterLabel,
		"connector_version": Version,
	}
	if strings.TrimSpace(config.Command) != "" {
		capabilities["adapter_command"] = filepathBase(config.Command)
	}
	if strings.TrimSpace(config.WorkingDir) != "" {
		capabilities["working_dir"] = config.WorkingDir
	}
	if config.TimeoutSeconds > 0 {
		capabilities["timeout_seconds"] = config.TimeoutSeconds
	}
	if config.MaxOutputBytes > 0 {
		capabilities["max_output_bytes"] = config.MaxOutputBytes
	}
	return capabilities
}

func filepathBase(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// ─── P4-4 probe pipeline ────────────────────────────────────────────────────

// enqueuePendingProbesFromMetadata reads pending_cli_probe_requests out of the
// heartbeat response's connector.metadata blob, dedups against probes that are
// already queued or in-flight, and wakes the probe worker.
func (s *Service) enqueuePendingProbesFromMetadata(connector *models.LocalConnector) {
	if connector == nil || connector.Metadata == nil {
		return
	}
	raw, ok := connector.Metadata["pending_cli_probe_requests"]
	if !ok {
		return
	}
	pending, ok := decodePendingProbes(raw)
	if !ok || len(pending) == 0 {
		return
	}
	s.probeWorkerMu.Lock()
	if s.pendingProbeIDs == nil {
		s.pendingProbeIDs = map[string]bool{}
	}
	added := 0
	for _, p := range pending {
		if p.ProbeID == "" || s.pendingProbeIDs[p.ProbeID] {
			continue
		}
		s.pendingProbeIDs[p.ProbeID] = true
		s.probeQueue = append(s.probeQueue, p)
		added++
	}
	s.probeWorkerMu.Unlock()
	if added > 0 {
		select {
		case s.probeWorkerWake <- struct{}{}:
		default:
		}
	}
}

// ensureProbeWorker starts the background probe runner once. Subsequent Run()
// invocations are a no-op. The worker exits when ctx is cancelled.
func (s *Service) ensureProbeWorker(ctx context.Context) {
	s.probeWorkerOnce.Do(func() {
		s.probeWorkerWake = make(chan struct{}, 1)
		go s.runProbeWorker(ctx)
	})
}

func (s *Service) runProbeWorker(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		next, ok := s.dequeueNextProbe()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.probeWorkerWake:
				continue
			case <-time.After(5 * time.Second):
				continue
			}
		}
		fmt.Fprintf(s.Stdout, "cli probe [%s] binding=%s model=%s — running…\n", next.ProbeID, next.BindingID, next.ModelID)
		result := ExecuteProbe(ctx, next)
		if result.OK {
			fmt.Fprintf(s.Stdout, "cli probe [%s] ok in %d ms\n", next.ProbeID, result.LatencyMS)
		} else {
			fmt.Fprintf(s.Stderr, "cli probe [%s] failed: %s\n", next.ProbeID, result.ErrorMessage)
		}
		s.mu.Lock()
		s.probeResultsOut = append(s.probeResultsOut, result)
		s.mu.Unlock()
		s.probeWorkerMu.Lock()
		delete(s.pendingProbeIDs, next.ProbeID)
		s.probeWorkerMu.Unlock()
	}
}

func (s *Service) dequeueNextProbe() (models.PendingCliProbeRequest, bool) {
	s.probeWorkerMu.Lock()
	defer s.probeWorkerMu.Unlock()
	if len(s.probeQueue) == 0 {
		return models.PendingCliProbeRequest{}, false
	}
	next := s.probeQueue[0]
	s.probeQueue = s.probeQueue[1:]
	return next, true
}

// decodePendingProbes round-trips the untyped metadata value through JSON
// into the typed struct slice, matching the server-side readPendingProbes
// pattern exactly. Returns (nil, false) on any decode failure so the caller
// can log and retry on the next heartbeat without crashing the worker loop.
func decodePendingProbes(raw interface{}) ([]models.PendingCliProbeRequest, bool) {
	if raw == nil {
		return nil, false
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var out []models.PendingCliProbeRequest
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil, false
	}
	// Drop any entries missing the fields the worker needs so a malformed
	// row does not loop forever.
	filtered := out[:0]
	for _, p := range out {
		if strings.TrimSpace(p.ProbeID) == "" || strings.TrimSpace(p.BindingID) == "" {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered, true
}
