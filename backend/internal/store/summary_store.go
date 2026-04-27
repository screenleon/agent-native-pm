package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type SummaryStore struct {
	db               *sql.DB
	taskStore        *TaskStore
	documentStore    *DocumentStore
	syncRunStore     *SyncRunStore
	driftSignalStore *DriftSignalStore
	agentRunStore    *AgentRunStore
}

func NewSummaryStore(db *sql.DB, ts *TaskStore, ds *DocumentStore, srs *SyncRunStore, drs *DriftSignalStore, ars *AgentRunStore) *SummaryStore {
	return &SummaryStore{
		db:               db,
		taskStore:        ts,
		documentStore:    ds,
		syncRunStore:     srs,
		driftSignalStore: drs,
		agentRunStore:    ars,
	}
}

func (s *SummaryStore) ComputeSummary(projectID string) (*models.ProjectSummary, error) {
	taskCounts, err := s.taskStore.CountByProjectAndStatus(projectID)
	if err != nil {
		return nil, err
	}

	totalDocs, staleDocs, err := s.documentStore.CountByProject(projectID)
	if err != nil {
		return nil, err
	}

	totalTasks := 0
	for _, c := range taskCounts {
		totalTasks += c
	}

	healthScore := computeHealthScore(taskCounts, totalTasks, totalDocs, staleDocs)

	summary := &models.ProjectSummary{
		ProjectID:       projectID,
		SnapshotDate:    time.Now().UTC().Format("2006-01-02"),
		TotalTasks:      totalTasks,
		TasksTodo:       taskCounts["todo"],
		TasksInProgress: taskCounts["in_progress"],
		TasksDone:       taskCounts["done"],
		TasksCancelled:  taskCounts["cancelled"],
		TotalDocuments:  totalDocs,
		StaleDocuments:  staleDocs,
		HealthScore:     healthScore,
	}

	return summary, nil
}

func (s *SummaryStore) ComputeCurrentSummary(projectID string) (*models.ProjectSummary, error) {
	summary, err := s.ComputeSummary(projectID)
	if err != nil {
		return nil, err
	}
	if err := s.SaveDailySnapshot(summary); err != nil {
		return nil, err
	}
	return summary, nil
}

func (s *SummaryStore) ComputeDashboardSummary(projectID string) (*models.DashboardSummary, error) {
	summary, err := s.ComputeCurrentSummary(projectID)
	if err != nil {
		return nil, err
	}

	dashboard := &models.DashboardSummary{
		ProjectID:       projectID,
		Summary:         *summary,
		LatestSyncRun:   nil,
		OpenDriftCount:  0,
		RecentAgentRuns: []models.AgentRun{},
	}

	if s.syncRunStore != nil {
		latestSyncRun, err := s.syncRunStore.GetLatestByProject(projectID)
		if err != nil {
			return nil, err
		}
		dashboard.LatestSyncRun = latestSyncRun
	}

	if s.driftSignalStore != nil {
		openDriftCount, err := s.driftSignalStore.CountOpenByProject(projectID)
		if err != nil {
			return nil, err
		}
		dashboard.OpenDriftCount = openDriftCount
	}

	if s.agentRunStore != nil {
		recentAgentRuns, err := s.agentRunStore.ListRecentByProject(projectID, 5)
		if err != nil {
			return nil, err
		}
		dashboard.RecentAgentRuns = recentAgentRuns
	}

	// Phase 3B PR-4: avg acceptance rate over the past 7 days.
	// Only counts planning_runs where every candidate has been reviewed
	// (no pending candidates remain). Best-effort — failure leaves the field at 0.
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	rows, qErr := s.db.Query(`
		SELECT
			SUM(approved_count) AS total_approved,
			SUM(reviewed_count) AS total_reviewed,
			COUNT(*) AS run_count
		FROM (
			SELECT
				pr.id,
				SUM(CASE WHEN bc.status = 'approved' THEN 1 ELSE 0 END) AS approved_count,
				SUM(CASE WHEN bc.status IN ('approved','rejected') THEN 1 ELSE 0 END) AS reviewed_count,
				SUM(CASE WHEN bc.status = 'pending' THEN 1 ELSE 0 END) AS pending_count
			FROM planning_runs pr
			JOIN backlog_candidates bc ON bc.planning_run_id = pr.id
			WHERE pr.project_id = $1
			  AND pr.completed_at >= $2
			GROUP BY pr.id
			HAVING SUM(CASE WHEN bc.status = 'pending' THEN 1 ELSE 0 END) = 0
		) AS reviewed_runs
	`, projectID, cutoff)
	if qErr == nil {
		defer rows.Close()
		if rows.Next() {
			var totalApproved, totalReviewed, runCount sql.NullInt64
			if scanErr := rows.Scan(&totalApproved, &totalReviewed, &runCount); scanErr == nil && totalReviewed.Valid && totalReviewed.Int64 > 0 {
				dashboard.AvgPlanningAcceptanceRate = float64(totalApproved.Int64) / float64(totalReviewed.Int64)
				dashboard.PlanningRunsReviewedCount = int(runCount.Int64)
			}
		}
	}

	return dashboard, nil
}

