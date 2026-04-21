package planning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

const (
	openAICompatibleChatCompletionsPath = "/chat/completions"
	defaultOpenAICompatibleMaxBytes     = 128 * 1024
	maxOpenAICompatibleCandidates       = 5
	// defaultOpenAICompatibleMaxRequestBytes caps the size of the JSON request
	// body sent to the remote provider. Mirrors wire.DefaultMaxSourcesBytes so
	// the server-side prompt path cannot egress more context than the local
	// connector path. See docs/local-connector-context.md §6.
	defaultOpenAICompatibleMaxRequestBytes = 256 * 1024
)

type OpenAICompatibleProvider struct {
	baseURL          string
	apiKey           string
	httpClient       *http.Client
	maxCandidates    int
	maxResponseBytes int64
	maxRequestBytes  int
}

type openAICompatibleChatRequest struct {
	Model       string                        `json:"model"`
	Messages    []openAICompatibleChatMessage `json:"messages"`
	Temperature float64                       `json:"temperature,omitempty"`
}

type openAICompatibleChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAICompatibleChatResponse struct {
	Choices []struct {
		Message struct {
			Content openAICompatibleMessageContent `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAICompatibleMessageContent struct {
	Text string
}

type openAICompatibleResponseEnvelope struct {
	Candidates []openAICompatibleGeneratedCandidate `json:"candidates"`
}

type openAICompatibleGeneratedCandidate struct {
	SuggestionType string `json:"suggestion_type"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Rationale      string `json:"rationale"`
}

func newOpenAICompatibleRegisteredProvider(config OpenAICompatibleProviderConfig) (*RegisteredProvider, error) {
	if !config.Enabled {
		if strings.TrimSpace(config.DefaultProviderID) == models.PlanningProviderOpenAICompatible {
			return nil, fmt.Errorf("default planning provider %q requires PLANNING_OPENAI_COMPAT_ENABLED=true", models.PlanningProviderOpenAICompatible)
		}
		return nil, nil
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("planning openai-compatible provider requires a base URL")
	}
	if len(config.Models) == 0 {
		return nil, fmt.Errorf("planning openai-compatible provider requires at least one configured model")
	}
	if config.Timeout <= 0 {
		config.Timeout = 45
	}
	if config.MaxCandidates < 1 {
		config.MaxCandidates = 3
	}
	if config.MaxCandidates > maxOpenAICompatibleCandidates {
		config.MaxCandidates = maxOpenAICompatibleCandidates
	}
	if config.MaxResponseBytes < 1 {
		config.MaxResponseBytes = defaultOpenAICompatibleMaxBytes
	}

	modelDescriptors := make([]models.PlanningProviderModel, 0, len(config.Models))
	for _, modelID := range config.Models {
		trimmed := strings.TrimSpace(modelID)
		if trimmed == "" {
			continue
		}
		modelDescriptors = append(modelDescriptors, models.PlanningProviderModel{
			ID:          trimmed,
			Label:       trimmed,
			Description: "Configured OpenAI-compatible planning model.",
			Enabled:     true,
		})
	}
	if len(modelDescriptors) == 0 {
		return nil, fmt.Errorf("planning openai-compatible provider requires at least one non-empty model")
	}

	provider := &OpenAICompatibleProvider{
		baseURL:          strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		apiKey:           strings.TrimSpace(config.APIKey),
		httpClient:       &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		maxCandidates:    config.MaxCandidates,
		maxResponseBytes: config.MaxResponseBytes,
		maxRequestBytes:  defaultOpenAICompatibleMaxRequestBytes,
	}

	defaultModelID := modelDescriptors[0].ID
	return &RegisteredProvider{
		Descriptor: models.PlanningProviderDescriptor{
			ID:             models.PlanningProviderOpenAICompatible,
			Label:          "OpenAI-Compatible Planner",
			Kind:           "llm",
			Description:    "Remote planning provider using a configured OpenAI-compatible chat completions endpoint.",
			DefaultModelID: defaultModelID,
			Models:         modelDescriptors,
		},
		Implementation: provider,
	}, nil
}

func (p *OpenAICompatibleProvider) Generate(ctx context.Context, requirement *models.Requirement, planningContext PlanningContext, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	if requirement == nil {
		return nil, fmt.Errorf("requirement is required")
	}
	if strings.TrimSpace(selection.ModelID) == "" {
		return nil, fmt.Errorf("planning model is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// T3.B: server-side LLM egress now consumes the same sanitized + reduced
	// wire DTO that the local connector path uses, so secret redaction,
	// per-field caps, and the 256 KiB sources budget are guaranteed by a
	// single contract (DECISIONS 2026-04-21, 2026-04-22).
	sanitizedWire := sanitizeForOpenAIEgress(requirement, planningContext)

	payload := openAICompatibleChatRequest{
		Model: selection.ModelID,
		Messages: []openAICompatibleChatMessage{
			{Role: "system", Content: openAICompatibleSystemPrompt(p.maxCandidates)},
			{Role: "user", Content: openAICompatibleUserPrompt(requirement, sanitizedWire, p.maxCandidates)},
		},
		Temperature: 0.2,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if p.maxRequestBytes > 0 && len(body) > p.maxRequestBytes {
		return nil, fmt.Errorf("openai-compatible planning request exceeds %d bytes (got %d); reduce planning context limits", p.maxRequestBytes, len(body))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+openAICompatibleChatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openai-compatible planning provider: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, p.maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read openai-compatible planning response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-compatible planning provider returned %d: %s", resp.StatusCode, openAICompatibleErrorMessage(bodyBytes))
	}

	content, err := extractOpenAICompatibleContent(bodyBytes)
	if err != nil {
		return nil, err
	}

	generated, err := parseOpenAICompatibleCandidates(content)
	if err != nil {
		return nil, err
	}

	drafts := buildOpenAICompatibleDrafts(requirement, planningContext, generated)
	if len(drafts) == 0 {
		return nil, fmt.Errorf("openai-compatible planning provider returned no valid candidates")
	}
	return drafts, nil
}

func extractOpenAICompatibleContent(body []byte) (string, error) {
	var response openAICompatibleChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode openai-compatible planning response: %w", err)
	}
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", fmt.Errorf("openai-compatible planning provider error: %s", strings.TrimSpace(response.Error.Message))
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("openai-compatible planning provider returned no choices")
	}
	content := strings.TrimSpace(response.Choices[0].Message.Content.Text)
	if content == "" {
		return "", fmt.Errorf("openai-compatible planning provider returned empty content")
	}
	return content, nil
}

func parseOpenAICompatibleCandidates(content string) ([]openAICompatibleGeneratedCandidate, error) {
	cleaned := sanitizeOpenAICompatibleJSON(content)
	var envelope openAICompatibleResponseEnvelope
	if err := json.Unmarshal([]byte(cleaned), &envelope); err != nil {
		return nil, fmt.Errorf("parse openai-compatible planning JSON: %w", err)
	}
	if len(envelope.Candidates) == 0 {
		return nil, fmt.Errorf("openai-compatible planning provider returned no candidates")
	}
	return envelope.Candidates, nil
}

func buildOpenAICompatibleDrafts(requirement *models.Requirement, context PlanningContext, generated []openAICompatibleGeneratedCandidate) []models.BacklogCandidateDraft {
	openTaskTitles := summarizeOpenTaskTitles(context.OpenTasks, planningEvidenceSampleLimit)
	relatedDocumentEntries := buildDocumentEvidenceEntries(requirement, selectPlanningDocuments(requirement, context.RecentDocuments, planningEvidenceSampleLimit), "Related project context influenced ranking.")
	relatedDocuments := summarizeDocumentEvidenceEntries(relatedDocumentEntries)
	staleDocumentEntries := buildStaleDocumentEvidenceEntries(context.RecentDocuments, planningEvidenceSampleLimit)
	staleDocuments := summarizeDocumentEvidenceEntries(staleDocumentEntries)
	driftEntries, driftHighlights, hasHighSeverityDrift := buildDriftSignalEvidenceEntries(context.OpenDriftSignals, planningEvidenceSampleLimit)
	latestSyncDetail, latestSyncEvidence, latestSyncFailed := buildSyncRunEvidence(context.LatestSyncRun)
	recentFailureEntries, recentFailureEvidence, hasRecentFailures := buildRecentFailedAgentRunEvidence(context.RecentAgentRuns, planningFailureSampleLimit)
	recentActivityEntries, recentActivityEvidence := buildRecentAgentActivityEvidence(context.RecentAgentRuns, planningEvidenceSampleLimit)

	urgency := requirementUrgency(requirement)
	baseOrderBonus := 0.03

	drafts := make([]models.BacklogCandidateDraft, 0, len(generated))
	seenTitles := map[string]bool{}
	for index, candidate := range generated {
		title := strings.TrimSpace(candidate.Title)
		if title == "" {
			continue
		}
		normalizedTitle := normalizeTitle(title)
		if seenTitles[normalizedTitle] {
			continue
		}
		seenTitles[normalizedTitle] = true

		suggestionType := normalizeGeneratedSuggestionType(candidate.SuggestionType, index)
		duplicates := findDuplicateTitles(title, context.OpenTasks)
		duplicateEntries := buildDuplicateEvidenceEntries(duplicates, duplicateReasonForSuggestionType(suggestionType))
		orderBonus := clampUnit(baseOrderBonus - (float64(index) * 0.01))

		description := strings.TrimSpace(candidate.Description)
		if description == "" {
			description = joinParagraphs(requirement.Summary, requirement.Description)
		}
		rationale := strings.TrimSpace(candidate.Rationale)
		if rationale == "" {
			rationale = fallbackRationaleForSuggestionType(suggestionType)
		}

		var draft models.BacklogCandidateDraft
		switch suggestionType {
		case "integration":
			evidence := append(requirementEvidence(requirement), workflowEvidence(openTaskTitles)...)
			evidence = append(evidence, documentEvidence(relatedDocuments)...)
			if latestSyncEvidence != "" {
				evidence = append(evidence, latestSyncEvidence)
			}
			draft = buildDraftCandidate(
				suggestionType,
				title,
				description,
				rationale,
				evidence,
				extractDuplicateTitles(duplicateEntries),
				models.PlanningEvidenceDetail{
					Summary:    dedupeStrings(evidence),
					Documents:  cloneDocumentEvidenceEntries(relatedDocumentEntries),
					SyncRun:    cloneSyncRunEvidence(latestSyncDetail),
					Duplicates: cloneDuplicateEvidenceEntries(duplicateEntries),
				},
				suggestionFactors{impact: 0.76 + orderBonus, urgency: clampUnit(urgency + 0.02), dependencyUnlock: dependencyUnlockScore(openTaskTitles, relatedDocuments, latestSyncFailed, 0.79), riskReduction: 0.57, effort: 0.48, confidence: 0.69 + orderBonus, duplicatePenalty: duplicatePenalty(duplicates)},
			)
		case "validation":
			validationUrgency := clampUnit(urgency - 0.03)
			validationRiskReduction := 0.76
			validationConfidence := 0.65 + orderBonus
			if latestSyncFailed || hasRecentFailures {
				validationUrgency = clampUnit(validationUrgency + 0.12)
				validationRiskReduction = clampUnit(validationRiskReduction + 0.12)
				validationConfidence = clampUnit(validationConfidence + 0.08)
			}
			if hasHighSeverityDrift || len(staleDocuments) > 0 {
				validationUrgency = clampUnit(validationUrgency + 0.08)
				validationRiskReduction = clampUnit(validationRiskReduction + 0.08)
				validationConfidence = clampUnit(validationConfidence + 0.06)
			}
			evidence := append(requirementEvidence(requirement), validationEvidence(duplicates, openTaskTitles)...)
			evidence = append(evidence, documentEvidence(staleDocuments)...)
			evidence = append(evidence, driftEvidence(driftHighlights)...)
			if latestSyncEvidence != "" {
				evidence = append(evidence, latestSyncEvidence)
			}
			evidence = append(evidence, recentFailureEvidence...)
			draft = buildDraftCandidate(
				suggestionType,
				title,
				description,
				rationale,
				evidence,
				extractDuplicateTitles(duplicateEntries),
				models.PlanningEvidenceDetail{
					Summary:      dedupeStrings(evidence),
					Documents:    cloneDocumentEvidenceEntries(staleDocumentEntries),
					DriftSignals: cloneDriftSignalEvidenceEntries(driftEntries),
					SyncRun:      cloneSyncRunEvidence(latestSyncDetail),
					AgentRuns:    cloneAgentRunEvidenceEntries(recentFailureEntries),
					Duplicates:   cloneDuplicateEvidenceEntries(duplicateEntries),
				},
				suggestionFactors{impact: 0.63 + orderBonus, urgency: validationUrgency, dependencyUnlock: dependencyUnlockScore(openTaskTitles, staleDocuments, latestSyncFailed, 0.46), riskReduction: validationRiskReduction, effort: 0.39, confidence: validationConfidence, duplicatePenalty: duplicatePenalty(duplicates)},
			)
		default:
			implementationConfidence := 0.72 + orderBonus
			implementationRiskReduction := 0.46
			if len(relatedDocuments) > 0 {
				implementationConfidence = clampUnit(implementationConfidence + 0.04)
				implementationRiskReduction = clampUnit(implementationRiskReduction + 0.05)
			}
			evidence := append(requirementEvidence(requirement), duplicateEvidence(duplicates)...)
			evidence = append(evidence, documentEvidence(relatedDocuments)...)
			evidence = append(evidence, recentActivityEvidence...)
			draft = buildDraftCandidate(
				suggestionType,
				title,
				description,
				rationale,
				evidence,
				extractDuplicateTitles(duplicateEntries),
				models.PlanningEvidenceDetail{
					Summary:    dedupeStrings(evidence),
					Documents:  cloneDocumentEvidenceEntries(relatedDocumentEntries),
					AgentRuns:  cloneAgentRunEvidenceEntries(recentActivityEntries),
					Duplicates: cloneDuplicateEvidenceEntries(duplicateEntries),
				},
				suggestionFactors{impact: 0.88 + orderBonus, urgency: urgency, dependencyUnlock: dependencyUnlockScore(openTaskTitles, relatedDocuments, latestSyncFailed, 0.60), riskReduction: implementationRiskReduction, effort: 0.54, confidence: implementationConfidence, duplicatePenalty: duplicatePenalty(duplicates)},
			)
		}
		drafts = append(drafts, draft)
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
	return drafts
}

func normalizeGeneratedSuggestionType(raw string, index int) string {
	suggestionType := strings.ToLower(strings.TrimSpace(raw))
	switch suggestionType {
	case "implementation", "integration", "validation":
		return suggestionType
	}
	if index == 1 {
		return "integration"
	}
	if index >= 2 {
		return "validation"
	}
	return "implementation"
}

func duplicateReasonForSuggestionType(suggestionType string) string {
	switch suggestionType {
	case "integration":
		return "Possible overlap with existing workflow integration work."
	case "validation":
		return "Validation recommendation overlaps with active work and needs review."
	default:
		return "Exact-title overlap with open work reduced ranking confidence."
	}
}

func fallbackRationaleForSuggestionType(suggestionType string) string {
	switch suggestionType {
	case "integration":
		return "Recommended because the requirement should connect to the current workflow before more tasks are applied."
	case "validation":
		return "Recommended because review and apply safety should be strengthened before materializing more tasks."
	default:
		return "Recommended because it is the most direct implementation slice for the requirement."
	}
}

func openAICompatibleSystemPrompt(maxCandidates int) string {
	return fmt.Sprintf(`You are a planning assistant for a software project backlog.
Return only valid JSON with this exact shape:
{"candidates":[{"suggestion_type":"implementation|integration|validation","title":"string","description":"string","rationale":"string"}]}

Rules:
- Return between 1 and %d candidates.
- Keep titles concise and actionable.
- Do not include markdown fences.
- Do not include explanations outside the JSON object.
- suggestion_type must be one of implementation, integration, validation.`, maxCandidates)
}

func openAICompatibleUserPrompt(requirement *models.Requirement, wireCtx *wire.PlanningContextV1, maxCandidates int) string {
	projectContext := map[string]any{
		"open_tasks":         compactOpenTasksFromWire(wireCtx.Sources.OpenTasks),
		"documents":          compactDocumentsFromWire(wireCtx.Sources.RecentDocuments),
		"open_drift_signals": compactDriftSignalsFromWire(wireCtx.Sources.OpenDriftSignals),
		"latest_sync_run":    compactSyncRunFromWire(wireCtx.Sources.LatestSyncRun),
		"recent_agent_runs":  compactAgentRunsFromWire(wireCtx.Sources.RecentAgentRuns),
	}
	request := map[string]any{
		"instruction": fmt.Sprintf("Propose up to %d backlog candidates for this requirement. Prefer distinct slices instead of duplicates.", maxCandidates),
		"requirement": map[string]string{
			"title":       strings.TrimSpace(requirement.Title),
			"summary":     strings.TrimSpace(requirement.Summary),
			"description": strings.TrimSpace(requirement.Description),
		},
		"project_context": projectContext,
		"context_meta": map[string]any{
			"schema_version":    wireCtx.SchemaVersion,
			"sanitizer_version": wireCtx.SanitizerVersion,
			"sources_bytes":     wireCtx.Meta.SourcesBytes,
			"dropped_counts":    wireCtx.Meta.DroppedCounts,
			"warnings":          wireCtx.Meta.Warnings,
		},
		"constraints": []string{
			"Use only the provided project metadata; do not invent repository files or system state.",
			"Keep each candidate suitable for review in a product planning UI.",
			"Prefer implementation, integration, and validation slices when useful.",
		},
	}
	body, _ := json.MarshalIndent(request, "", "  ")
	return string(body)
}

// sanitizeForOpenAIEgress translates an internal PlanningContext into the
// wire DTO, runs the v1 sanitizer (secret redaction + per-field rune caps),
// and applies the sources byte cap. The returned value is safe to forward
// verbatim to a remote LLM endpoint.
func sanitizeForOpenAIEgress(requirement *models.Requirement, planningContext PlanningContext) *wire.PlanningContextV1 {
	// Use the same per-source selection helper as the connector path so
	// the egress is bounded the same way regardless of provider.
	selectedDocuments := selectPlanningDocuments(requirement, planningContext.RecentDocuments, planningEvidenceSampleLimit)
	narrowed := planningContext
	narrowed.RecentDocuments = selectedDocuments

	limits := wire.DefaultLimits()
	sources := translatePlanningContextToWire(narrowed)
	ctx := wire.PlanningContextV1{
		SchemaVersion:    wire.ContextSchemaV1,
		GeneratedBy:      wire.GeneratedByServer,
		SanitizerVersion: wire.SanitizerVersion,
		Limits:           limits,
		Sources:          sources,
		Meta: wire.PlanningContextMeta{
			Ranking:       wire.DefaultRanking(),
			DroppedCounts: map[string]int{},
			SourcesBytes:  0,
			Warnings:      []string{},
		},
	}
	ctx = wire.SanitizePlanningContextV1(ctx)
	reduced, dropped, sourcesBytes := wire.ReduceSources(ctx.Sources, limits.MaxSourcesBytes)
	ctx.Sources = reduced
	ctx.Meta.DroppedCounts = dropped
	ctx.Meta.SourcesBytes = sourcesBytes
	return &ctx
}

func compactOpenTasksFromWire(tasks []wire.WireTask) []map[string]string {
	items := make([]map[string]string, 0, len(tasks))
	for _, task := range tasks {
		if task.Status == "done" || task.Status == "cancelled" {
			continue
		}
		items = append(items, map[string]string{
			"title":  strings.TrimSpace(task.Title),
			"status": strings.TrimSpace(task.Status),
		})
		if len(items) == planningEvidenceSampleLimit {
			break
		}
	}
	return items
}

func compactDocumentsFromWire(documents []wire.WireDocument) []map[string]any {
	items := make([]map[string]any, 0, len(documents))
	for _, document := range documents {
		items = append(items, map[string]any{
			"title":          strings.TrimSpace(document.Title),
			"file_path":      strings.TrimSpace(document.FilePath),
			"doc_type":       strings.TrimSpace(document.DocType),
			"is_stale":       document.IsStale,
			"staleness_days": document.StalenessDays,
		})
		if len(items) == planningEvidenceSampleLimit {
			break
		}
	}
	return items
}

func compactDriftSignalsFromWire(signals []wire.WireDriftSignal) []map[string]any {
	items := make([]map[string]any, 0, len(signals))
	for _, signal := range signals {
		items = append(items, map[string]any{
			"document_title": strings.TrimSpace(signal.DocumentTitle),
			"trigger_type":   strings.TrimSpace(signal.TriggerType),
			"trigger_detail": strings.TrimSpace(signal.TriggerDetail),
			"severity":       signal.Severity,
		})
		if len(items) == planningEvidenceSampleLimit {
			break
		}
	}
	return items
}

func compactSyncRunFromWire(syncRun *wire.WireSyncRun) map[string]any {
	if syncRun == nil {
		return nil
	}
	return map[string]any{
		"status":        strings.TrimSpace(syncRun.Status),
		"error_message": syncRun.ErrorMessage,
	}
}

func compactAgentRunsFromWire(agentRuns []wire.WireAgentRun) []map[string]string {
	items := make([]map[string]string, 0, len(agentRuns))
	for _, agentRun := range agentRuns {
		if agentRun.AgentName == PlannerAgentName && agentRun.ActionType == plannerAction {
			continue
		}
		items = append(items, map[string]string{
			"agent_name":  strings.TrimSpace(agentRun.AgentName),
			"action_type": strings.TrimSpace(agentRun.ActionType),
			"status":      strings.TrimSpace(agentRun.Status),
			"summary":     agentRun.Summary,
		})
		if len(items) == planningEvidenceSampleLimit {
			break
		}
	}
	return items
}

func sanitizeOpenAICompatibleJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
		trimmed = strings.TrimPrefix(trimmed, "json")
		trimmed = strings.TrimSpace(trimmed)
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func openAICompatibleErrorMessage(body []byte) string {
	var response openAICompatibleChatResponse
	if err := json.Unmarshal(body, &response); err == nil && response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return strings.TrimSpace(response.Error.Message)
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 240 {
		return message[:240]
	}
	if message == "" {
		return http.StatusText(http.StatusBadGateway)
	}
	return message
}

func (c *openAICompatibleMessageContent) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		c.Text = asString
		return nil
	}
	var asParts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &asParts); err == nil {
		parts := make([]string, 0, len(asParts))
		for _, part := range asParts {
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, part.Text)
		}
		c.Text = strings.Join(parts, "\n")
		return nil
	}
	return fmt.Errorf("unsupported message content format")
}
