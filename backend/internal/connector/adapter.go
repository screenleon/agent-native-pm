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

// ConnectorProtocolVersion is the wire-protocol revision this binary
// understands. 1 means "knows how to read cli_binding from claim-next-run
// and pass cli_selection to the adapter on stdin." Bump in lockstep with
// the server-side handler if the wire shape changes (Path B S2).
const ConnectorProtocolVersion = 1

// MaxAdapterStdinBytes caps the marshalled stdin envelope passed to the
// adapter subprocess (256 KiB planning context ceiling + 8 KiB headroom
// for run/requirement/cli_selection). Exceeding the cap returns
// success=false with error_kind=adapter_protocol_error to the server
// rather than launching a doomed subprocess (R5 / R7 mitigation).
const MaxAdapterStdinBytes = 264 * 1024

// AdapterCliSelection mirrors the per-run CLI binding picked at run-create
// time on the server. Sent on adapter stdin under the `cli_selection`
// key. The adapter's documented precedence (D4): cli_selection >
// ANPM_ADAPTER_* env vars > built-in default.
type AdapterCliSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id,omitempty"`
	CliCommand string `json:"cli_command,omitempty"`
}

type ExecJSONInput struct {
	Run                    *models.PlanningRun     `json:"run"`
	Requirement            *models.Requirement     `json:"requirement"`
	Project                *models.Project         `json:"project,omitempty"`
	RequestedMaxCandidates int                     `json:"requested_max_candidates"`
	PlanningContext        *wire.PlanningContextV1 `json:"planning_context,omitempty"`
	// CliSelection is populated by the connector when the claim response
	// carried a `cli_binding` block (Path B S2). Optional; when omitted
	// the adapter falls back to env vars / built-in default per D4.
	CliSelection *AdapterCliSelection `json:"cli_selection,omitempty"`
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
	// Path B S2: refuse to spawn the subprocess when the marshalled
	// envelope exceeds MaxAdapterStdinBytes. This catches misconfigured
	// planning contexts AND defends the OS pipe buffer (R5 / R7
	// mitigation, design §6.3). The error_kind hint is consumed by the
	// S5a remediation catalog ("Update the reference adapter — version
	// mismatch."), which is the right surface even though the actual
	// cause is server-side: the alternative is a partial write that the
	// adapter parses as truncated JSON and reports as adapter_protocol_error
	// anyway, so we just classify it correctly here.
	if len(payload) > MaxAdapterStdinBytes {
		return models.LocalConnectorSubmitRunResultRequest{
			Success:      false,
			ErrorMessage: fmt.Sprintf("adapter stdin envelope %d bytes exceeds %d byte cap (adapter_protocol_error)", len(payload), MaxAdapterStdinBytes),
		}
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
