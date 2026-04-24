package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/secrets"
)

// PostgreSQL constraint / index names used by the unique-violation classifier.
// SQLite does not expose these in the error text — see classifyAccountBindingUniqueViolation
// for the fallback path.
const (
	activeBindingConstraint  = "idx_account_bindings_active_unique"  // migration 015
	primaryBindingConstraint = "idx_account_bindings_primary_unique" // migration 021
	// PostgreSQL auto-names inline UNIQUE constraints as <table>_<col1>_..._<colN>_key.
	// See migration 014: `UNIQUE(user_id, provider_id, label)`.
	labelBindingConstraint = "account_bindings_user_id_provider_id_label_key"
)

// classifyAccountBindingUniqueViolation maps a DB unique-constraint violation
// to a typed sentinel so the handler can emit the correct 409 message.
//
// Two driver paths, mirroring planning_run_store.go and agent_run_store.go:
//
//   - PostgreSQL: pq.Error.Constraint carries the constraint/index name
//     deterministically. Switch on it.
//   - SQLite (modernc.org/sqlite): the error text reads
//     `UNIQUE constraint failed: account_bindings.col1, account_bindings.col2`.
//     Partial unique indexes built on expressions (idx_account_bindings_primary_unique
//     uses a CASE on provider_id) do not surface the index name. Disambiguate by
//     the column list — label conflict carries `account_bindings.label`, active
//     conflict carries user_id+provider_id without label, primary conflict is the
//     fallthrough (the CASE expression collapses to a bare user_id reference in
//     the SQLite error text).
//
// Returns the original err if it is not a unique violation.
func classifyAccountBindingUniqueViolation(err error) error {
	if err == nil {
		return nil
	}

	// PostgreSQL path: constraint name is deterministic.
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
		switch pqErr.Constraint {
		case activeBindingConstraint:
			return fmt.Errorf("%w: a binding with this provider is already active", ErrAccountBindingActiveConflict)
		case primaryBindingConstraint:
			return fmt.Errorf("%w: another primary binding already exists in this namespace", ErrAccountBindingPrimaryConflict)
		case labelBindingConstraint:
			return fmt.Errorf("%w: a binding with this provider and label already exists", ErrAccountBindingDuplicateLabel)
		}
		// Unrecognized PG constraint — fall through to the message-based path.
	}

	// SQLite path (and PG fallback): inspect error text.
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") &&
		!strings.Contains(msg, "duplicate key value violates unique constraint") {
		return err
	}

	// Label conflict: most specific column signature.
	if strings.Contains(msg, "account_bindings.label") || strings.Contains(msg, "_label_key") {
		return fmt.Errorf("%w: a binding with this provider and label already exists", ErrAccountBindingDuplicateLabel)
	}
	// Active conflict: (user_id, provider_id) without label.
	if strings.Contains(msg, "account_bindings.user_id") && strings.Contains(msg, "account_bindings.provider_id") {
		return fmt.Errorf("%w: a binding with this provider is already active", ErrAccountBindingActiveConflict)
	}
	// Primary conflict fallthrough: the CASE-expression partial index doesn't
	// surface column names cleanly; if we got here with a UNIQUE error it is
	// most likely the primary index.
	return fmt.Errorf("%w: another primary binding already exists in this namespace", ErrAccountBindingPrimaryConflict)
}

// Sentinel errors so the handler can map store outcomes to proper HTTP
// status codes (400 / 403 / 404 / 409). Keeping these typed avoids the
// existing pattern where every store error became a 400 — which masked
// uniqueness collisions as validation errors.
var (
	ErrAccountBindingValidation       = errors.New("account binding validation failed")
	ErrAccountBindingForbidden        = errors.New("account binding action forbidden")
	ErrAccountBindingNotFound         = errors.New("account binding not found")
	ErrAccountBindingActiveConflict   = errors.New("account binding active uniqueness conflict")
	ErrAccountBindingPrimaryConflict  = errors.New("account binding primary uniqueness conflict")
	ErrAccountBindingDuplicateLabel   = errors.New("account binding label conflict")
)

// cliCommandSanityRe matches the server-side sanity regex from design §6.2
// rule 5 / §10. Real hardening (realpath, allowed-roots, interpreter
// blocklist, setuid rejection) lives connector-side in S2/S5b — the server
// cannot validate filesystem state of the connector host.
var cliCommandSanityRe = regexp.MustCompile(`^/[A-Za-z0-9_./\-]+$`)

type AccountBindingStore struct {
	db  *sql.DB
	box *secrets.Box
}

