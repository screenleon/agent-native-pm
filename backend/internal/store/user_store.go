package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(req models.CreateUserRequest) (*models.User, error) {
	if req.Role == "" {
		req.Role = "member"
	}
	if !models.ValidUserRoles[req.Role] {
		return nil, fmt.Errorf("invalid role: %s", req.Role)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	_, err = s.db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, TRUE, $6, $7)
	`, id, req.Username, req.Email, string(hash), req.Role, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *UserStore) GetByID(id string) (*models.User, error) {
	var u models.User
	var isActive bool
	err := s.db.QueryRow(`
		SELECT id, username, email, role, is_active, created_at, updated_at
		FROM users WHERE id=$1
	`, id).Scan(&u.ID, &u.Username, &u.Email, &u.Role, &isActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsActive = isActive
	return &u, nil
}

func (s *UserStore) GetByUsername(username string) (*models.User, string, error) {
	var u models.User
	var hash string
	var isActive bool
	err := s.db.QueryRow(`
		SELECT id, username, email, role, is_active, password_hash, created_at, updated_at
		FROM users WHERE username=$1
	`, username).Scan(&u.ID, &u.Username, &u.Email, &u.Role, &isActive, &hash, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	u.IsActive = isActive
	return &u, hash, nil
}

func (s *UserStore) Authenticate(username, password string) (*models.User, error) {
	u, hash, err := s.GetByUsername(username)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive {
		return nil, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, nil
	}
	return u, nil
}

func (s *UserStore) List() ([]models.User, error) {
	rows, err := s.db.Query(`
		SELECT id, username, email, role, is_active, created_at, updated_at
		FROM users ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		var isActive bool
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &isActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.IsActive = isActive
		users = append(users, u)
	}
	if users == nil {
		users = []models.User{}
	}
	return users, rows.Err()
}

func (s *UserStore) Update(id string, req models.UpdateUserRequest) (*models.User, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email=$%d", pos))
		args = append(args, *req.Email)
		pos++
	}
	if req.Role != nil {
		setClauses = append(setClauses, fmt.Sprintf("role=$%d", pos))
		args = append(args, *req.Role)
		pos++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active=$%d", pos))
		args = append(args, *req.IsActive)
		pos++
	}
	if len(setClauses) == 0 {
		return s.GetByID(id)
	}
	setClauses = append(setClauses, fmt.Sprintf("updated_at=$%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	_, err := s.db.Exec("UPDATE users SET "+strings.Join(setClauses, ", ")+fmt.Sprintf(" WHERE id=$%d", pos), args...)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *UserStore) CountAdmins() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND is_active=TRUE`).Scan(&count)
	return count, err
}

func (s *UserStore) CountAll() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}
