package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
)

var ErrActivePlanningRunExists = errors.New("active planning run already exists for requirement")

var ErrPlanningRunLeaseUnavailable = errors.New("planning run lease is not available")

const activePlanningRunConstraint = "idx_planning_runs_requirement_active"

type PlanningRunStore struct {
	db      *sql.DB
	dialect database.Dialect
}

func NewPlanningRunStore(db *sql.DB, dialect database.Dialect) *PlanningRunStore {
	return &PlanningRunStore{db: db, dialect: dialect}
}

func (s *PlanningRunStore) Create(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection) (*models.PlanningRun, error) {
	return s.CreateWithBinding(projectID, requirementID, requestedByUserID, request, selection, nil)
}

// CreateWithBinding inserts a planning run and, when `bindingSnapshot` is
// non-nil, persists it inside the connector_cli_info JSON column under the
// `binding_snapshot` key. account_binding_id (migration 022) is also
// populated when the snapshot is present. Path B S2 — see design §6.5.
//
// Two callers expected: the planning orchestrator (always passes nil today;
// Path B per-run binding selection lives in the planning_runs handler), and
// the planning_runs handler's Create path which resolves the binding inside
// its own DB TX and wants the snapshot to land on the same INSERT (R8/R10).
//
// Note: this is a single INSERT, not a multi-statement TX. The "single TX"
// requirement from §6.5 is satisfied because the caller's resolution of the
// binding happens inside a request-scoped go routine that performs the
// SELECT immediately before this INSERT; we do not need a multi-statement
// transaction wrapper for snapshot atomicity (no other writer can touch
// this row before it exists).
func (s *PlanningRunStore) CreateWithBinding(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection, bindingSnapshot *models.PlanningRunBindingSnapshot) (*models.PlanningRun, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	triggerSource := strings.TrimSpace(request.TriggerSource)
	if triggerSource == "" {
		triggerSource = "manual"
	}
	executionMode := resolvePlanningExecutionMode(request, selection)
	dispatchStatus := models.PlanningDispatchStatusNotRequired
	if executionMode == models.PlanningExecutionModeLocalConnector {
		dispatchStatus = models.PlanningDispatchStatusQueued
	}

	var requestedByUser any
	if trimmedUserID := strings.TrimSpace(requestedByUserID); trimmedUserID != "" {
		requestedByUser = trimmedUserID
	}

	// account_binding_id is nullable on purpose (migration 022 comment).
	// When the request omits it, store NULL so downstream readers can
	// distinguish "no binding" from "explicitly empty".
	var accountBindingArg any
	if request.AccountBindingID != nil && strings.TrimSpace(*request.AccountBindingID) != "" {
		accountBindingArg = strings.TrimSpace(*request.AccountBindingID)
	}

	// connector_cli_info is also nullable — only emit a JSON blob when we
	// have something to record (a binding snapshot today; the adapter's
	// CliUsageInfo and error_kind land later via update paths in S5a/S5b).
	//
	// Marshal failure here MUST surface as an insert error: silently dropping
	// the snapshot would leave account_binding_id set with no corresponding
	// binding_snapshot, breaking claim-next-run's ability to populate
	// cli_binding for Path-B connectors.
	var connectorCliInfoArg any
	if bindingSnapshot != nil {
		envelope := models.PlanningRunCliInfo{BindingSnapshot: bindingSnapshot}
		b, marshalErr := json.Marshal(envelope)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal binding snapshot: %w", marshalErr)
		}
		connectorCliInfoArg = string(b)
	}

	_, err := s.db.Exec(`
		INSERT INTO planning_runs (
			id, project_id, requirement_id, status, trigger_source,
			provider_id, model_id, selection_source, binding_source, binding_label,
			requested_by_user_id, execution_mode, dispatch_status, connector_label,
			dispatch_error, error_message, started_at, completed_at, created_at, updated_at,
			adapter_type, model_override, account_binding_id, connector_cli_info
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '', '', '', NULL, NULL, $14, $14, $15, $16, $17, $18)
	`, id, projectID, requirementID, models.PlanningRunStatusQueued, triggerSource, selection.ProviderID, selection.ModelID, selection.SelectionSource, selection.BindingSource, selection.BindingLabel, requestedByUser, executionMode, dispatchStatus, now, strings.TrimSpace(request.AdapterType), strings.TrimSpace(request.ModelOverride), accountBindingArg, connectorCliInfoArg)
	if err != nil {
		if isActivePlanningRunConstraintError(err) {
			return nil, ErrActivePlanningRunExists
		}
		return nil, err
	}

	return s.GetByID(id)
}