type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func NewAccountBindingStore(db *sql.DB, box *secrets.Box) *AccountBindingStore {
	return &AccountBindingStore{db: db, box: box}
}

func deactivateOtherActiveBindings(exec sqlExecutor, userID, providerID, keepID string) error {
	query := `
		UPDATE account_bindings
		SET is_active = FALSE,
		    updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND provider_id = $2 AND is_active = TRUE
	`
	args := []interface{}{userID, providerID}
	if strings.TrimSpace(keepID) != "" {
		query += ` AND id <> $3`
		args = append(args, keepID)
	}
	_, err := exec.Exec(query, args...)
	return err
}

// demoteOtherPrimaryBindings flips is_primary off for any other binding in
// the same `(user_id, namespace)` group, where namespace is `cli` for
// `cli:*` providers and `api` otherwise. Required inside the same TX as
// any INSERT/UPDATE that sets is_primary=TRUE; the partial unique index
// `idx_account_bindings_primary_unique` would otherwise reject the change
// (see migration 021).
func demoteOtherPrimaryBindings(exec sqlExecutor, userID, providerID, keepID string) error {
	namespace := primaryNamespaceForProvider(providerID)
	query := `
		UPDATE account_bindings
		SET is_primary = FALSE,
		    updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $1
		  AND is_primary = TRUE
		  AND CASE WHEN provider_id LIKE 'cli:%' THEN 'cli' ELSE 'api' END = $2
	`
	args := []interface{}{userID, namespace}
	if strings.TrimSpace(keepID) != "" {
		query += ` AND id <> $3`
		args = append(args, keepID)
	}
	_, err := exec.Exec(query, args...)
	return err
}

// primaryNamespaceForProvider mirrors the SQL CASE expression in the
// idx_account_bindings_primary_unique index. Keep these two in lockstep:
// changing one without the other introduces silent multi-primary bugs.
func primaryNamespaceForProvider(providerID string) string {
	if models.IsCLIAccountBindingProvider(providerID) {
		return "cli"
	}
	return "api"
}

// countActiveByNamespace returns the number of active bindings the user has
// in the given namespace (`cli` or `api`). Used by Create to auto-set
// is_primary=TRUE on the first binding in a namespace.
func countActiveByNamespace(exec interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}, userID, namespace string) (int, error) {
	var n int
	err := exec.QueryRow(`
		SELECT COUNT(*) FROM account_bindings
		WHERE user_id = $1
		  AND is_active = TRUE
		  AND CASE WHEN provider_id LIKE 'cli:%' THEN 'cli' ELSE 'api' END = $2
	`, userID, namespace).Scan(&n)
	return n, err
}

func (s *AccountBindingStore) ListByUser(userID string) ([]models.AccountBinding, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_configured, is_active, cli_command, is_primary, created_at, updated_at,
		       last_probe_at, last_probe_ok, last_probe_ms
		FROM account_bindings
		WHERE user_id = $1
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bindings []models.AccountBinding
	for rows.Next() {
		var b models.AccountBinding
		var configuredModelsRaw []byte
		var lastProbeAt sql.NullTime
		var lastProbeOk sql.NullBool
		var lastProbeMs sql.NullInt32
		if err := rows.Scan(
			&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
			&configuredModelsRaw, &b.APIKeyConfigured, &b.IsActive,
			&b.CliCommand, &b.IsPrimary, &b.CreatedAt, &b.UpdatedAt,
			&lastProbeAt, &lastProbeOk, &lastProbeMs,
		); err != nil {
			return nil, err
		}
		if len(configuredModelsRaw) > 0 {
			if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
				return nil, fmt.Errorf("decode account binding configured models: %w", err)
			}
		}
		if b.ConfiguredModels == nil {
			b.ConfiguredModels = []string{}
		}
		if lastProbeAt.Valid {
			t := lastProbeAt.Time
			b.LastProbeAt = &t
		}
		if lastProbeOk.Valid {
			v := lastProbeOk.Bool
			b.LastProbeOk = &v
		}
		if lastProbeMs.Valid {
			v := int(lastProbeMs.Int32)
			b.LastProbeMs = &v
		}
		bindings = append(bindings, b)
	}
	if bindings == nil {
		bindings = []models.AccountBinding{}
	}
	return bindings, rows.Err()
}

