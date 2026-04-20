package planning

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type taskContextSource interface {
	ListByProject(projectID string, page, perPage int, sort, order string, filters models.TaskListFilters) ([]models.Task, int, error)
}

type documentContextSource interface {
	ListByProject(projectID string, page, perPage int) ([]models.Document, int, error)
}

type driftContextSource interface {
	ListByProject(projectID, statusFilter, sortBy string, page, perPage int) ([]models.DriftSignal, int, error)
}

type syncContextSource interface {
	GetLatestByProject(projectID string) (*models.SyncRun, error)
}

type agentRunContextSource interface {
	ListRecentByProject(projectID string, limit int) ([]models.AgentRun, error)
}

type ProjectContextBuilder struct {
	tasks        taskContextSource
	documents    documentContextSource
	driftSignals driftContextSource
	syncRuns     syncContextSource
	agentRuns    agentRunContextSource
}

func NewProjectContextBuilder(tasks taskContextSource, documents documentContextSource, driftSignals driftContextSource, syncRuns syncContextSource, agentRuns agentRunContextSource) *ProjectContextBuilder {
	return &ProjectContextBuilder{
		tasks:        tasks,
		documents:    documents,
		driftSignals: driftSignals,
		syncRuns:     syncRuns,
		agentRuns:    agentRuns,
	}
}

// BuildResult bundles a PlanningContext with non-fatal warnings encountered
// while loading individual sources. Callers that need the warnings (e.g. the
// local connector path that surfaces meta.warnings to adapters) should use
// BuildWithWarnings; existing callers can keep using Build, which discards
// warnings after logging them.
type BuildResult struct {
	Context  PlanningContext
	Warnings []string
}

func (b *ProjectContextBuilder) Build(requirement *models.Requirement) (PlanningContext, error) {
	result, err := b.BuildWithWarnings(requirement)
	return result.Context, err
}

func (b *ProjectContextBuilder) BuildWithWarnings(requirement *models.Requirement) (BuildResult, error) {
	context := PlanningContext{
		OpenTasks:        []models.Task{},
		RecentDocuments:  []models.Document{},
		OpenDriftSignals: []models.DriftSignal{},
		RecentAgentRuns:  []models.AgentRun{},
	}
	result := BuildResult{Context: context, Warnings: []string{}}
	if requirement == nil {
		return result, nil
	}

	if b != nil && b.tasks != nil {
		tasks, _, err := b.tasks.ListByProject(requirement.ProjectID, 1, planningTaskContextLimit, "updated_at", "desc", models.TaskListFilters{})
		if err != nil {
			// Tasks are the only mandatory source: failure here is propagated
			// because duplicate detection downstream depends on it.
			result.Context = context
			return result, err
		}
		for _, task := range tasks {
			if task.Status == "done" || task.Status == "cancelled" {
				continue
			}
			context.OpenTasks = append(context.OpenTasks, task)
		}
	}

	if b != nil && b.documents != nil {
		if documents, _, err := b.documents.ListByProject(requirement.ProjectID, 1, planningDocumentContextLimit*2); err == nil {
			context.RecentDocuments = selectPlanningDocuments(requirement, documents, planningDocumentContextLimit)
		} else {
			result.Warnings = b.recordSourceWarning(result.Warnings, "documents", requirement.ProjectID, err)
		}
	}

	if b != nil && b.driftSignals != nil {
		if driftSignals, _, err := b.driftSignals.ListByProject(requirement.ProjectID, "open", "severity", 1, planningDriftContextLimit); err == nil {
			context.OpenDriftSignals = driftSignals
		} else {
			result.Warnings = b.recordSourceWarning(result.Warnings, "drift_signals", requirement.ProjectID, err)
		}
	}

	if b != nil && b.syncRuns != nil {
		if latestSyncRun, err := b.syncRuns.GetLatestByProject(requirement.ProjectID); err == nil {
			context.LatestSyncRun = latestSyncRun
		} else {
			result.Warnings = b.recordSourceWarning(result.Warnings, "latest_sync_run", requirement.ProjectID, err)
		}
	}

	if b != nil && b.agentRuns != nil {
		if recentAgentRuns, err := b.agentRuns.ListRecentByProject(requirement.ProjectID, planningAgentRunContextLimit*2); err == nil {
			context.RecentAgentRuns = filterPlanningAgentRuns(recentAgentRuns, planningAgentRunContextLimit)
		} else {
			result.Warnings = b.recordSourceWarning(result.Warnings, "recent_agent_runs", requirement.ProjectID, err)
		}
	}

	result.Context = context
	return result, nil
}

