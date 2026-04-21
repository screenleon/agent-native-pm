package planning

import (
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

// BuildContextV1 builds a sanitized, byte-capped wire.PlanningContextV1
// payload suitable for delivery to local connector adapters. It composes
// the existing Build() output with the wire-package sanitizer and reducer.
//
// Phase A uses package-level defaults (wire.DefaultLimits) for per-source
// caps and the sources byte cap. A future Phase B will accept a
// caller-supplied ContextBudget.
//
// Returns a fully populated *PlanningContextV1 or an error if the underlying
// builder fails. Callers should log build errors and proceed with a nil
// payload (see docs/local-connector-context.md §6).
func (b *ProjectContextBuilder) BuildContextV1(requirement *models.Requirement) (*wire.PlanningContextV1, error) {
	built, err := b.BuildWithWarnings(requirement)
	if err != nil {
		return nil, err
	}
	rawContext := built.Context
	warnings := built.Warnings
	if warnings == nil {
		warnings = []string{}
	}

	limits := wire.DefaultLimits()
	sources := translatePlanningContextToWire(rawContext)
	sanitized := wire.PlanningContextV1{
		SchemaVersion:    wire.ContextSchemaV1,
		GeneratedAt:      time.Now().UTC(),
		GeneratedBy:      wire.GeneratedByServer,
		SanitizerVersion: wire.SanitizerVersion,
		Limits:           limits,
		Sources:          sources,
		Meta: wire.PlanningContextMeta{
			Ranking:       wire.DefaultRanking(),
			DroppedCounts: map[string]int{},
			SourcesBytes:  0,
			Warnings:      warnings,
		},
	}

	sanitized = wire.SanitizePlanningContextV1(sanitized)

	reduced, dropped, sourcesBytes := wire.ReduceSources(sanitized.Sources, limits.MaxSourcesBytes)
	sanitized.Sources = reduced
	sanitized.Meta.DroppedCounts = dropped
	sanitized.Meta.SourcesBytes = sourcesBytes

	return &sanitized, nil
}

// translatePlanningContextToWire converts the internal PlanningContext into
// the wire-level DTO shape. Documents are metadata-only (no body), matching
// compactDocumentsForPrompt behavior.
func translatePlanningContextToWire(ctx PlanningContext) wire.PlanningContextSources {
	out := wire.PlanningContextSources{
		OpenTasks:        make([]wire.WireTask, 0, len(ctx.OpenTasks)),
		RecentDocuments:  make([]wire.WireDocument, 0, len(ctx.RecentDocuments)),
		OpenDriftSignals: make([]wire.WireDriftSignal, 0, len(ctx.OpenDriftSignals)),
		RecentAgentRuns:  make([]wire.WireAgentRun, 0, len(ctx.RecentAgentRuns)),
	}

	for _, task := range ctx.OpenTasks {
		out.OpenTasks = append(out.OpenTasks, wire.WireTask{
			ID:        task.ID,
			Title:     task.Title,
			Status:    task.Status,
			Priority:  task.Priority,
			UpdatedAt: task.UpdatedAt,
		})
	}

	for _, document := range ctx.RecentDocuments {
		out.RecentDocuments = append(out.RecentDocuments, wire.WireDocument{
			ID:            document.ID,
			Title:         document.Title,
			FilePath:      document.FilePath,
			DocType:       document.DocType,
			IsStale:       document.IsStale,
			StalenessDays: document.StalenessDays,
		})
	}

	for _, drift := range ctx.OpenDriftSignals {
		out.OpenDriftSignals = append(out.OpenDriftSignals, wire.WireDriftSignal{
			ID:            drift.ID,
			DocumentTitle: drift.DocumentTitle,
			TriggerType:   drift.TriggerType,
			TriggerDetail: drift.TriggerDetail,
			Severity:      severityLabel(drift.Severity),
			OpenedAt:      drift.CreatedAt,
		})
	}

	if ctx.LatestSyncRun != nil {
		syncRun := &wire.WireSyncRun{
			ID:           ctx.LatestSyncRun.ID,
			Status:       ctx.LatestSyncRun.Status,
			StartedAt:    ctx.LatestSyncRun.StartedAt,
			CompletedAt:  ctx.LatestSyncRun.CompletedAt,
			ErrorMessage: ctx.LatestSyncRun.ErrorMessage,
		}
		out.LatestSyncRun = syncRun
	}

	for _, run := range ctx.RecentAgentRuns {
		out.RecentAgentRuns = append(out.RecentAgentRuns, wire.WireAgentRun{
			ID:         run.ID,
			AgentName:  run.AgentName,
			ActionType: run.ActionType,
			Status:     run.Status,
			StartedAt:  run.StartedAt,
			Summary:    run.Summary,
		})
	}

	return out
}

// severityLabel converts the internal DriftSignal integer severity (1=low,
// 2=medium, 3=high) into a wire-stable string label. Unknown values fall
// back to the empty string so adapters can detect and log the condition.
func severityLabel(severity int) string {
	switch severity {
	case 1:
		return "low"
	case 2:
		return "medium"
	case 3:
		return "high"
	default:
		return ""
	}
}
