package planning

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type DeterministicProvider struct{}

type suggestionFactors struct {
	impact           float64
	urgency          float64
	dependencyUnlock float64
	riskReduction    float64
	effort           float64
	confidence       float64
	duplicatePenalty float64
}

func NewDeterministicProvider() *DeterministicProvider {
	return &DeterministicProvider{}
}

func (p *DeterministicProvider) Generate(ctx context.Context, requirement *models.Requirement, context PlanningContext, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	_ = ctx
	_ = selection
	subject := requirementSubject(requirement)
	openTaskTitles := summarizeOpenTaskTitles(context.OpenTasks, planningEvidenceSampleLimit)
	primaryTitle := strings.TrimSpace(requirement.Title)
	primaryDuplicates := findDuplicateTitles(primaryTitle, context.OpenTasks)
	integrationTitle := buildIntegrationTitle(subject, requirement)
	validationTitle := buildValidationTitle(subject, requirement)
	relatedDocumentEntries := buildDocumentEvidenceEntries(requirement, selectPlanningDocuments(requirement, context.RecentDocuments, planningEvidenceSampleLimit), "Related project context influenced ranking.")
	relatedDocuments := summarizeDocumentEvidenceEntries(relatedDocumentEntries)
	staleDocumentEntries := buildStaleDocumentEvidenceEntries(context.RecentDocuments, planningEvidenceSampleLimit)
	staleDocuments := summarizeDocumentEvidenceEntries(staleDocumentEntries)
	driftEntries, driftHighlights, hasHighSeverityDrift := buildDriftSignalEvidenceEntries(context.OpenDriftSignals, planningEvidenceSampleLimit)
	latestSyncDetail, latestSyncEvidence, latestSyncFailed := buildSyncRunEvidence(context.LatestSyncRun)
	recentFailureEntries, recentFailureEvidence, hasRecentFailures := buildRecentFailedAgentRunEvidence(context.RecentAgentRuns, planningFailureSampleLimit)
	recentActivityEntries, recentActivityEvidence := buildRecentAgentActivityEvidence(context.RecentAgentRuns, planningEvidenceSampleLimit)
	primaryDuplicateEntries := buildDuplicateEvidenceEntries(primaryDuplicates, "Exact-title overlap with open work reduced ranking confidence.")
	integrationDuplicateEntries := buildDuplicateEvidenceEntries(findDuplicateTitles(integrationTitle, context.OpenTasks), "Possible overlap with existing workflow integration work.")
	validationDuplicateEntries := buildDuplicateEvidenceEntries(findDuplicateTitles(validationTitle, context.OpenTasks), "Validation recommendation overlaps with active work and needs review.")

	implementationEvidence := append(requirementEvidence(requirement), duplicateEvidence(primaryDuplicates)...)
	implementationEvidence = append(implementationEvidence, documentEvidence(relatedDocuments)...)
	implementationEvidence = append(implementationEvidence, recentActivityEvidence...)

	integrationDuplicates := extractDuplicateTitles(integrationDuplicateEntries)
	integrationEvidence := append(requirementEvidence(requirement), workflowEvidence(openTaskTitles)...)
	integrationEvidence = append(integrationEvidence, documentEvidence(relatedDocuments)...)
	if latestSyncEvidence != "" {
		integrationEvidence = append(integrationEvidence, latestSyncEvidence)
	}

	validationDuplicates := extractDuplicateTitles(validationDuplicateEntries)
	validationEvidenceSet := append(requirementEvidence(requirement), validationEvidence(primaryDuplicates, openTaskTitles)...)
	validationEvidenceSet = append(validationEvidenceSet, documentEvidence(staleDocuments)...)
	validationEvidenceSet = append(validationEvidenceSet, driftEvidence(driftHighlights)...)
	if latestSyncEvidence != "" {
		validationEvidenceSet = append(validationEvidenceSet, latestSyncEvidence)
	}
	validationEvidenceSet = append(validationEvidenceSet, recentFailureEvidence...)

	implementationUrgency := requirementUrgency(requirement)
	integrationUrgency := clampUnit(implementationUrgency - 0.04)
	validationUrgency := clampUnit(implementationUrgency - 0.08)
	if latestSyncFailed || hasRecentFailures {
		integrationUrgency = clampUnit(integrationUrgency + 0.06)
		validationUrgency = clampUnit(validationUrgency + 0.10)
	}
	if hasHighSeverityDrift || len(staleDocuments) > 0 {
		validationUrgency = clampUnit(validationUrgency + 0.08)
	}

	implementationRiskReduction := 0.44
	integrationRiskReduction := 0.57
	validationRiskReduction := 0.84
	if len(relatedDocuments) > 0 {
		implementationRiskReduction = clampUnit(implementationRiskReduction + 0.06)
		integrationRiskReduction = clampUnit(integrationRiskReduction + 0.05)
	}
	if hasHighSeverityDrift || latestSyncFailed || hasRecentFailures {
		validationRiskReduction = clampUnit(validationRiskReduction + 0.14)
		validationUrgency = clampUnit(validationUrgency + 0.04)
	}
	if len(staleDocuments) > 0 {
		validationRiskReduction = clampUnit(validationRiskReduction + 0.05)
		validationUrgency = clampUnit(validationUrgency + 0.03)
	}

	implementationConfidence := 0.73
	integrationConfidence := 0.68
	validationConfidence := 0.65
	if len(relatedDocuments) > 0 {
		implementationConfidence += 0.04
		integrationConfidence += 0.07
	}
	if len(staleDocuments) > 0 || len(context.OpenDriftSignals) > 0 {
		validationConfidence += 0.10
	}
	if latestSyncFailed {
		integrationConfidence -= 0.02
		validationConfidence += 0.08
	}
	if hasRecentFailures {
		validationConfidence += 0.06
	}

	drafts := []models.BacklogCandidateDraft{
		buildDraftCandidate(
			"implementation",
			primaryTitle,
			buildPrimaryDescription(requirement, relatedDocuments, latestSyncFailed),
			buildPrimaryRationale(primaryDuplicates, relatedDocuments, latestSyncFailed),
			implementationEvidence,
			primaryDuplicates,
			models.PlanningEvidenceDetail{
				Summary:    dedupeStrings(implementationEvidence),
				Documents:  cloneDocumentEvidenceEntries(relatedDocumentEntries),
				AgentRuns:  cloneAgentRunEvidenceEntries(recentActivityEntries),
				Duplicates: cloneDuplicateEvidenceEntries(primaryDuplicateEntries),
			},
			suggestionFactors{impact: 0.92, urgency: implementationUrgency, dependencyUnlock: dependencyUnlockScore(openTaskTitles, relatedDocuments, latestSyncFailed, 0.62), riskReduction: implementationRiskReduction, effort: 0.52, confidence: implementationConfidence, duplicatePenalty: duplicatePenalty(primaryDuplicates)},
		),
		buildDraftCandidate(
			"integration",
			integrationTitle,
			buildIntegrationDescription(requirement, openTaskTitles, relatedDocuments, latestSyncEvidence),
			buildIntegrationRationale(openTaskTitles, relatedDocuments, latestSyncFailed),
			integrationEvidence,
			integrationDuplicates,
			models.PlanningEvidenceDetail{
				Summary:    dedupeStrings(integrationEvidence),
				Documents:  cloneDocumentEvidenceEntries(relatedDocumentEntries),
				SyncRun:    cloneSyncRunEvidence(latestSyncDetail),
				Duplicates: cloneDuplicateEvidenceEntries(integrationDuplicateEntries),
			},
			suggestionFactors{impact: 0.74, urgency: integrationUrgency, dependencyUnlock: dependencyUnlockScore(openTaskTitles, relatedDocuments, latestSyncFailed, 0.81), riskReduction: integrationRiskReduction, effort: 0.46, confidence: integrationConfidence, duplicatePenalty: duplicatePenalty(integrationDuplicates)},
		),
		buildDraftCandidate(
			"validation",
			validationTitle,
			buildValidationDescription(requirement, openTaskTitles, staleDocuments, driftHighlights, latestSyncFailed, hasRecentFailures),
			buildValidationRationale(primaryDuplicates, openTaskTitles, staleDocuments, driftHighlights, latestSyncFailed, hasRecentFailures),
			validationEvidenceSet,
			validationDuplicates,
			models.PlanningEvidenceDetail{
				Summary:      dedupeStrings(validationEvidenceSet),
				Documents:    cloneDocumentEvidenceEntries(staleDocumentEntries),
				DriftSignals: cloneDriftSignalEvidenceEntries(driftEntries),
				SyncRun:      cloneSyncRunEvidence(latestSyncDetail),
				AgentRuns:    cloneAgentRunEvidenceEntries(recentFailureEntries),
				Duplicates:   cloneDuplicateEvidenceEntries(validationDuplicateEntries),
			},
			suggestionFactors{impact: 0.61, urgency: validationUrgency, dependencyUnlock: dependencyUnlockScore(openTaskTitles, staleDocuments, latestSyncFailed, 0.48), riskReduction: validationRiskReduction, effort: 0.38, confidence: validationConfidence, duplicatePenalty: duplicatePenalty(validationDuplicates)},
		),
	}

	for index := range drafts {
		drafts[index].PriorityScore = computePriorityScore(drafts[index].Evidence, drafts[index].DuplicateTitles, drafts[index].PriorityScore)
		drafts[index].Confidence = computeConfidence(drafts[index].Confidence, drafts[index].Evidence, drafts[index].DuplicateTitles)
		drafts[index].EvidenceDetail.ScoreBreakdown = buildScoreBreakdown(drafts[index])
	}

	sort.SliceStable(drafts, func(i, j int) bool {
		if drafts[i].PriorityScore == drafts[j].PriorityScore {
			return drafts[i].Title < drafts[j].Title
		}
		return drafts[i].PriorityScore > drafts[j].PriorityScore
	})

	for index := range drafts {
		drafts[index].Rank = index + 1
	}

	return drafts, nil
}