func (b *ProjectContextBuilder) recordSourceWarning(warnings []string, source, projectID string, err error) []string {
	msg := fmt.Sprintf("%s: %v", source, err)
	log.Printf("planning context: degraded source %q for project %s: %v", source, projectID, err)
	return append(warnings, msg)
}

func selectPlanningDocuments(requirement *models.Requirement, documents []models.Document, limit int) []models.Document {
	if limit < 1 {
		limit = 1
	}
	scoredDocuments := make([]models.Document, len(documents))
	copy(scoredDocuments, documents)
	sort.SliceStable(scoredDocuments, func(i, j int) bool {
		leftScore := planningDocumentRelevanceScore(requirement, scoredDocuments[i])
		rightScore := planningDocumentRelevanceScore(requirement, scoredDocuments[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if scoredDocuments[i].IsStale != scoredDocuments[j].IsStale {
			return scoredDocuments[i].IsStale
		}
		if scoredDocuments[i].StalenessDays != scoredDocuments[j].StalenessDays {
			return scoredDocuments[i].StalenessDays > scoredDocuments[j].StalenessDays
		}
		return scoredDocuments[i].UpdatedAt.After(scoredDocuments[j].UpdatedAt)
	})

	selected := make([]models.Document, 0, limit)
	for _, document := range scoredDocuments {
		if len(selected) == limit {
			break
		}
		selected = append(selected, document)
	}
	return selected
}

func planningDocumentRelevanceScore(requirement *models.Requirement, document models.Document) int {
	score := 0
	if document.IsStale {
		score += 2
	}
	for _, keyword := range requirementKeywords(requirement) {
		if strings.Contains(strings.ToLower(document.Title), keyword) {
			score += 5
		}
		if strings.Contains(strings.ToLower(document.FilePath), keyword) {
			score += 4
		}
		if strings.Contains(strings.ToLower(document.DocType), keyword) {
			score += 2
		}
	}
	return score
}

func filterPlanningAgentRuns(agentRuns []models.AgentRun, limit int) []models.AgentRun {
	if limit < 1 {
		limit = 1
	}
	filtered := make([]models.AgentRun, 0, limit)
	for _, agentRun := range agentRuns {
		if agentRun.AgentName == PlannerAgentName && agentRun.ActionType == plannerAction && agentRun.Status == models.AgentRunStatusRunning {
			continue
		}
		filtered = append(filtered, agentRun)
		if len(filtered) == limit {
			break
		}
	}
	if filtered == nil {
		return []models.AgentRun{}
	}
	return filtered
}

func requirementKeywords(requirement *models.Requirement) []string {
	if requirement == nil {
		return []string{}
	}
	raw := strings.ToLower(strings.TrimSpace(requirement.Title + " " + requirement.Summary + " " + requirement.Description))
	if raw == "" {
		return []string{}
	}
	replacer := strings.NewReplacer(
		",", " ",
		".", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		"/", " ",
		"_", " ",
		"-", " ",
		"\n", " ",
	)
	words := strings.Fields(replacer.Replace(raw))
	keywords := make([]string, 0, len(words))
	seen := map[string]bool{}
	for _, word := range words {
		if len(word) < 4 {
			continue
		}
		if seen[word] {
			continue
		}
		seen[word] = true
		keywords = append(keywords, word)
	}
	if keywords == nil {
		return []string{}
	}
	return keywords
}
