package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type plannerFactory func(userID string) planning.DraftPlanner

type PlanningRunHandler struct {
	store               *store.PlanningRunStore
	candidateStore      *store.BacklogCandidateStore
	projectStore        *store.ProjectStore
	requirementStore    *store.RequirementStore
	agentRunStore       *store.AgentRunStore
	localConnectorStore *store.LocalConnectorStore
	accountBindings     *store.AccountBindingStore
	notifications       *store.NotificationStore
	planner             planning.DraftPlanner
	plannerFactory      plannerFactory
}

func NewPlanningRunHandler(s *store.PlanningRunStore, cs *store.BacklogCandidateStore, ps *store.ProjectStore, rs *store.RequirementStore, ars *store.AgentRunStore, planner planning.DraftPlanner) *PlanningRunHandler {
	return &PlanningRunHandler{
		store:            s,
		candidateStore:   cs,
		projectStore:     ps,
		requirementStore: rs,
		agentRunStore:    ars,
		planner:          planner,
	}
}

func (h *PlanningRunHandler) WithPlannerFactory(factory plannerFactory) *PlanningRunHandler {
	h.plannerFactory = factory
	return h
}

func (h *PlanningRunHandler) WithLocalConnectorStore(localConnectorStore *store.LocalConnectorStore) *PlanningRunHandler {
	h.localConnectorStore = localConnectorStore
	return h
}

// WithAccountBindings wires the account-bindings store so the Create path
// can validate `account_binding_id`, snapshot the binding into the run
// (Path B S2), and auto-resolve the user's primary cli:* binding when the
// request omits the field (design §6.2 / §6.5). Optional: when nil the
// request's `account_binding_id` is silently ignored and Create falls back
// to the pre-Path-B behavior.
func (h *PlanningRunHandler) WithAccountBindings(bindings *store.AccountBindingStore) *PlanningRunHandler {
	h.accountBindings = bindings
	return h
}

// WithNotifications wires the notification store so the Create path can
// emit the R3 "connector outdated" warning at run-creation time when the
// user's only active connector reports protocol_version < 1 yet a CLI
// binding was selected (design §6.2). Optional: when nil the warning is
// returned in the response envelope only.
func (h *PlanningRunHandler) WithNotifications(notifications *store.NotificationStore) *PlanningRunHandler {
	h.notifications = notifications
	return h
}

