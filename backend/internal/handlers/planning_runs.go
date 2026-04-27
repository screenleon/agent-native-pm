package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/connector"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type plannerFactory func(userID string) planning.DraftPlanner

// roleSuggesterFn is the function type for suggest-role calls.
// Kept as a type alias so main.go can wire connector.SuggestRole directly
// without the handler struct hard-coding the connector package at field level.
type roleSuggesterFn func(ctx context.Context, taskTitle, taskDescription, requirement, projectContext string, cliSel *connector.AdapterCliSelection) connector.SuggestRoleResult

type PlanningRunHandler struct {
	store                *store.PlanningRunStore
	candidateStore       *store.BacklogCandidateStore
	projectStore         *store.ProjectStore
	requirementStore     *store.RequirementStore
	agentRunStore        *store.AgentRunStore
	localConnectorStore  *store.LocalConnectorStore
	accountBindings      *store.AccountBindingStore
	notifications        *store.NotificationStore
	planner              planning.DraftPlanner
	plannerFactory       plannerFactory
	// Phase 3B PR-2: context snapshot retrieval. nil when not wired.
	contextSnapshotStore ContextSnapshotGetter
	// Phase 6c PR-3: suggest-role. nil when not wired (suggest endpoint returns 503).
	roleSuggester roleSuggesterFn
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

// WithRoleSuggester wires the Phase 6c PR-3 role-suggestion function.
// When nil (default), POST /backlog-candidates/:id/suggest-role returns 503.
func (h *PlanningRunHandler) WithRoleSuggester(fn roleSuggesterFn) *PlanningRunHandler {
	h.roleSuggester = fn
	return h
}

// WithContextSnapshotStore wires the Phase 3B PR-2 context snapshot store.
// When nil (default), GET /planning-runs/:id/context-snapshot returns
// {available: false} for all runs.
func (h *PlanningRunHandler) WithContextSnapshotStore(s ContextSnapshotGetter) *PlanningRunHandler {
	h.contextSnapshotStore = s
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
	if req.AdapterType != "" && req.AdapterType != "backlog" && req.AdapterType != "whatsnext" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid adapter_type %q: must be backlog or whatsnext", req.AdapterType))
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

	// Step 0 (Phase 6a UX-B3): explicit connector_id + cli_config_id path.
	// When present, this is the new per-connector authoring surface — take
	// precedence over the legacy account_binding path. Both fields MUST be
	// set together so a caller cannot reference a config on an unspecified
	// connector.
	connectorID := ""
	if req.ConnectorID != nil {
		connectorID = strings.TrimSpace(*req.ConnectorID)
	}
	cliConfigID := ""
	if req.CliConfigID != nil {
		cliConfigID = strings.TrimSpace(*req.CliConfigID)
	}
	if cliConfigID != "" || connectorID != "" {
		if cliConfigID == "" || connectorID == "" {
			writeError(w, http.StatusBadRequest, "connector_id and cli_config_id must be supplied together")
			return nil, nil, false
		}
		if !isLocalConnector {
			writeError(w, http.StatusBadRequest, "cli_config_id is only valid for execution_mode=local_connector")
			return nil, nil, false
		}
		if h.localConnectorStore == nil {
			writeError(w, http.StatusInternalServerError, "local connector store not configured")
			return nil, nil, false
		}
		cfg, err := h.localConnectorStore.GetCliConfig(connectorID, requestingUserID, cliConfigID)
		if err != nil {
			if errors.Is(err, store.ErrCliConfigNotFound) {
				writeError(w, http.StatusBadRequest, "cli_config_id not found on the named connector for this user")
				return nil, nil, false
			}
			writeError(w, http.StatusInternalServerError, "failed to load cli config")
			return nil, nil, false
		}
		// Snapshot the CliConfig directly — bypass the account_bindings
		// validation path entirely. PlanningRunCliBindingPayload shape on
		// the claim side is identical so the connector daemon does not
		// notice the authoring change.
		snapshot := &models.PlanningRunBindingSnapshot{
			ProviderID: cfg.ProviderID,
			ModelID:    cfg.ModelID,
			CliCommand: cfg.CliCommand,
			Label:      cfg.Label,
			IsPrimary:  cfg.IsPrimary,
		}
		// adapter_type is a semantic planning type ("backlog", "whatsnext")
		// and is independent of cfg.ProviderID ("cli:claude", "cli:codex").
		// Preserve the caller's intent; default to "backlog" if omitted.
		// The provider identity is captured in the binding_snapshot.ProviderID
		// — no need to overwrite adapter_type with it.
		if strings.TrimSpace(req.AdapterType) == "" {
			req.AdapterType = "backlog"
		}
		if mo := strings.TrimSpace(req.ModelOverride); mo != "" && cfg.ModelID != "" && mo != cfg.ModelID {
			writeError(w, http.StatusBadRequest, "model_override does not match the cli_config model_id")
			return nil, nil, false
		}
		req.ModelOverride = cfg.ModelID
		// Clear any caller-supplied account_binding_id so it does not get
		// persisted alongside the cli_config snapshot — otherwise the
		// resulting row points at two different authoring sources at
		// once (Critic SHOULD-FIX #2 / Copilot review on PR #23).
		req.AccountBindingID = nil
		// Pin the run to the chosen connector so any other online
		// connector belonging to the same user cannot claim it (the
		// chosen cli_command + provider_id only exist on this connector;
		// Copilot review on PR #23). Stored on the run so the lease
		// query can filter on it.
		pinned := strings.TrimSpace(connectorID)
		req.TargetConnectorID = &pinned
		return snapshot, warnings, true
	}

	// Step 1: explicit binding id (LEGACY path).
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
	// local_connector goes first: for subscription-only users it is the
	// only usable mode, so it should be the default (index 0).
	// Users who have both a connector and an API key benefit from the
	// local path too since it uses their existing CLI subscription.
	options.AvailableExecutionModes = append(
		[]string{models.PlanningExecutionModeLocalConnector},
		options.AvailableExecutionModes...,
	)
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
	// Phase 6c PR-2 (risk-reviewer H1): surface execution_role
	// authoring metadata so the frontend can render set-by/at/by-whom
	// without a second round-trip.
	h.candidateStore.EnrichWithAuthoring(r.Context(), candidates)

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

	// Phase 3B PR-3: validate feedback_kind at the handler boundary so
	// the 400 fires before any store work is attempted.
	if req.FeedbackKind != nil && !models.IsValidFeedbackKind(*req.FeedbackKind) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid feedback_kind: %q; allowed: %v", *req.FeedbackKind, models.AllFeedbackKinds))
		return
	}

	// Phase 6c PR-2: PATCH is now audit-aware when execution_role is
	// the field being changed. The actor is the authenticated caller
	// — distinguish session-user vs api-key per critic round 1 #3 +
	// risk-reviewer M1 so the audit trail correctly attributes the
	// change.
	patchActor := buildAuthoringActor(r, "patch backlog candidate")
	updated, err := h.candidateStore.Update(id, req, patchActor)
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
		case errors.Is(err, store.ErrBacklogCandidateUnknownExecutionRole):
			writeError(w, http.StatusBadRequest, err.Error())
		// Phase 3B PR-3: feedback_kind validation error from store layer
		// (belt-and-suspenders — handler already checks above).
		case store.IsInvalidFeedbackKindError(err):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to update backlog candidate")
		}
		return
	}

	// Phase 6c PR-2 (risk-reviewer H1): surface authoring metadata.
	if updated != nil {
		one := []models.BacklogCandidate{*updated}
		h.candidateStore.EnrichWithAuthoring(r.Context(), one)
		updated = &one[0]
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}