func buildDraftCandidate(kind, title, description, rationale string, evidence, duplicateTitles []string, evidenceDetail models.PlanningEvidenceDetail, factors suggestionFactors) models.BacklogCandidateDraft {
	evidenceDetail.ScoreBreakdown = models.PlanningScoreBreakdown{
		Impact:           roundTenth(clampPercentage(factors.impact * 100)),
		Urgency:          roundTenth(clampPercentage(factors.urgency * 100)),
		DependencyUnlock: roundTenth(clampPercentage(factors.dependencyUnlock * 100)),
		RiskReduction:    roundTenth(clampPercentage(factors.riskReduction * 100)),
		Effort:           roundTenth(clampPercentage(factors.effort * 100)),
		ConfidenceSeed:   confidenceSeed(factors.confidence),
	}
	return models.BacklogCandidateDraft{
		SuggestionType:  kind,
		Title:           strings.TrimSpace(title),
		Description:     strings.TrimSpace(description),
		Rationale:       strings.TrimSpace(rationale),
		PriorityScore:   weightedPriorityScore(factors),
		Confidence:      confidenceSeed(factors.confidence),
		Evidence:        dedupeStrings(evidence),
		EvidenceDetail:  evidenceDetail,
		DuplicateTitles: dedupeStrings(duplicateTitles),
	}
}