func (h *PlanningRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	requirementID := chi.URLParam(r, "id")
	requirement, err := h.requirementStore.GetByID(requirementID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify requirement")
		return
	}
	if requirement == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	if !requestAllowsProject(r, requirement.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req models.CreatePlanningRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if normalizedMode := strings.TrimSpace(req.ExecutionMode); normalizedMode != "" && !models.ValidPlanningExecutionModes[normalizedMode] {
		writeError(w, http.StatusBadRequest, "invalid execution mode")
		return
	}
	req.ProviderID = ""
	requestingUserID := ""
	if apiKey := middleware.APIKeyFromContext(r.Context()); apiKey != nil && strings.TrimSpace(req.ExecutionMode) == models.PlanningExecutionModeLocalConnector {
		writeError(w, http.StatusForbidden, "local connector planning requires a signed-in user session")
		return
	}
	if user := middleware.UserFromContext(r.Context()); user != nil && middleware.APIKeyFromContext(r.Context()) == nil {
		requestingUserID = user.ID
	}
	if strings.TrimSpace(req.ExecutionMode) == models.PlanningExecutionModeLocalConnector {
		if requestingUserID == "" {
			writeError(w, http.StatusForbidden, "local connector planning requires a signed-in user session")
			return
		}
		usableConnector, err := h.usableLocalConnector(requestingUserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify local connector availability")
			return
		}
		if usableConnector == nil {
			writeError(w, http.StatusBadRequest, "no paired local connector is available for this account")
			return
		}
	}

	// Path B S2: resolve account_binding_id (explicit or auto-primary) and
	// build the snapshot the orchestrator will embed into the run.
	// Returns warnings to emit in the success envelope (stale_cli_health,
	// connector_outdated). 4xx errors are written directly.
	snapshot, warnings, ok := h.resolvePathBBinding(w, r, &req, requirement, requestingUserID)
	if !ok {
		return
	}

	plannerToUse := h.resolvePlanner(r)
	orchestrator := planning.NewOrchestrator(h.store, h.agentRunStore, h.candidateStore, plannerToUse)
	run, err := orchestrator.RunWithBindingSnapshot(r.Context(), requirement, req, requestingUserID, snapshot)
	if err != nil {
		if errors.Is(err, store.ErrActivePlanningRunExists) {
			writeError(w, http.StatusConflict, "an active planning run already exists for this requirement")
			return
		}
		if errors.Is(err, planning.ErrUnknownPlanningProvider) || errors.Is(err, planning.ErrUnknownPlanningModel) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		switch {
		case errors.Is(err, planning.ErrStartPlanningRun):
			writeError(w, http.StatusInternalServerError, planning.ErrStartPlanningRun.Error())
		case errors.Is(err, planning.ErrCreatePlanningAgentRun):
			writeError(w, http.StatusInternalServerError, planning.ErrCreatePlanningAgentRun.Error())
		case errors.Is(err, planning.ErrGenerateDraftCandidates):
			writeError(w, http.StatusInternalServerError, planning.ErrGenerateDraftCandidates.Error())
		case errors.Is(err, planning.ErrPersistDraftCandidates):
			writeError(w, http.StatusInternalServerError, planning.ErrPersistDraftCandidates.Error())
		case errors.Is(err, planning.ErrFinalizePlanningAgent):
			writeError(w, http.StatusInternalServerError, planning.ErrFinalizePlanningAgent.Error())
		case errors.Is(err, planning.ErrCompletePlanningRun):
			writeError(w, http.StatusInternalServerError, planning.ErrCompletePlanningRun.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create planning run")
		}
		return
	}

	writeSuccessWithWarnings(w, http.StatusCreated, run, nil, warnings)
}

// resolvePathBBinding implements the design §6.2 / §6.5 server-side
// validation + snapshot for the planning-run create endpoint. Steps:
//
//  1. Validate `account_binding_id` if provided: row must exist, belong to
//     the requesting user (R2), and (for local_connector mode) be a cli:%
//     active binding. Reject 400 otherwise.
//  2. If absent AND execution_mode is local_connector AND user has at
//     least one active cli:* binding, auto-resolve to the user's primary
//     cli binding (one primary per user-namespace per design D2).
//  3. If absent AND user has zero cli:* bindings, leave the field nil; the
//     run still creates and the connector falls back to its env-var
//     default (backwards compatible).
//  4. Build the snapshot to persist inside connector_cli_info.binding_snapshot.
//  5. Append envelope warnings for stale CLI health and pre-Path-B
//     connector. Fire a one-time notification for the connector_outdated
//     case (R3).
//
// Returns (snapshot, warnings, ok). When ok=false the function has already
// written the HTTP error response.
func (h *PlanningRunHandler) resolvePathBBinding(w http.ResponseWriter, r *http.Request, req *models.CreatePlanningRunRequest, requirement *models.Requirement, requestingUserID string) (*models.PlanningRunBindingSnapshot, []models.EnvelopeWarning, bool) {
	if h.accountBindings == nil {
		// Account bindings store is optional in tests. When unwired we
		// short-circuit and ignore any account_binding_id the caller sent
		// (no snapshot, no warnings) — same shape as a Phase-0 deployment.
		return nil, nil, true
	}
	executionMode := strings.TrimSpace(req.ExecutionMode)
	isLocalConnector := executionMode == models.PlanningExecutionModeLocalConnector

	var binding *models.StoredAccountBinding
	var warnings []models.EnvelopeWarning

	// Step 1: explicit binding id.
	if req.AccountBindingID != nil && strings.TrimSpace(*req.AccountBindingID) != "" {
		bindingID := strings.TrimSpace(*req.AccountBindingID)
		// Three-way ownership check (design §6.2): the current schema has
		// no per-project owner column, so the third leg (== project owner)
		// collapses to "binding belongs to the requesting user". GetByID
		// already scopes to userID, so a row owned by user B returns nil.
		fetched, err := h.accountBindings.GetByID(bindingID, requestingUserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify account binding")
			return nil, nil, false
		}
		if fetched == nil {
			writeError(w, http.StatusBadRequest, "account_binding_id does not belong to this user")
			return nil, nil, false
		}
		if isLocalConnector {
			if !models.IsCLIAccountBindingProvider(fetched.ProviderID) {
				writeError(w, http.StatusBadRequest, "account_binding_id must be a cli:* provider for local_connector execution mode")
				return nil, nil, false
			}
			if !fetched.IsActive {
				writeError(w, http.StatusBadRequest, "account_binding_id must reference an active binding")
				return nil, nil, false
			}
		}
		binding = fetched
	}

	// Step 2: auto-resolve primary cli binding when caller omitted the field.
	if binding == nil && isLocalConnector {
		listed, err := h.accountBindings.ListByUser(requestingUserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load account bindings")
			return nil, nil, false
		}
		var primaryCli *models.AccountBinding
		var firstActiveCli *models.AccountBinding
		for i := range listed {
			b := listed[i]
			if !models.IsCLIAccountBindingProvider(b.ProviderID) || !b.IsActive {
				continue
			}
			if firstActiveCli == nil {
				firstActiveCli = &listed[i]
			}
			if b.IsPrimary {
				primaryCli = &listed[i]
				break
			}
		}
		// Primary takes precedence; fall back to the first active cli
		// binding if no primary is set (covers users who haven't toggled
		// is_primary on a single-binding setup before S3 ships the UI).
		picked := primaryCli
		if picked == nil {
			picked = firstActiveCli
		}
		if picked != nil {
			fetched, err := h.accountBindings.GetByID(picked.ID, requestingUserID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load account binding")
				return nil, nil, false
			}
			if fetched != nil {
				binding = fetched
				bindingID := fetched.ID
				req.AccountBindingID = &bindingID
			}
		}
		// Step 3: zero cli bindings → leave nil, no snapshot, success.
	}

	if binding == nil {
		return nil, nil, true
	}

	// Step 4: build snapshot. Persisted by the orchestrator inside
	// connector_cli_info.binding_snapshot via PlanningRunStore.CreateWithBinding.
	snapshot := &models.PlanningRunBindingSnapshot{
		ProviderID: binding.ProviderID,
		ModelID:    binding.ModelID,
		CliCommand: binding.CliCommand,
		Label:      binding.Label,
		IsPrimary:  binding.IsPrimary,
	}

	// Mirror the snapshot into the existing 015/018 audit columns so the
	// run row is self-describing without parsing the JSON envelope.
	if strings.TrimSpace(req.AdapterType) == "" {
		req.AdapterType = binding.ProviderID
	}
	if strings.TrimSpace(req.ModelOverride) == "" {
		req.ModelOverride = binding.ModelID
	}

	// Step 5a: stale_cli_health warning. cli_health is added in S5b under
	// local_connectors.metadata; that column doesn't exist in the current
	// schema, so we cannot meaningfully emit this warning yet. Keep the
	// hook so S5b just has to flip a flag — design §6.2 explicitly says S2
	// is a stub here.

	// Step 5b: connector_outdated warning (R3 mitigation). If the user
	// only has pre-Path-B connectors (protocol_version < 1) yet a CLI
	// binding was selected, the run will sit queued. Surface it now so
	// the operator knows what to do.
	if isLocalConnector && h.localConnectorStore != nil {
		hasUpToDate, outdated, lookupErr := h.userConnectorProtocolStatus(requestingUserID)
		if lookupErr == nil && !hasUpToDate && outdated != nil {
			warning := models.EnvelopeWarning{
				Code:    "connector_outdated",
				Message: "Update anpm-connector to claim this run.",
				Details: map[string]interface{}{
					"connector_id":     outdated.ID,
					"connector_label":  outdated.Label,
					"protocol_version": outdated.ProtocolVersion,
				},
			}
			warnings = append(warnings, warning)
			h.fireConnectorOutdatedNotification(requirement, requestingUserID, outdated)
		}
	}

	return snapshot, warnings, true
}

// userConnectorProtocolStatus reports whether the requesting user has at
// least one usable connector that knows the Path B claim wire (protocol
// version >= 1), and returns the most recently active outdated connector
// if not. The result fuels the R3 "connector outdated" envelope warning.
func (h *PlanningRunHandler) userConnectorProtocolStatus(userID string) (bool, *models.LocalConnector, error) {
	if h.localConnectorStore == nil {
		return false, nil, nil
	}
	connectors, err := h.localConnectorStore.ListByUser(userID)
	if err != nil {
		return false, nil, err
	}
	hasUpToDate := false
	var outdated *models.LocalConnector
	for i := range connectors {
		c := connectors[i]
		if c.Status == models.LocalConnectorStatusRevoked {
			continue
		}
		if c.ProtocolVersion >= 1 {
			hasUpToDate = true
			continue
		}
		// ListByUser returns connectors ordered by created_at DESC, so the
		// first outdated row we encounter is the most recently created one.
		// Lock it in and don't overwrite — without this break the loop ends
		// up returning the OLDEST outdated connector, which is the wrong
		// one to nudge the operator about.
		if outdated == nil {
			outdated = &connectors[i]
		}
	}
	return hasUpToDate, outdated, nil
}

// fireConnectorOutdatedNotification is a best-effort helper that drops a
// warning notification for the user. Failures (e.g. the store is unwired
// or the insert fails) are swallowed — the envelope warning is the
// load-bearing surface; the notification is just a nudge.
func (h *PlanningRunHandler) fireConnectorOutdatedNotification(requirement *models.Requirement, userID string, connector *models.LocalConnector) {
	if h.notifications == nil || connector == nil || strings.TrimSpace(userID) == "" {
		return
	}
	requirementTitle := "(untitled requirement)"
	if requirement != nil && strings.TrimSpace(requirement.Title) != "" {
		requirementTitle = requirement.Title
	}
	projectIDPtr := (*string)(nil)
	link := ""
	if requirement != nil && strings.TrimSpace(requirement.ProjectID) != "" {
		pid := requirement.ProjectID
		projectIDPtr = &pid
		link = "/projects/" + pid
	}
	body := fmt.Sprintf("Run on requirement %q is waiting for an updated connector. Update anpm-connector to claim this run.", requirementTitle)
	_, _ = h.notifications.Create(models.CreateNotificationRequest{
		UserID:    userID,
		ProjectID: projectIDPtr,
		Kind:      "warning",
		Title:     "Connector update required",
		Body:      body,
		Link:      link,
	})
}

func (h *PlanningRunHandler) ProviderOptions(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, project.ID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	options := h.resolvePlanner(r).Options()
	decorated, err := h.decorateProviderOptions(r, options)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load planning provider options")
		return
	}
	writeSuccess(w, http.StatusOK, decorated, nil)
}

// ListAppliedLineage GET /api/projects/:id/task-lineage
// Returns denormalised task_lineage entries (lineage_kind='applied_candidate')
// for the project, each joined with the task / requirement / planning_run /
// backlog_candidate titles. Powers the Planning Workspace applied-lineage
// lane (S4) without forcing the client to make N extra API calls per row.
func (h *PlanningRunHandler) ListAppliedLineage(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, project.ID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	entries, err := h.candidateStore.ListAppliedLineageByProject(project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task lineage")
		return
	}
	writeSuccess(w, http.StatusOK, entries, nil)
}

func (h *PlanningRunHandler) resolvePlanner(r *http.Request) planning.DraftPlanner {
	if h.plannerFactory == nil {
		return h.planner
	}
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		return h.planner
	}
	resolved := h.plannerFactory(user.ID)
	if resolved == nil {
		return h.planner
	}
	return resolved
}

