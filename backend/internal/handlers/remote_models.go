package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// maxProviderResponseBytes caps the response body read from any provider
// endpoint. Matches the inbound-request cap used by the planning layer.
const maxProviderResponseBytes = 1 << 20 // 1 MiB

// RemoteModelsHandler proxies model-discovery and connection-probe requests
// to any OpenAI-compatible endpoint on behalf of the frontend.
type RemoteModelsHandler struct {
	fetchClient *http.Client // short timeout — models list should be fast
	probeClient *http.Client // longer timeout — chat completion may be slow
	bindingStore *store.AccountBindingStore
}

func NewRemoteModelsHandler(bindingStore *store.AccountBindingStore) *RemoteModelsHandler {
	return &RemoteModelsHandler{
		fetchClient:  &http.Client{Timeout: 10 * time.Second},
		probeClient:  &http.Client{Timeout: 45 * time.Second},
		bindingStore: bindingStore,
	}
}

// ── Fetch models ──────────────────────────────────────────────────────────────

type remoteModelsRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type remoteModelsResponse struct {
	Models []string `json:"models"`
}

type openAIModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Fetch handles POST /api/me/remote-models.
func (h *RemoteModelsHandler) Fetch(w http.ResponseWriter, r *http.Request) {
	var req remoteModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	baseURL, err := validateBaseURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	httpReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL+"/models", nil)
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := h.fetchClient.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("provider unreachable: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("provider returned HTTP %d", resp.StatusCode))
		return
	}

	var list openAIModelList
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProviderResponseBytes)).Decode(&list); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse provider response")
		return
	}

	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID != "" && !isNonChatModel(m.ID) {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)

	writeSuccess(w, http.StatusOK, remoteModelsResponse{Models: ids}, nil)
}

// ── Probe model ───────────────────────────────────────────────────────────────

type probeModelRequest struct {
	BaseURL   string  `json:"base_url"`
	APIKey    string  `json:"api_key"`
	ModelID   string  `json:"model_id"`
	BindingID *string `json:"binding_id,omitempty"`
}

type probeUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type probeModelResponse struct {
	OK        bool        `json:"ok"`
	LatencyMS int64       `json:"latency_ms"`
	ModelUsed string      `json:"model_used"`
	Content   string      `json:"content"`
	Error     string      `json:"error,omitempty"`
	Usage     *probeUsage `json:"usage,omitempty"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Probe handles POST /api/me/probe-model.
// It sends a minimal chat completion request and returns a structured report.
// If binding_id is provided it resolves base_url and api_key from the stored
// (encrypted) binding; otherwise base_url and api_key must be in the body.
func (h *RemoteModelsHandler) Probe(w http.ResponseWriter, r *http.Request) {
	var req probeModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve credentials from stored binding if binding_id given.
	if req.BindingID != nil && *req.BindingID != "" {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		b, err := h.bindingStore.GetByID(*req.BindingID, user.ID)
		if err != nil || b == nil {
			writeError(w, http.StatusNotFound, "binding not found")
			return
		}
		if req.BaseURL == "" {
			req.BaseURL = b.BaseURL
		}
		if req.ModelID == "" {
			req.ModelID = b.ModelID
		}
		if b.APIKeyCiphertext != "" {
			plainKey, err := h.bindingStore.DecryptAPIKey(b.APIKeyCiphertext)
			if err == nil {
				req.APIKey = plainKey
			}
		}
	}

	if req.ModelID == "" {
		writeError(w, http.StatusBadRequest, "model_id is required")
		return
	}

	baseURL, err := validateBaseURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	body, _ := json.Marshal(chatRequest{
		Model:     req.ModelID,
		Messages:  []chatMessage{{Role: "user", Content: "Respond with exactly the word: ok"}},
		MaxTokens: 10,
		Stream:    false,
	})

	httpReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost,
		baseURL+"/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	start := time.Now()
	resp, err := h.probeClient.Do(httpReq)
	latencyMS := time.Since(start).Milliseconds()

	if err != nil {
		writeSuccess(w, http.StatusOK, probeModelResponse{
			OK: false, LatencyMS: latencyMS,
			Error: fmt.Sprintf("provider unreachable: %v", err),
		}, nil)
		return
	}
	defer resp.Body.Close()

	var chat chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProviderResponseBytes)).Decode(&chat); err != nil {
		writeSuccess(w, http.StatusOK, probeModelResponse{
			OK: false, LatencyMS: latencyMS,
			Error: fmt.Sprintf("HTTP %d — could not parse response", resp.StatusCode),
		}, nil)
		return
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if chat.Error != nil && chat.Error.Message != "" {
			errMsg += " — " + chat.Error.Message
		}
		writeSuccess(w, http.StatusOK, probeModelResponse{
			OK: false, LatencyMS: latencyMS, Error: errMsg,
		}, nil)
		return
	}

	content := ""
	if len(chat.Choices) > 0 {
		content = strings.TrimSpace(chat.Choices[0].Message.Content)
	}
	modelUsed := chat.Model
	if modelUsed == "" {
		modelUsed = req.ModelID
	}

	result := probeModelResponse{
		OK:        true,
		LatencyMS: latencyMS,
		ModelUsed: modelUsed,
		Content:   content,
	}
	if chat.Usage != nil {
		result.Usage = &probeUsage{
			PromptTokens:     chat.Usage.PromptTokens,
			CompletionTokens: chat.Usage.CompletionTokens,
		}
	}

	writeSuccess(w, http.StatusOK, result, nil)
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func validateBaseURL(raw string) (string, error) {
	cleaned := strings.TrimRight(strings.TrimSpace(raw), "/")
	if cleaned == "" {
		return "", fmt.Errorf("base_url is required")
	}
	u, err := url.Parse(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid base_url")
	}
	if u.Scheme != "https" && !isLocalHost(u.Hostname()) {
		return "", fmt.Errorf("base_url must use https or target a local address")
	}
	return cleaned, nil
}

// isNonChatModel returns true for model IDs that cannot be used for chat
// completions (embeddings, moderation, reranking, etc.).
// Keyword matches use substring only where the keyword is unambiguous; legacy
// OpenAI model families (ada, babbage, curie, davinci) are matched as exact
// word-boundary segments to avoid false-positives on future model names.
func isNonChatModel(id string) bool {
	lower := strings.ToLower(id)
	// Unambiguous substrings safe to match anywhere in the ID.
	for _, kw := range []string{"embed", "moderat", "rerank", "whisper", "tts-", "dall-e"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Legacy OpenAI model families: match as full token between separators
	// to avoid false-positives (e.g. "granada", "arcadia").
	for _, family := range []string{"davinci", "babbage", "ada", "curie"} {
		if lower == family ||
			strings.HasPrefix(lower, family+"-") ||
			strings.HasSuffix(lower, "-"+family) ||
			strings.Contains(lower, "-"+family+"-") {
			return true
		}
	}
	return false
}

func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "host.docker.internal":
		return true
	}
	return false
}
