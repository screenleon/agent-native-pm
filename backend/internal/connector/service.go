package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
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

func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	claim, err := s.Client.ClaimNextRun(ctx)
	if err != nil {
		return false, err
	}
	if claim == nil || claim.Run == nil || claim.Requirement == nil {
		return false, nil
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
	var result models.LocalConnectorSubmitRunResultRequest
	if strings.TrimSpace(s.State.Adapter.Command) == "" {
		result = ExecuteBuiltin(ctx, execInput)
	} else {
		result = ExecuteExecJSON(ctx, s.State.Adapter, execInput)
	}
	if _, err := s.Client.SubmitRunResult(ctx, claim.Run.ID, result); err != nil {
		return true, fmt.Errorf("submit run result for %s: %w", claim.Run.ID, err)
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
