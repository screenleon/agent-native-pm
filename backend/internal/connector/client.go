package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type Client struct {
	BaseURL        string
	HTTPClient     *http.Client
	ConnectorToken string
	UserAgent      string
}

type apiEnvelope[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error"`
}

func NewClient(baseURL, connectorToken string) *Client {
	return &Client{
		BaseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTPClient:     http.DefaultClient,
		ConnectorToken: strings.TrimSpace(connectorToken),
		UserAgent:      "anpm-connector/" + Version,
	}
}

func (c *Client) Pair(ctx context.Context, req models.PairLocalConnectorRequest) (*models.PairLocalConnectorResponse, error) {
	var response models.PairLocalConnectorResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/connector/pair", "", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) Heartbeat(ctx context.Context, req models.LocalConnectorHeartbeatRequest) (*models.LocalConnector, error) {
	var connector models.LocalConnector
	if err := c.doJSON(ctx, http.MethodPost, "/api/connector/heartbeat", c.ConnectorToken, req, &connector); err != nil {
		return nil, err
	}
	return &connector, nil
}

func (c *Client) ClaimNextRun(ctx context.Context) (*models.LocalConnectorClaimNextRunResponse, error) {
	var response models.LocalConnectorClaimNextRunResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/connector/claim-next-run", c.ConnectorToken, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) SubmitRunResult(ctx context.Context, planningRunID string, req models.LocalConnectorSubmitRunResultRequest) (*models.PlanningRun, error) {
	var run models.PlanningRun
	path := "/api/connector/planning-runs/" + strings.TrimSpace(planningRunID) + "/result"
	if err := c.doJSON(ctx, http.MethodPost, path, c.ConnectorToken, req, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ClaimNextTask calls POST /api/connector/claim-next-task and returns the
// next queued role_dispatch task for this connector's user, or nil when the
// queue is empty. Phase 6b.
func (c *Client) ClaimNextTask(ctx context.Context) (*ClaimNextTaskResponse, error) {
	var resp ClaimNextTaskResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/connector/claim-next-task", c.ConnectorToken, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SubmitTaskResult calls POST /api/connector/tasks/:task_id/execution-result.
// Phase 6b.
func (c *Client) SubmitTaskResult(ctx context.Context, taskID string, req SubmitTaskResultRequest) error {
	path := "/api/connector/tasks/" + strings.TrimSpace(taskID) + "/execution-result"
	return c.doJSON(ctx, http.MethodPost, path, c.ConnectorToken, req, nil)
}

// ReportActivity calls POST /api/connector/activity with the current activity
// snapshot. Phase 6c PR-4.
func (c *Client) ReportActivity(ctx context.Context, a models.ConnectorActivity) error {
	return c.doJSON(ctx, http.MethodPost, "/api/connector/activity", c.ConnectorToken, a, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path, connectorToken string, requestBody any, responseBody any) error {
	if c == nil {
		return fmt.Errorf("connector client is required")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("connector server URL is required")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	var bodyReader io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.UserAgent)
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(connectorToken) != "" {
		request.Header.Set("X-Connector-Token", connectorToken)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer response.Body.Close()
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return decodeAPIError(response.StatusCode, rawResponse)
	}
	if responseBody == nil {
		return nil
	}
	wrapped := apiEnvelope[json.RawMessage]{}
	if err := json.Unmarshal(rawResponse, &wrapped); err != nil {
		return fmt.Errorf("decode response envelope: %w", err)
	}
	if wrapped.Error != "" {
		return fmt.Errorf("%s", wrapped.Error)
	}
	if len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
		return nil
	}
	// Decoder discipline (Path B S2, design §8 T-S2-8): we deliberately
	// use json.Unmarshal here, NOT json.Decoder.DisallowUnknownFields().
	// Unknown future fields in the server's response (e.g. a v3 cli_binding
	// extension that today's connector doesn't understand) MUST be ignored
	// silently so a server upgrade does not strand older connector binaries.
	// If you ever change this to a strict decoder, you'll break backwards
	// compatibility — add a versioned response shape first.
	if err := json.Unmarshal(wrapped.Data, responseBody); err != nil {
		return fmt.Errorf("decode response payload: %w", err)
	}
	return nil
}

func decodeAPIError(statusCode int, raw []byte) error {
	wrapped := apiEnvelope[json.RawMessage]{}
	if err := json.Unmarshal(raw, &wrapped); err == nil && strings.TrimSpace(wrapped.Error) != "" {
		return fmt.Errorf("request failed (%d): %s", statusCode, strings.TrimSpace(wrapped.Error))
	}
	message := strings.TrimSpace(string(raw))
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return fmt.Errorf("request failed (%d): %s", statusCode, message)
}