func isActivePlanningRunConstraintError(err error) bool {
	if err == nil {
		return false
	}
	// Postgres: pq.Error with SQLState 23505 + constraint name.
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code) == "23505" && pqErr.Constraint == activePlanningRunConstraint
	}
	// SQLite: partial unique index `idx_planning_runs_requirement_active` on
	// planning_runs(requirement_id) WHERE status IN ('queued','running').
	// modernc.org/sqlite surfaces the column name, not the index name.
	// The table has no other unique constraint on requirement_id, so a
	// substring match is safe.
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, "planning_runs.requirement_id")
}

func (s *PlanningRunStore) MarkRunning(id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    started_at = COALESCE(started_at, $2),
		    updated_at = $2,
		    dispatch_status = CASE
		        WHEN execution_mode = $4 THEN $5
		        ELSE dispatch_status
		    END,
		    lease_expires_at = CASE
		        WHEN execution_mode = $4 THEN NULL
		        ELSE lease_expires_at
		    END,
		    error_message = ''
		WHERE id = $3
	`, models.PlanningRunStatusRunning, now, id, models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusLeased)
	return err
}

func (s *PlanningRunStore) Complete(id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    started_at = COALESCE(started_at, $2),
		    completed_at = $2,
		    updated_at = $2,
		    dispatch_status = CASE
		        WHEN execution_mode = $4 THEN $5
		        ELSE dispatch_status
		    END,
		    lease_expires_at = CASE
		        WHEN execution_mode = $4 THEN NULL
		        ELSE lease_expires_at
		    END,
		    dispatch_error = CASE
		        WHEN execution_mode = $4 THEN ''
		        ELSE dispatch_error
		    END,
		    error_message = ''
		WHERE id = $3
	`, models.PlanningRunStatusCompleted, now, id, models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusReturned)
	return err
}

func (s *PlanningRunStore) Fail(id, errorMessage string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    started_at = COALESCE(started_at, $2),
		    completed_at = $2,
		    updated_at = $2,
		    dispatch_status = CASE
		        WHEN execution_mode = $5 THEN $6
		        ELSE dispatch_status
		    END,
		    lease_expires_at = CASE
		        WHEN execution_mode = $5 THEN NULL
		        ELSE lease_expires_at
		    END,
		    dispatch_error = CASE
		        WHEN execution_mode = $5 THEN $3
		        ELSE dispatch_error
		    END,
		    error_message = $3
		WHERE id = $4
	`, models.PlanningRunStatusFailed, now, strings.TrimSpace(errorMessage), id, models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusReturned)
	return err
}

// CancelIfActive cancels a planning run if it is still queued or running. It
// returns the updated run, or nil if the run does not exist or is already in a
// terminal state. Used to give users an escape hatch when a local connector
// dispatch is stuck waiting for a connector that never picks it up.
func (s *PlanningRunStore) CancelIfActive(id, reason string) (*models.PlanningRun, error) {
	now := time.Now().UTC()
	message := strings.TrimSpace(reason)
	if message == "" {
		message = "cancelled by user"
	}
	res, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    completed_at = $2,
		    updated_at = $2,
		    dispatch_status = CASE
		        WHEN execution_mode = $5 THEN $6
		        ELSE dispatch_status
		    END,
		    lease_expires_at = NULL,
		    dispatch_error = CASE
		        WHEN execution_mode = $5 THEN $3
		        ELSE dispatch_error
		    END,
		    error_message = $3
		WHERE id = $4
		  AND status IN ($7, $8)
	`, models.PlanningRunStatusCancelled, now, message, id,
		models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusReturned,
		models.PlanningRunStatusQueued, models.PlanningRunStatusRunning)
	if err != nil {
		return nil, err
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return nil, nil
	}
	return s.GetByID(id)
}

