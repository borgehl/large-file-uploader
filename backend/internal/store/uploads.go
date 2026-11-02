package store

import (
	"database/sql"
	"errors"
	"time"
)

// Upload states.
const (
	StateCreated    = "created"
	StateReceived   = "received"
	StateVerifying  = "verifying"
	StateComplete   = "complete"
	StateFailedHash = "failed_hash"
	StateAborted    = "aborted"
	StateExpired    = "expired"
)

type Upload struct {
	ID            string `json:"id"`
	UserID        int64  `json:"user_id"`
	OwnerName     string `json:"owner_name"`
	OriginalName  string `json:"original_name"`
	SizeBytes     int64  `json:"size_bytes"`
	BytesReceived int64  `json:"bytes_received"`
	ClientSHA256  string `json:"client_sha256"`
	ServerSHA256  string `json:"server_sha256"`
	State         string `json:"state"`
	Error         string `json:"error"`
	FinalPath     string `json:"-"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	CompletedAt   *int64 `json:"completed_at"`
}

const uploadCols = `id,user_id,owner_name,original_name,size_bytes,bytes_received,client_sha256,server_sha256,state,error,final_path,created_at,updated_at,completed_at`

func scanUpload(r interface{ Scan(...any) error }) (*Upload, error) {
	var u Upload
	var ca sql.NullInt64
	err := r.Scan(&u.ID, &u.UserID, &u.OwnerName, &u.OriginalName, &u.SizeBytes, &u.BytesReceived,
		&u.ClientSHA256, &u.ServerSHA256, &u.State, &u.Error, &u.FinalPath, &u.CreatedAt, &u.UpdatedAt, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ca.Valid {
		u.CompletedAt = &ca.Int64
	}
	return &u, nil
}

func (s *Store) CreateUpload(id string, userID int64, owner, name string, size int64) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO uploads(id,user_id,owner_name,original_name,size_bytes,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, userID, owner, name, size, StateCreated, now, now)
	return err
}

func (s *Store) UploadByID(id string) (*Upload, error) {
	return scanUpload(s.DB.QueryRow(`SELECT `+uploadCols+` FROM uploads WHERE id=?`, id))
}

func (s *Store) ListUploads(userID int64, all bool) ([]*Upload, error) {
	q := `SELECT ` + uploadCols + ` FROM uploads`
	var args []any
	if !all {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY created_at DESC LIMIT 500`
	return s.queryUploads(q, args...)
}

func (s *Store) UploadsInState(states ...string) ([]*Upload, error) {
	q := `SELECT ` + uploadCols + ` FROM uploads WHERE state IN (`
	args := make([]any, len(states))
	for i, st := range states {
		if i > 0 {
			q += ","
		}
		q += "?"
		args[i] = st
	}
	q += `)`
	return s.queryUploads(q, args...)
}

func (s *Store) queryUploads(q string, args ...any) ([]*Upload, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetClientSHA256 stores the digest once; a different later value is a conflict.
func (s *Store) SetClientSHA256(id, digest string) error {
	u, err := s.UploadByID(id)
	if err != nil {
		return err
	}
	if u.ClientSHA256 != "" {
		if u.ClientSHA256 == digest {
			return nil
		}
		return ErrConflict
	}
	res, err := s.DB.Exec(`UPDATE uploads SET client_sha256=?, updated_at=? WHERE id=? AND client_sha256=''`, digest, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) SetState(id, state, errMsg string) error {
	_, err := s.DB.Exec(`UPDATE uploads SET state=?, error=?, updated_at=? WHERE id=?`, state, errMsg, time.Now().Unix(), id)
	return err
}

// MarkReceived moves created -> received (idempotent for later states).
func (s *Store) MarkReceived(id string, size int64) (bool, error) {
	res, err := s.DB.Exec(`UPDATE uploads SET state=?, bytes_received=?, updated_at=? WHERE id=? AND state=?`,
		StateReceived, size, time.Now().Unix(), id, StateCreated)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) MarkComplete(id, serverSHA, finalPath string) error {
	now := time.Now().Unix()
	_, err := s.DB.Exec(`UPDATE uploads SET state=?, server_sha256=?, final_path=?, error='', updated_at=?, completed_at=? WHERE id=?`,
		StateComplete, serverSHA, finalPath, now, now, id)
	return err
}

func (s *Store) MarkFailedHash(id, serverSHA, path, msg string) error {
	_, err := s.DB.Exec(`UPDATE uploads SET state=?, server_sha256=?, final_path=?, error=?, updated_at=? WHERE id=?`,
		StateFailedHash, serverSHA, path, msg, time.Now().Unix(), id)
	return err
}

func (s *Store) TouchUpload(id string) {
	s.DB.Exec(`UPDATE uploads SET updated_at=? WHERE id=?`, time.Now().Unix(), id)
}

func (s *Store) DeleteUpload(id string) error {
	_, err := s.DB.Exec(`DELETE FROM uploads WHERE id=?`, id)
	return err
}

func (s *Store) StaleCreated(before time.Time) ([]*Upload, error) {
	return s.queryUploads(`SELECT `+uploadCols+` FROM uploads WHERE state=? AND updated_at<?`, StateCreated, before.Unix())
}

// InFlightUploads returns uploads still receiving data (state created).
func (s *Store) InFlightUploads() ([]*Upload, error) {
	return s.UploadsInState(StateCreated)
}

// ClaimVerifying atomically moves received -> verifying; false if someone else did.
func (s *Store) ClaimVerifying(id string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE uploads SET state=?, error='', updated_at=? WHERE id=? AND state=?`,
		StateVerifying, time.Now().Unix(), id, StateReceived)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RequeueVerifying puts interrupted verifications back to received (startup).
func (s *Store) RequeueVerifying() error {
	_, err := s.DB.Exec(`UPDATE uploads SET state=? WHERE state=?`, StateReceived, StateVerifying)
	return err
}

// FailVerify returns an upload to received with an error (I/O problem, not a hash mismatch).
func (s *Store) FailVerify(id, msg string) error {
	_, err := s.DB.Exec(`UPDATE uploads SET state=?, error=?, updated_at=? WHERE id=?`, StateReceived, msg, time.Now().Unix(), id)
	return err
}