func buildScoreBreakdown(draft models.BacklogCandidateDraft) models.PlanningScoreBreakdown {
	evidenceBonus := math.Min(float64(len(draft.Evidence))*1.8, 6)
	duplicatePenalty := float64(len(draft.DuplicateTitles)) * 6.5
	breakdown := draft.EvidenceDetail.ScoreBreakdown
	breakdown.EvidenceBonus = roundTenth(evidenceBonus)
	breakdown.DuplicatePenalty = roundTenth(duplicatePenalty)
	breakdown.FinalPriorityScore = draft.PriorityScore
	breakdown.FinalConfidence = draft.Confidence
	return breakdown
}

func requirementSubject(requirement *models.Requirement) string {
	if requirement == nil {
		return "this requirement"
	}
	title := strings.TrimSpace(requirement.Title)
	if title == "" {
		return "this requirement"
	}
	subject := title
	for _, prefix := range []string{"Add ", "Improve ", "Implement ", "Create ", "Build ", "Support ", "Enable ", "Surface ", "Fix ", "Refine "} {
		if strings.HasPrefix(strings.ToLower(subject), strings.ToLower(prefix)) {
			subject = strings.TrimSpace(subject[len(prefix):])
			break
		}
	}
	if subject == "" {
		return title
	}
	return subject
}

func buildPrimaryDescription(requirement *models.Requirement, relatedDocuments []string, latestSyncFailed bool) string {
	parts := []string{requirement.Summary, requirement.Description, "Deliver the core requirement as the first shippable backlog slice."}
	if len(relatedDocuments) > 0 {
		parts = append(parts, "Use the currently relevant documentation as grounding so the implementation recommendation stays aligned with the existing project context.")
	}
	if latestSyncFailed {
		parts = append(parts, "Keep recent sync instability in mind while shaping the slice so implementation work does not outrun the current repo baseline.")
	}
	return joinParagraphs(parts...)
}

func buildPrimaryRationale(duplicates, relatedDocuments []string, latestSyncFailed bool) string {
	if len(duplicates) > 0 {
		return "Top recommendation because it is the closest implementation slice to the requirement, but it needs review against existing open work before apply."
	}
	if len(relatedDocuments) > 0 && !latestSyncFailed {
		return "Top recommendation because the requirement has enough nearby project context to move directly into a grounded implementation slice instead of staying generic."
	}
	if latestSyncFailed {
		return "Top recommendation remains the core requirement slice, but it is tempered by current sync instability so follow-on validation should be reviewed closely."
	}
	return "Top recommendation because it is the closest implementation slice to the stated requirement and can move directly into review once copy is confirmed."
}

