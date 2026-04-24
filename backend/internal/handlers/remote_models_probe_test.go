package handlers_test

// Test matrix for Phase 3-A-1 — Probe result persistence (migration 024).
// Each test is named to match the design DoD ID.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// fakeLLMServer returns a minimal OpenAI-compatible chat completion response
// for any POST /chat/completions request.
func fakeLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "test-model",
			"choices": [{"message": {"content": "ok"}}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 1}
		}`))
	}))
}

// probeFixture wires AccountBindingHandler + RemoteModelsHandler into one
// router so T-3A1-7 and T-3A1-8 can round-trip through both endpoints.
type probeFixture struct {
	srv    http.Handler
	bs     *store.AccountBindingStore
	userID string
}

func newProbeFixture(t *testing.T) probeFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('local-admin', 'local', 'local@example.com', '', 'admin', TRUE)
	`); err != nil {
		t.Fatalf("seed local-admin: %v", err)
	}
	bs := store.NewAccountBindingStore(db, nil)
	abh := handlers.NewAccountBindingHandler(bs).WithLocalMode(true)
	rmh := handlers.NewRemoteModelsHandler(bs)

	srv := router.New(router.Deps{
		AccountBindingHandler: abh,
		RemoteModelsHandler:   rmh,
		LocalModeMiddleware:   middleware.InjectLocalAdmin,
	})
	return probeFixture{srv: srv, bs: bs, userID: "local-admin"}
}

// createAPIBinding posts a new openai-compatible binding and returns its id.
func createAPIBinding(t *testing.T, srv http.Handler, baseURL string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"provider_id": "openai-compatible",
		"label":       "test",
		"base_url":    baseURL,
		"model_id":    "test-model",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/me/account-bindings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create binding: want 201, got %d — %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data models.AccountBinding `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp.Data.ID
}

// T-3A1-4: RecordProbe with valid (id, userID) writes all three columns.
func Test3A1_4RecordProbeWritesColumns(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('u1', 'u', 'u@e.com', '', 'admin', TRUE)
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	bs := store.NewAccountBindingStore(db, nil)
	binding, err := bs.Create("u1", models.CreateAccountBindingRequest{
		ProviderID: "openai-compatible",
		Label:      "test",
		BaseURL:    "http://localhost:11434/v1",
		ModelID:    "llama3",
	})
	if err != nil {
		t.Fatalf("create binding: %v", err)
	}

	if err := bs.RecordProbe(binding.ID, "u1", true, 42); err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}

	bindings, err := bs.ListByUser("u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	b := bindings[0]
	if b.LastProbeAt == nil {
		t.Fatal("want last_probe_at set, got nil")
	}
	if b.LastProbeOk == nil || !*b.LastProbeOk {
		t.Fatalf("want last_probe_ok=true, got %v", b.LastProbeOk)
	}
	if b.LastProbeMs == nil || *b.LastProbeMs != 42 {
		t.Fatalf("want last_probe_ms=42, got %v", b.LastProbeMs)
	}
}

// T-3A1-5: RecordProbe with unknown id or wrong userID is silent (no error).
func Test3A1_5RecordProbeUnknownIDNoError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	bs := store.NewAccountBindingStore(db, nil)

	// Unknown binding id — must not return an error.
	if err := bs.RecordProbe("does-not-exist", "user-x", false, 10); err != nil {
		t.Fatalf("want nil error for unknown binding, got: %v", err)
	}
}

// T-3A1-6: ListByUser returns nil probe columns when binding was never probed.
func Test3A1_6ListUserNullProbeColumns(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('u2', 'u2', 'u2@e.com', '', 'admin', TRUE)
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	bs := store.NewAccountBindingStore(db, nil)
	if _, err := bs.Create("u2", models.CreateAccountBindingRequest{
		ProviderID: "openai-compatible",
		Label:      "never-probed",
		BaseURL:    "http://localhost:11434/v1",
		ModelID:    "llama3",
	}); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bindings, err := bs.ListByUser("u2")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	b := bindings[0]
	if b.LastProbeAt != nil || b.LastProbeOk != nil || b.LastProbeMs != nil {
		t.Fatalf("want all probe columns nil for unprobed binding, got at=%v ok=%v ms=%v",
			b.LastProbeAt, b.LastProbeOk, b.LastProbeMs)
	}
}

// T-3A1-7: POST /api/me/probe-model with binding_id persists probe result.
// Uses a fake LLM server so the probe HTTP call succeeds without a real provider.
func Test3A1_7ProbeWithBindingIDPersists(t *testing.T) {
	llm := fakeLLMServer(t)
	defer llm.Close()

	f := newProbeFixture(t)
	bindingID := createAPIBinding(t, f.srv, llm.URL+"/v1")

	// POST probe-model referencing the binding.
	probeBody, _ := json.Marshal(map[string]interface{}{
		"binding_id": bindingID,
		"model_id":   "test-model",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/me/probe-model", bytes.NewReader(probeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("probe-model: want 200, got %d — %s", w.Code, w.Body.String())
	}
	var probeResp struct {
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&probeResp); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if !probeResp.Data.OK {
		t.Fatal("want probe ok=true")
	}

	// GET account-bindings and verify probe columns are populated.
	req2 := httptest.NewRequest(http.MethodGet, "/api/me/account-bindings", nil)
	w2 := httptest.NewRecorder()
	f.srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list bindings: want 200, got %d", w2.Code)
	}
	var listResp struct {
		Data []models.AccountBinding `json:"data"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Data) == 0 {
		t.Fatal("want at least one binding")
	}
	b := listResp.Data[0]
	if b.LastProbeAt == nil {
		t.Fatal("want last_probe_at set after probe, got nil")
	}
	if b.LastProbeOk == nil || !*b.LastProbeOk {
		t.Fatalf("want last_probe_ok=true, got %v", b.LastProbeOk)
	}
	if b.LastProbeMs == nil {
		t.Fatal("want last_probe_ms set, got nil")
	}
}

// T-3A1-8: POST /api/me/probe-model without binding_id does not touch any binding row.
func Test3A1_8ProbeWithoutBindingIDNoPersist(t *testing.T) {
	llm := fakeLLMServer(t)
	defer llm.Close()

	f := newProbeFixture(t)
	// Create a binding so we can verify it was untouched afterwards.
	createAPIBinding(t, f.srv, "http://localhost:11434/v1")

	// POST probe-model with inline credentials (no binding_id).
	probeBody, _ := json.Marshal(map[string]interface{}{
		"base_url": llm.URL + "/v1",
		"model_id": "test-model",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/me/probe-model", bytes.NewReader(probeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("probe-model: want 200, got %d — %s", w.Code, w.Body.String())
	}

	// Verify that no binding row has probe columns populated.
	bindings, err := f.bs.ListByUser(f.userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	for _, b := range bindings {
		if b.LastProbeAt != nil || b.LastProbeOk != nil || b.LastProbeMs != nil {
			t.Fatalf("probe without binding_id must not write to any binding row; got at=%v ok=%v ms=%v",
				b.LastProbeAt, b.LastProbeOk, b.LastProbeMs)
		}
	}
}
