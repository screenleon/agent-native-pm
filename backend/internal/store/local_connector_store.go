package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	connector, err := s.GetByToken(token)
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

	// Merge cli_health entries into existing metadata (D3: non-destructive merge).
	metadata := connector.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	if len(req.CliHealth) > 0 {
		existing, _ := metadata["cli_health"].(map[string]interface{})
		if existing == nil {
			existing = map[string]interface{}{}
		}
		for _, entry := range req.CliHealth {
			if strings.TrimSpace(entry.BindingID) == "" {
				continue
			}
			existing[entry.BindingID] = map[string]interface{}{
				"status":              entry.Status,
				"version_string":      entry.VersionString,
				"checked_at":          entry.CheckedAt.UTC().Format(time.RFC3339),
				"probe_error_message": entry.ProbeErrorMessage,
			}
		}
		metadata["cli_health"] = existing
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	now := time.Now().UTC()
	lastError := strings.TrimSpace(req.LastError)

	_, err = s.db.Exec(`
		UPDATE local_connectors
		SET status = $1, capabilities = $2, metadata = $3, last_seen_at = $4, last_error = $5, updated_at = $4
		WHERE id = $6
	`, models.LocalConnectorStatusOnline, capabilitiesJSON, metadataJSON, now, lastError, connector.ID)
	if err != nil {
		return nil, err
	}

	return s.GetByID(connector.ID, connector.UserID)
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

// ScrubCliHealthKey removes the given bindingID from metadata.cli_health in
// all connectors belonging to userID. Called when an account binding is
// deleted to prevent stale health data from re-aliasing a future binding
// that reuses the same ID (R12 / D3).
func (s *LocalConnectorStore) ScrubCliHealthKey(userID, bindingID string) error {
	rows, err := s.db.Query(`
		SELECT id, metadata FROM local_connectors WHERE user_id = $1
	`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id       string
		metadata []byte
	}
	var connectors []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.metadata); err != nil {
			return err
		}
		connectors = append(connectors, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range connectors {
		meta := map[string]interface{}{}
		if len(c.metadata) > 0 {
			if err := json.Unmarshal(c.metadata, &meta); err != nil {
				continue
			}
		}
		health, ok := meta["cli_health"].(map[string]interface{})
		if !ok || health == nil {
			continue
		}
		if _, found := health[bindingID]; !found {
			continue
		}
		delete(health, bindingID)
		meta["cli_health"] = health
		updated, err := json.Marshal(meta)
		if err != nil {
			continue
		}
		_, _ = s.db.Exec(`UPDATE local_connectors SET metadata = $1, updated_at = $2 WHERE id = $3`,
			updated, time.Now().UTC(), c.id)
	}
	return nil
}