// RecordProbe writes the result of a connection probe back to the binding row.
// Called best-effort after POST /api/me/probe-model; failures are logged by
// the caller but do not affect the HTTP response already in flight.
func (s *AccountBindingStore) RecordProbe(id, userID string, ok bool, latencyMS int64) error {
	now := time.Now().UTC()
	ms := int(latencyMS)
	_, err := s.db.Exec(`
		UPDATE account_bindings
		SET last_probe_at = $1, last_probe_ok = $2, last_probe_ms = $3, updated_at = $4
		WHERE id = $5 AND user_id = $6
	`, now, ok, ms, now, id, userID)
	return err
}

func (s *AccountBindingStore) GetByID(id, userID string) (*models.StoredAccountBinding, error) {
	var b models.StoredAccountBinding
	var configuredModelsRaw []byte
	err := s.db.QueryRow(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_ciphertext, api_key_configured, is_active,
		       cli_command, is_primary, created_at, updated_at
		FROM account_bindings
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
		&configuredModelsRaw, &b.APIKeyCiphertext, &b.APIKeyConfigured, &b.IsActive,
		&b.CliCommand, &b.IsPrimary, &b.CreatedAt, &b.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(configuredModelsRaw) > 0 {
		if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
			return nil, fmt.Errorf("decode account binding configured models: %w", err)
		}
	}
	if b.ConfiguredModels == nil {
		b.ConfiguredModels = []string{}
	}
	return &b, nil
}