func (h *PlanningRunHandler) decorateProviderOptions(r *http.Request, options models.PlanningProviderOptions) (models.PlanningProviderOptions, error) {
	baseExecutionMode := models.PlanningExecutionModeServerProvider
	if strings.TrimSpace(options.DefaultSelection.ProviderID) == models.PlanningProviderDeterministic {
		baseExecutionMode = models.PlanningExecutionModeDeterministic
	}
	options.AvailableExecutionModes = []string{baseExecutionMode}
	if middleware.APIKeyFromContext(r.Context()) != nil {
		return options, nil
	}
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		return options, nil
	}
	connector, err := h.usableLocalConnector(user.ID)
	if err != nil {
		return options, err
	}
	if connector == nil {
		return options, nil
	}
	options.PairedConnectorAvailable = true
	options.ActiveConnectorLabel = connector.Label
	options.AvailableExecutionModes = append(options.AvailableExecutionModes, models.PlanningExecutionModeLocalConnector)
	return options, nil
}

func (h *PlanningRunHandler) usableLocalConnector(userID string) (*models.LocalConnector, error) {
	if h.localConnectorStore == nil || strings.TrimSpace(userID) == "" {
		return nil, nil
	}
	return h.localConnectorStore.GetFirstUsableByUser(userID)
}

