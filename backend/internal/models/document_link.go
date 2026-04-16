package models

import "time"

// DocumentLink maps a document to a code file path it covers.
type DocumentLink struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	CodePath   string    `json:"code_path"`
	LinkType   string    `json:"link_type"` // covers | references | depends_on
	CreatedAt  time.Time `json:"created_at"`
}

type CreateDocumentLinkRequest struct {
	CodePath string `json:"code_path"`
	LinkType string `json:"link_type"`
}

var ValidLinkTypes = map[string]bool{
	"covers":     true,
	"references": true,
	"depends_on": true,
}
