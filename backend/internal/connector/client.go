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