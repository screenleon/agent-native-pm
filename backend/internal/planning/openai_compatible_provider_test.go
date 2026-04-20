package planning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

func TestNewDefaultProviderRegistryRegistersOpenAICompatibleProvider(t *testing.T) {
	registry, err := NewDefaultProviderRegistry(models.PlanningProviderDeterministic, models.PlanningProviderModelDeterministic, OpenAICompatibleProviderConfig{
		Enabled: true,
		BaseURL: "http://127.0.0.1:8080/v1",
		Models:  []string{"gpt-5", "openai/gpt-5-mini"},
		Timeout: 30,
	})
	if err != nil {
		fatalf(t, "new default provider registry: %v", err)
	}
	options := registry.Options()
	if len(options.Providers) != 2 {
		fatalf(t, "expected 2 providers, got %d", len(options.Providers))
	}
	_, selection, err := registry.Resolve(models.CreatePlanningRunRequest{ProviderID: models.PlanningProviderOpenAICompatible, ModelID: "gpt-5"})
	if err != nil {
		fatalf(t, "resolve openai-compatible provider: %v", err)
	}
	if selection.ProviderID != models.PlanningProviderOpenAICompatible || selection.ModelID != "gpt-5" {
		fatalf(t, "unexpected selection: %+v", selection)
	}
}

func TestNewDefaultProviderRegistryFailsFastForMisconfiguredDefaultProvider(t *testing.T) {
	_, err := NewDefaultProviderRegistry(models.PlanningProviderOpenAICompatible, "gpt-5", OpenAICompatibleProviderConfig{})
	if err == nil {
		t.Fatal("expected registry init to fail when openai-compatible is selected but disabled")
	}
}

func TestOpenAICompatibleProviderGenerateSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != openAICompatibleChatCompletionsPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header %q", got)
		}
		var payload openAICompatibleChatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "gpt-5" {
			t.Fatalf("expected model gpt-5, got %s", payload.Model)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"candidates":[{"suggestion_type":"implementation","title":"Improve sync recovery UX","description":"Add a first shippable recovery slice.","rationale":"Primary implementation slice."},{"suggestion_type":"validation","title":"Validate sync recovery apply safeguards","description":"Protect review and apply flow.","rationale":"Validation should follow implementation."}]}`,
				},
			}},
		})
	}))
	defer server.Close()

	provider := &OpenAICompatibleProvider{
		baseURL:          server.URL,
		apiKey:           "test-key",
		httpClient:       &http.Client{Timeout: 2 * time.Second},
		maxCandidates:    3,
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
	}

	drafts, err := provider.Generate(context.Background(), &models.Requirement{
		ProjectID:   "project-1",
		Title:       "Improve sync recovery UX",
		Summary:     "Expose recovery options before creating tasks",
		Description: "Users should be able to inspect draft backlog candidates before apply.",
	}, PlanningContext{
		OpenTasks:       []models.Task{{ID: "task-1", Title: "Existing sync integration", Status: "todo"}},
		RecentDocuments: []models.Document{{ID: "doc-1", Title: "Sync Recovery Guide", DocType: "guide", IsStale: true}},
		LatestSyncRun:   &models.SyncRun{ID: "sync-1", Status: "failed", ErrorMessage: "unknown revision"},
	}, models.PlanningProviderSelection{ProviderID: models.PlanningProviderOpenAICompatible, ModelID: "gpt-5"})
	if err != nil {
		fatalf(t, "generate drafts: %v", err)
	}
	if len(drafts) != 2 {
		fatalf(t, "expected 2 drafts, got %d", len(drafts))
	}
	if drafts[0].PriorityScore <= 0 || drafts[0].Confidence <= 0 {
		fatalf(t, "expected server-owned scoring, got score=%v confidence=%v", drafts[0].PriorityScore, drafts[0].Confidence)
	}
	if len(drafts[0].EvidenceDetail.Summary) == 0 {
		fatalf(t, "expected evidence detail summary to be populated")
	}
}

func TestOpenAICompatibleProviderGenerateRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": `not-json`},
			}},
		})
	}))
	defer server.Close()

	provider := &OpenAICompatibleProvider{
		baseURL:          server.URL,
		httpClient:       &http.Client{Timeout: 2 * time.Second},
		maxCandidates:    3,
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
	}
	_, err := provider.Generate(context.Background(), &models.Requirement{ProjectID: "project-1", Title: "Improve sync recovery UX"}, PlanningContext{}, models.PlanningProviderSelection{ProviderID: models.PlanningProviderOpenAICompatible, ModelID: "gpt-5"})
	if err == nil || !strings.Contains(err.Error(), "parse openai-compatible planning JSON") {
		fatalf(t, "expected malformed JSON error, got %v", err)
	}
}

func TestOpenAICompatibleProviderGenerateHandlesUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "gateway unavailable"},
		})
	}))
	defer server.Close()

	provider := &OpenAICompatibleProvider{
		baseURL:          server.URL,
		httpClient:       &http.Client{Timeout: 2 * time.Second},
		maxCandidates:    3,
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
	}
	_, err := provider.Generate(context.Background(), &models.Requirement{ProjectID: "project-1", Title: "Improve sync recovery UX"}, PlanningContext{}, models.PlanningProviderSelection{ProviderID: models.PlanningProviderOpenAICompatible, ModelID: "gpt-5"})
	if err == nil || !strings.Contains(err.Error(), "gateway unavailable") {
		fatalf(t, "expected upstream error, got %v", err)
	}
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