func buildIntegrationTitle(subject string, requirement *models.Requirement) string {
	combined := strings.ToLower(strings.TrimSpace(requirement.Title + " " + requirement.Summary + " " + requirement.Description))
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "this requirement"
	}
	if strings.Contains(combined, "ui") || strings.Contains(combined, "ux") || strings.Contains(combined, "screen") || strings.Contains(combined, "page") || strings.Contains(combined, "frontend") || strings.Contains(combined, "tab") {
		return fmt.Sprintf("Surface %s in the existing planning workflow", subject)
	}
	if strings.Contains(combined, "api") || strings.Contains(combined, "endpoint") || strings.Contains(combined, "backend") || strings.Contains(combined, "schema") || strings.Contains(combined, "persist") {
		return fmt.Sprintf("Wire %s through the planning backend", subject)
	}
	return fmt.Sprintf("Connect %s to the active project workflow", subject)
}

func buildValidationTitle(subject string, requirement *models.Requirement) string {
	combined := strings.ToLower(strings.TrimSpace(requirement.Title + " " + requirement.Summary + " " + requirement.Description))
	if strings.Contains(combined, "duplicate") || strings.Contains(combined, "rank") || strings.Contains(combined, "review") || strings.Contains(combined, "apply") || strings.Contains(combined, "backlog") {
		return fmt.Sprintf("Validate %s ranking and review safety", subject)
	}
	return fmt.Sprintf("Validate %s with apply safeguards", subject)
}

func buildIntegrationDescription(requirement *models.Requirement, openTaskTitles, relatedDocuments []string, latestSyncEvidence string) string {
	base := "Connect the requirement to the current data flow, persistence path, and review surface so the result is visible in the existing project workspace."
	if len(openTaskTitles) > 0 {
		base += " Current open work should be considered so the suggestion fits without colliding with active tasks."
	}
	if len(relatedDocuments) > 0 {
		base += " Relevant documentation should be kept in view so the integration path stays tied to existing product and implementation guidance."
	}
	if latestSyncEvidence != "" {
		base += " Use recent sync state as a guardrail for how aggressively the integration path should move."
	}
	return joinParagraphs(requirement.Summary, base)
}

func buildIntegrationRationale(openTaskTitles, relatedDocuments []string, latestSyncFailed bool) string {
	if latestSyncFailed {
		return "Integration is elevated because recent sync instability means the requirement should be wired through the current workflow carefully, not only described as an isolated feature slice."
	}
	if len(relatedDocuments) > 0 {
		return "Second recommendation because nearby documents and active workflow context make an integration slice more trustworthy than a generic follow-up task."
	}
	if len(openTaskTitles) > 0 {
		return "Second recommendation because the backlog suggestion becomes more trustworthy once it is grounded against the current workflow and active work inventory."
	}
	return "Second recommendation because user-facing backlog suggestions only matter once the requirement is connected to the existing planning workflow."
}

func buildValidationDescription(requirement *models.Requirement, openTaskTitles, staleDocuments, driftHighlights []string, latestSyncFailed, hasRecentFailures bool) string {
	base := "Add review, duplicate, and apply safeguards so ranked suggestions stay safe to approve and materialize into tasks."
	if len(openTaskTitles) > 0 {
		base += " This is especially useful when the project already has active work that could overlap with the new suggestion set."
	}
	if len(staleDocuments) > 0 || len(driftHighlights) > 0 {
		base += " The current documentation baseline shows signs of drift, so validation should protect against recommendations that look plausible but are not yet aligned."
	}
	if latestSyncFailed || hasRecentFailures {
		base += " Recent operational failures also raise the value of a validation slice before more work is applied into the task system."
	}
	return joinParagraphs(requirement.Summary, base)
}

func buildValidationRationale(primaryDuplicates, openTaskTitles, staleDocuments, driftHighlights []string, latestSyncFailed, hasRecentFailures bool) string {
	if len(primaryDuplicates) > 0 {
		return "Validation is elevated because exact-title overlap already exists in open work, so review safety and duplicate detection matter before apply."
	}
	if len(staleDocuments) > 0 || len(driftHighlights) > 0 || latestSyncFailed || hasRecentFailures {
		return "Validation is elevated because the broader project context shows drift or recent failures, so recommendation safety has to improve before more backlog is materialized."
	}
	if len(openTaskTitles) > 0 {
		return "Validation stays in the suggestion set because active project work increases the chance of overlap and review mistakes even when there is no exact duplicate."
	}
	return "Validation stays in the suggestion set so review/apply behavior remains safe as planning output becomes more autonomous."
}