func (h *PlanningRunHandler) ListByRequirement(w http.ResponseWriter, r *http.Request) {
	requirementID := chi.URLParam(r, "id")
	requirement, err := h.requirementStore.GetByID(requirementID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify requirement")
		return
	}
	if requirement == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	if !requestAllowsProject(r, requirement.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	page, perPage := parsePagination(r)
	runs, total, err := h.store.ListByRequirement(requirement.ID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list planning runs")
		return
	}
	if runs == nil {
		runs = []models.PlanningRun{}
	}

	writeSuccess(w, http.StatusOK, runs, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *PlanningRunHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	writeSuccess(w, http.StatusOK, run, nil)
}

// Cancel transitions a queued or running planning run into the cancelled
// terminal state. Useful when a local connector dispatch is stuck because the
// connector is offline or never picked up the run.
func (h *PlanningRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
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
	if run.Status != models.PlanningRunStatusQueued && run.Status != models.PlanningRunStatusRunning {
		writeError(w, http.StatusConflict, "planning run is already in a terminal state")
		return
	}
	reason := "cancelled by user"
	if user := middleware.UserFromContext(r.Context()); user != nil {
		reason = fmt.Sprintf("cancelled by %s", user.Username)
	}
	updated, err := h.store.CancelIfActive(id, reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel planning run")
		return
	}
	if updated == nil {
		writeError(w, http.StatusConflict, "planning run is no longer cancellable")
		return
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}

func (h *PlanningRunHandler) ListBacklogCandidates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify planning run")
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

	page, perPage := parsePagination(r)
	candidates, total, err := h.candidateStore.ListByPlanningRun(run.ID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list backlog candidates")
		return
	}
	if candidates == nil {
		candidates = []models.BacklogCandidate{}
	}

	writeSuccess(w, http.StatusOK, candidates, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *PlanningRunHandler) UpdateBacklogCandidate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	candidate, err := h.candidateStore.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify backlog candidate")
		return
	}
	if candidate == nil {
		writeError(w, http.StatusNotFound, "backlog candidate not found")
		return
	}
	if !requestAllowsProject(r, candidate.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req models.UpdateBacklogCandidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.candidateStore.Update(id, req)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrBacklogCandidateNotMutable):
			writeError(w, http.StatusBadRequest, "applied backlog candidates cannot be edited")
		case errors.Is(err, store.ErrBacklogCandidateNoChanges):
			writeError(w, http.StatusBadRequest, "at least one mutable field must change")
		case errors.Is(err, store.ErrBacklogCandidateBlankTitle):
			writeError(w, http.StatusBadRequest, "title cannot be blank")
		case errors.Is(err, store.ErrBacklogCandidateBadStatus):
			writeError(w, http.StatusBadRequest, "invalid backlog candidate status")
		default:
			writeError(w, http.StatusInternalServerError, "failed to update backlog candidate")
		}
		return
	}

	writeSuccess(w, http.StatusOK, updated, nil)
}

