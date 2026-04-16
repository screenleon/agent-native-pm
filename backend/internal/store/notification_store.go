package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type NotificationStore struct {
	db *sql.DB
}

func NewNotificationStore(db *sql.DB) *NotificationStore {
	return &NotificationStore{db: db}
}

func (s *NotificationStore) Create(req models.CreateNotificationRequest) (*models.Notification, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO notifications (id, user_id, project_id, kind, title, body, is_read, link, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE, $7, $8)
	`, id, req.UserID, req.ProjectID, req.Kind, req.Title, req.Body, req.Link, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *NotificationStore) GetByID(id string) (*models.Notification, error) {
	var n models.Notification
	var pid sql.NullString
	var isRead bool
	err := s.db.QueryRow(`
		SELECT id, user_id, project_id, kind, title, body, is_read, link, created_at
		FROM notifications WHERE id=$1
	`, id).Scan(&n.ID, &n.UserID, &pid, &n.Kind, &n.Title, &n.Body, &isRead, &n.Link, &n.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.IsRead = isRead
	if pid.Valid {
		n.ProjectID = &pid.String
	}
	return &n, nil
}

func (s *NotificationStore) ListByUser(userID string, unreadOnly bool, page, perPage int) ([]models.Notification, int, error) {
	var countQ string
	var countArgs []interface{}
	if unreadOnly {
		countQ = `SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`
		countArgs = []interface{}{userID}
	} else {
		countQ = `SELECT COUNT(*) FROM notifications WHERE user_id=$1`
		countArgs = []interface{}{userID}
	}
	var total int
	if err := s.db.QueryRow(countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	var rows *sql.Rows
	var err error
	if unreadOnly {
		rows, err = s.db.Query(`
			SELECT id, user_id, project_id, kind, title, body, is_read, link, created_at
			FROM notifications WHERE user_id=$1 AND is_read=FALSE
			ORDER BY created_at DESC LIMIT $2 OFFSET $3
		`, userID, perPage, offset)
	} else {
		rows, err = s.db.Query(`
			SELECT id, user_id, project_id, kind, title, body, is_read, link, created_at
			FROM notifications WHERE user_id=$1
			ORDER BY created_at DESC LIMIT $2 OFFSET $3
		`, userID, perPage, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var notes []models.Notification
	for rows.Next() {
		var n models.Notification
		var pid sql.NullString
		var isRead bool
		if err := rows.Scan(&n.ID, &n.UserID, &pid, &n.Kind, &n.Title, &n.Body, &isRead, &n.Link, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		n.IsRead = isRead
		if pid.Valid {
			n.ProjectID = &pid.String
		}
		notes = append(notes, n)
	}
	if notes == nil {
		notes = []models.Notification{}
	}
	return notes, total, rows.Err()
}

func (s *NotificationStore) MarkRead(id string) error {
	_, err := s.db.Exec(`UPDATE notifications SET is_read=TRUE WHERE id=$1`, id)
	return err
}

func (s *NotificationStore) MarkUnread(id string) error {
	_, err := s.db.Exec(`UPDATE notifications SET is_read=FALSE WHERE id=$1`, id)
	return err
}

func (s *NotificationStore) MarkAllRead(userID string) error {
	_, err := s.db.Exec(`UPDATE notifications SET is_read=TRUE WHERE user_id=$1 AND is_read=FALSE`, userID)
	return err
}

func (s *NotificationStore) CountUnread(userID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`, userID).Scan(&count)
	return count, err
}