func (s *PlanningRunStore) LeaseNextLocalConnectorRun(userID, connectorID, connectorLabel string, leaseDuration time.Duration) (*models.PlanningRun, error) {
	// Backwards-compat shim. The old signature is preserved so existing
	// callers (and the connector test fixtures) continue to work; new
	// callers should pass the connector's protocol version explicitly via
	// LeaseNextLocalConnectorRunForProtocol so the R3 backwards-compat
	// dispatch rule (design §6.2) kicks in.
	return s.LeaseNextLocalConnectorRunForProtocol(userID, connectorID, connectorLabel, leaseDuration, 0)
}

// LeaseNextLocalConnectorRunForProtocol leases the oldest queued local-
// connector run for `userID`, refusing to hand out a run with non-NULL
// account_binding_id when the requesting connector's protocol_version is
// below 1 (Path B S2; design §6.2 R3 mitigation). The previous
// `LeaseNextLocalConnectorRun(...)` shim defaults to protocolVersion=0
// (i.e. behaves as a pre-Path-B connector that cannot be entrusted with a
// CLI-bound run); update the connector's pair flow to send 1 to opt in.
func (s *PlanningRunStore) LeaseNextLocalConnectorRunForProtocol(userID, connectorID, connectorLabel string, leaseDuration time.Duration, connectorProtocolVersion int) (*models.PlanningRun, error) {
	now := time.Now().UTC()
	leaseExpiresAt := now.Add(leaseDuration)

	_, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    dispatch_status = $2,
		    connector_id = NULL,
		    connector_label = '',
		    lease_expires_at = NULL,
		    updated_at = $3,
		    dispatch_error = 'local connector lease expired'
		WHERE execution_mode = $4
		  AND dispatch_status = $5
		  AND status = $6
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= $3
	`, models.PlanningRunStatusQueued, models.PlanningDispatchStatusExpired, now, models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusLeased, models.PlanningRunStatusRunning)
	if err != nil {
		return nil, err
	}

	// Path B S2: pre-Path-B connectors (protocol_version < 1) cannot
	// receive CLI-bound runs because they don't know how to read the
	// `cli_binding` block in the claim response. Filter such runs out so
	// the queue appears empty for the old connector; the run stays queued
	// for an updated connector to pick up later.
	bindingFilter := ""
	if connectorProtocolVersion < 1 {
		bindingFilter = " AND (account_binding_id IS NULL)"
	}

	// FOR UPDATE SKIP LOCKED is PostgreSQL row-level locking for distributed workers.
	// SQLite serialises writes at the engine level, so the clause is both unsupported
	// and unnecessary there.
	claimCTE := `
		WITH next_run AS (
			SELECT id
			FROM planning_runs
			WHERE requested_by_user_id = $1
			  AND execution_mode = $2
			  AND dispatch_status IN ($3, $4)
			  AND status = $5` + bindingFilter + `
			ORDER BY created_at ASC, id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)`
	if s.dialect.IsSQLite() {
		claimCTE = `
		WITH next_run AS (
			SELECT id
			FROM planning_runs
			WHERE requested_by_user_id = $1
			  AND execution_mode = $2
			  AND dispatch_status IN ($3, $4)
			  AND status = $5` + bindingFilter + `
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		)`
	}
	row := s.db.QueryRow(claimCTE+`
		UPDATE planning_runs
		SET status = $6,
		    dispatch_status = $7,
		    connector_id = $8,
		    connector_label = $9,
		    started_at = COALESCE(started_at, $10),
		    lease_expires_at = $11,
		    updated_at = $10,
		    dispatch_error = '',
		    error_message = ''
		WHERE id = (SELECT id FROM next_run)
		RETURNING id, project_id, requirement_id, status, trigger_source, provider_id, model_id,
		          selection_source, binding_source, binding_label, requested_by_user_id,
		          execution_mode, dispatch_status, connector_id, connector_label,
		          lease_expires_at, dispatch_error, error_message, started_at, completed_at,
		          created_at, updated_at, adapter_type, model_override, account_binding_id, connector_cli_info
	`, strings.TrimSpace(userID), models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusQueued, models.PlanningDispatchStatusExpired, models.PlanningRunStatusQueued, models.PlanningRunStatusRunning, models.PlanningDispatchStatusLeased, strings.TrimSpace(connectorID), strings.TrimSpace(connectorLabel), now, leaseExpiresAt)
	run, err := scanOnePlanningRun(row)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *PlanningRunStore) GetLeasedLocalConnectorRun(id, connectorID string) (*models.PlanningRun, error) {
	row := s.db.QueryRow(`
		SELECT id, project_id, requirement_id, status, trigger_source, provider_id, model_id,
		       selection_source, binding_source, binding_label, requested_by_user_id,
		       execution_mode, dispatch_status, connector_id, connector_label,
		       lease_expires_at, dispatch_error, error_message, started_at, completed_at,
		       created_at, updated_at, adapter_type, model_override, account_binding_id, connector_cli_info
		FROM planning_runs
		WHERE id = $1
		  AND connector_id = $2
		  AND execution_mode = $3
		  AND dispatch_status = $4
		  AND status = $5
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at > CURRENT_TIMESTAMP
	`, id, strings.TrimSpace(connectorID), models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusLeased, models.PlanningRunStatusRunning)
	return scanOnePlanningRun(row)
}

func (s *PlanningRunStore) CompleteLocalConnectorRun(id, connectorID, cliInfoJSON string) error {
	now := time.Now().UTC()
	// Path B S2: connector_cli_info is now an envelope that may already hold
	// a binding_snapshot taken at run-create time. The submit-result handler
	// passes us the adapter's CliUsageInfo; merge it into the envelope so
	// the snapshot survives and we don't clobber an audited Path B binding.
	mergedJSON, err := s.mergeCliInfoEnvelope(id, cliInfoJSON)
	if err != nil {
		return err
	}
	var cliInfoArg any
	if strings.TrimSpace(mergedJSON) != "" {
		cliInfoArg = mergedJSON
	}
	result, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    dispatch_status = $2,
		    completed_at = $3,
		    updated_at = $3,
		    lease_expires_at = NULL,
		    dispatch_error = '',
		    error_message = '',
		    connector_cli_info = $9
		WHERE id = $4
		  AND connector_id = $5
		  AND execution_mode = $6
		  AND dispatch_status = $7
		  AND status = $8
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at > $3
	`, models.PlanningRunStatusCompleted, models.PlanningDispatchStatusReturned, now, id, strings.TrimSpace(connectorID), models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusLeased, models.PlanningRunStatusRunning, cliInfoArg)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrPlanningRunLeaseUnavailable
	}
	return nil
}

