package models

import "time"

type Project struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	RepoURL       string     `json:"repo_url"`
	RepoPath      string     `json:"repo_path"`
	DefaultBranch string     `json:"default_branch"`
	LastSyncAt    *time.Time `json:"last_sync_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name               string                         `json:"name"`
	Description        string                         `json:"description"`
	RepoURL            string                         `json:"repo_url"`
	RepoPath           string                         `json:"repo_path"`
	DefaultBranch      string                         `json:"default_branch"`
	InitialRepoMapping *CreateProjectRepoMappingRequest `json:"initial_repo_mapping,omitempty"`
}

type UpdateProjectRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	RepoURL       *string `json:"repo_url,omitempty"`
	RepoPath      *string `json:"repo_path,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
}
