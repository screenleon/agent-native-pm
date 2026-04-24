package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type LocalConnectorHandler struct {
	store          *store.LocalConnectorStore
	planningRuns   *store.PlanningRunStore
	requirements   *store.RequirementStore
	candidates     *store.BacklogCandidateStore
	agentRuns      *store.AgentRunStore
	projects       *store.ProjectStore
	notifications  *store.NotificationStore
	contextBuilder *planning.ProjectContextBuilder
	// bindings is optional; when set the probe-binding handler can resolve a
	// CLI binding row so the connector receives cli_command + model_id. Wired
	// in main.go via WithAccountBindingStore.
	bindings *store.AccountBindingStore
}

// WithAccountBindingStore allows the probe-binding handler to look up the
// caller's CLI binding. Without it the POST /probe-binding endpoint returns
// 500 rather than silently enqueueing an invalid probe (caller misconfigured
// the server wiring).
func (h *LocalConnectorHandler) WithAccountBindingStore(bindings *store.AccountBindingStore) *LocalConnectorHandler {
	h.bindings = bindings
	return h
}

func NewLocalConnectorHandler(s *store.LocalConnectorStore, planningRuns *store.PlanningRunStore, requirements *store.RequirementStore, candidates *store.BacklogCandidateStore, agentRuns *store.AgentRunStore) *LocalConnectorHandler {
	return &LocalConnectorHandler{store: s, planningRuns: planningRuns, requirements: requirements, candidates: candidates, agentRuns: agentRuns}
}

// WithProjectStore attaches a project store so claim responses include the
// owning project. When nil the field is omitted (multi-project scheduling
// still works because run.project_id is always present).
func (h *LocalConnectorHandler) WithProjectStore(projects *store.ProjectStore) *LocalConnectorHandler {
	h.projects = projects
	return h
}

// WithNotificationStore enables in-app notifications when planning runs reach
// a terminal state. When nil notifications are silently skipped (the run still
// completes successfully).
func (h *LocalConnectorHandler) WithNotificationStore(notifications *store.NotificationStore) *LocalConnectorHandler {
	h.notifications = notifications
	return h
}

// WithContextBuilder attaches a planning context builder so claim responses
// carry a wire.PlanningContextV1 payload. When nil (or if the builder fails),
// the handler returns the claim response without a planning context and the
// claim still succeeds.
func (h *LocalConnectorHandler) WithContextBuilder(builder *planning.ProjectContextBuilder) *LocalConnectorHandler {
	h.contextBuilder = builder
	return h
}

func (h *LocalConnectorHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	connectors, err := h.store.ListByUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list local connectors")
		return
	}
	writeSuccess(w, http.StatusOK, connectors, nil)
}

func (h *LocalConnectorHandler) CreatePairingSession(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req models.CreateLocalConnectorPairingSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.store.CreatePairingSession(user.ID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusCreated, resp, nil)
}

func (h *LocalConnectorHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "connector id is required")
		return
	}
	if err := h.store.Revoke(id, user.ID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}

func (h *LocalConnectorHandler) Pair(w http.ResponseWriter, r *http.Request) {
	var req models.PairLocalConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.store.ClaimPairingSession(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusCreated, resp, nil)
}

func (h *LocalConnectorHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.Header.Get("X-Connector-Token"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "connector token required")
		return
	}
	var req models.LocalConnectorHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		// Strict-by-design (T-S1 / security): a connector sending malformed
		// heartbeat body gets 400 and loses online status. We log the error
		// server-side so an operator can distinguish "connector unreachable"
		// from "connector buggy" when a connector repeatedly drops offline.
		// Token hash is NOT logged (sensitive); the caller IP and decode
		// error are sufficient to triage.
		log.Printf("heartbeat decode failed: remote=%s err=%v", r.RemoteAddr, err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	connector, err := h.store.HeartbeatByToken(token, req)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, connector, nil)
}

