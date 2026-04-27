package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// ContextSnapshotGetter is the minimal store interface required by the
// GetContextSnapshot handler. Satisfied by *store.ContextSnapshotStore.
type ContextSnapshotGetter interface {
	GetByRunID(planningRunID string) (*store.ContextSnapshot, error)
}

// ContextSnapshotResponse is the structured response for
// GET /api/planning-runs/:id/context-snapshot.
// When Available is false all other fields are zero/empty.
type ContextSnapshotResponse struct {
	PackID        string          `json:"pack_id"`
	PlanningRunID string          `json:"planning_run_id"`
	SchemaVersion string          `json:"schema_version"`
	SourcesBytes  int             `json:"sources_bytes"`
	DroppedCounts map[string]int  `json:"dropped_counts"`
	OpenTaskCount int             `json:"open_task_count"`
	DocumentCount int             `json:"document_count"`
	DriftCount    int             `json:"drift_count"`
	AgentRunCount int             `json:"agent_run_count"`
	HasSyncRun    bool            `json:"has_sync_run"`
	// V2 envelope fields — populated when schema_version == "context.v2".
	Role          string          `json:"role,omitempty"`
	IntentMode    string          `json:"intent_mode,omitempty"`
	TaskScale     string          `json:"task_scale,omitempty"`
	SourceOfTruth []wire.SourceRef `json:"source_of_truth,omitempty"`
	// Available is false for runs that predate Phase 3B snapshot saving.
	Available bool `json:"available"`
}

// GetContextSnapshot handles GET /api/planning-runs/:id/context-snapshot.
//
// Query param ?raw=1 returns the raw JSON snapshot blob as the data payload.
// Default returns a structured ContextSnapshotResponse.
//
// Auth: uses the same project-member pattern as PlanningRunHandler.Get —
// look up the run to get the project ID, then check requestAllowsProject.
//
// Runs that predate snapshot saving return 200 with {available: false}.
// Nonexistent runs return 404.
func (h *PlanningRunHandler) GetContextSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get planning run")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "planning run not found")
		return
	}
	if !requestAllowsProject(r, run.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	if h.contextSnapshotStore == nil {
		// Store not wired (e.g. tests that don't need snapshot support).
		writeSuccess(w, http.StatusOK, ContextSnapshotResponse{Available: false}, nil)
		return
	}

	snap, err := h.contextSnapshotStore.GetByRunID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load context snapshot")
		return
	}
	if snap == nil {
		writeSuccess(w, http.StatusOK, ContextSnapshotResponse{Available: false}, nil)
		return
	}

	// ?raw=1 — return the raw JSON blob directly.
	if r.URL.Query().Get("raw") == "1" {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(snap.Snapshot), &raw); err != nil {
			log.Printf("context snapshot: unmarshal raw failed for run %s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "failed to parse context snapshot")
			return
		}
		writeSuccess(w, http.StatusOK, raw, nil)
		return
	}

	// Default: structured response.
	resp := buildContextSnapshotResponse(snap)
	writeSuccess(w, http.StatusOK, resp, nil)
}

// buildContextSnapshotResponse parses the stored snapshot JSON and populates
// the structured response. Unknown or malformed snapshot data results in a
// partially populated response (Available=true but counts may be zero).
func buildContextSnapshotResponse(snap *store.ContextSnapshot) ContextSnapshotResponse {
	resp := ContextSnapshotResponse{
		PackID:        snap.PackID,
		PlanningRunID: snap.PlanningRunID,
		SchemaVersion: snap.SchemaVersion,
		SourcesBytes:  snap.SourcesBytes,
		Available:     true,
	}

	// Parse dropped_counts.
	if snap.DroppedCounts != "" && snap.DroppedCounts != "{}" {
		var dc map[string]int
		if err := json.Unmarshal([]byte(snap.DroppedCounts), &dc); err == nil {
			resp.DroppedCounts = dc
		}
	}
	if resp.DroppedCounts == nil {
		resp.DroppedCounts = map[string]int{}
	}

	// Parse the V2 snapshot for counts and envelope fields.
	if snap.Snapshot == "" {
		return resp
	}

	var v2 wire.PlanningContextV2
	if err := json.Unmarshal([]byte(snap.Snapshot), &v2); err != nil {
		log.Printf("context snapshot: unmarshal V2 failed for run %s: %v", snap.PlanningRunID, err)
		return resp
	}

	// Source counts from V2 sources.
	resp.OpenTaskCount = len(v2.Sources.OpenTasks)
	resp.DocumentCount = len(v2.Sources.RecentDocuments)
	resp.DriftCount = len(v2.Sources.OpenDriftSignals)
	resp.AgentRunCount = len(v2.Sources.RecentAgentRuns)
	resp.HasSyncRun = v2.Sources.LatestSyncRun != nil

	// V2 envelope fields.
	resp.Role = v2.Role
	resp.IntentMode = string(v2.IntentMode)
	resp.TaskScale = string(v2.TaskScale)
	resp.SourceOfTruth = v2.SourceOfTruth

	return resp
}