// ListByEvidence returns candidate summaries that reference a given document
// or drift signal in their evidence_detail.
// Exactly one of ?document_id or ?drift_signal_id must be supplied.
func (h *PlanningRunHandler) ListByEvidence(w http.ResponseWriter, r *http.Request) {
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
	if !requestAllowsProject(r, projectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	q := r.URL.Query()
	docID := q.Get("document_id")
	driftID := q.Get("drift_signal_id")

	if (docID == "") == (driftID == "") {
		writeError(w, http.StatusBadRequest, "exactly one of document_id or drift_signal_id is required")
		return
	}

	var summaries []models.CandidateEvidenceSummary
	if docID != "" {
		summaries, err = h.candidateStore.ListByEvidenceDocument(projectID, docID)
	} else {
		summaries, err = h.candidateStore.ListByEvidenceDriftSignal(projectID, driftID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query evidence links")
		return
	}
	if summaries == nil {
		summaries = []models.CandidateEvidenceSummary{}
	}
	writeSuccess(w, http.StatusOK, summaries, nil)
}

// DemoSeed POST /api/projects/:id/demo-seed
// Creates one demo requirement, one completed planning run, and three draft
// backlog candidates. Returns 409 if the project already has requirements.
// Intended for new empty projects so operators can explore the UI without a
// real planning run.
func (h *PlanningRunHandler) DemoSeed(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	// Verify project exists.
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Atomically guard + create the demo requirement so concurrent requests
	// cannot both pass the emptiness check.
	req, err := h.requirementStore.CreateIfProjectEmpty(projectID, models.CreateRequirementRequest{
		Title:       "Demo: Build a task-tracker SaaS",
		Source:      "demo",
		Description: "A lightweight SaaS that lets small teams track tasks, deadlines, and progress without the overhead of enterprise PM tools.",
	})
	if errors.Is(err, store.ErrProjectNotEmpty) {
		writeError(w, http.StatusConflict, "project is not empty; demo seed only works on empty projects")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create demo requirement")
		return
	}

	// Create a planning run using the deterministic provider (no connector needed).
	selection := models.PlanningProviderSelection{
		ProviderID:      models.PlanningProviderDeterministic,
		ModelID:         models.PlanningProviderModelDeterministic,
		SelectionSource: models.PlanningSelectionSourceServerDefault,
	}
	run, err := h.store.Create(projectID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "demo",
		ExecutionMode: models.PlanningExecutionModeDeterministic,
		AdapterType:   "backlog",
	}, selection)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create demo planning run")
		return
	}

	// Insert the three demo backlog candidates.
	drafts := []models.BacklogCandidateDraft{
		{
			Title:         "Set up project scaffolding with Next.js + PostgreSQL",
			Description:   "Bootstrap the monorepo with Next.js 14, Drizzle ORM, and a PostgreSQL schema for tasks, users, and projects.",
			Rationale:     "Foundation layer — everything else builds on this.",
			PriorityScore: 0.95,
			Confidence:    0.9,
			Rank:          1,
		},
		{
			Title:         "Design the task board UI with drag-and-drop",
			Description:   "Build a Kanban-style board using @dnd-kit. Columns: To Do / In Progress / Done. Cards show assignee + due date.",
			Rationale:     "The core user-facing feature that differentiates the product.",
			PriorityScore: 0.80,
			Confidence:    0.85,
			Rank:          2,
		},
		{
			Title:         "Add Slack notification for due-date reminders",
			Description:   "Send a Slack webhook 24h before a task's due date. Configurable per-workspace.",
			Rationale:     "Reduces missed deadlines — common pain point in small teams.",
			PriorityScore: 0.65,
			Confidence:    0.75,
			Rank:          3,
		},
	}
	if _, err := h.candidateStore.CreateDraftsForPlanningRun(req, run.ID, drafts); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create demo backlog candidates")
		return
	}

	// Mark the run as completed.
	if err := h.store.Complete(run.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete demo planning run")
		return
	}

	writeSuccess(w, http.StatusCreated, map[string]string{
		"requirement_id":  req.ID,
		"planning_run_id": run.ID,
		"message":         "Demo seed created successfully",
	}, nil)
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

	// Phase 5 B3 + Phase 6c PR-2: request body carries execution_mode
	// and (for role_dispatch) execution_role. Missing body / empty field
	// = "manual" (back-compat with Phase 4 and earlier callers).
	//
	// Trim BEFORE enum comparison so `" manual "` behaves the same as
	// `"manual"` — the previous code trimmed only for the empty-check
	// and then forwarded the untrimmed value, which would have returned
	// 400 for whitespace-only input differences (Copilot PR#22).
	executionMode := models.ApplyExecutionModeManual
	executionRole := ""
	if r.ContentLength != 0 {
		var req models.ApplyBacklogCandidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if trimmed := strings.TrimSpace(req.ExecutionMode); trimmed != "" {
			executionMode = trimmed
		}
		executionRole = strings.TrimSpace(req.ExecutionRole)
	}

	// Phase 6c PR-2: assemble actor info for the audit row. Distinguish
	// session-authenticated humans from API-key automations — both
	// pass through the same RequireAuth gate but the audit trail must
	// be able to tell them apart (critic round 1 finding #3). API-key
	// callers identify by the key's id rather than a user id.
	actor := buildAuthoringActor(r, "apply backlog candidate")

	result, err := h.candidateStore.ApplyToTaskWithMode(id, executionMode, executionRole, actor)
	if err != nil {
		var conflictErr *store.BacklogCandidateTaskConflictError
		switch {
		case errors.Is(err, store.ErrBacklogCandidateInvalidExecutionMode):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, store.ErrBacklogCandidateMissingExecutionRole):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, store.ErrBacklogCandidateUnknownExecutionRole):
			writeError(w, http.StatusBadRequest, err.Error())
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

	// Phase 6c PR-2 (risk-reviewer H1): surface authoring metadata on
	// the returned candidate so the frontend's apply-success view can
	// render "set by ${actor.kind} just now" without a refetch.
	one := []models.BacklogCandidate{result.Candidate}
	h.candidateStore.EnrichWithAuthoring(r.Context(), one)
	result.Candidate = one[0]

	status := http.StatusCreated
	if result.AlreadyApplied {
		status = http.StatusOK
	}
	writeSuccess(w, status, result, nil)
}

