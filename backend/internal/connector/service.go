package connector

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type Service struct {
	Client            *Client
	State             *State
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
	Stdout            io.Writer
	Stderr            io.Writer
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
	lastError := ""
	nextHeartbeat := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(nextHeartbeat) {
			connector, err := s.Client.Heartbeat(ctx, models.LocalConnectorHeartbeatRequest{
				Capabilities: buildCapabilities(s.State.Adapter),
				LastError:    lastError,
			})
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

func buildCapabilities(config ExecJSONAdapterConfig) map[string]interface{} {
	capabilities := map[string]interface{}{
		"adapter":           "exec-json",
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