// GetActiveByUserAndProvider returns the first active binding for a user+provider pair.
func (s *AccountBindingStore) GetActiveByUserAndProvider(userID, providerID string) (*models.StoredAccountBinding, error) {
	var b models.StoredAccountBinding
	var configuredModelsRaw []byte
	err := s.db.QueryRow(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_ciphertext, api_key_configured, is_active,
		       cli_command, is_primary, created_at, updated_at
		FROM account_bindings
		WHERE user_id = $1 AND provider_id = $2 AND is_active = TRUE
		ORDER BY created_at ASC
		LIMIT 1
	`, userID, providerID).Scan(
		&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
		&configuredModelsRaw, &b.APIKeyCiphertext, &b.APIKeyConfigured, &b.IsActive,
		&b.CliCommand, &b.IsPrimary, &b.CreatedAt, &b.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(configuredModelsRaw) > 0 {
		if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
			return nil, fmt.Errorf("decode account binding configured models: %w", err)
		}
	}
	if b.ConfiguredModels == nil {
		b.ConfiguredModels = []string{}
	}
	return &b, nil
}

// validateConfiguredModels enforces the design §6.2 rule 4 cap: at most 16
// entries, each at most 64 chars after trim. Returns a wrapped
// ErrAccountBindingValidation that the handler maps to 400.
func validateConfiguredModels(values []string) error {
	if len(values) > models.MaxAccountBindingConfiguredModels {
		return fmt.Errorf("%w: configured_models has %d entries; max is %d",
			ErrAccountBindingValidation, len(values), models.MaxAccountBindingConfiguredModels)
	}
	for i, v := range values {
		trimmed := strings.TrimSpace(v)
		if len(trimmed) > models.MaxAccountBindingModelIDLength {
			return fmt.Errorf("%w: configured_models[%d] is %d chars; max is %d",
				ErrAccountBindingValidation, i, len(trimmed), models.MaxAccountBindingModelIDLength)
		}
	}
	return nil
}

// validateCLIShape enforces the design §6.2 rule 3 shape constraints for a
// `cli:*` provider: base_url empty after trim, api_key absent or empty
// after trim, model_id REQUIRED non-empty after trim. Each violation is a
// distinct error so the operator gets a precise message.
func validateCLIShape(req models.CreateAccountBindingRequest) error {
	if strings.TrimSpace(req.BaseURL) != "" {
		return fmt.Errorf("%w: base_url must be empty for cli:* providers", ErrAccountBindingValidation)
	}
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		return fmt.Errorf("%w: api_key must be empty for cli:* providers", ErrAccountBindingValidation)
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return fmt.Errorf("%w: model_id is required for cli:* providers", ErrAccountBindingValidation)
	}
	return nil
}

// validateCLICommand enforces the design §6.2 rule 5 sanity regex for a
// non-empty cli_command. Real hardening (realpath, interpreter blocklist,
// setuid rejection) lives connector-side per design §10.
func validateCLICommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return nil
	}
	if !cliCommandSanityRe.MatchString(trimmed) {
		return fmt.Errorf("%w: cli_command must be an absolute path matching ^/[A-Za-z0-9_./\\-]+$ (no shell metacharacters)", ErrAccountBindingValidation)
	}
	return nil
}

func (s *AccountBindingStore) Create(userID string, req models.CreateAccountBindingRequest) (*models.AccountBinding, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("%w: provider_id is required", ErrAccountBindingValidation)
	}
	if !models.AllowedAccountBindingProviderIDs[providerID] {
		return nil, fmt.Errorf("%w: unsupported provider_id %q (allowed: openai-compatible, cli:claude, cli:codex)",
			ErrAccountBindingValidation, providerID)
	}

	isCLI := models.IsCLIAccountBindingProvider(providerID)
	if isCLI {
		if err := validateCLIShape(req); err != nil {
			return nil, err
		}
	}
	if err := validateCLICommand(req.CliCommand); err != nil {
		return nil, err
	}
	if err := validateConfiguredModels(req.ConfiguredModels); err != nil {
		return nil, err
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "default"
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	modelID := strings.TrimSpace(req.ModelID)
	cliCommand := strings.TrimSpace(req.CliCommand)
	configuredModels := normalizeConfiguredModels(req.ConfiguredModels)
	if modelID == "" && len(configuredModels) > 0 {
		modelID = configuredModels[0]
	}
	if len(configuredModels) == 0 && modelID != "" {
		configuredModels = []string{modelID}
	}

	var apiKeyCiphertext string
	apiKeyConfigured := false
	if !isCLI && req.APIKey != nil {
		plaintext := strings.TrimSpace(*req.APIKey)
		if plaintext != "" {
			ciphertext, err := s.encryptAPIKey(plaintext)
			if err != nil {
				return nil, err
			}
			apiKeyCiphertext = ciphertext
			apiKeyConfigured = true
		}
	}

	configuredModelsJSON, err := json.Marshal(configuredModels)
	if err != nil {
		return nil, fmt.Errorf("encode configured models: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := deactivateOtherActiveBindings(tx, userID, providerID, ""); err != nil {
		return nil, err
	}

	// Determine the effective is_primary value:
	//   - explicit is_primary=true → demote others in the same namespace.
	//   - explicit is_primary=false → leave others alone, store FALSE.
	//   - is_primary omitted (nil) → auto-promote when this is the user's
	//     first active binding in the namespace (design §6.2 rule 7).
	isPrimary := false
	if req.IsPrimary != nil {
		isPrimary = *req.IsPrimary
	} else {
		count, countErr := countActiveByNamespace(tx, userID, primaryNamespaceForProvider(providerID))
		if countErr != nil {
			return nil, countErr
		}
		// The active-uniqueness deactivate above runs in the same TX, so any
		// existing active row in this namespace is now flipped off; the only
		// remaining active rows belong to a different `provider_id`. Auto-
		// promote when there are none.
		if count == 0 {
			isPrimary = true
		}
	}
	if isPrimary {
		if err := demoteOtherPrimaryBindings(tx, userID, providerID, ""); err != nil {
			return nil, err
		}
	}

	_, err = tx.Exec(`
		INSERT INTO account_bindings (id, user_id, provider_id, label, base_url, model_id, configured_models,
		                              api_key_ciphertext, api_key_configured, is_active,
		                              cli_command, is_primary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, $10, $11, $12, $12)
	`, id, userID, providerID, label, baseURL, modelID, configuredModelsJSON,
		apiKeyCiphertext, apiKeyConfigured, cliCommand, isPrimary, now)
	if err != nil {
		// Three unique indexes can collide here, all → 409 to the caller:
		//   - idx_account_bindings_active_unique  (migration 015)
		//   - idx_account_bindings_primary_unique (migration 021)
		//   - account_bindings_user_id_provider_id_label_key (migration 014)
		// Auto-demote in deactivateOtherActiveBindings + demoteOtherPrimaryBindings
		// makes the first two effectively unreachable from the user-facing path,
		// but they remain as defense in depth (e.g. concurrent creates).
		if database.IsUniqueViolation(err) {
			return nil, classifyAccountBindingUniqueViolation(err)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.AccountBinding{
		ID:               id,
		UserID:           userID,
		ProviderID:       providerID,
		Label:            label,
		BaseURL:          baseURL,
		ModelID:          modelID,
		ConfiguredModels: configuredModels,
		APIKeyConfigured: apiKeyConfigured,
		IsActive:         true,
		CliCommand:       cliCommand,
		IsPrimary:        isPrimary,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (s *AccountBindingStore) Update(id, userID string, req models.UpdateAccountBindingRequest) (*models.AccountBinding, error) {
	existing, err := s.GetByID(id, userID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("%w", ErrAccountBindingNotFound)
	}

	isCLI := models.IsCLIAccountBindingProvider(existing.ProviderID)

	if req.Label != nil {
		existing.Label = strings.TrimSpace(*req.Label)
	}
	if req.BaseURL != nil {
		trimmed := strings.TrimSpace(*req.BaseURL)
		if isCLI && trimmed != "" {
			return nil, fmt.Errorf("%w: base_url must be empty for cli:* providers", ErrAccountBindingValidation)
		}
		existing.BaseURL = trimmed
	}
	if req.ModelID != nil {
		trimmed := strings.TrimSpace(*req.ModelID)
		if isCLI && trimmed == "" {
			return nil, fmt.Errorf("%w: model_id is required for cli:* providers", ErrAccountBindingValidation)
		}
		existing.ModelID = trimmed
	}
	if req.ConfiguredModels != nil {
		if err := validateConfiguredModels(req.ConfiguredModels); err != nil {
			return nil, err
		}
		existing.ConfiguredModels = normalizeConfiguredModels(req.ConfiguredModels)
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.CliCommand != nil {
		trimmed := strings.TrimSpace(*req.CliCommand)
		if err := validateCLICommand(trimmed); err != nil {
			return nil, err
		}
		existing.CliCommand = trimmed
	}

	if req.ClearAPIKey {
		existing.APIKeyCiphertext = ""
		existing.APIKeyConfigured = false
	} else if req.APIKey != nil {
		plaintext := strings.TrimSpace(*req.APIKey)
		if isCLI && plaintext != "" {
			return nil, fmt.Errorf("%w: api_key must be empty for cli:* providers", ErrAccountBindingValidation)
		}
		if plaintext == "" {
			existing.APIKeyCiphertext = ""
			existing.APIKeyConfigured = false
		} else {
			ciphertext, err := s.encryptAPIKey(plaintext)
			if err != nil {
				return nil, err
			}
			existing.APIKeyCiphertext = ciphertext
			existing.APIKeyConfigured = true
		}
	}

	// Apply primary flag last so demotion runs against the post-update row
	// values (specifically the existing.ProviderID, which never changes here
	// but we still respect the same TX boundary as Create).
	wantPrimary := existing.IsPrimary
	if req.IsPrimary != nil {
		wantPrimary = *req.IsPrimary
	}

	configuredModelsJSON, err := json.Marshal(existing.ConfiguredModels)
	if err != nil {
		return nil, fmt.Errorf("encode configured models: %w", err)
	}

	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if existing.IsActive {
		if err := deactivateOtherActiveBindings(tx, userID, existing.ProviderID, existing.ID); err != nil {
			return nil, err
		}
	}
	if wantPrimary {
		if err := demoteOtherPrimaryBindings(tx, userID, existing.ProviderID, existing.ID); err != nil {
			return nil, err
		}
	}
	existing.IsPrimary = wantPrimary

	_, err = tx.Exec(`
		UPDATE account_bindings
		SET label = $1, base_url = $2, model_id = $3, configured_models = $4,
		    api_key_ciphertext = $5, api_key_configured = $6, is_active = $7,
		    cli_command = $8, is_primary = $9, updated_at = $10
		WHERE id = $11 AND user_id = $12
	`, existing.Label, existing.BaseURL, existing.ModelID, configuredModelsJSON,
		existing.APIKeyCiphertext, existing.APIKeyConfigured, existing.IsActive,
		existing.CliCommand, existing.IsPrimary, now, id, userID)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, classifyAccountBindingUniqueViolation(err)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.AccountBinding{
		ID:               existing.ID,
		UserID:           existing.UserID,
		ProviderID:       existing.ProviderID,
		Label:            existing.Label,
		BaseURL:          existing.BaseURL,
		ModelID:          existing.ModelID,
		ConfiguredModels: existing.ConfiguredModels,
		APIKeyConfigured: existing.APIKeyConfigured,
		IsActive:         existing.IsActive,
		CliCommand:       existing.CliCommand,
		IsPrimary:        existing.IsPrimary,
		CreatedAt:        existing.CreatedAt,
		UpdatedAt:        now,
	}, nil
}

func (s *AccountBindingStore) Delete(id, userID string) error {
	result, err := s.db.Exec(`DELETE FROM account_bindings WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w", ErrAccountBindingNotFound)
	}
	return nil
}

func (s *AccountBindingStore) DecryptAPIKey(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Decrypt(ciphertext)
}

func (s *AccountBindingStore) encryptAPIKey(plaintext string) (string, error) {
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Encrypt(plaintext)
}