func requirementEvidence(requirement *models.Requirement) []string {
	evidence := []string{}
	if requirement == nil {
		return evidence
	}
	if summary := strings.TrimSpace(requirement.Summary); summary != "" {
		evidence = append(evidence, "Requirement summary: "+summary)
	}
	if description := strings.TrimSpace(requirement.Description); description != "" {
		evidence = append(evidence, "Requirement description captured for planning context.")
	}
	if len(evidence) == 0 && strings.TrimSpace(requirement.Title) != "" {
		evidence = append(evidence, "Requirement title: "+strings.TrimSpace(requirement.Title))
	}
	return evidence
}

func workflowEvidence(openTaskTitles []string) []string {
	if len(openTaskTitles) == 0 {
		return []string{"No exact-title overlap was found in current open tasks."}
	}
	return []string{"Active open tasks that should be considered: " + strings.Join(openTaskTitles, ", ")}
}

func documentEvidence(documents []string) []string {
	if len(documents) == 0 {
		return nil
	}
	return []string{"Related project context from documents: " + strings.Join(documents, ", ")}
}

func driftEvidence(highlights []string) []string {
	if len(highlights) == 0 {
		return nil
	}
	return []string{"Open drift signals relevant to planning: " + strings.Join(highlights, ", ")}
}

func validationEvidence(duplicates, openTaskTitles []string) []string {
	if len(duplicates) > 0 {
		return []string{"Exact-title overlap found in open tasks: " + strings.Join(duplicates, ", ")}
	}
	if len(openTaskTitles) > 0 {
		return []string{"Project currently has active work that may overlap indirectly: " + strings.Join(openTaskTitles, ", ")}
	}
	return []string{"No exact-title overlap detected, so validation can focus on review and apply safety."}
}

func duplicateEvidence(duplicates []string) []string {
	if len(duplicates) == 0 {
		return nil
	}
	return []string{"Potential duplicate open tasks: " + strings.Join(duplicates, ", ")}
}

func dependencyUnlockScore(openTaskTitles, relatedDocuments []string, latestSyncFailed bool, base float64) float64 {
	score := base
	if len(openTaskTitles) > 0 {
		score += 0.04
	}
	if len(relatedDocuments) > 0 {
		score += 0.06
	}
	if latestSyncFailed {
		score -= 0.03
	}
	return clampUnit(score)
}

func buildDocumentEvidenceEntries(requirement *models.Requirement, documents []models.Document, reason string) []models.PlanningDocumentEvidence {
	keywords := requirementKeywords(requirement)
	entries := make([]models.PlanningDocumentEvidence, 0, len(documents))
	for _, document := range documents {
		entry := models.PlanningDocumentEvidence{
			DocumentID:          document.ID,
			Title:               strings.TrimSpace(document.Title),
			FilePath:            strings.TrimSpace(document.FilePath),
			DocType:             strings.TrimSpace(document.DocType),
			IsStale:             document.IsStale,
			StalenessDays:       document.StalenessDays,
			MatchedKeywords:     matchedDocumentKeywords(document, keywords),
			ContributionReasons: []string{strings.TrimSpace(reason)},
		}
		entries = append(entries, entry)
	}
	return entries
}

func buildStaleDocumentEvidenceEntries(documents []models.Document, limit int) []models.PlanningDocumentEvidence {
	if limit < 1 {
		limit = 1
	}
	entries := make([]models.PlanningDocumentEvidence, 0, limit)
	for _, document := range documents {
		if !document.IsStale {
			continue
		}
		entries = append(entries, models.PlanningDocumentEvidence{
			DocumentID:    document.ID,
			Title:         strings.TrimSpace(document.Title),
			FilePath:      strings.TrimSpace(document.FilePath),
			DocType:       strings.TrimSpace(document.DocType),
			IsStale:       true,
			StalenessDays: document.StalenessDays,
			ContributionReasons: []string{
				"Stale documentation increases validation and reconciliation priority.",
			},
		})
		if len(entries) == limit {
			break
		}
	}
	return entries
}

func summarizeDocumentEvidenceEntries(entries []models.PlanningDocumentEvidence) []string {
	titles := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := strings.TrimSpace(entry.Title)
		if label == "" {
			label = strings.TrimSpace(entry.FilePath)
		}
		if label == "" {
			continue
		}
		if entry.IsStale {
			label += " (stale)"
		}
		titles = append(titles, label)
	}
	return dedupeStrings(titles)
}

