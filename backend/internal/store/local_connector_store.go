package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type LocalConnectorStore struct {
	db      *sql.DB
	dialect database.Dialect
}

func NewLocalConnectorStore(db *sql.DB, dialect database.Dialect) *LocalConnectorStore {
	return &LocalConnectorStore{db: db, dialect: dialect}
}

func (s *LocalConnectorStore) ListByUser(userID string) ([]models.LocalConnector, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	connectors := make([]models.LocalConnector, 0)
	for rows.Next() {
		connector, err := scanLocalConnector(rows)
		if err != nil {
			return nil, err
		}
		connectors = append(connectors, *connector)
	}
	return connectors, rows.Err()
}

func (s *LocalConnectorStore) CreatePairingSession(userID string, req models.CreateLocalConnectorPairingSessionRequest) (*models.CreateLocalConnectorPairingSessionResponse, error) {
	code, err := generateReadableSecret(10)
	if err != nil {
		return nil, fmt.Errorf("generate pairing code: %w", err)
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "My Machine"
	}

	now := time.Now().UTC()
	session := models.ConnectorPairingSession{
		ID:        uuid.NewString(),
		UserID:    userID,
		Label:     label,
		Status:    models.ConnectorPairingStatusPending,
		ExpiresAt: now.Add(10 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.Exec(`
		INSERT INTO connector_pairing_sessions (id, user_id, pairing_code_hash, label, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`, session.ID, session.UserID, hashSecret(normalizePairingCode(code)), session.Label, session.Status, session.ExpiresAt, session.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &models.CreateLocalConnectorPairingSessionResponse{Session: session, PairingCode: code}, nil
}

func (s *LocalConnectorStore) ClaimPairingSession(req models.PairLocalConnectorRequest) (*models.PairLocalConnectorResponse, error) {
	code := normalizePairingCode(req.PairingCode)
	if code == "" {
		return nil, fmt.Errorf("pairing_code is required")
	}

	label := strings.TrimSpace(req.Label)
	platform := strings.TrimSpace(req.Platform)
	clientVersion := strings.TrimSpace(req.ClientVersion)
	capabilitiesJSON, err := json.Marshal(emptyCapabilitiesIfNil(req.Capabilities))
	if err != nil {
		return nil, fmt.Errorf("encode capabilities: %w", err)
	}

	now := time.Now().UTC()
	connectorToken, err := generateReadableSecret(24)
	if err != nil {
		return nil, fmt.Errorf("generate connector token: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var session models.ConnectorPairingSession
	claimQuery := `
		SELECT id, user_id, label, status, expires_at, COALESCE(connector_id, ''), created_at, updated_at
		FROM connector_pairing_sessions
		WHERE pairing_code_hash = $1 AND status = $2 AND expires_at > CURRENT_TIMESTAMP
		FOR UPDATE`
	if s.dialect.IsSQLite() {
		// SQLite serialises writes natively; FOR UPDATE is unsupported syntax.
		claimQuery = `
		SELECT id, user_id, label, status, expires_at, COALESCE(connector_id, ''), created_at, updated_at
		FROM connector_pairing_sessions
		WHERE pairing_code_hash = $1 AND status = $2 AND expires_at > CURRENT_TIMESTAMP`
	}
	err = tx.QueryRow(claimQuery, hashSecret(code), models.ConnectorPairingStatusPending).Scan(
		&session.ID, &session.UserID, &session.Label, &session.Status, &session.ExpiresAt,
		&session.ConnectorID, &session.CreatedAt, &session.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pairing session not found or expired")
	}
	if err != nil {
		return nil, err
	}

	if label == "" {
		label = session.Label
	}

	connectorID := uuid.NewString()
	// Path B S2: persist the connector's reported protocol_version so the
	// dispatcher can refuse to hand CLI-bound runs to a connector that
	// can't read the cli_binding response block (R3 mitigation, design
	// §6.2). Old clients that omit the field default to 0 server-side.
	protocolVersion := req.ProtocolVersion
	if protocolVersion < 0 {
		protocolVersion = 0
	}
	_, err = tx.Exec(`
		INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, '', $10, $10)
	`, connectorID, session.UserID, label, platform, clientVersion, models.LocalConnectorStatusPending, capabilitiesJSON, protocolVersion, hashSecret(connectorToken), now)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		UPDATE connector_pairing_sessions
		SET status = $1, connector_id = $2, updated_at = $3
		WHERE id = $4
	`, models.ConnectorPairingStatusClaimed, connectorID, now, session.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	connector, err := s.GetByID(connectorID, session.UserID)
	if err != nil {
		return nil, err
	}
	if connector == nil {
		return nil, fmt.Errorf("connector not found after pairing")
	}

	return &models.PairLocalConnectorResponse{
		Connector:      *connector,
		ConnectorToken: connectorToken,
	}, nil
}

func (s *LocalConnectorStore) HeartbeatByToken(token string, req models.LocalConnectorHeartbeatRequest) (*models.LocalConnector, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("connector token is required")
	}

	tx, err := s.beginWriteTx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Lock the connector row inside the transaction so concurrent heartbeats
	// and out-of-band metadata writers (probe enqueue / binding delete scrub)
	// serialise instead of racing on a read-modify-write of metadata.
	connector, err := txSelectConnectorByTokenForUpdate(tx, s.dialect, token)
	if err != nil {
		return nil, err
	}
	if connector == nil {
		return nil, fmt.Errorf("connector not found")
	}
	if connector.Status == models.LocalConnectorStatusRevoked {
		return nil, fmt.Errorf("connector has been revoked")
	}

	capabilities := connector.Capabilities
	if req.Capabilities != nil {
		capabilities = emptyCapabilitiesIfNil(req.Capabilities)
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("encode capabilities: %w", err)
	}

	metadata := cloneMetadata(connector.Metadata)
	// cli_last_healthy_at is overwritten on every heartbeat that carries it.
	if req.LastCliHealthyAt != nil {
		metadata["cli_last_healthy_at"] = req.LastCliHealthyAt.UTC().Format(time.RFC3339)
	}
	// S1 guard (critic): only accept probe results whose probe_id actually
	// appears in pending_cli_probe_requests. Silently discard anything else
	// so a buggy/hostile connector cannot write arbitrary keys into metadata.
	if len(req.CliProbeResults) > 0 {
		foldProbeResultsInto(metadata, req.CliProbeResults)
	}
	gcOldProbeResults(metadata)
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	now := time.Now().UTC()
	lastError := strings.TrimSpace(req.LastError)

	if _, err := tx.Exec(`
		UPDATE local_connectors
		SET status = $1, capabilities = $2, metadata = $3, last_seen_at = $4, last_error = $5, updated_at = $4
		WHERE id = $6
	`, models.LocalConnectorStatusOnline, capabilitiesJSON, metadataJSON, now, lastError, connector.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetByID(connector.ID, connector.UserID)
}

// foldProbeResultsInto merges reported probe outcomes into the metadata map:
//   - Each result is written to metadata.cli_probe_results[probe_id] ONLY if
//     a matching pending entry exists (guards against arbitrary key injection).
//   - The matching pending entry is removed.
//   - Empty probe_id values are discarded.
//   - Missing completed_at defaults to now().
func foldProbeResultsInto(metadata map[string]interface{}, incoming []models.CliProbeResult) {
	if len(incoming) == 0 {
		return
	}
	pending := readPendingProbes(metadata)
	pendingIndex := make(map[string]int, len(pending))
	for i, p := range pending {
		pendingIndex[p.ProbeID] = i
	}
	results := readProbeResults(metadata)
	accepted := map[string]bool{}
	for _, r := range incoming {
		probeID := strings.TrimSpace(r.ProbeID)
		if probeID == "" {
			continue
		}
		idx, ok := pendingIndex[probeID]
		if !ok {
			continue // unsolicited result — ignore
		}
		if r.CompletedAt.IsZero() {
			r.CompletedAt = time.Now().UTC()
		}
		// Preserve binding_id from the pending entry so the client can cross-
		// reference the result even if the connector did not echo it back.
		if strings.TrimSpace(r.BindingID) == "" {
			r.BindingID = pending[idx].BindingID
		}
		results[probeID] = r
		accepted[probeID] = true
	}
	if len(accepted) == 0 {
		return
	}
	kept := pending[:0]
	for _, p := range pending {
		if !accepted[p.ProbeID] {
			kept = append(kept, p)
		}
	}
	writePendingProbes(metadata, kept)
	writeProbeResults(metadata, results)
}

// txSelectConnectorByTokenForUpdate returns the connector row locked for the
// duration of the enclosing transaction. Shared by HeartbeatByToken (and is
// intentionally NOT used by read-only GetByToken to keep that call cheap).
func txSelectConnectorByTokenForUpdate(tx *sql.Tx, dialect database.Dialect, token string) (*models.LocalConnector, error) {
	query := `
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE token_hash = $1
		FOR UPDATE`
	if dialect.IsSQLite() {
		query = `
			SELECT id, user_id, label, platform, client_version, status, capabilities,
			       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
			FROM local_connectors
			WHERE token_hash = $1`
	}
	row := tx.QueryRow(query, hashSecret(token))
	return scanOneLocalConnector(row)
}

// txSelectConnectorByIDForUpdate returns the connector row locked by id + user.
func txSelectConnectorByIDForUpdate(tx *sql.Tx, dialect database.Dialect, connectorID, userID string) (*models.LocalConnector, error) {
	query := `
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`
	if dialect.IsSQLite() {
		query = `
			SELECT id, user_id, label, platform, client_version, status, capabilities,
			       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
			FROM local_connectors
			WHERE id = $1 AND user_id = $2`
	}
	row := tx.QueryRow(query, connectorID, userID)
	return scanOneLocalConnector(row)
}

func (s *LocalConnectorStore) Revoke(id, userID string) error {
	result, err := s.db.Exec(`
		UPDATE local_connectors
		SET status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND user_id = $3 AND status <> $1
	`, models.LocalConnectorStatusRevoked, id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("local connector not found")
	}
	return nil
}

func (s *LocalConnectorStore) GetByID(id, userID string) (*models.LocalConnector, error) {
	row := s.db.QueryRow(`
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	return scanOneLocalConnector(row)
}

func (s *LocalConnectorStore) GetByToken(token string) (*models.LocalConnector, error) {
	row := s.db.QueryRow(`
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE token_hash = $1
	`, hashSecret(token))
	return scanOneLocalConnector(row)
}

// LocalConnectorLivenessWindow is how recently a connector must have polled
// the server for it to be considered "usable" for dispatching a planning run.
// Must exceed the connector's typical poll interval (currently ~10s on the
// reference local connector) and stay shorter than the planning lease window
// (2 minutes) to avoid handing work to a connector that has stopped polling.
const LocalConnectorLivenessWindow = 90 * time.Second

func (s *LocalConnectorStore) GetFirstUsableByUser(userID string) (*models.LocalConnector, error) {
	seconds := fmt.Sprintf("%d", int(LocalConnectorLivenessWindow.Seconds()))
	var query string
	if s.dialect.IsSQLite() {
		// SQLite date arithmetic: subtract seconds using datetime modifier.
		query = `
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE user_id = $1
		  AND status = $2
		  AND last_seen_at IS NOT NULL
		  AND last_seen_at >= datetime('now', '-' || $3 || ' seconds')
		ORDER BY last_seen_at DESC, created_at DESC
		LIMIT 1`
	} else {
		query = `
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE user_id = $1
		  AND status = $2
		  AND last_seen_at IS NOT NULL
		  AND last_seen_at >= NOW() - ($3 || ' seconds')::interval
		ORDER BY last_seen_at DESC NULLS LAST, created_at DESC
		LIMIT 1`
	}
	row := s.db.QueryRow(query, userID, models.LocalConnectorStatusOnline, seconds)
	return scanOneLocalConnector(row)
}

type localConnectorScanner interface {
	Scan(dest ...interface{}) error
}

func scanOneLocalConnector(row localConnectorScanner) (*models.LocalConnector, error) {
	connector, err := scanLocalConnector(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return connector, nil
}

func scanLocalConnector(scanner localConnectorScanner) (*models.LocalConnector, error) {
	var connector models.LocalConnector
	var capabilitiesRaw []byte
	var metadataRaw []byte
	var lastSeenAt sql.NullTime
	err := scanner.Scan(
		&connector.ID,
		&connector.UserID,
		&connector.Label,
		&connector.Platform,
		&connector.ClientVersion,
		&connector.Status,
		&capabilitiesRaw,
		&connector.ProtocolVersion,
		&metadataRaw,
		&lastSeenAt,
		&connector.LastError,
		&connector.CreatedAt,
		&connector.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	connector.Capabilities = map[string]interface{}{}
	if len(capabilitiesRaw) > 0 {
		if err := json.Unmarshal(capabilitiesRaw, &connector.Capabilities); err != nil {
			return nil, fmt.Errorf("decode local connector capabilities: %w", err)
		}
	}
	connector.Metadata = map[string]interface{}{}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &connector.Metadata); err != nil {
			return nil, fmt.Errorf("decode local connector metadata: %w", err)
		}
	}
	if lastSeenAt.Valid {
		connector.LastSeenAt = &lastSeenAt.Time
	}
	return &connector, nil
}

func emptyCapabilitiesIfNil(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	return input
}

func normalizePairingCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func generateReadableSecret(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	encoded := strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), "=")
	encoded = strings.ToUpper(encoded)
	if len(encoded) > 8 {
		return encoded[:len(encoded)/2] + "-" + encoded[len(encoded)/2:], nil
	}
	return encoded, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// P4-4: CLI-binding probe-on-connector persistence
//
// The probe lifecycle lives entirely inside LocalConnector.Metadata:
//   metadata.pending_cli_probe_requests : []PendingCliProbeRequest
//   metadata.cli_probe_results          : map[probe_id] CliProbeResult
//
// No dedicated table. The metadata JSONB is already round-tripped through
// every Heartbeat, so the connector side reads pending requests from
// `connector.metadata` and writes results back via `cli_probe_results` in
// the next heartbeat.
// ─────────────────────────────────────────────────────────────────────────────

const (
	metadataKeyPendingCliProbes = "pending_cli_probe_requests"
	metadataKeyCliProbeResults  = "cli_probe_results"
	metadataKeyCliConfigs       = "cli_configs" // Phase 6a UX-B1
	// probeResultRetention caps how long a completed probe result survives
	// in metadata. Anything older is dropped at enqueue/heartbeat time.
	probeResultRetention = 24 * time.Hour
	// maxPendingProbesPerConnector is a defensive cap on the pending list
	// length (S-3). Dedup by binding_id already bounds the list to O(#CLI
	// bindings), but this cap protects against runaway growth if dedup ever
	// regresses or if a user has an unreasonable number of bindings. A 64-
	// entry list is ~20 KB worst case, well under the JSONB row-size ceiling.
	maxPendingProbesPerConnector = 64
)

// ErrPendingProbeCapReached is returned by EnqueueCliProbe when the connector
// already has maxPendingProbesPerConnector pending probes queued. The handler
// surface maps this to HTTP 429 so the UI can back off.
var ErrPendingProbeCapReached = errors.New("connector pending-probe queue is full")

// Phase 6a UX-B1 error sentinels for cli_configs CRUD.
var (
	ErrCliConfigNotFound            = errors.New("cli_config not found")
	ErrCliConfigDuplicateLabel      = errors.New("cli_config with the same provider_id and label already exists on this connector")
	ErrCliConfigCapReached          = errors.New("connector already has the maximum number of cli_configs")
	ErrCliConfigInvalidProvider     = errors.New("provider_id must be one of the allowed cli:* providers")
	ErrCliConfigInvalidCliCommand   = errors.New("cli_command must be empty or an absolute path with safe characters")
	ErrCliConfigModelIDRequired     = errors.New("model_id is required")
)

// EnqueueCliProbe appends a pending probe request to the named connector's
// metadata. Dedup policy (S7): if a pending entry for the same binding
// exists AND is still in-flight (no matching completed result), return the
// existing probe_id; otherwise allocate a fresh probe_id so the UI does not
// silently show a stale result on a subsequent click. Transactional + row-
// locked to prevent concurrent enqueue/heartbeat races (M1).
func (s *LocalConnectorStore) EnqueueCliProbe(connectorID, userID string, req models.PendingCliProbeRequest) (string, error) {
	tx, err := s.beginWriteTx()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	connector, err := txSelectConnectorByIDForUpdate(tx, s.dialect, connectorID, userID)
	if err != nil {
		return "", err
	}
	if connector == nil {
		return "", sql.ErrNoRows
	}

	metadata := cloneMetadata(connector.Metadata)
	pending := readPendingProbes(metadata)
	results := readProbeResults(metadata)

	for _, existing := range pending {
		if existing.BindingID == req.BindingID {
			// In-flight probe (no completed result for this probe_id). Return it.
			if _, done := results[existing.ProbeID]; !done {
				if err := tx.Commit(); err != nil {
					return "", err
				}
				return existing.ProbeID, nil
			}
			// Stale pending entry (completed but never cleaned) — drop before append.
			break
		}
	}

	// Remove any stale pending entry referencing this binding (bounded list).
	kept := pending[:0]
	for _, p := range pending {
		if p.BindingID != req.BindingID {
			kept = append(kept, p)
		}
	}
	pending = kept

	// S-3 cap: reject if the pending list is already at the hard cap after
	// removing stale same-binding entries. Dedup means this only trips when
	// the user has > 64 distinct bindings all in-flight simultaneously —
	// extremely unusual, and the 429 gives the UI a clear signal.
	if len(pending) >= maxPendingProbesPerConnector {
		return "", ErrPendingProbeCapReached
	}

	if strings.TrimSpace(req.ProbeID) == "" {
		req.ProbeID = uuid.NewString()
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}
	// Evict any cached completed result for this binding so a subsequent poll
	// on the fresh probe_id does not accidentally surface a prior outcome.
	for probeID, r := range results {
		if r.BindingID == req.BindingID {
			delete(results, probeID)
		}
	}
	writeProbeResults(metadata, results)

	pending = append(pending, req)
	writePendingProbes(metadata, pending)
	gcOldProbeResults(metadata)

	if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return req.ProbeID, nil
}

// GetCliProbeResult returns the stored probe outcome or nil if still pending.
// The returned status is one of: "pending", "completed", "not_found".
func (s *LocalConnectorStore) GetCliProbeResult(connectorID, userID, probeID string) (string, *models.CliProbeResult, error) {
	connector, err := s.GetByID(connectorID, userID)
	if err != nil {
		return "", nil, err
	}
	if connector == nil {
		return "not_found", nil, nil
	}
	results := readProbeResults(connector.Metadata)
	if r, ok := results[probeID]; ok {
		return "completed", &r, nil
	}
	for _, p := range readPendingProbes(connector.Metadata) {
		if p.ProbeID == probeID {
			return "pending", nil, nil
		}
	}
	return "not_found", nil, nil
}

// ScrubCliProbesForBinding removes any pending + completed probe entries for
// the given binding across all connectors owned by the user. All connector
// rows are locked inside a single transaction (S-2 fix) so N sequential
// round-trips collapse to one and a concurrent heartbeat on any connector
// cannot resurrect a scrubbed entry mid-loop. A connector added AFTER the
// SELECT lock is acquired is not scrubbed in this pass — that small window
// is closed by the 24h retention sweep (`probeResultRetention`).
func (s *LocalConnectorStore) ScrubCliProbesForBinding(userID, bindingID string) error {
	if strings.TrimSpace(bindingID) == "" {
		return nil
	}
	tx, err := s.beginWriteTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	connectors, err := txListConnectorsByUserForUpdate(tx, s.dialect, userID)
	if err != nil {
		return err
	}
	for i := range connectors {
		connector := &connectors[i]
		metadata := cloneMetadata(connector.Metadata)
		changed := false
		pending := readPendingProbes(metadata)
		kept := pending[:0]
		for _, p := range pending {
			if p.BindingID == bindingID {
				changed = true
				continue
			}
			kept = append(kept, p)
		}
		if changed {
			writePendingProbes(metadata, kept)
		}
		results := readProbeResults(metadata)
		for probeID, r := range results {
			if r.BindingID == bindingID {
				delete(results, probeID)
				changed = true
			}
		}
		if !changed {
			continue
		}
		writeProbeResults(metadata, results)
		if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// txListConnectorsByUserForUpdate returns all of the user's connector rows
// locked for the enclosing transaction. Used by ScrubCliProbesForBinding so
// all scrubs commit atomically.
func txListConnectorsByUserForUpdate(tx *sql.Tx, dialect database.Dialect, userID string) ([]models.LocalConnector, error) {
	query := `
		SELECT id, user_id, label, platform, client_version, status, capabilities,
		       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
		FROM local_connectors
		WHERE user_id = $1
		ORDER BY created_at DESC
		FOR UPDATE`
	if dialect.IsSQLite() {
		query = `
			SELECT id, user_id, label, platform, client_version, status, capabilities,
			       protocol_version, metadata, last_seen_at, last_error, created_at, updated_at
			FROM local_connectors
			WHERE user_id = $1
			ORDER BY created_at DESC`
	}
	rows, err := tx.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.LocalConnector, 0)
	for rows.Next() {
		c, err := scanLocalConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// beginWriteTx starts a transaction that takes the writer-side lock eagerly.
// On Postgres, `db.Begin()` is sufficient because row-level locks are acquired
// by `SELECT ... FOR UPDATE`. On SQLite, the default `BEGIN` is `DEFERRED` —
// the write lock is not acquired until the first UPDATE, so two goroutines
// can both SELECT the same metadata, both compute new JSON, and the second
// to commit overwrites the first with stale pre-read data (classic
// read-modify-write race). `BEGIN IMMEDIATE` acquires the reserved lock at
// BEGIN time, so the second writer blocks immediately and re-reads fresh
// metadata once the first commits. The default retry behaviour of the Go
// SQLite driver handles SQLITE_BUSY as a transparent retry.
func (s *LocalConnectorStore) beginWriteTx() (*sql.Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if s.dialect.IsSQLite() {
		if _, err := tx.Exec("ROLLBACK; BEGIN IMMEDIATE"); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}

func txWriteConnectorMetadata(tx *sql.Tx, connectorID string, metadata map[string]interface{}) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}
	_, err = tx.Exec(`UPDATE local_connectors SET metadata = $1, updated_at = $2 WHERE id = $3`,
		metadataJSON, time.Now().UTC(), connectorID)
	return err
}

// ─── metadata read/write helpers ────────────────────────────────────────────

func cloneMetadata(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input)+2)
	for k, v := range input {
		out[k] = v
	}
	return out
}

func readPendingProbes(metadata map[string]interface{}) []models.PendingCliProbeRequest {
	raw, ok := metadata[metadataKeyPendingCliProbes]
	if !ok {
		return nil
	}
	// JSON round-trip preserves the original encoding regardless of what the
	// DB driver returned (map[string]any / []any / json.RawMessage).
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []models.PendingCliProbeRequest
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil
	}
	return out
}

func writePendingProbes(metadata map[string]interface{}, list []models.PendingCliProbeRequest) {
	if len(list) == 0 {
		delete(metadata, metadataKeyPendingCliProbes)
		return
	}
	metadata[metadataKeyPendingCliProbes] = list
}

func readProbeResults(metadata map[string]interface{}) map[string]models.CliProbeResult {
	raw, ok := metadata[metadataKeyCliProbeResults]
	if !ok {
		return map[string]models.CliProbeResult{}
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return map[string]models.CliProbeResult{}
	}
	out := map[string]models.CliProbeResult{}
	if err := json.Unmarshal(bytes, &out); err != nil {
		return map[string]models.CliProbeResult{}
	}
	return out
}

func writeProbeResults(metadata map[string]interface{}, results map[string]models.CliProbeResult) {
	if len(results) == 0 {
		delete(metadata, metadataKeyCliProbeResults)
		return
	}
	metadata[metadataKeyCliProbeResults] = results
}

func gcOldProbeResults(metadata map[string]interface{}) map[string]interface{} {
	results := readProbeResults(metadata)
	cutoff := time.Now().Add(-probeResultRetention)
	changed := false
	for id, r := range results {
		if r.CompletedAt.Before(cutoff) {
			delete(results, id)
			changed = true
		}
	}
	if changed {
		writeProbeResults(metadata, results)
	}
	return metadata
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase 6a UX-B1: cli_configs[] in connector metadata
//
// Per-connector CLI + model combinations. Replaces the user-level cli:*
// account_bindings as the primary way to express "this machine runs this
// CLI with this model". No migration — existing cli:* rows stay as legacy.
// ─────────────────────────────────────────────────────────────────────────────

// cliCommandRegex is the same sanity check as account_binding_store's
// validateCLICommand — absolute path or empty, restricted character set.
var cliCommandRegex = regexp.MustCompile(`^/[A-Za-z0-9_./\-]+$`)

// ListCliConfigs returns the connector's CLI configs (empty slice if unset).
// Requires user-scope match — cross-user reads return ErrCliConfigNotFound.
func (s *LocalConnectorStore) ListCliConfigs(connectorID, userID string) ([]models.CliConfig, error) {
	connector, err := s.GetByID(connectorID, userID)
	if err != nil {
		return nil, err
	}
	if connector == nil {
		return nil, ErrCliConfigNotFound
	}
	return readCliConfigs(connector.Metadata), nil
}

// AddCliConfig appends a new config to the connector's metadata. If this is
// the first config OR the caller sets IsPrimary=true, the new entry becomes
// primary and any previous primary is demoted — exactly one primary per
// connector, atomically.
func (s *LocalConnectorStore) AddCliConfig(connectorID, userID string, req models.CreateCliConfigRequest) (*models.CliConfig, error) {
	if err := validateCliConfigShape(req.ProviderID, req.ModelID, req.CliCommand); err != nil {
		return nil, err
	}
	tx, err := s.beginWriteTx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	connector, err := txSelectConnectorByIDForUpdate(tx, s.dialect, connectorID, userID)
	if err != nil {
		return nil, err
	}
	if connector == nil {
		return nil, ErrCliConfigNotFound
	}

	metadata := cloneMetadata(connector.Metadata)
	configs := readCliConfigs(metadata)

	if len(configs) >= models.MaxCliConfigsPerConnector {
		return nil, ErrCliConfigCapReached
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = defaultCliConfigLabel(req.ProviderID)
	}
	for _, existing := range configs {
		if existing.ProviderID == req.ProviderID && existing.Label == label {
			return nil, ErrCliConfigDuplicateLabel
		}
	}

	// Primary rule: first config auto-becomes primary; explicit
	// IsPrimary=true also wins and demotes all others. A non-primary
	// addition when some other is already primary keeps its false value.
	wantsPrimary := len(configs) == 0
	if req.IsPrimary != nil && *req.IsPrimary {
		wantsPrimary = true
	}
	if wantsPrimary {
		for i := range configs {
			configs[i].IsPrimary = false
		}
	}

	now := time.Now().UTC()
	created := models.CliConfig{
		ID:         uuid.NewString(),
		ProviderID: req.ProviderID,
		CliCommand: strings.TrimSpace(req.CliCommand),
		ModelID:    strings.TrimSpace(req.ModelID),
		Label:      label,
		IsPrimary:  wantsPrimary,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	configs = append(configs, created)
	writeCliConfigs(metadata, configs)

	if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateCliConfig patches specific fields on an existing config.
// Setting IsPrimary=true demotes other configs on the same connector
// atomically; IsPrimary=false on the currently-primary config is
// rejected (caller must promote another config instead, mirroring the
// account_bindings invariant that at least one stays primary after the
// first addition).
func (s *LocalConnectorStore) UpdateCliConfig(connectorID, userID, configID string, req models.UpdateCliConfigRequest) (*models.CliConfig, error) {
	tx, err := s.beginWriteTx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	connector, err := txSelectConnectorByIDForUpdate(tx, s.dialect, connectorID, userID)
	if err != nil {
		return nil, err
	}
	if connector == nil {
		return nil, ErrCliConfigNotFound
	}

	metadata := cloneMetadata(connector.Metadata)
	configs := readCliConfigs(metadata)
	idx := -1
	for i, c := range configs {
		if c.ID == configID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrCliConfigNotFound
	}

	if req.CliCommand != nil {
		if err := validateCliCommand(*req.CliCommand); err != nil {
			return nil, err
		}
		configs[idx].CliCommand = strings.TrimSpace(*req.CliCommand)
	}
	if req.ModelID != nil {
		m := strings.TrimSpace(*req.ModelID)
		if m == "" {
			return nil, ErrCliConfigModelIDRequired
		}
		configs[idx].ModelID = m
	}
	if req.Label != nil {
		nextLabel := strings.TrimSpace(*req.Label)
		if nextLabel == "" {
			nextLabel = defaultCliConfigLabel(configs[idx].ProviderID)
		}
		// Duplicate-label guard with self-exclusion.
		for i, existing := range configs {
			if i == idx {
				continue
			}
			if existing.ProviderID == configs[idx].ProviderID && existing.Label == nextLabel {
				return nil, ErrCliConfigDuplicateLabel
			}
		}
		configs[idx].Label = nextLabel
	}
	if req.IsPrimary != nil {
		if *req.IsPrimary {
			for i := range configs {
				configs[i].IsPrimary = (i == idx)
			}
		}
		// IsPrimary=false on the current primary is a no-op — the
		// invariant is "exactly one primary if any exist", not "you can
		// demote the primary without promoting a replacement". Callers
		// should SetPrimary on a different config instead.
	}
	configs[idx].UpdatedAt = time.Now().UTC()
	writeCliConfigs(metadata, configs)

	if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated := configs[idx]
	return &updated, nil
}

// DeleteCliConfig removes a config. If the deleted config was primary AND
// other configs remain, the first remaining config is auto-promoted to
// primary (so "at least one primary when non-empty" invariant holds).
func (s *LocalConnectorStore) DeleteCliConfig(connectorID, userID, configID string) error {
	tx, err := s.beginWriteTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	connector, err := txSelectConnectorByIDForUpdate(tx, s.dialect, connectorID, userID)
	if err != nil {
		return err
	}
	if connector == nil {
		return ErrCliConfigNotFound
	}

	metadata := cloneMetadata(connector.Metadata)
	configs := readCliConfigs(metadata)
	idx := -1
	for i, c := range configs {
		if c.ID == configID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrCliConfigNotFound
	}

	wasPrimary := configs[idx].IsPrimary
	configs = append(configs[:idx], configs[idx+1:]...)
	if wasPrimary && len(configs) > 0 {
		configs[0].IsPrimary = true
	}
	writeCliConfigs(metadata, configs)

	if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPrimaryCliConfig promotes the named config to primary and demotes
// every other config on the same connector.
func (s *LocalConnectorStore) SetPrimaryCliConfig(connectorID, userID, configID string) error {
	tx, err := s.beginWriteTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	connector, err := txSelectConnectorByIDForUpdate(tx, s.dialect, connectorID, userID)
	if err != nil {
		return err
	}
	if connector == nil {
		return ErrCliConfigNotFound
	}
	metadata := cloneMetadata(connector.Metadata)
	configs := readCliConfigs(metadata)
	found := false
	for i, c := range configs {
		if c.ID == configID {
			configs[i].IsPrimary = true
			configs[i].UpdatedAt = time.Now().UTC()
			found = true
		} else {
			configs[i].IsPrimary = false
		}
	}
	if !found {
		return ErrCliConfigNotFound
	}
	writeCliConfigs(metadata, configs)
	if err := txWriteConnectorMetadata(tx, connector.ID, metadata); err != nil {
		return err
	}
	return tx.Commit()
}

// GetCliConfig returns one config by id, user-scoped.
func (s *LocalConnectorStore) GetCliConfig(connectorID, userID, configID string) (*models.CliConfig, error) {
	configs, err := s.ListCliConfigs(connectorID, userID)
	if err != nil {
		return nil, err
	}
	for i := range configs {
		if configs[i].ID == configID {
			return &configs[i], nil
		}
	}
	return nil, ErrCliConfigNotFound
}

// ── cli_configs metadata helpers ────────────────────────────────────────────

func readCliConfigs(metadata map[string]interface{}) []models.CliConfig {
	raw, ok := metadata[metadataKeyCliConfigs]
	if !ok {
		return nil
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []models.CliConfig
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil
	}
	return out
}

func writeCliConfigs(metadata map[string]interface{}, configs []models.CliConfig) {
	if len(configs) == 0 {
		delete(metadata, metadataKeyCliConfigs)
		return
	}
	metadata[metadataKeyCliConfigs] = configs
}

func validateCliConfigShape(providerID, modelID, cliCommand string) error {
	if !models.AllowedAccountBindingProviderIDs[providerID] || !models.IsCLIAccountBindingProvider(providerID) {
		return ErrCliConfigInvalidProvider
	}
	if strings.TrimSpace(modelID) == "" {
		return ErrCliConfigModelIDRequired
	}
	return validateCliCommand(cliCommand)
}

func validateCliCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return nil
	}
	if !cliCommandRegex.MatchString(trimmed) {
		return ErrCliConfigInvalidCliCommand
	}
	return nil
}

func defaultCliConfigLabel(providerID string) string {
	switch providerID {
	case "cli:claude":
		return "My Claude"
	case "cli:codex":
		return "My Codex"
	default:
		return "My CLI"
	}
}