func (h *LocalConnectorHandler) ClaimNextRun(w http.ResponseWriter, r *http.Request) {
	connector, ok := h.authenticatedConnector(w, r)
	if !ok {
		return
	}
	// Path B S2: scope leasing by the connector's reported protocol_version
	// so a pre-Path-B connector (version 0) silently skips any queued run
	// that has a non-NULL account_binding_id (R3 mitigation; design §6.2).
	run, err := h.planningRuns.LeaseNextLocalConnectorRunForProtocol(connector.UserID, connector.ID, connector.Label, 10*time.Minute, connector.ProtocolVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim next planning run")
		return
	}
	if run == nil {
		// When the connector is pre-Path-B AND there is at least one CLI-
		// bound run queued, stamp the run with a dispatch_warning flag so
		// an updated connector picks it up immediately on its next claim
		// (and so the UI can surface "waiting for connector update"). This
		// is best-effort — ignore failures.
		if connector.ProtocolVersion < 1 {
			h.markCliBoundRunsAwaitingUpdate(connector.UserID)
		}
		writeSuccess(w, http.StatusOK, models.LocalConnectorClaimNextRunResponse{}, nil)
		return
	}
	requirement, err := h.requirements.GetByID(run.RequirementID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load planning run requirement")
		return
	}
	response := models.LocalConnectorClaimNextRunResponse{Run: run, Requirement: requirement}
	if h.projects != nil && strings.TrimSpace(run.ProjectID) != "" {
		if project, projErr := h.projects.GetByID(run.ProjectID); projErr != nil {
			log.Printf("claim-next-run: failed to load project %s: %v", run.ProjectID, projErr)
		} else if project != nil {
			response.Project = project
		}
	}
	if h.contextBuilder != nil && requirement != nil {
		if ctx, buildErr := h.contextBuilder.BuildContextV1(requirement); buildErr != nil {
			log.Printf("planning context build failed for requirement %s: %v", requirement.ID, buildErr)
		} else {
			response.PlanningContext = ctx
		}
	}
	// Path B S2: populate cli_binding from the snapshot stored on the run.
	// We never re-read the live account_bindings row because the binding
	// could have been deleted between create and claim; the snapshot
	// preserves the audit trail (R10 mitigation, design §6.2 / §6.5).
	if run.AccountBindingID != nil && run.ConnectorCliInfo != nil && run.ConnectorCliInfo.BindingSnapshot != nil {
		snap := run.ConnectorCliInfo.BindingSnapshot
		response.CliBinding = &models.PlanningRunCliBindingPayload{
			ID:         strings.TrimSpace(*run.AccountBindingID),
			ProviderID: snap.ProviderID,
			ModelID:    snap.ModelID,
			CliCommand: snap.CliCommand,
			Label:      snap.Label,
		}
	}
	writeSuccess(w, http.StatusOK, response, nil)
}

// markCliBoundRunsAwaitingUpdate stamps a one-shot "awaiting connector
// update" hint on every CLI-bound run currently queued for the user. The
// stamp lives inside connector_cli_info.dispatch_warning and is
// best-effort: errors are swallowed because the user has already received
// the notification at run-creation time and the stamp itself is just an
// audit / debugging breadcrumb (a future S2-aware connector picks the run
// up on its next claim regardless).
func (h *LocalConnectorHandler) markCliBoundRunsAwaitingUpdate(userID string) {
	if h.planningRuns == nil {
		return
	}
	ids, err := h.planningRuns.ListQueuedCliBoundRunIDsForUser(userID)
	if err != nil || len(ids) == 0 {
		return
	}
	const stamp = "awaiting connector update — pre-Path-B connector skipped this run"
	for _, id := range ids {
		// Best-effort. A failed stamp doesn't block other runs and doesn't
		// re-fire the user notification (already sent at run creation).
		_ = h.planningRuns.MarkDispatchWarning(id, stamp)
	}
}

