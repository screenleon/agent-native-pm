package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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

	_, err := s.db.Exec(`
		INSERT INTO planning_runs (
			id, project_id, requirement_id, status, trigger_source,
			provider_id, model_id, selection_source, binding_source, binding_label,
			requested_by_user_id, execution_mode, dispatch_status, connector_label,
			dispatch_error, error_message, started_at, completed_at, created_at, updated_at,
			adapter_type, model_override
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '', '', '', NULL, NULL, $14, $14, $15, $16)
	`, id, projectID, requirementID, models.PlanningRunStatusQueued, triggerSource, selection.ProviderID, selection.ModelID, selection.SelectionSource, selection.BindingSource, selection.BindingLabel, requestedByUser, executionMode, dispatchStatus, now, strings.TrimSpace(request.AdapterType), strings.TrimSpace(request.ModelOverride))
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
			  AND status = $5
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
			  AND status = $5
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
		          created_at, updated_at, adapter_type, model_override, connector_cli_info
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
		       created_at, updated_at, adapter_type, model_override, connector_cli_info
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
	var cliInfoArg any
	if strings.TrimSpace(cliInfoJSON) != "" {
		cliInfoArg = cliInfoJSON
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
		       created_at, updated_at, adapter_type, model_override, connector_cli_info
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
		       created_at, updated_at, adapter_type, model_override, connector_cli_info
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
	if connectorCliInfo.Valid && connectorCliInfo.String != "" {
		var info models.CliUsageInfo
		if err := json.Unmarshal([]byte(connectorCliInfo.String), &info); err == nil {
			run.ConnectorCliInfo = &info
		}
	}
	return &run, nil
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
