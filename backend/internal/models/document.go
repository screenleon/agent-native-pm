package models

import "time"

type Document struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Title         string     `json:"title"`
	FilePath      string     `json:"file_path"`
	DocType       string     `json:"doc_type"`
	LastUpdatedAt *time.Time `json:"last_updated_at"`
	StalenessDays int        `json:"staleness_days"`
	IsStale       bool       `json:"is_stale"`
	Source        string     `json:"source"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CreateDocumentRequest struct {
	Title    string `json:"title"`
	FilePath string `json:"file_path"`
	DocType  string `json:"doc_type"`
	Source   string `json:"source"`
}

type UpdateDocumentRequest struct {
	Title    *string `json:"title,omitempty"`
	FilePath *string `json:"file_path,omitempty"`
	DocType  *string `json:"doc_type,omitempty"`
	Source   *string `json:"source,omitempty"`
}

var ValidDocTypes = map[string]bool{
	"api":          true,
	"architecture": true,
	"guide":        true,
	"adr":          true,
	"general":      true,
}
