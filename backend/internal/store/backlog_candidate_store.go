package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
)

var (
	ErrBacklogCandidateNotMutable  = errors.New("backlog candidate is not mutable")
	ErrBacklogCandidateNoChanges   = errors.New("no backlog candidate changes requested")
	ErrBacklogCandidateBlankTitle  = errors.New("backlog candidate title cannot be blank")
	ErrBacklogCandidateBadStatus   = errors.New("invalid backlog candidate status")
	ErrBacklogCandidateNotApproved         = errors.New("backlog candidate must be approved before applying")
	ErrBacklogCandidateInvalidExecutionMode = errors.New("invalid execution_mode (expected 'manual' or 'role_dispatch')")
)

const appliedCandidateTaskSource = "agent:planning-orchestrator"

type BacklogCandidateTaskConflictError struct {
	Task *models.Task
}

func (e *BacklogCandidateTaskConflictError) Error() string {
	if e == nil || e.Task == nil {
		return "open task with matching title already exists"
	}
	return "open task with matching title already exists: " + e.Task.Title
}

type BacklogCandidateStore struct {
	db      *sql.DB
	dialect database.Dialect
}

func NewBacklogCandidateStore(db *sql.DB, dialect database.Dialect) *BacklogCandidateStore {
	return &BacklogCandidateStore{db: db, dialect: dialect}
}

