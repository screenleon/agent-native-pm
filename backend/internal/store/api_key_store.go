package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type APIKeyStore struct {
	db *sql.DB
}

func NewAPIKeyStore(db *sql.DB) *APIKeyStore {
	return &APIKeyStore{db: db}
}

// generateKey creates a random 32-byte hex key prefixed with "anpm_".
func generateKey() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	raw = "anpm_" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return
}

func (s *APIKeyStore) Create(req models.CreateAPIKeyRequest) (*models.APIKeyWithSecret, error) {
	raw, hash, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()

	_, err = s.db.Exec(`
		INSERT INTO api_keys (id, project_id, key_hash, label, is_active, created_at)
		VALUES ($1, $2, $3, $4, TRUE, $5)
	`, id, req.ProjectID, hash, req.Label, now)
	if err != nil {
		return nil, err
	}

	key, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	return &models.APIKeyWithSecret{APIKey: *key, Key: raw}, nil
}

func (s *APIKeyStore) getByID(id string) (*models.APIKey, error) {
	var k models.APIKey
	var projectID sql.NullString
	var lastUsed sql.NullTime
	var isActive bool
	err := s.db.QueryRow(`
		SELECT id, project_id, label, is_active, last_used_at, created_at
		FROM api_keys WHERE id=$1
	`, id).Scan(&k.ID, &projectID, &k.Label, &isActive, &lastUsed, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.IsActive = isActive
	if projectID.Valid {
		k.ProjectID = &projectID.String
	}
	if lastUsed.Valid {
		k.LastUsedAt = &lastUsed.Time
	}
	return &k, nil
}

func (s *APIKeyStore) List(projectID *string) ([]models.APIKey, error) {
	var rows *sql.Rows
	var err error
	if projectID != nil {
		rows, err = s.db.Query(`
			SELECT id, project_id, label, is_active, last_used_at, created_at
			FROM api_keys WHERE project_id=$1 ORDER BY created_at DESC
		`, *projectID)
	} else {
		rows, err = s.db.Query(`
			SELECT id, project_id, label, is_active, last_used_at, created_at
			FROM api_keys ORDER BY created_at DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		var pid sql.NullString
		var lu sql.NullTime
		var isActive bool
		if err := rows.Scan(&k.ID, &pid, &k.Label, &isActive, &lu, &k.CreatedAt); err != nil {
			return nil, err
		}
		k.IsActive = isActive
		if pid.Valid {
			k.ProjectID = &pid.String
		}
		if lu.Valid {
			k.LastUsedAt = &lu.Time
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []models.APIKey{}
	}
	return keys, rows.Err()
}

// Revoke marks a key as inactive.
func (s *APIKeyStore) Revoke(id string) error {
	_, err := s.db.Exec(`UPDATE api_keys SET is_active=FALSE WHERE id=$1`, id)
	return err
}

// Validate looks up a key by its SHA-256 hash, returns the key record if active.
func (s *APIKeyStore) Validate(raw string) (*models.APIKey, error) {
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	var k models.APIKey
	var projectID sql.NullString
	var lastUsed sql.NullTime
	var isActive bool
	err := s.db.QueryRow(`
		SELECT id, project_id, label, is_active, last_used_at, created_at
		FROM api_keys WHERE key_hash=$1
	`, hash).Scan(&k.ID, &projectID, &k.Label, &isActive, &lastUsed, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !isActive {
		return nil, nil
	}
	k.IsActive = true
	if projectID.Valid {
		k.ProjectID = &projectID.String
	}
	if lastUsed.Valid {
		k.LastUsedAt = &lastUsed.Time
	}

	// Update last_used_at
	now := time.Now().UTC()
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at=$1 WHERE id=$2`, now, k.ID)

	return &k, nil
}