func (h *LocalConnectorHandler) SubmitPlanningRunResult(w http.ResponseWriter, r *http.Request) {
	connector, ok := h.authenticatedConnector(w, r)
	if !ok {
		return
	}
	runID := chi.URLParam(r, "id")
	if strings.TrimSpace(runID) == "" {
		writeError(w, http.StatusBadRequest, "planning run id is required")
		return
	}
	var req models.LocalConnectorSubmitRunResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := h.planningRuns.GetLeasedLocalConnectorRun(runID, connector.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify planning run lease")
		return
	}
	if run == nil {
		writeError(w, http.StatusConflict, "planning run is not currently leased to this connector")
		return
	}
	requirement, err := h.requirements.GetByID(run.RequirementID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load requirement")
		return
	}
	if requirement == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}

	agentRun, err := h.agentRuns.GetByIdempotencyKey(planning.PlanningAgentRunKey(run.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load planning audit run")
		return
	}

	if !req.Success {
		message := strings.TrimSpace(req.ErrorMessage)
		if message == "" {
			message = "local connector planning failed"
		}
		if agentRun != nil {
			failedSummary := fmt.Sprintf("Planning run failed on local connector %q: %s.", connector.Label, message)
			if _, err := h.agentRuns.Update(agentRun.ID, models.UpdateAgentRunRequest{
				Status:       models.AgentRunStatusFailed,
				Summary:      &failedSummary,
				ErrorMessage: &message,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to finalize planning audit run")
				return
			}
		}
		if err := h.planningRuns.FailLocalConnectorRun(run.ID, connector.ID, message); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark planning run as failed")
			return
		}
		// Normalize and persist error_kind (S5a). Best-effort: don't fail the
		// entire request if envelope update fails.
		errorKind := strings.TrimSpace(req.ErrorKind)
		if errorKind == "" || !models.AllowedErrorKinds[errorKind] {
			errorKind = models.ErrorKindUnknown
		}
		hint := models.ErrorKindRemediations[errorKind]
		if mergeErr := h.planningRuns.MarkErrorKind(run.ID, errorKind, hint); mergeErr != nil {
			log.Printf("submit-result: failed to persist error_kind for run %s: %v", run.ID, mergeErr)
		}
		h.notifyPlanningRunTerminal(connector, run, requirement, false, 0, message)
		updatedRun, err := h.planningRuns.GetByID(run.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load planning run")
			return
		}
		writeSuccess(w, http.StatusOK, updatedRun, nil)
		return
	}

	drafts, err := connectorDraftsToBacklogCandidates(req.Candidates)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(drafts) == 0 {
		writeError(w, http.StatusBadRequest, "at least one backlog candidate is required for a successful result")
		return
	}
	if _, err := h.candidates.CreateDraftsForPlanningRun(requirement, run.ID, drafts); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist backlog candidates")
		return
	}
	if agentRun != nil {
		completedSummary := fmt.Sprintf("Planning run completed on local connector %q with %d ranked backlog candidates ready for review.", connector.Label, len(drafts))
		if _, err := h.agentRuns.Update(agentRun.ID, models.UpdateAgentRunRequest{
			Status:  models.AgentRunStatusCompleted,
			Summary: &completedSummary,
		}); err != nil {
			_ = h.candidates.DeleteByPlanningRun(run.ID)
			writeError(w, http.StatusInternalServerError, "failed to finalize planning audit run")
			return
		}
	}
	cliInfoJSON := ""
	if req.CliInfo != nil {
		if b, err := json.Marshal(req.CliInfo); err == nil {
			cliInfoJSON = string(b)
		}
	}
	if err := h.planningRuns.CompleteLocalConnectorRun(run.ID, connector.ID, cliInfoJSON); err != nil {
		_ = h.candidates.DeleteByPlanningRun(run.ID)
		if agentRun != nil {
			message := store.ErrPlanningRunLeaseUnavailable.Error()
			failedSummary := fmt.Sprintf("Planning run failed while finalizing local connector result: %s.", message)
			_, _ = h.agentRuns.Update(agentRun.ID, models.UpdateAgentRunRequest{
				Status:       models.AgentRunStatusFailed,
				Summary:      &failedSummary,
				ErrorMessage: &message,
			})
		}
		writeError(w, http.StatusInternalServerError, "failed to finalize planning run")
		return
	}
	h.notifyPlanningRunTerminal(connector, run, requirement, true, len(drafts), "")
	updatedRun, err := h.planningRuns.GetByID(run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load planning run")
		return
	}
	writeSuccess(w, http.StatusOK, updatedRun, nil)
}

