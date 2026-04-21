package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

type ExecJSONInput struct {
	Run                    *models.PlanningRun     `json:"run"`
	Requirement            *models.Requirement     `json:"requirement"`
	Project                *models.Project         `json:"project,omitempty"`
	RequestedMaxCandidates int                     `json:"requested_max_candidates"`
	PlanningContext        *wire.PlanningContextV1 `json:"planning_context,omitempty"`
}

type ExecJSONOutput struct {
	Candidates   []models.ConnectorBacklogCandidateDraft `json:"candidates"`
	ErrorMessage string                                  `json:"error_message,omitempty"`
	CliInfo      *models.CliUsageInfo                    `json:"cli_info,omitempty"`
}

type limitedBuffer struct {
	max      int64
	overflow bool
	buf      bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 {
		b.max = defaultAdapterMaxOutput
	}
	remaining := b.max - int64(b.buf.Len())
	if remaining <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:int(remaining)])
		b.overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	return strings.TrimSpace(b.buf.String())
}

func ExecuteExecJSON(parent context.Context, config ExecJSONAdapterConfig, input ExecJSONInput) models.LocalConnectorSubmitRunResultRequest {
	normalized, err := config.Normalized()
	if err != nil {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: err.Error()}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("encode exec-json input: %v", err)}
	}
	runContext, cancel := context.WithTimeout(parent, time.Duration(normalized.TimeoutSeconds)*time.Second)
	defer cancel()
	command := exec.CommandContext(runContext, normalized.Command, normalized.Args...)
	command.Dir = normalized.WorkingDir
	command.Stdin = bytes.NewReader(payload)
	stdout := &limitedBuffer{max: normalized.MaxOutputBytes}
	stderr := &limitedBuffer{max: normalized.MaxOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if runContext.Err() == context.DeadlineExceeded {
			return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("adapter timed out after %ds", normalized.TimeoutSeconds)}
		}
		if stdout.overflow || stderr.overflow {
			return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("adapter output exceeded %d bytes", normalized.MaxOutputBytes)}
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("adapter execution failed: %s", message)}
	}
	if stdout.overflow || stderr.overflow {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("adapter output exceeded %d bytes", normalized.MaxOutputBytes)}
	}
	var output ExecJSONOutput
	if err := json.Unmarshal([]byte(stdout.String()), &output); err != nil {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: fmt.Sprintf("decode adapter stdout JSON: %v", err)}
	}
	if strings.TrimSpace(output.ErrorMessage) != "" {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: strings.TrimSpace(output.ErrorMessage)}
	}
	if len(output.Candidates) == 0 {
		return models.LocalConnectorSubmitRunResultRequest{Success: false, ErrorMessage: "adapter returned no backlog candidates"}
	}
	return models.LocalConnectorSubmitRunResultRequest{Success: true, Candidates: output.Candidates, CliInfo: output.CliInfo}
}
