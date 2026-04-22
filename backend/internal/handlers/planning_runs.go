package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	plannerToUse := h.resolvePlanner(r)
	orchestrator := planning.NewOrchestrator(h.store, h.agentRunStore, h.candidateStore, plannerToUse)
	run, err := orchestrator.Run(r.Context(), requirement, req, requestingUserID)
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

	writeSuccess(w, http.StatusCreated, run, nil)
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
		_ = h.requirementStore.PromoteToPlannedIfDraft(result.Candidate.RequirementID)
	}

	status := http.StatusCreated
	if result.AlreadyApplied {
		status = http.StatusOK
	}
	writeSuccess(w, status, result, nil)
}
