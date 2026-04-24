package connector

import (
	"context"
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
// probed with `<cmd> --version` as a health check. These are generic language
// runtimes whose `--version` output does not reflect whether the intended CLI
// agent is installed and working correctly (T-S5b-5 / R12 mitigation).
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
	// Zero means use the default (5 minutes). Set to a negative value or
	// enable CliHealthDisabled to disable probing entirely.
	CliHealthInterval time.Duration
	// CliHealthDisabled suppresses the health probe loop entirely.
	CliHealthDisabled bool
	Stdout            io.Writer
	Stderr            io.Writer

	// knownCliBindings tracks binding_id → (command, last_probe_time).
	// Populated lazily from ClaimNextRun responses.
	mu               sync.Mutex
	knownCliBindings map[string]knownCliBinding
}

type knownCliBinding struct {
	command     string
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

	lastError := ""
	nextHeartbeat := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(nextHeartbeat) {
			hbReq := models.LocalConnectorHeartbeatRequest{
				Capabilities: buildCapabilities(s.State.Adapter),
				LastError:    lastError,
			}
			if !s.CliHealthDisabled {
				hbReq.CliHealth = s.collectDueProbes(ctx, cliHealthInterval)
			}
			connector, err := s.Client.Heartbeat(ctx, hbReq)
			if err != nil {
				lastError = err.Error()
				fmt.Fprintf(s.Stderr, "heartbeat failed: %s\n", lastError)
			} else {
				lastError = ""
				fmt.Fprintf(s.Stdout, "connector online: %s (%s)\n", connector.Label, connector.Status)
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
		// Track this binding so the health probe loop can probe it at the
		// next heartbeat cycle (Path B S5b).
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

// trackCliBinding registers a binding_id → command mapping. If the command
// is empty (binding used the PATH-resolved default), we still record an entry
// so we can probe it later once the effective command is known.
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
	// Update the command if it changed (e.g. binding was patched between runs).
	if existing.command != command {
		s.knownCliBindings[bindingID] = knownCliBinding{command: command, lastProbedAt: existing.lastProbedAt}
	}
}

// collectDueProbes runs the CLI health probe for every known binding whose
// last probe is older than the given interval. Returns entries for all probed
// bindings (the heartbeat handler merges them into the connector's metadata).
func (s *Service) collectDueProbes(ctx context.Context, interval time.Duration) []models.LocalConnectorHeartbeatCliHealth {
	s.mu.Lock()
	due := make(map[string]knownCliBinding)
	for id, b := range s.knownCliBindings {
		if time.Since(b.lastProbedAt) >= interval {
			due[id] = b
		}
	}
	s.mu.Unlock()

	if len(due) == 0 {
		return nil
	}

	var results []models.LocalConnectorHeartbeatCliHealth
	for id, b := range due {
		entry := s.probeCliHealth(ctx, b.command)
		entry.BindingID = id
		results = append(results, entry)
		s.mu.Lock()
		if existing, ok := s.knownCliBindings[id]; ok {
			s.knownCliBindings[id] = knownCliBinding{command: existing.command, lastProbedAt: time.Now()}
		}
		s.mu.Unlock()
	}
	return results
}

// probeCliHealth runs `<command> --version` and classifies the result.
// Commands on the interpreter blocklist are skipped with status "unknown".
func (s *Service) probeCliHealth(ctx context.Context, command string) models.LocalConnectorHeartbeatCliHealth {
	now := time.Now().UTC()
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return models.LocalConnectorHeartbeatCliHealth{
			Status:            "unknown",
			ProbeErrorMessage: "cli_command not configured",
			CheckedAt:         now,
		}
	}
	// Blocklist: skip interpreters whose --version output doesn't reflect CLI health.
	base := strings.ToLower(filepath.Base(cmd))
	// Strip extension (e.g. python3.exe on Windows).
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	if cliInterpreterBlocklist[base] {
		return models.LocalConnectorHeartbeatCliHealth{
			Status:            "unknown",
			ProbeErrorMessage: "skipped: interpreter command",
			CheckedAt:         now,
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, cmd, "--version").CombinedOutput()
	if err != nil {
		status := "cli_not_found"
		if probeCtx.Err() != nil {
			status = "cli_timeout"
		}
		return models.LocalConnectorHeartbeatCliHealth{
			Status:            status,
			ProbeErrorMessage: err.Error(),
			CheckedAt:         now,
		}
	}
	return models.LocalConnectorHeartbeatCliHealth{
		Status:        "healthy",
		VersionString: strings.TrimSpace(string(out)),
		CheckedAt:     now,
	}
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