func (h *LocalConnectorHandler) authenticatedConnector(w http.ResponseWriter, r *http.Request) (*models.LocalConnector, bool) {
	token := strings.TrimSpace(r.Header.Get("X-Connector-Token"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "connector token required")
		return nil, false
	}
	connector, err := h.store.GetByToken(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify connector token")
		return nil, false
	}
	if connector == nil || connector.Status == models.LocalConnectorStatusRevoked {
		writeError(w, http.StatusUnauthorized, "connector token is invalid")
		return nil, false
	}
	return connector, true
}

// notifyPlanningRunTerminal emits a best-effort in-app notification for the
// owning user when a local-connector planning run reaches a terminal state.
// Failures are logged and swallowed: notification delivery must never block
// run finalization.
func (h *LocalConnectorHandler) notifyPlanningRunTerminal(connector *models.LocalConnector, run *models.PlanningRun, requirement *models.Requirement, success bool, candidateCount int, failureMessage string) {
	if h.notifications == nil || connector == nil || run == nil {
		return
	}
	requirementTitle := "(untitled requirement)"
	if requirement != nil && strings.TrimSpace(requirement.Title) != "" {
		requirementTitle = requirement.Title
	}
	projectID := strings.TrimSpace(run.ProjectID)
	var projectIDPtr *string
	if projectID != "" {
		projectIDPtr = &projectID
	}
	link := ""
	if projectID != "" {
		link = fmt.Sprintf("/projects/%s", projectID)
	}
	connectorLabel := connector.Label
	if strings.TrimSpace(connectorLabel) == "" {
		connectorLabel = "local connector"
	}

	req := models.CreateNotificationRequest{
		UserID:    run.RequestedByUserID,
		ProjectID: projectIDPtr,
		Link:      link,
	}
	if success {
		req.Kind = "info"
		req.Title = fmt.Sprintf("Planning run completed: %s", requirementTitle)
		req.Body = fmt.Sprintf("%d backlog candidate%s ready for review (via %s).", candidateCount, plural(candidateCount), connectorLabel)
	} else {
		message := strings.TrimSpace(failureMessage)
		if message == "" {
			message = "unknown error"
		}
		if len(message) > 280 {
			message = message[:277] + "..."
		}
		req.Kind = "error"
		req.Title = fmt.Sprintf("Planning run failed: %s", requirementTitle)
		req.Body = fmt.Sprintf("Local connector %s reported: %s", connectorLabel, message)
	}
	if strings.TrimSpace(req.UserID) == "" {
		// Fall back to the connector owner if the run did not record a user
		// (older rows may not). This keeps notifications scoped to the user
		// who actually owns the connector that produced the result.
		req.UserID = connector.UserID
	}
	if strings.TrimSpace(req.UserID) == "" {
		log.Printf("notifyPlanningRunTerminal: skipped notification for run %s (no user id)", run.ID)
		return
	}
	if _, err := h.notifications.Create(req); err != nil {
		log.Printf("notifyPlanningRunTerminal: failed to insert notification for run %s: %v", run.ID, err)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func connectorDraftsToBacklogCandidates(candidates []models.ConnectorBacklogCandidateDraft) ([]models.BacklogCandidateDraft, error) {
	drafts := make([]models.BacklogCandidateDraft, 0, len(candidates))
	for index, candidate := range candidates {
		title := strings.TrimSpace(candidate.Title)
		if title == "" {
			return nil, fmt.Errorf("candidate %d title is required", index+1)
		}
		priorityScore := candidate.PriorityScore
		if priorityScore <= 0 {
			priorityScore = float64(100 - (index * 5))
			if priorityScore < 10 {
				priorityScore = 10
			}
		}
		confidence := candidate.Confidence
		if confidence <= 0 {
			confidence = float64(85 - (index * 3))
			if confidence < 10 {
				confidence = 10
			}
		}
		rank := candidate.Rank
		if rank < 1 {
			rank = index + 1
		}
		drafts = append(drafts, models.BacklogCandidateDraft{
			SuggestionType:     strings.TrimSpace(candidate.SuggestionType),
			Title:              title,
			Description:        strings.TrimSpace(candidate.Description),
			Rationale:          strings.TrimSpace(candidate.Rationale),
			ValidationCriteria: strings.TrimSpace(candidate.ValidationCriteria),
			PODecision:         strings.TrimSpace(candidate.PODecision),
			PriorityScore:      priorityScore,
			Confidence:         confidence,
			Rank:               rank,
			Evidence:           append([]string(nil), candidate.Evidence...),
			DuplicateTitles:    append([]string(nil), candidate.DuplicateTitles...),
		})
	}
	return drafts, nil
}

// ProbeBinding enqueues a CLI-binding probe for the named connector.
// POST /api/me/local-connectors/:id/probe-binding  { binding_id }
// Returns { probe_id } that the UI polls via GET …/probe-binding/:probe_id.
func (h *LocalConnectorHandler) ProbeBinding(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	connectorID := chi.URLParam(r, "id")
	if strings.TrimSpace(connectorID) == "" {
		writeError(w, http.StatusBadRequest, "connector id is required")
		return
	}
	var req models.ProbeBindingOnConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bindingID := strings.TrimSpace(req.BindingID)
	if bindingID == "" {
		writeError(w, http.StatusBadRequest, "binding_id is required")
		return
	}
	if h.bindings == nil {
		writeError(w, http.StatusInternalServerError, "probe-binding handler is not configured with an account binding store")
		return
	}
	binding, err := h.bindings.GetByID(bindingID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load binding")
		return
	}
	if binding == nil {
		writeError(w, http.StatusNotFound, "binding not found")
		return
	}
	if !models.IsCLIAccountBindingProvider(binding.ProviderID) {
		writeError(w, http.StatusBadRequest, "probe-binding is only supported for cli:* bindings")
		return
	}
	probeID, err := h.store.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID:   bindingID,
		ProviderID:  binding.ProviderID,
		ModelID:     binding.ModelID,
		CliCommand:  binding.CliCommand,
		Label:       binding.Label,
		RequestedAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, store.ErrPendingProbeCapReached) {
			writeError(w, http.StatusTooManyRequests, "too many pending probes on this connector; wait for one to finish before starting another")
			return
		}
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	writeSuccess(w, http.StatusOK, models.ProbeBindingOnConnectorResponse{ProbeID: probeID}, nil)
}

// GetProbeResult returns the stored probe outcome or status=pending.
// GET /api/me/local-connectors/:id/probe-binding/:probe_id
func (h *LocalConnectorHandler) GetProbeResult(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	connectorID := chi.URLParam(r, "id")
	probeID := chi.URLParam(r, "probe_id")
	if strings.TrimSpace(connectorID) == "" || strings.TrimSpace(probeID) == "" {
		writeError(w, http.StatusBadRequest, "connector id and probe id are required")
		return
	}
	status, result, err := h.store.GetCliProbeResult(connectorID, user.ID, probeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load probe result")
		return
	}
	writeSuccess(w, http.StatusOK, models.CliProbeStatusResponse{Status: status, Result: result}, nil)
}

// RunStats returns planning run counts for the authenticated user across
// several time windows (today / 7 days / 30 days / all time).
func (h *LocalConnectorHandler) RunStats(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stats, err := h.planningRuns.RunStatsByUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load run stats")
		return
	}
	writeSuccess(w, http.StatusOK, stats, nil)
}