func (s *SummaryStore) SaveSnapshot(summary *models.ProjectSummary) error {
	id := uuid.New().String()
	_, err := s.db.Exec(`
		INSERT INTO summary_snapshots (id, project_id, snapshot_date, total_tasks, tasks_todo, tasks_in_progress, tasks_done, tasks_cancelled, total_documents, stale_documents, health_score)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, id, summary.ProjectID, summary.SnapshotDate, summary.TotalTasks, summary.TasksTodo,
		summary.TasksInProgress, summary.TasksDone, summary.TasksCancelled,
		summary.TotalDocuments, summary.StaleDocuments, summary.HealthScore)
	return err
}

func (s *SummaryStore) SaveDailySnapshot(summary *models.ProjectSummary) error {
	var existing int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM summary_snapshots
		WHERE project_id = $1 AND snapshot_date = $2
	`, summary.ProjectID, summary.SnapshotDate).Scan(&existing)
	if err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	return s.SaveSnapshot(summary)
}

func (s *SummaryStore) GetHistory(projectID string, limit int) ([]models.ProjectSummary, error) {
	rows, err := s.db.Query(`
		SELECT project_id, snapshot_date, total_tasks, tasks_todo, tasks_in_progress, tasks_done, tasks_cancelled, total_documents, stale_documents, health_score
		FROM summary_snapshots WHERE project_id = $1 ORDER BY snapshot_date DESC LIMIT $2
	`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.ProjectSummary
	for rows.Next() {
		var s models.ProjectSummary
		if err := rows.Scan(&s.ProjectID, &s.SnapshotDate, &s.TotalTasks, &s.TasksTodo,
			&s.TasksInProgress, &s.TasksDone, &s.TasksCancelled,
			&s.TotalDocuments, &s.StaleDocuments, &s.HealthScore); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// computeHealthScore calculates a 0.0–1.0 health score.
// Factors: task completion ratio (70%) and document freshness (30%).
func computeHealthScore(taskCounts map[string]int, totalTasks, totalDocs, staleDocs int) float64 {
	if totalTasks == 0 && totalDocs == 0 {
		return 1.0
	}

	var taskScore float64 = 1.0
	if totalTasks > 0 {
		completed := taskCounts["done"]
		active := totalTasks - taskCounts["cancelled"]
		if active > 0 {
			taskScore = float64(completed) / float64(active)
		}
	}

	var docScore float64 = 1.0
	if totalDocs > 0 {
		docScore = float64(totalDocs-staleDocs) / float64(totalDocs)
	}

	// Weighted: 70% task progress, 30% doc freshness
	score := taskScore*0.7 + docScore*0.3

	// Clamp to [0, 1]
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}

	// Round to 2 decimal places
	return float64(int(score*100)) / 100
}

// CountPendingReviewByProject returns the number of draft backlog candidates
// for the given project that belong to completed planning runs. Candidates from
// in-flight or failed runs are excluded to avoid showing transient counts while
// a planning run is still writing its results.
func (s *SummaryStore) CountPendingReviewByProject(projectID string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM backlog_candidates bc
		JOIN planning_runs pr ON pr.id = bc.planning_run_id
		WHERE bc.project_id = $1
		  AND bc.status = 'draft'
		  AND pr.status = 'completed'
	`, projectID).Scan(&n)
	return n, err
}
