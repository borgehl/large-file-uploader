package store

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	PasswordHash       string `json:"-"`
	Role               string `json:"role"`
	IsRoot             bool   `json:"is_root"`
	Disabled           bool   `json:"disabled"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          int64  `json:"created_at"`
	LastLoginAt        *int64 `json:"last_login_at"`
}

const userCols = `id, username, password_hash, role, is_root, disabled, must_change_password, created_at, last_login_at`

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	var root, dis, mcp int
	var ll sql.NullInt64
	if err := r.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &root, &dis, &mcp, &u.CreatedAt, &ll); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.IsRoot, u.Disabled, u.MustChangePassword = root == 1, dis == 1, mcp == 1
	if ll.Valid {
		u.LastLoginAt = &ll.Int64
	}
	return &u, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, hash, role string, isRoot, mustChange bool) (*User, error) {
	res, err := s.DB.Exec(`INSERT INTO users(username, password_hash, role, is_root, must_change_password, created_at) VALUES(?,?,?,?,?,?)`,
		username, hash, role, b2i(isRoot), b2i(mustChange), time.Now().Unix())
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(id)
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) UserByName(name string) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.DB.Query(`SELECT ` + userCols + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetPassword(id int64, hash string, mustChange bool) error {
	_, err := s.DB.Exec(`UPDATE users SET password_hash=?, must_change_password=? WHERE id=?`, hash, b2i(mustChange), id)
	return err
}

func (s *Store) SetDisabled(id int64, disabled bool) error {
	_, err := s.DB.Exec(`UPDATE users SET disabled=? WHERE id=? AND is_root=0`, b2i(disabled), id)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM users WHERE id=? AND is_root=0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchLogin(id int64) {
	s.DB.Exec(`UPDATE users SET last_login_at=? WHERE id=?`, time.Now().Unix(), id)
}