func (s *BacklogCandidateStore) CreateDraftsForPlanningRun(requirement *models.Requirement, planningRunID string, drafts []models.BacklogCandidateDraft) ([]models.BacklogCandidate, error) {
	if requirement == nil {
		return nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	created := make([]models.BacklogCandidate, 0, len(drafts))
	for index, draft := range drafts {
		id := uuid.New().String()
		now := time.Now().UTC()
		evidenceJSON, err := marshalStringList(draft.Evidence)
		if err != nil {
			return nil, err
		}
		duplicateJSON, err := marshalStringList(draft.DuplicateTitles)
		if err != nil {
			return nil, err
		}
		evidenceDetailJSON, err := marshalEvidenceDetail(draft.EvidenceDetail)
		if err != nil {
			return nil, err
		}

		candidate := models.BacklogCandidate{
			ID:                 id,
			ProjectID:          requirement.ProjectID,
			RequirementID:      requirement.ID,
			PlanningRunID:      planningRunID,
			ParentCandidateID:  strings.TrimSpace(draft.ParentCandidateID),
			SuggestionType:     normalizeSuggestionType(draft.SuggestionType),
			Title:              strings.TrimSpace(draft.Title),
			Description:        strings.TrimSpace(draft.Description),
			Status:             models.BacklogCandidateStatusDraft,
			Rationale:          strings.TrimSpace(draft.Rationale),
			ValidationCriteria: strings.TrimSpace(draft.ValidationCriteria),
			PODecision:         strings.TrimSpace(draft.PODecision),
			PriorityScore:      draft.PriorityScore,
			Confidence:         draft.Confidence,
			Rank:               draft.Rank,
			Evidence:           cloneStringSlice(draft.Evidence),
			EvidenceDetail:     cloneEvidenceDetail(draft.EvidenceDetail),
			DuplicateTitles:    cloneStringSlice(draft.DuplicateTitles),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if candidate.Rank < 1 {
			candidate.Rank = index + 1
		}

		var parentCandidateID any
		if candidate.ParentCandidateID != "" {
			parentCandidateID = candidate.ParentCandidateID
		}

		var executionRole any
		if er := strings.TrimSpace(draft.ExecutionRole); er != "" {
			executionRole = er
			candidate.ExecutionRole = &er
		}

		_, err = tx.Exec(`
			INSERT INTO backlog_candidates (
				id, project_id, requirement_id, planning_run_id, parent_candidate_id,
				suggestion_type, title, description, status, rationale, validation_criteria, po_decision,
				priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles,
				execution_role, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $20)
		`, candidate.ID, candidate.ProjectID, candidate.RequirementID, candidate.PlanningRunID, parentCandidateID, candidate.SuggestionType, candidate.Title, candidate.Description, candidate.Status, candidate.Rationale, candidate.ValidationCriteria, candidate.PODecision, candidate.PriorityScore, candidate.Confidence, candidate.Rank, evidenceJSON, evidenceDetailJSON, duplicateJSON, executionRole, candidate.CreatedAt)
		if err != nil {
			return nil, err
		}

		created = append(created, candidate)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return created, nil
}

func (s *BacklogCandidateStore) GetByID(id string) (*models.BacklogCandidate, error) {
	return scanBacklogCandidate(
		s.db.QueryRow(`
			SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, execution_role, created_at, updated_at
			FROM backlog_candidates
			WHERE id = $1
		`, id),
	)
}

func (s *BacklogCandidateStore) ListByPlanningRun(planningRunID string, page, perPage int) ([]models.BacklogCandidate, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM backlog_candidates WHERE planning_run_id = $1`, planningRunID).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, execution_role, created_at, updated_at
		FROM backlog_candidates
		WHERE planning_run_id = $1
		ORDER BY rank ASC, priority_score DESC, created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`, planningRunID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	candidates := make([]models.BacklogCandidate, 0)
	for rows.Next() {
		var candidate models.BacklogCandidate
		var parentCandidateID sql.NullString
		var executionRole sql.NullString
		var evidenceJSON []byte
		var evidenceDetailJSON []byte
		var duplicateJSON []byte
		if err := rows.Scan(
			&candidate.ID,
			&candidate.ProjectID,
			&candidate.RequirementID,
			&candidate.PlanningRunID,
			&parentCandidateID,
			&candidate.SuggestionType,
			&candidate.Title,
			&candidate.Description,
			&candidate.Status,
			&candidate.Rationale,
			&candidate.ValidationCriteria,
			&candidate.PODecision,
			&candidate.PriorityScore,
			&candidate.Confidence,
			&candidate.Rank,
			&evidenceJSON,
			&evidenceDetailJSON,
			&duplicateJSON,
			&executionRole,
			&candidate.CreatedAt,
			&candidate.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if parentCandidateID.Valid {
			candidate.ParentCandidateID = parentCandidateID.String
		}
		if executionRole.Valid {
			v := executionRole.String
			candidate.ExecutionRole = &v
		}
		candidate.Evidence = unmarshalStringList(evidenceJSON)
		candidate.EvidenceDetail = unmarshalEvidenceDetail(evidenceDetailJSON)
		candidate.DuplicateTitles = unmarshalStringList(duplicateJSON)
		candidates = append(candidates, candidate)
	}

	return candidates, total, rows.Err()
}

func (s *BacklogCandidateStore) DeleteByPlanningRun(planningRunID string) error {
	_, err := s.db.Exec(`DELETE FROM backlog_candidates WHERE planning_run_id = $1`, planningRunID)
	return err
}

func (s *BacklogCandidateStore) Update(id string, req models.UpdateBacklogCandidateRequest) (*models.BacklogCandidate, error) {
	candidate, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, nil
	}
	if candidate.Status == models.BacklogCandidateStatusApplied {
		return nil, ErrBacklogCandidateNotMutable
	}

	title := candidate.Title
	description := candidate.Description
	status := candidate.Status
	// Carry the current value (or "") so a partial patch leaves it alone.
	var executionRoleValue any
	if candidate.ExecutionRole != nil {
		executionRoleValue = *candidate.ExecutionRole
	}
	changed := false

	if req.Title != nil {
		normalizedTitle := strings.TrimSpace(*req.Title)
		if normalizedTitle == "" {
			return nil, ErrBacklogCandidateBlankTitle
		}
		if normalizedTitle != title {
			title = normalizedTitle
			changed = true
		}
	}

	if req.Description != nil {
		if *req.Description != description {
			description = *req.Description
			changed = true
		}
	}

	if req.Status != nil {
		normalizedStatus := strings.TrimSpace(*req.Status)
		if !models.ValidBacklogCandidateReviewStatuses[normalizedStatus] {
			return nil, ErrBacklogCandidateBadStatus
		}
		if normalizedStatus != status {
			status = normalizedStatus
			changed = true
		}
	}

	if req.ExecutionRole != nil {
		trimmed := strings.TrimSpace(*req.ExecutionRole)
		// Empty string clears the column (NULL). Non-empty sets it.
		// Catalog validation is deliberately deferred to Phase 6 (see
		// docs/phase5-plan.md §8 Q2). Do NOT add a role-allowlist check
		// here without bumping the Phase 5 DECISIONS entry — the contract
		// explicitly allows unknown role strings today so Phase 6 can
		// introduce catalog enforcement as a single atomic change with
		// migration of any existing rows.
		if trimmed == "" {
			if candidate.ExecutionRole != nil {
				executionRoleValue = nil
				changed = true
			}
		} else {
			if candidate.ExecutionRole == nil || *candidate.ExecutionRole != trimmed {
				executionRoleValue = trimmed
				changed = true
			}
		}
	}

	if !changed {
		return nil, ErrBacklogCandidateNoChanges
	}

	now := time.Now().UTC()
	_, err = s.db.Exec(`
		UPDATE backlog_candidates
		SET title = $1,
		    description = $2,
		    status = $3,
		    execution_role = $4,
		    updated_at = $5
		WHERE id = $6
	`, title, description, status, executionRoleValue, now, id)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

// ApplyToTask is the Phase-4-and-earlier entry point: always applies with
// execution mode "manual". Kept as a shim so existing call sites compile
// without change. New callers should use ApplyToTaskWithMode.
func (s *BacklogCandidateStore) ApplyToTask(id string) (*models.ApplyBacklogCandidateResponse, error) {
	return s.ApplyToTaskWithMode(id, models.ApplyExecutionModeManual)
}

// ApplyToTaskWithMode (Phase 5 B3) applies the candidate with an explicit
// execution mode. "manual" behaves identically to the pre-Phase-5 flow.
// "role_dispatch" marks the created task's `source` with the candidate's
// execution_role so the audit trail distinguishes dispatch-earmarked
// tasks; actual dispatch is Phase 6.
func (s *BacklogCandidateStore) ApplyToTaskWithMode(id, executionMode string) (*models.ApplyBacklogCandidateResponse, error) {
	if executionMode == "" {
		executionMode = models.ApplyExecutionModeManual
	}
	if !models.ValidApplyExecutionModes[executionMode] {
		return nil, ErrBacklogCandidateInvalidExecutionMode
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	candidate, err := s.getByIDForUpdate(tx, id)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, nil
	}

	normalizedTitle := normalizeCandidateTitle(candidate.Title)
	if normalizedTitle == "" {
		return nil, ErrBacklogCandidateBlankTitle
	}

	if err := s.lockCandidateApplyKey(tx, candidate.ProjectID, normalizedTitle); err != nil {
		return nil, err
	}

	lineage, err := getTaskLineageByCandidateID(tx, candidate.ID)
	if err != nil {
		return nil, err
	}
	if lineage != nil {
		task, err := getTaskByID(tx, lineage.TaskID)
		if err != nil {
			return nil, err
		}
		if task == nil {
			lineage = nil
		} else {
			if candidate.Status != models.BacklogCandidateStatusApplied {
				now := time.Now().UTC()
				if err := updateBacklogCandidateStatus(tx, candidate.ID, models.BacklogCandidateStatusApplied, now); err != nil {
					return nil, err
				}
				candidate.Status = models.BacklogCandidateStatusApplied
				candidate.UpdatedAt = now
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &models.ApplyBacklogCandidateResponse{
				Task:           *task,
				Candidate:      *candidate,
				Lineage:        *lineage,
				AlreadyApplied: true,
			}, nil
		}
	}

	if candidate.Status == models.BacklogCandidateStatusRejected {
		return nil, ErrBacklogCandidateNotApproved
	}

	duplicateTask, err := findOpenTaskByNormalizedTitle(tx, candidate.ProjectID, normalizedTitle)
	if err != nil {
		return nil, err
	}
	if duplicateTask != nil {
		return nil, &BacklogCandidateTaskConflictError{Task: duplicateTask}
	}

	// Compute the task source. Manual = the pre-Phase-5 constant.
	// role_dispatch = append the candidate's execution_role (if any) so
	// the audit trail reflects the dispatch intent.
	source := appliedCandidateTaskSource
	if executionMode == models.ApplyExecutionModeRoleDispatch {
		if candidate.ExecutionRole != nil && strings.TrimSpace(*candidate.ExecutionRole) != "" {
			source = "role_dispatch:" + strings.TrimSpace(*candidate.ExecutionRole)
		} else {
			source = "role_dispatch"
		}
		// Rune-aware truncation — Phase 5 does not enforce a role
		// catalog so execution_role can contain non-ASCII. Byte-slicing
		// could split a multi-byte codepoint and produce invalid UTF-8
		// in task.source (Copilot PR#22). Convert to []rune and slice
		// by rune count; 80 codepoints is a generous cap that still
		// fits any sensible role id.
		if runes := []rune(source); len(runes) > 80 {
			source = string(runes[:80])
		}
	}
	task, err := createAppliedCandidateTaskWithSource(tx, candidate.ProjectID, candidate.Title, candidate.Description, source)
	if err != nil {
		return nil, err
	}

	lineage, err = createTaskLineage(tx, candidate, task.ID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := updateBacklogCandidateStatus(tx, candidate.ID, models.BacklogCandidateStatusApplied, now); err != nil {
		return nil, err
	}
	candidate.Status = models.BacklogCandidateStatusApplied
	candidate.UpdatedAt = now

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.ApplyBacklogCandidateResponse{
		Task:           *task,
		Candidate:      *candidate,
		Lineage:        *lineage,
		AlreadyApplied: false,
	}, nil
}
func (s *BacklogCandidateStore) getByIDForUpdate(tx *sql.Tx, id string) (*models.BacklogCandidate, error) {
	// FOR UPDATE is Postgres row-level locking; SQLite's single-writer model
	// already serialises writes so the clause must be omitted.
	query := `
		SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, execution_role, created_at, updated_at
		FROM backlog_candidates
		WHERE id = $1 ` + s.dialect.ForUpdate()
	return scanBacklogCandidate(tx.QueryRow(query, id))
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanBacklogCandidate(row rowScanner) (*models.BacklogCandidate, error) {
	var candidate models.BacklogCandidate
	var parentCandidateID sql.NullString
	var executionRole sql.NullString
	var evidenceJSON []byte
	var evidenceDetailJSON []byte
	var duplicateJSON []byte
	err := row.Scan(
		&candidate.ID,
		&candidate.ProjectID,
		&candidate.RequirementID,
		&candidate.PlanningRunID,
		&parentCandidateID,
		&candidate.SuggestionType,
		&candidate.Title,
		&candidate.Description,
		&candidate.Status,
		&candidate.Rationale,
		&candidate.ValidationCriteria,
		&candidate.PODecision,
		&candidate.PriorityScore,
		&candidate.Confidence,
		&candidate.Rank,
		&evidenceJSON,
		&evidenceDetailJSON,
		&duplicateJSON,
		&executionRole,
		&candidate.CreatedAt,
		&candidate.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if parentCandidateID.Valid {
		candidate.ParentCandidateID = parentCandidateID.String
	}
	if executionRole.Valid {
		v := executionRole.String
		candidate.ExecutionRole = &v
	}
	candidate.Evidence = unmarshalStringList(evidenceJSON)
	candidate.EvidenceDetail = unmarshalEvidenceDetail(evidenceDetailJSON)
	candidate.DuplicateTitles = unmarshalStringList(duplicateJSON)
	return &candidate, nil
}

func normalizeSuggestionType(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "implementation"
	}
	return trimmed
}

func marshalStringList(values []string) ([]byte, error) {
	return json.Marshal(cloneStringSlice(values))
}

func marshalEvidenceDetail(detail models.PlanningEvidenceDetail) ([]byte, error) {
	return json.Marshal(cloneEvidenceDetail(detail))
}

func unmarshalStringList(raw []byte) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []string{}
	}
	values := []string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return cloneStringSlice(values)
}

func unmarshalEvidenceDetail(raw []byte) models.PlanningEvidenceDetail {
	if len(bytes.TrimSpace(raw)) == 0 {
		return models.PlanningEvidenceDetail{}
	}
	var detail models.PlanningEvidenceDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return models.PlanningEvidenceDetail{}
	}
	return cloneEvidenceDetail(detail)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	cloned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		cloned = append(cloned, trimmed)
	}
	if len(cloned) == 0 {
		return []string{}
	}
	return cloned
}

func cloneEvidenceDetail(detail models.PlanningEvidenceDetail) models.PlanningEvidenceDetail {
	cloned := detail
	cloned.Summary = cloneStringSlice(detail.Summary)
	cloned.Documents = append([]models.PlanningDocumentEvidence{}, detail.Documents...)
	for index := range cloned.Documents {
		cloned.Documents[index].MatchedKeywords = cloneStringSlice(detail.Documents[index].MatchedKeywords)
		cloned.Documents[index].ContributionReasons = cloneStringSlice(detail.Documents[index].ContributionReasons)
	}
	cloned.DriftSignals = append([]models.PlanningDriftSignalEvidence{}, detail.DriftSignals...)
	for index := range cloned.DriftSignals {
		cloned.DriftSignals[index].ContributionReasons = cloneStringSlice(detail.DriftSignals[index].ContributionReasons)
	}
	if detail.SyncRun != nil {
		syncRun := *detail.SyncRun
		syncRun.ContributionReasons = cloneStringSlice(detail.SyncRun.ContributionReasons)
		cloned.SyncRun = &syncRun
	}
	cloned.AgentRuns = append([]models.PlanningAgentRunEvidence{}, detail.AgentRuns...)
	for index := range cloned.AgentRuns {
		cloned.AgentRuns[index].ContributionReasons = cloneStringSlice(detail.AgentRuns[index].ContributionReasons)
	}
	cloned.Duplicates = append([]models.PlanningDuplicateEvidence{}, detail.Duplicates...)
	for index := range cloned.Duplicates {
		cloned.Duplicates[index].ContributionReasons = cloneStringSlice(detail.Duplicates[index].ContributionReasons)
	}
	return cloned
}

func scanTask(row rowScanner) (*models.Task, error) {
	var task models.Task
	var executionResultRaw sql.NullString
	err := row.Scan(
		&task.ID, &task.ProjectID, &task.Title, &task.Description,
		&task.Status, &task.Priority, &task.Assignee, &task.Source,
		&task.DispatchStatus, &executionResultRaw,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if executionResultRaw.Valid && executionResultRaw.String != "" {
		task.ExecutionResult = json.RawMessage(executionResultRaw.String)
	}
	if task.DispatchStatus == "" {
		task.DispatchStatus = models.TaskDispatchStatusNone
	}
	return &task, nil
}

func scanTaskLineage(row rowScanner) (*models.TaskLineage, error) {
	var lineage models.TaskLineage
	var requirementID sql.NullString
	var planningRunID sql.NullString
	var backlogCandidateID sql.NullString
	err := row.Scan(
		&lineage.ID,
		&lineage.ProjectID,
		&lineage.TaskID,
		&requirementID,
		&planningRunID,
		&backlogCandidateID,
		&lineage.LineageKind,
		&lineage.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if requirementID.Valid {
		lineage.RequirementID = requirementID.String
	}
	if planningRunID.Valid {
		lineage.PlanningRunID = planningRunID.String
	}
	if backlogCandidateID.Valid {
		lineage.BacklogCandidateID = backlogCandidateID.String
	}
	return &lineage, nil
}

// ListAppliedLineageByProject returns denormalised applied-candidate
// lineage rows for a project, joined with requirement / planning_run /
// backlog_candidate / task titles. The Planning Workspace
// applied-lineage lane consumes this to render the
// requirement → run → candidate → task chain without N extra API calls.
//
// FK semantics from migration 009 determine the join types:
//   - task_lineage.task_id: ON DELETE CASCADE (lineage row dies with the
//     task, so INNER JOIN would normally be safe) — we still use LEFT JOIN
//     + COALESCE defensively so a future FK change or a stray NULL never
//     makes a lineage row disappear silently from the lane.
//   - requirement_id / planning_run_id / backlog_candidate_id:
//     ON DELETE SET NULL — the lineage row survives with NULL refs, so
//     LEFT JOIN + COALESCE is required to keep it visible with graceful
//     fallbacks.
//
// Only entries with lineage_kind = 'applied_candidate' are returned —
// manual / merged kinds are out of scope for the planning lane.
// Results are ordered by created_at DESC, id DESC.
func (s *BacklogCandidateStore) ListAppliedLineageByProject(projectID string) ([]models.AppliedLineageEntry, error) {
	const query = `
		SELECT
			tl.id,
			tl.project_id,
			tl.task_id,
			COALESCE(t.title, ''),
			COALESCE(t.status, ''),
			COALESCE(tl.requirement_id, ''),
			COALESCE(r.title, ''),
			COALESCE(tl.planning_run_id, ''),
			COALESCE(pr.status, ''),
			COALESCE(tl.backlog_candidate_id, ''),
			COALESCE(bc.title, ''),
			tl.lineage_kind,
			tl.created_at
		FROM task_lineage tl
		LEFT JOIN tasks t ON t.id = tl.task_id
		LEFT JOIN requirements r ON r.id = tl.requirement_id
		LEFT JOIN planning_runs pr ON pr.id = tl.planning_run_id
		LEFT JOIN backlog_candidates bc ON bc.id = tl.backlog_candidate_id
		WHERE tl.project_id = $1
		  AND tl.lineage_kind = $2
		ORDER BY tl.created_at DESC, tl.id DESC
	`
	rows, err := s.db.Query(query, projectID, models.TaskLineageKindAppliedCandidate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AppliedLineageEntry, 0)
	for rows.Next() {
		var e models.AppliedLineageEntry
		if err := rows.Scan(
			&e.LineageID,
			&e.ProjectID,
			&e.TaskID,
			&e.TaskTitle,
			&e.TaskStatus,
			&e.RequirementID,
			&e.RequirementTitle,
			&e.PlanningRunID,
			&e.PlanningRunStatus,
			&e.BacklogCandidateID,
			&e.BacklogCandidateTitle,
			&e.LineageKind,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getTaskLineageByCandidateID(tx *sql.Tx, candidateID string) (*models.TaskLineage, error) {
	return scanTaskLineage(
		tx.QueryRow(`
			SELECT id, project_id, task_id, requirement_id, planning_run_id, backlog_candidate_id, lineage_kind, created_at
			FROM task_lineage
			WHERE backlog_candidate_id = $1
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		`, candidateID),
	)
}

func getTaskByID(tx *sql.Tx, id string) (*models.Task, error) {
	return scanTask(
		tx.QueryRow(`
			SELECT id, project_id, title, description, status, priority, assignee, source,
			       dispatch_status, execution_result, created_at, updated_at
			FROM tasks
			WHERE id = $1
		`, id),
	)
}

func normalizeCandidateTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

// lockCandidateApplyKey serialises "apply candidate" attempts that target
// the same (project, normalised title) tuple. The caller then performs a
// read-check (findOpenTaskByNormalizedTitle) followed by an insert, and that
// read-before-write pattern is only safe if the transaction already holds a
// write-level lock when the read happens.
//
// PostgreSQL: transaction-scoped advisory lock keyed on hashtext(projectID,
// normalizedTitle). Two transactions collide only when the tuple actually
// conflicts; unrelated candidates never block each other.
//
// SQLite: sql.DB.Begin starts a DEFERRED transaction, which only upgrades to
// a write lock on the first write statement. That means two concurrent
// Begin/read/insert sequences could both pass the duplicate-title read
// before either writer promotes. We force the upgrade here with a no-op
// UPDATE: SQLite parses and locks for the UPDATE even though WHERE 1=0
// touches no rows, so the transaction becomes the single writer before the
// caller reads. The busy_timeout (5s) set in configureSQLite then causes a
// competing Begin/UPDATE to wait rather than race. This is coarser than the
// Postgres keyed-lock behaviour (it serialises ALL apply-candidate
// transactions, not just those that target the same title), but that is
// acceptable because SQLite writes already serialise engine-wide.
func (s *BacklogCandidateStore) lockCandidateApplyKey(tx *sql.Tx, projectID, normalizedTitle string) error {
	if s.dialect.IsSQLite() {
		_, err := tx.Exec(`UPDATE tasks SET updated_at = updated_at WHERE 1 = 0`)
		return err
	}
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, projectID, normalizedTitle)
	return err
}

func findOpenTaskByNormalizedTitle(tx *sql.Tx, projectID, normalizedTitle string) (*models.Task, error) {
	return scanTask(
		tx.QueryRow(`
			SELECT id, project_id, title, description, status, priority, assignee, source,
			       dispatch_status, execution_result, created_at, updated_at
			FROM tasks
			WHERE project_id = $1
			  AND status IN ('todo', 'in_progress')
			  AND LOWER(TRIM(title)) = $2
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, projectID, normalizedTitle),
	)
}

func createAppliedCandidateTask(tx *sql.Tx, projectID, title, description string) (*models.Task, error) {
	return createAppliedCandidateTaskWithSource(tx, projectID, title, description, appliedCandidateTaskSource)
}

// createAppliedCandidateTaskWithSource is the Phase-5-aware variant.
// Phase 4 callers go through createAppliedCandidateTask which pins the
// source to the pre-existing AppliedCandidateTaskSource sentinel.
//
// Phase 6b: when source starts with "role_dispatch" the task is given
// dispatch_status = 'queued' so the connector polling loop can claim it.
func createAppliedCandidateTaskWithSource(tx *sql.Tx, projectID, title, description, source string) (*models.Task, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	trimmedTitle := strings.TrimSpace(title)
	trimmedDescription := strings.TrimSpace(description)
	trimmedSource := strings.TrimSpace(source)
	if trimmedSource == "" {
		trimmedSource = appliedCandidateTaskSource
	}

	// Phase 6b: role_dispatch tasks enter the connector queue immediately.
	dispatchStatus := models.TaskDispatchStatusNone
	if strings.HasPrefix(trimmedSource, "role_dispatch") {
		dispatchStatus = models.TaskDispatchStatusQueued
	}

	_, err := tx.Exec(`
		INSERT INTO tasks (id, project_id, title, description, status, priority, assignee, source, dispatch_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
	`, id, projectID, trimmedTitle, trimmedDescription, "todo", "medium", "", trimmedSource, dispatchStatus, now)
	if err != nil {
		return nil, err
	}

	return &models.Task{
		ID:             id,
		ProjectID:      projectID,
		Title:          trimmedTitle,
		Description:    trimmedDescription,
		Status:         "todo",
		Priority:       "medium",
		Assignee:       "",
		Source:         trimmedSource,
		DispatchStatus: dispatchStatus,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func createTaskLineage(tx *sql.Tx, candidate *models.BacklogCandidate, taskID string) (*models.TaskLineage, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := tx.Exec(`
		INSERT INTO task_lineage (id, project_id, task_id, requirement_id, planning_run_id, backlog_candidate_id, lineage_kind, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, candidate.ProjectID, taskID, candidate.RequirementID, candidate.PlanningRunID, candidate.ID, models.TaskLineageKindAppliedCandidate, now)
	if err != nil {
		return nil, err
	}

	return &models.TaskLineage{
		ID:                 id,
		ProjectID:          candidate.ProjectID,
		TaskID:             taskID,
		RequirementID:      candidate.RequirementID,
		PlanningRunID:      candidate.PlanningRunID,
		BacklogCandidateID: candidate.ID,
		LineageKind:        models.TaskLineageKindAppliedCandidate,
		CreatedAt:          now,
	}, nil
}

func updateBacklogCandidateStatus(tx *sql.Tx, id, status string, updatedAt time.Time) error {
	_, err := tx.Exec(`
		UPDATE backlog_candidates
		SET status = $1,
		    updated_at = $2
		WHERE id = $3
	`, status, updatedAt, id)
	return err
}

// ListByEvidenceDocument returns lightweight summaries of backlog candidates
// belonging to projectID whose evidence_detail references documentID.
// Matching is done in Go after loading all candidates for the project to
// stay compatible with both SQLite and Postgres without JSON operator
// differences.
func (s *BacklogCandidateStore) ListByEvidenceDocument(projectID, documentID string) ([]models.CandidateEvidenceSummary, error) {
	return s.listByEvidencePredicate(projectID, func(ed models.PlanningEvidenceDetail) bool {
		for _, doc := range ed.Documents {
			if doc.DocumentID == documentID {
				return true
			}
		}
		return false
	})
}

// ListByEvidenceDriftSignal returns lightweight summaries of backlog
// candidates belonging to projectID whose evidence_detail references
// driftSignalID.
func (s *BacklogCandidateStore) ListByEvidenceDriftSignal(projectID, driftSignalID string) ([]models.CandidateEvidenceSummary, error) {
	return s.listByEvidencePredicate(projectID, func(ed models.PlanningEvidenceDetail) bool {
		for _, ds := range ed.DriftSignals {
			if ds.DriftSignalID == driftSignalID {
				return true
			}
		}
		return false
	})
}

func (s *BacklogCandidateStore) listByEvidencePredicate(
	projectID string,
	match func(models.PlanningEvidenceDetail) bool,
) ([]models.CandidateEvidenceSummary, error) {
	rows, err := s.db.Query(`
		SELECT bc.id, bc.title, bc.status, bc.planning_run_id, bc.requirement_id,
		       bc.evidence_detail, COALESCE(r.title, '')
		FROM backlog_candidates bc
		LEFT JOIN requirements r ON r.id = bc.requirement_id
		WHERE bc.project_id = $1
		ORDER BY bc.rank ASC, bc.priority_score DESC, bc.created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.CandidateEvidenceSummary, 0)
	for rows.Next() {
		var cs models.CandidateEvidenceSummary
		var evidenceDetailJSON []byte
		if err := rows.Scan(
			&cs.ID,
			&cs.Title,
			&cs.Status,
			&cs.PlanningRunID,
			&cs.RequirementID,
			&evidenceDetailJSON,
			&cs.RequirementTitle,
		); err != nil {
			return nil, err
		}
		ed := unmarshalEvidenceDetail(evidenceDetailJSON)
		if match(ed) {
			out = append(out, cs)
		}
	}
	return out, rows.Err()
}
