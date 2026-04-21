package models

import "time"

type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Assignee    string    `json:"assignee"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Assignee    string `json:"assignee"`
	Source      string `json:"source"`
}

type UpdateTaskRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	Priority    *string `json:"priority,omitempty"`
	Assignee    *string `json:"assignee,omitempty"`
}

type BatchUpdateTaskChanges struct {
	Status   *string `json:"status,omitempty"`
	Priority *string `json:"priority,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
}

func (c BatchUpdateTaskChanges) HasChanges() bool {
	return c.Status != nil || c.Priority != nil || c.Assignee != nil
}

type BatchUpdateTaskRequest struct {
	TaskIDs []string               `json:"task_ids"`
	Changes BatchUpdateTaskChanges `json:"changes"`
}

type BatchUpdateTaskResponse struct {
	UpdatedCount int    `json:"updated_count"`
	Tasks        []Task `json:"tasks"`
}

type TaskListFilters struct {
	Status   string
	Priority string
	Assignee string
}

var ValidTaskStatuses = map[string]bool{
	"todo":        true,
	"in_progress": true,
	"done":        true,
	"cancelled":   true,
}

var ValidTaskPriorities = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}