func buildDriftSignalEvidenceEntries(signals []models.DriftSignal, limit int) ([]models.PlanningDriftSignalEvidence, []string, bool) {
	if limit < 1 {
		limit = 1
	}
	entries := make([]models.PlanningDriftSignalEvidence, 0, limit)
	highlights := make([]string, 0, limit)
	hasHighSeverity := false
	for _, signal := range signals {
		if signal.Severity >= 3 {
			hasHighSeverity = true
		}
		label := strings.TrimSpace(signal.DocumentTitle)
		if label == "" {
			label = strings.TrimSpace(signal.TriggerDetail)
		}
		if label == "" {
			label = signal.TriggerType
		}
		highlights = append(highlights, fmt.Sprintf("%s (%s)", label, signal.TriggerType))
		entries = append(entries, models.PlanningDriftSignalEvidence{
			DriftSignalID: signal.ID,
			DocumentID:    signal.DocumentID,
			DocumentTitle: strings.TrimSpace(signal.DocumentTitle),
			Severity:      signal.Severity,
			TriggerType:   signal.TriggerType,
			TriggerDetail: strings.TrimSpace(signal.TriggerDetail),
			ContributionReasons: []string{
				"Open drift signals raise confidence that validation or cleanup work should be prioritized.",
			},
		})
		if len(entries) == limit {
			break
		}
	}
	return entries, dedupeStrings(highlights), hasHighSeverity
}

func buildSyncRunEvidence(syncRun *models.SyncRun) (*models.PlanningSyncRunEvidence, string, bool) {
	message, failed := summarizeLatestSync(syncRun)
	if syncRun == nil {
		return nil, message, failed
	}
	detail := &models.PlanningSyncRunEvidence{
		SyncRunID:      syncRun.ID,
		Status:         syncRun.Status,
		CommitsScanned: syncRun.CommitsScanned,
		FilesChanged:   syncRun.FilesChanged,
		ErrorMessage:   strings.TrimSpace(syncRun.ErrorMessage),
		ContributionReasons: []string{
			"Latest sync status affects confidence in how current repository signals should influence backlog decomposition.",
		},
	}
	return detail, message, failed
}

func buildRecentFailedAgentRunEvidence(agentRuns []models.AgentRun, limit int) ([]models.PlanningAgentRunEvidence, []string, bool) {
	messages, hasFailures := summarizeRecentAgentRunFailures(agentRuns, limit)
	if !hasFailures {
		return []models.PlanningAgentRunEvidence{}, messages, false
	}
	entries := make([]models.PlanningAgentRunEvidence, 0, limit)
	for _, agentRun := range agentRuns {
		if agentRun.Status != models.AgentRunStatusFailed {
			continue
		}
		entries = append(entries, models.PlanningAgentRunEvidence{
			AgentRunID:   agentRun.ID,
			AgentName:    strings.TrimSpace(agentRun.AgentName),
			ActionType:   strings.TrimSpace(agentRun.ActionType),
			Status:       agentRun.Status,
			Summary:      strings.TrimSpace(agentRun.Summary),
			ErrorMessage: strings.TrimSpace(agentRun.ErrorMessage),
			ContributionReasons: []string{
				"Recent agent failures increase the value of validation and recovery-oriented backlog slices.",
			},
		})
		if len(entries) == limit {
			break
		}
	}
	return entries, messages, true
}

func buildRecentAgentActivityEvidence(agentRuns []models.AgentRun, limit int) ([]models.PlanningAgentRunEvidence, []string) {
	messages := summarizeRecentAgentActivity(agentRuns, limit)
	entries := make([]models.PlanningAgentRunEvidence, 0, limit)
	for _, agentRun := range agentRuns {
		if agentRun.Status != models.AgentRunStatusCompleted {
			continue
		}
		entries = append(entries, models.PlanningAgentRunEvidence{
			AgentRunID: agentRun.ID,
			AgentName:  strings.TrimSpace(agentRun.AgentName),
			ActionType: strings.TrimSpace(agentRun.ActionType),
			Status:     agentRun.Status,
			Summary:    strings.TrimSpace(agentRun.Summary),
			ContributionReasons: []string{
				"Recent successful agent activity improves confidence that nearby project context is still actionable.",
			},
		})
		if len(entries) == limit {
			break
		}
	}
	return entries, messages
}

