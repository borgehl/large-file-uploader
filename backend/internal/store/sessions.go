package store

import (
	"database/sql"
	"errors"
	"time"
)

type Session struct {
	TokenHash  string
	UserID     int64
	CreatedAt  int64
	LastSeenAt int64
	ExpiresAt  int64
}

func (s *Store) CreateSession(tokenHash string, userID int64, ttl time.Duration, ip, ua string) error {
	now := time.Now()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	_, err := s.DB.Exec(`INSERT INTO sessions(token_hash,user_id,created_at,last_seen_at,expires_at,ip,user_agent) VALUES(?,?,?,?,?,?,?)`,
		tokenHash, userID, now.Unix(), now.Unix(), now.Add(ttl).Unix(), ip, ua)
	return err
}

func (s *Store) SessionByHash(tokenHash string) (*Session, error) {
	var x Session
	err := s.DB.QueryRow(`SELECT token_hash,user_id,created_at,last_seen_at,expires_at FROM sessions WHERE token_hash=?`, tokenHash).
		Scan(&x.TokenHash, &x.UserID, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

func (s *Store) TouchSession(tokenHash string, at int64) {
	s.DB.Exec(`UPDATE sessions SET last_seen_at=? WHERE token_hash=?`, at, tokenHash)
}

func (s *Store) DeleteSession(tokenHash string) {
	s.DB.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash)
}

// DeleteUserSessions revokes all sessions for a user, optionally keeping one.
func (s *Store) DeleteUserSessions(userID int64, keepHash string) {
	s.DB.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, userID, keepHash)
}

func (s *Store) PruneSessions(idle time.Duration) {
	now := time.Now()
	s.DB.Exec(`DELETE FROM sessions WHERE expires_at<? OR last_seen_at<?`, now.Unix(), now.Add(-idle).Unix())
}