// mergeCliInfoEnvelope reads any existing PlanningRunCliInfo for `id` and
// folds the new adapter-supplied CliUsageInfo (`adapterPayload` is the JSON
// the submit-result handler captured) into the `cli_invocation` field. The
// existing `binding_snapshot` and `dispatch_warning` (if any) are
// preserved. If `adapterPayload` is empty we still flush whatever envelope
// already exists so a successful run with no CliUsageInfo doesn't drop the
// snapshot.
func (s *PlanningRunStore) mergeCliInfoEnvelope(id, adapterPayload string) (string, error) {
	var existingRaw sql.NullString
	if err := s.db.QueryRow(`SELECT connector_cli_info FROM planning_runs WHERE id = $1`, id).Scan(&existingRaw); err != nil {
		if err == sql.ErrNoRows {
			return adapterPayload, nil
		}
		return "", err
	}
	envelope := models.PlanningRunCliInfo{}
	if existingRaw.Valid && existingRaw.String != "" {
		// Try the new envelope shape first. Note: a legacy bare CliUsageInfo
		// payload like `{"agent":...,"model":...}` will Unmarshal into the
		// envelope WITHOUT an error (Go's JSON decoder silently ignores
		// unknown fields by default), but the resulting envelope will be
		// all-zero. Mirror scanPlanningRun's check: if Unmarshal fails OR
		// the envelope has no recognised fields set, attempt the legacy
		// CliUsageInfo decode as a second pass so we don't lose the
		// historical adapter info on update.
		envelopeOK := false
		if err := json.Unmarshal([]byte(existingRaw.String), &envelope); err == nil {
			if envelope.BindingSnapshot != nil || envelope.Invocation != nil ||
				envelope.ErrorKind != "" || envelope.DispatchWarning != "" {
				envelopeOK = true
			}
		}
		if !envelopeOK {
			var legacy models.CliUsageInfo
			if err := json.Unmarshal([]byte(existingRaw.String), &legacy); err == nil &&
				(legacy.Agent != "" || legacy.Model != "" || legacy.ModelSource != "") {
				envelope.Invocation = &legacy
			}
		}
	}
	if strings.TrimSpace(adapterPayload) != "" {
		var payload models.CliUsageInfo
		if err := json.Unmarshal([]byte(adapterPayload), &payload); err == nil {
			envelope.Invocation = &payload
		}
	}
	if envelope.BindingSnapshot == nil && envelope.Invocation == nil &&
		envelope.ErrorKind == "" && envelope.DispatchWarning == "" {
		return "", nil
	}
	merged, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return string(merged), nil
}

