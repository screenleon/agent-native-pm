package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type SummaryStore struct {
	db            *sql.DB
	taskStore     *TaskStore
	documentStore *DocumentStore
}

func NewSummaryStore(db *sql.DB, ts *TaskStore, ds *DocumentStore) *SummaryStore {
	return &SummaryStore{db: db, taskStore: ts, documentStore: ds}
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
