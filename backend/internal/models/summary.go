package models

type ProjectSummary struct {
	ProjectID       string  `json:"project_id"`
	SnapshotDate    string  `json:"snapshot_date"`
	TotalTasks      int     `json:"total_tasks"`
	TasksTodo       int     `json:"tasks_todo"`
	TasksInProgress int     `json:"tasks_in_progress"`
	TasksDone       int     `json:"tasks_done"`
	TasksCancelled  int     `json:"tasks_cancelled"`
	TotalDocuments  int     `json:"total_documents"`
	StaleDocuments  int     `json:"stale_documents"`
	HealthScore     float64 `json:"health_score"`
}
