package models

import "time"

// APIKey represents an authentication key for agent or automated access.
type APIKey struct {
	ID         string     `json:"id"`
	ProjectID  *string    `json:"project_id"` // nil = global key
	Label      string     `json:"label"`
	IsActive   bool       `json:"is_active"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	// KeyHash is never serialized to JSON
}

// APIKeyWithSecret is returned once on creation and never again.
type APIKeyWithSecret struct {
	APIKey
	Key string `json:"key"` // the raw key — visible only at creation
}

type CreateAPIKeyRequest struct {
	ProjectID *string `json:"project_id,omitempty"`
	Label     string  `json:"label"`
}