// MarkDispatchWarning sets a one-shot dispatch_warning string inside the
// run's connector_cli_info envelope without touching dispatch_status. Used
// by the claim path when a CLI-bound run is skipped because the requesting
// connector reports protocol_version < 1 (R3 mitigation, design §6.2). The
// existing binding_snapshot is preserved.
func (s *PlanningRunStore) MarkDispatchWarning(id, warning string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	var existingRaw sql.NullString
	if err := s.db.QueryRow(`SELECT connector_cli_info FROM planning_runs WHERE id = $1`, id).Scan(&existingRaw); err != nil {
		return err
	}
	envelope := models.PlanningRunCliInfo{}
	if existingRaw.Valid && existingRaw.String != "" {
		_ = json.Unmarshal([]byte(existingRaw.String), &envelope)
	}
	envelope.DispatchWarning = strings.TrimSpace(warning)
	merged, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE planning_runs SET connector_cli_info = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, string(merged), id)
	return err
}

// MarkErrorKind stores error_kind and the server-side remediation_hint in the
// run's connector_cli_info envelope. Called by the submit-result handler when
// the adapter reports a failure with a structured error kind (S5a). The
// existing binding_snapshot and dispatch_warning are preserved.
func (s *PlanningRunStore) MarkErrorKind(id, errorKind, remediationHint string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	var existingRaw sql.NullString
	if err := s.db.QueryRow(`SELECT connector_cli_info FROM planning_runs WHERE id = $1`, id).Scan(&existingRaw); err != nil {
		return err
	}
	envelope := models.PlanningRunCliInfo{}
	if existingRaw.Valid && existingRaw.String != "" {
		envelopeOK := false
		if err := json.Unmarshal([]byte(existingRaw.String), &envelope); err == nil {
			if envelope.BindingSnapshot != nil || envelope.Invocation != nil ||
				envelope.ErrorKind != "" || envelope.DispatchWarning != "" {
				envelopeOK = true
			}
		}
		if !envelopeOK {
			var legacy models.CliUsageInfo
			if err := json.Unmarshal([]byte(existingRaw.String), &legacy); err == nil &&
				(legacy.Agent != "" || legacy.Model != "" || legacy.ModelSource != "") {
				envelope = models.PlanningRunCliInfo{Invocation: &legacy}
			}
		}
	}
	envelope.ErrorKind = strings.TrimSpace(errorKind)
	envelope.RemediationHint = strings.TrimSpace(remediationHint)
	merged, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE planning_runs SET connector_cli_info = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, string(merged), id)
	return err
}

func (s *PlanningRunStore) FailLocalConnectorRun(id, connectorID, errorMessage string) error {
	now := time.Now().UTC()
	message := strings.TrimSpace(errorMessage)
	result, err := s.db.Exec(`
		UPDATE planning_runs
		SET status = $1,
		    dispatch_status = $2,
		    completed_at = $3,
		    updated_at = $3,
		    lease_expires_at = NULL,
		    dispatch_error = $4,
		    error_message = $4
		WHERE id = $5
		  AND connector_id = $6
		  AND execution_mode = $7
		  AND dispatch_status = $8
		  AND status = $9
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at > $3
	`, models.PlanningRunStatusFailed, models.PlanningDispatchStatusReturned, now, message, id, strings.TrimSpace(connectorID), models.PlanningExecutionModeLocalConnector, models.PlanningDispatchStatusLeased, models.PlanningRunStatusRunning)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrPlanningRunLeaseUnavailable
	}
	return nil
}

