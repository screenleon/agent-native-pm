package models

import "time"

type ProjectRepoMapping struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	Alias         string    `json:"alias"`
	RepoPath      string    `json:"repo_path"`
	DefaultBranch string    `json:"default_branch"`
	IsPrimary     bool      `json:"is_primary"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateProjectRepoMappingRequest struct {
	Alias         string `json:"alias"`
	RepoPath      string `json:"repo_path"`
	DefaultBranch string `json:"default_branch"`
	IsPrimary     bool   `json:"is_primary"`
}

type UpdateProjectRepoMappingRequest struct {
	DefaultBranch *string `json:"default_branch,omitempty"`
}

type DiscoveredMirrorRepo struct {
	RepoName              string `json:"repo_name"`
	RepoPath              string `json:"repo_path"`
	SuggestedAlias        string `json:"suggested_alias"`
	DetectedDefaultBranch string `json:"detected_default_branch"`
	IsMappedToProject     bool   `json:"is_mapped_to_project"`
	IsPrimaryForProject   bool   `json:"is_primary_for_project"`
}

type MirrorRepoDiscovery struct {
	MirrorRoot string                 `json:"mirror_root"`
	Repos      []DiscoveredMirrorRepo `json:"repos"`
}
