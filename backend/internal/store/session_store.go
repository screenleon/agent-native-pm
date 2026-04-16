package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type SessionStore struct {
	db        *sql.DB
	userStore *UserStore
}

func NewSessionStore(db *sql.DB, userStore *UserStore) *SessionStore {
	return &SessionStore{db: db, userStore: userStore}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create makes a new session lasting 24 hours.
func (s *SessionStore) Create(userID string) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	_, err = s.db.Exec(`
		INSERT INTO sessions (id, user_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4)
	`, token, userID, expires, now)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Validate returns the User associated with the session token, or nil if expired/invalid.
func (s *SessionStore) Validate(token string) (*models.User, error) {
	var userID string
	var expiresAt time.Time
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE id=$1`, token).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, nil
	}
	return s.userStore.GetByID(userID)
}

// Delete removes a session (logout).
func (s *SessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=$1`, token)
	return err
}

// PurgeExpired removes all expired sessions.
func (s *SessionStore) PurgeExpired() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < $1`, time.Now().UTC())
	return err
}