func buildDuplicateEvidenceEntries(duplicates []string, reason string) []models.PlanningDuplicateEvidence {
	entries := make([]models.PlanningDuplicateEvidence, 0, len(duplicates))
	for _, title := range duplicates {
		trimmed := strings.TrimSpace(title)
		if trimmed == "" {
			continue
		}
		entries = append(entries, models.PlanningDuplicateEvidence{
			Title:               trimmed,
			ContributionReasons: []string{strings.TrimSpace(reason)},
		})
	}
	return entries
}

func extractDuplicateTitles(entries []models.PlanningDuplicateEvidence) []string {
	titles := make([]string, 0, len(entries))
	for _, entry := range entries {
		titles = append(titles, entry.Title)
	}
	return dedupeStrings(titles)
}

func matchedDocumentKeywords(document models.Document, keywords []string) []string {
	combined := strings.ToLower(strings.TrimSpace(document.Title + " " + document.FilePath + " " + document.DocType))
	matched := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if strings.Contains(combined, strings.ToLower(keyword)) {
			matched = append(matched, keyword)
		}
	}
	return dedupeStrings(matched)
}

func cloneDocumentEvidenceEntries(entries []models.PlanningDocumentEvidence) []models.PlanningDocumentEvidence {
	cloned := append([]models.PlanningDocumentEvidence{}, entries...)
	for index := range cloned {
		cloned[index].MatchedKeywords = dedupeStrings(entries[index].MatchedKeywords)
		cloned[index].ContributionReasons = dedupeStrings(entries[index].ContributionReasons)
	}
	return cloned
}

func cloneDriftSignalEvidenceEntries(entries []models.PlanningDriftSignalEvidence) []models.PlanningDriftSignalEvidence {
	cloned := append([]models.PlanningDriftSignalEvidence{}, entries...)
	for index := range cloned {
		cloned[index].ContributionReasons = dedupeStrings(entries[index].ContributionReasons)
	}
	return cloned
}

func cloneAgentRunEvidenceEntries(entries []models.PlanningAgentRunEvidence) []models.PlanningAgentRunEvidence {
	cloned := append([]models.PlanningAgentRunEvidence{}, entries...)
	for index := range cloned {
		cloned[index].ContributionReasons = dedupeStrings(entries[index].ContributionReasons)
	}
	return cloned
}

func cloneDuplicateEvidenceEntries(entries []models.PlanningDuplicateEvidence) []models.PlanningDuplicateEvidence {
	cloned := append([]models.PlanningDuplicateEvidence{}, entries...)
	for index := range cloned {
		cloned[index].ContributionReasons = dedupeStrings(entries[index].ContributionReasons)
	}
	return cloned
}

func cloneSyncRunEvidence(detail *models.PlanningSyncRunEvidence) *models.PlanningSyncRunEvidence {
	if detail == nil {
		return nil
	}
	cloned := *detail
	cloned.ContributionReasons = dedupeStrings(detail.ContributionReasons)
	return &cloned
}

func summarizePlanningDocuments(requirement *models.Requirement, documents []models.Document, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	selected := selectPlanningDocuments(requirement, documents, limit)
	titles := make([]string, 0, len(selected))
	for _, document := range selected {
		label := strings.TrimSpace(document.Title)
		if label == "" {
			label = strings.TrimSpace(document.FilePath)
		}
		if label == "" {
			continue
		}
		if document.IsStale {
			label += " (stale)"
		}
		titles = append(titles, label)
	}
	return dedupeStrings(titles)
}

func summarizeStaleDocuments(documents []models.Document, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	stale := make([]string, 0, limit)
	for _, document := range documents {
		if !document.IsStale {
			continue
		}
		label := strings.TrimSpace(document.Title)
		if label == "" {
			label = strings.TrimSpace(document.FilePath)
		}
		if label == "" {
			continue
		}
		stale = append(stale, label)
		if len(stale) == limit {
			break
		}
	}
	return dedupeStrings(stale)
}

func summarizeDriftSignals(signals []models.DriftSignal, limit int) ([]string, bool) {
	if limit < 1 {
		limit = 1
	}
	highlights := make([]string, 0, limit)
	hasHighSeverity := false
	for _, signal := range signals {
		if signal.Severity >= 3 {
			hasHighSeverity = true
		}
		label := strings.TrimSpace(signal.DocumentTitle)
		if label == "" {
			label = strings.TrimSpace(signal.TriggerDetail)
		}
		if label == "" {
			label = signal.TriggerType
		}
		highlights = append(highlights, fmt.Sprintf("%s (%s)", label, signal.TriggerType))
		if len(highlights) == limit {
			break
		}
	}
	return dedupeStrings(highlights), hasHighSeverity
}

