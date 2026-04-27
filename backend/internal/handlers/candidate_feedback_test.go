package handlers_test

// Phase 3B PR-3: handler-level tests for feedback_kind validation on
// PATCH /api/backlog-candidates/:id.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/audit"
	"github.com/screenleon/agent-native-pm/internal/models"
)

// TestPatchCandidate_InvalidFeedbackKind_Returns400 verifies that the handler
// rejects an unrecognised feedback_kind with HTTP 400 before touching the
// store.
func TestPatchCandidate_InvalidFeedbackKind_Returns400(t *testing.T) {
	fx := newApplyFixture(t)
	c := fx.seedApprovedCandidate(t, "")

	body, _ := json.Marshal(map[string]string{"feedback_kind": "not_a_valid_kind"})
	req := httptest.NewRequest(http.MethodPatch, "/api/backlog-candidates/"+c.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid feedback_kind, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestPatchCandidate_ValidFeedbackKind_Returns200 verifies that a valid
// approved feedback_kind is accepted.
func TestPatchCandidate_ValidFeedbackKind_Returns200(t *testing.T) {
	fx := newApplyFixture(t)
	c := fx.seedApprovedCandidate(t, "")

	body, _ := json.Marshal(map[string]string{"feedback_kind": "good_fit", "feedback_note": "nice"})
	req := httptest.NewRequest(http.MethodPatch, "/api/backlog-candidates/"+c.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for valid feedback_kind, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data models.BacklogCandidate `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.FeedbackKind != "good_fit" {
		t.Errorf("want feedback_kind 'good_fit', got %q", resp.Data.FeedbackKind)
	}
}

// TestPatchCandidate_EmptyFeedbackKind_Returns200 verifies that an empty
// feedback_kind is accepted (feedback is optional).
func TestPatchCandidate_EmptyFeedbackKind_Returns200(t *testing.T) {
	fx := newApplyFixture(t)
	c := fx.seedApprovedCandidate(t, "")

	// First, set a kind.
	goodFit := "good_fit"
	if _, err := fx.candidates.Update(c.ID, models.UpdateBacklogCandidateRequest{FeedbackKind: &goodFit}, audit.ActorInfo{}); err != nil {
		t.Fatalf("set feedback kind: %v", err)
	}

	// Now clear it via PATCH with empty string.
	empty := ""
	body, _ := json.Marshal(map[string]*string{"feedback_kind": &empty})
	req := httptest.NewRequest(http.MethodPatch, "/api/backlog-candidates/"+c.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for empty feedback_kind, got %d: %s", rr.Code, rr.Body.String())
	}
}
