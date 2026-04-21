package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

var (
	ErrBacklogCandidateNotMutable  = errors.New("backlog candidate is not mutable")
	ErrBacklogCandidateNoChanges   = errors.New("no backlog candidate changes requested")
	ErrBacklogCandidateBlankTitle  = errors.New("backlog candidate title cannot be blank")
	ErrBacklogCandidateBadStatus   = errors.New("invalid backlog candidate status")
	ErrBacklogCandidateNotApproved = errors.New("backlog candidate must be approved before applying")
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
	db *sql.DB
}

func NewBacklogCandidateStore(db *sql.DB) *BacklogCandidateStore {
	return &BacklogCandidateStore{db: db}
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

		_, err = tx.Exec(`
			INSERT INTO backlog_candidates (
				id, project_id, requirement_id, planning_run_id, parent_candidate_id,
				suggestion_type, title, description, status, rationale, validation_criteria, po_decision,
				priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles,
				created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $19)
		`, candidate.ID, candidate.ProjectID, candidate.RequirementID, candidate.PlanningRunID, parentCandidateID, candidate.SuggestionType, candidate.Title, candidate.Description, candidate.Status, candidate.Rationale, candidate.ValidationCriteria, candidate.PODecision, candidate.PriorityScore, candidate.Confidence, candidate.Rank, evidenceJSON, evidenceDetailJSON, duplicateJSON, candidate.CreatedAt)
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
			SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, created_at, updated_at
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
		SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, created_at, updated_at
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
			&candidate.CreatedAt,
			&candidate.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if parentCandidateID.Valid {
			candidate.ParentCandidateID = parentCandidateID.String
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

	if !changed {
		return nil, ErrBacklogCandidateNoChanges
	}

	now := time.Now().UTC()
	_, err = s.db.Exec(`
		UPDATE backlog_candidates
		SET title = $1,
		    description = $2,
		    status = $3,
		    updated_at = $4
		WHERE id = $5
	`, title, description, status, now, id)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

func (s *BacklogCandidateStore) ApplyToTask(id string) (*models.ApplyBacklogCandidateResponse, error) {
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

	if err := lockCandidateApplyKey(tx, candidate.ProjectID, normalizedTitle); err != nil {
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

	if candidate.Status != models.BacklogCandidateStatusApproved && candidate.Status != models.BacklogCandidateStatusApplied {
		return nil, ErrBacklogCandidateNotApproved
	}

	duplicateTask, err := findOpenTaskByNormalizedTitle(tx, candidate.ProjectID, normalizedTitle)
	if err != nil {
		return nil, err
	}
	if duplicateTask != nil {
		return nil, &BacklogCandidateTaskConflictError{Task: duplicateTask}
	}

	task, err := createAppliedCandidateTask(tx, candidate.ProjectID, candidate.Title, candidate.Description)
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
	return scanBacklogCandidate(
		tx.QueryRow(`
			SELECT id, project_id, requirement_id, planning_run_id, parent_candidate_id, suggestion_type, title, description, status, rationale, validation_criteria, po_decision, priority_score, confidence, rank, evidence, evidence_detail, duplicate_titles, created_at, updated_at
			FROM backlog_candidates
			WHERE id = $1
			FOR UPDATE
		`, id),
	)
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanBacklogCandidate(row rowScanner) (*models.BacklogCandidate, error) {
	var candidate models.BacklogCandidate
	var parentCandidateID sql.NullString
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
	err := row.Scan(&task.ID, &task.ProjectID, &task.Title, &task.Description, &task.Status, &task.Priority, &task.Assignee, &task.Source, &task.CreatedAt, &task.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
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
			SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
			FROM tasks
			WHERE id = $1
		`, id),
	)
}

func normalizeCandidateTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

func lockCandidateApplyKey(tx *sql.Tx, projectID, normalizedTitle string) error {
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, projectID, normalizedTitle)
	return err
}

func findOpenTaskByNormalizedTitle(tx *sql.Tx, projectID, normalizedTitle string) (*models.Task, error) {
	return scanTask(
		tx.QueryRow(`
			SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
			FROM tasks
			WHERE project_id = $1
			  AND status IN ('todo', 'in_progress')
			  AND LOWER(BTRIM(title)) = $2
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, projectID, normalizedTitle),
	)
}

func createAppliedCandidateTask(tx *sql.Tx, projectID, title, description string) (*models.Task, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	trimmedTitle := strings.TrimSpace(title)
	trimmedDescription := strings.TrimSpace(description)

	_, err := tx.Exec(`
		INSERT INTO tasks (id, project_id, title, description, status, priority, assignee, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
	`, id, projectID, trimmedTitle, trimmedDescription, "todo", "medium", "", appliedCandidateTaskSource, now)
	if err != nil {
		return nil, err
	}

	return &models.Task{
		ID:          id,
		ProjectID:   projectID,
		Title:       trimmedTitle,
		Description: trimmedDescription,
		Status:      "todo",
		Priority:    "medium",
		Assignee:    "",
		Source:      appliedCandidateTaskSource,
		CreatedAt:   now,
		UpdatedAt:   now,
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