// SuggestRole implements POST /api/backlog-candidates/:id/suggest-role.
//
// Phase 6c PR-3 — suggest-only mode:
//   - Runs the dispatcher meta-prompt against the candidate's title,
//     description, and parent requirement.
//   - Returns {role_id, confidence, reasoning, alternatives} WITHOUT
//     persisting to actor_audit. The operator confirms by patching
//     execution_role on the candidate or selecting at apply time.
//   - Auto-apply (mode=role_dispatch_auto) is deferred to Phase 6d per
//     DECISIONS.md "Phase 6c scope decision B2".
func (h *PlanningRunHandler) SuggestRole(w http.ResponseWriter, r *http.Request) {
	if h.roleSuggester == nil {
		writeError(w, http.StatusServiceUnavailable, "role suggestion is not configured on this server")
		return
	}

	id := chi.URLParam(r, "id")
	candidate, err := h.candidateStore.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up backlog candidate")
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

	// Fetch the parent requirement for additional context.
	requirementCtx := ""
	if candidate.RequirementID != "" {
		req, reqErr := h.requirementStore.GetByID(candidate.RequirementID)
		if reqErr != nil {
			log.Printf("suggest-role: fetch requirement %s: %v", candidate.RequirementID, reqErr)
		} else if req != nil {
			parts := []string{"Title: " + req.Title}
			if req.Summary != "" {
				parts = append(parts, "Summary: "+req.Summary)
			}
			requirementCtx = strings.Join(parts, "\n")
		}
	}

	// Fetch project name for minimal project context.
	projectCtx := ""
	if project, projErr := h.projectStore.GetByID(candidate.ProjectID); projErr == nil && project != nil {
		projectCtx = "Project: " + project.Name
		if project.Description != "" {
			projectCtx += "\n" + project.Description
		}
	}

	result := h.roleSuggester(r.Context(), candidate.Title, candidate.Description, requirementCtx, projectCtx, nil)

	// On failure, return 422 with structured error detail so the frontend
	// can render a user-actionable message rather than a generic toast.
	if result.ErrorKind != "" {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("[%s] %s", result.ErrorKind, result.ErrorMessage))
		return
	}

	writeSuccess(w, http.StatusOK, result, nil)
}