func summarizeLatestSync(syncRun *models.SyncRun) (string, bool) {
	if syncRun == nil {
		return "", false
	}
	if syncRun.Status == "failed" {
		message := "Latest sync failed"
		if detail := strings.TrimSpace(syncRun.ErrorMessage); detail != "" {
			message += ": " + detail
		}
		return message, true
	}
	if syncRun.Status == "completed" {
		return fmt.Sprintf("Latest sync completed after scanning %d commits and %d files.", syncRun.CommitsScanned, syncRun.FilesChanged), false
	}
	return fmt.Sprintf("Latest sync status: %s", syncRun.Status), false
}

func summarizeRecentAgentRunFailures(agentRuns []models.AgentRun, limit int) ([]string, bool) {
	if limit < 1 {
		limit = 1
	}
	failures := make([]string, 0, limit)
	for _, agentRun := range agentRuns {
		if agentRun.Status != models.AgentRunStatusFailed {
			continue
		}
		label := strings.TrimSpace(agentRun.AgentName)
		if label == "" {
			label = "agent"
		}
		failures = append(failures, fmt.Sprintf("Recent failed %s run (%s)", label, agentRun.ActionType))
		if len(failures) == limit {
			break
		}
	}
	if len(failures) == 0 {
		return nil, false
	}
	return dedupeStrings(failures), true
}

func summarizeRecentAgentActivity(agentRuns []models.AgentRun, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	activity := make([]string, 0, limit)
	for _, agentRun := range agentRuns {
		if agentRun.Status != models.AgentRunStatusCompleted {
			continue
		}
		activity = append(activity, fmt.Sprintf("Recent completed %s run by %s", agentRun.ActionType, strings.TrimSpace(agentRun.AgentName)))
		if len(activity) == limit {
			break
		}
	}
	return dedupeStrings(activity)
}

func summarizeOpenTaskTitles(tasks []models.Task, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	items := make([]string, 0, limit)
	for _, task := range tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			continue
		}
		items = append(items, title)
		if len(items) == limit {
			break
		}
	}
	return items
}

func findDuplicateTitles(title string, tasks []models.Task) []string {
	normalizedTitle := normalizeTitle(title)
	if normalizedTitle == "" {
		return nil
	}
	duplicates := make([]string, 0, 2)
	for _, task := range tasks {
		if normalizeTitle(task.Title) == normalizedTitle {
			duplicates = append(duplicates, strings.TrimSpace(task.Title))
		}
	}
	return dedupeStrings(duplicates)
}

func requirementUrgency(requirement *models.Requirement) float64 {
	combined := strings.ToLower(strings.TrimSpace(requirement.Title + " " + requirement.Summary + " " + requirement.Description))
	urgency := 0.58
	for _, keyword := range []string{"urgent", "blocker", "currently", "now", "immediately", "broken", "fix", "squeeze", "擠壓", "主要目標"} {
		if strings.Contains(combined, keyword) {
			urgency += 0.12
		}
	}
	return clampUnit(urgency)
}

func duplicatePenalty(duplicates []string) float64 {
	if len(duplicates) == 0 {
		return 0
	}
	penalty := 0.10 + float64(len(duplicates)-1)*0.03
	return clampUnit(penalty)
}

func weightedPriorityScore(factors suggestionFactors) float64 {
	confidence := clampUnit(factors.confidence - factors.duplicatePenalty)
	score :=
		0.30*factors.impact +
			0.25*clampUnit(factors.urgency) +
			0.20*factors.dependencyUnlock +
			0.15*factors.riskReduction +
			0.10*confidence -
			0.15*factors.effort
	return roundTenth(clampPercentage(score * 100))
}

func computePriorityScore(evidence, duplicates []string, seed float64) float64 {
	bonus := math.Min(float64(len(evidence))*1.8, 6)
	penalty := float64(len(duplicates)) * 6.5
	return roundTenth(clampPercentage(seed + bonus - penalty))
}

func confidenceSeed(seed float64) float64 {
	return roundTenth(clampPercentage(seed * 100))
}

func computeConfidence(seed float64, evidence, duplicates []string) float64 {
	bonus := math.Min(float64(len(evidence))*2.5, 10)
	penalty := float64(len(duplicates)) * 9
	return roundTenth(clampPercentage(seed + bonus - penalty))
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func clampPercentage(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func roundTenth(value float64) float64 {
	return math.Round(value*10) / 10
}

func normalizeTitle(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func joinParagraphs(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		nonEmpty = append(nonEmpty, trimmed)
	}
	return strings.Join(nonEmpty, "\n\n")
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	return result
}