func (h *PlanningRunHandler) ApplyBacklogCandidate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	candidate, err := h.candidateStore.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify backlog candidate")
		return
	}
	if candidate == nil {
		writeError(w, http.StatusNotFound, "backlog candidate not found")
		return
	}
	if !requestAllowsProject(r, candidate.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	result, err := h.candidateStore.ApplyToTask(id)
	if err != nil {
		var conflictErr *store.BacklogCandidateTaskConflictError
		switch {
		case errors.Is(err, store.ErrBacklogCandidateNotApproved):
			writeError(w, http.StatusBadRequest, "only approved backlog candidates can be applied to tasks")
		case errors.Is(err, store.ErrBacklogCandidateBlankTitle):
			writeError(w, http.StatusBadRequest, "title cannot be blank")
		case errors.As(err, &conflictErr):
			writeError(w, http.StatusConflict, conflictErr.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to apply backlog candidate")
		}
		return
	}

	if !result.AlreadyApplied && result.Candidate.RequirementID != "" {
		if err := h.requirementStore.PromoteToPlannedIfDraft(result.Candidate.RequirementID); err != nil {
			log.Printf("apply-candidate: promote requirement %s: %v", result.Candidate.RequirementID, err)
		}
	}

	status := http.StatusCreated
	if result.AlreadyApplied {
		status = http.StatusOK
	}
	writeSuccess(w, status, result, nil)
}