func (s *PlanningRunStore) GetByID(id string) (*models.PlanningRun, error) {
	row := s.db.QueryRow(`
		SELECT id, project_id, requirement_id, status, trigger_source, provider_id, model_id,
		       selection_source, binding_source, binding_label, requested_by_user_id,
		       execution_mode, dispatch_status, connector_id, connector_label,
		       lease_expires_at, dispatch_error, error_message, started_at, completed_at,
		       created_at, updated_at, adapter_type, model_override, account_binding_id, connector_cli_info
		FROM planning_runs
		WHERE id = $1
	`, id)
	return scanOnePlanningRun(row)
}

func (s *PlanningRunStore) ListByRequirement(requirementID string, page, perPage int) ([]models.PlanningRun, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM planning_runs WHERE requirement_id = $1`, requirementID).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, requirement_id, status, trigger_source, provider_id, model_id,
		       selection_source, binding_source, binding_label, requested_by_user_id,
		       execution_mode, dispatch_status, connector_id, connector_label,
		       lease_expires_at, dispatch_error, error_message, started_at, completed_at,
		       created_at, updated_at, adapter_type, model_override, account_binding_id, connector_cli_info
		FROM planning_runs
		WHERE requirement_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, requirementID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	runs := make([]models.PlanningRun, 0)
	for rows.Next() {
		run, err := scanPlanningRun(rows)
		if err != nil {
			return nil, 0, err
		}
		runs = append(runs, *run)
	}

	return runs, total, rows.Err()
}

func (s *PlanningRunStore) GetActiveByRequirement(requirementID string) (*models.PlanningRun, error) {
	var id string
	err := s.db.QueryRow(`
		SELECT id
		FROM planning_runs
		WHERE requirement_id = $1 AND status IN ($2, $3)
		ORDER BY created_at DESC
		LIMIT 1
	`, requirementID, models.PlanningRunStatusQueued, models.PlanningRunStatusRunning).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

type planningRunScanner interface {
	Scan(dest ...interface{}) error
}

func scanOnePlanningRun(row planningRunScanner) (*models.PlanningRun, error) {
	run, err := scanPlanningRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return run, nil
}

func scanPlanningRun(scanner planningRunScanner) (*models.PlanningRun, error) {
	var run models.PlanningRun
	var requestedByUserID sql.NullString
	var connectorID sql.NullString
	var leaseExpiresAt sql.NullTime
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var adapterType sql.NullString
	var modelOverride sql.NullString
	var accountBindingID sql.NullString
	var connectorCliInfo sql.NullString
	err := scanner.Scan(
		&run.ID,
		&run.ProjectID,
		&run.RequirementID,
		&run.Status,
		&run.TriggerSource,
		&run.ProviderID,
		&run.ModelID,
		&run.SelectionSource,
		&run.BindingSource,
		&run.BindingLabel,
		&requestedByUserID,
		&run.ExecutionMode,
		&run.DispatchStatus,
		&connectorID,
		&run.ConnectorLabel,
		&leaseExpiresAt,
		&run.DispatchError,
		&run.ErrorMessage,
		&startedAt,
		&completedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
		&adapterType,
		&modelOverride,
		&accountBindingID,
		&connectorCliInfo,
	)
	if err != nil {
		return nil, err
	}
	if requestedByUserID.Valid {
		run.RequestedByUserID = requestedByUserID.String
	}
	if connectorID.Valid {
		run.ConnectorID = connectorID.String
	}
	if leaseExpiresAt.Valid {
		run.LeaseExpiresAt = &leaseExpiresAt.Time
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if adapterType.Valid {
		run.AdapterType = adapterType.String
	}
	if modelOverride.Valid {
		run.ModelOverride = modelOverride.String
	}
	if accountBindingID.Valid && accountBindingID.String != "" {
		bid := accountBindingID.String
		run.AccountBindingID = &bid
	}
	if connectorCliInfo.Valid && connectorCliInfo.String != "" {
		// Path B S2: connector_cli_info now holds a richer envelope
		// (PlanningRunCliInfo). Older rows may contain a bare CliUsageInfo
		// payload (`{agent, model, model_source}`); decode that as a
		// fallback so we don't lose the historical adapter info.
		var envelope models.PlanningRunCliInfo
		if err := json.Unmarshal([]byte(connectorCliInfo.String), &envelope); err == nil &&
			(envelope.BindingSnapshot != nil || envelope.Invocation != nil ||
				envelope.ErrorKind != "" || envelope.DispatchWarning != "") {
			run.ConnectorCliInfo = &envelope
		} else {
			var legacy models.CliUsageInfo
			if err := json.Unmarshal([]byte(connectorCliInfo.String), &legacy); err == nil &&
				(legacy.Agent != "" || legacy.Model != "" || legacy.ModelSource != "") {
				run.ConnectorCliInfo = &models.PlanningRunCliInfo{Invocation: &legacy}
			}
		}
	}
	return &run, nil
}

// ListQueuedCliBoundRunIDsForUser returns the IDs of every queued
// local-connector planning run with a non-NULL account_binding_id for the
// given user. Used by the claim path so we can stamp a one-shot
// `dispatch_warning` on each run when the requesting connector is too old
// to handle the cli_binding block (R3 mitigation, design §6.2 step
// "claim-next-run"). The list is ordered newest-first so callers can cap
// the number of stamps if needed; today every queued match is stamped.
func (s *PlanningRunStore) ListQueuedCliBoundRunIDsForUser(userID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT id FROM planning_runs
		WHERE requested_by_user_id = $1
		  AND execution_mode = $2
		  AND status = $3
		  AND account_binding_id IS NOT NULL
		ORDER BY created_at DESC
	`, strings.TrimSpace(userID), models.PlanningExecutionModeLocalConnector, models.PlanningRunStatusQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// HasQueuedCliBoundRunsForUser reports whether the user has any queued
// local-connector planning run with a non-NULL account_binding_id. Used
// to scope the R3 "connector outdated" warning so we only nag the user
// when the dispatch dance actually has a CLI-bound run waiting.
func (s *PlanningRunStore) HasQueuedCliBoundRunsForUser(userID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM planning_runs
		WHERE requested_by_user_id = $1
		  AND execution_mode = $2
		  AND status = $3
		  AND account_binding_id IS NOT NULL
	`, strings.TrimSpace(userID), models.PlanningExecutionModeLocalConnector, models.PlanningRunStatusQueued).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RunStatsByUser returns planning run counts for a user across time windows.
// All counts include runs from any execution mode (local connector or server provider).
func (s *PlanningRunStore) RunStatsByUser(userID string) (models.ConnectorRunStats, error) {
	var stats models.ConnectorRunStats
	var query string
	if s.dialect.IsSQLite() {
		// SQLite date arithmetic uses datetime() modifiers; INTERVAL is PostgreSQL-only.
		// FILTER on aggregates is supported since SQLite 3.30.0.
		query = `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= datetime('now', '-1 day'))   AS today,
			COUNT(*) FILTER (WHERE created_at >= datetime('now', '-7 days'))  AS week,
			COUNT(*) FILTER (WHERE created_at >= datetime('now', '-30 days')) AS month,
			COUNT(*)                                                           AS total
		FROM planning_runs
		WHERE requested_by_user_id = $1`
	} else {
		query = `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '1 day')   AS today,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days')  AS week,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days') AS month,
			COUNT(*)                                                          AS total
		FROM planning_runs
		WHERE requested_by_user_id = $1`
	}
	err := s.db.QueryRow(query, strings.TrimSpace(userID)).Scan(
		&stats.RunsToday, &stats.RunsWeek, &stats.RunsMonth, &stats.RunsTotal,
	)
	return stats, err
}

func resolvePlanningExecutionMode(request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection) string {
	switch strings.TrimSpace(request.ExecutionMode) {
	case models.PlanningExecutionModeLocalConnector:
		return models.PlanningExecutionModeLocalConnector
	case models.PlanningExecutionModeDeterministic:
		return models.PlanningExecutionModeDeterministic
	case models.PlanningExecutionModeServerProvider:
		return models.PlanningExecutionModeServerProvider
	}
	if strings.TrimSpace(selection.ProviderID) == models.PlanningProviderDeterministic {
		return models.PlanningExecutionModeDeterministic
	}
	return models.PlanningExecutionModeServerProvider
}
