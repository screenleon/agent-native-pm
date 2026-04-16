package models

import "time"

// Notification represents an in-app notification for a user.
type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ProjectID *string   `json:"project_id,omitempty"`
	Kind      string    `json:"kind"` // info | warning | error | drift | agent
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	IsRead    bool      `json:"is_read"`
	Link      string    `json:"link,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateNotificationRequest struct {
	UserID    string  `json:"user_id"`
	ProjectID *string `json:"project_id,omitempty"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Link      string  `json:"link,omitempty"`
}

// SearchResult aggregates hits from tasks and documents.
type SearchResult struct {
	Tasks     []Task     `json:"tasks"`
	Documents []Document `json:"documents"`
}
