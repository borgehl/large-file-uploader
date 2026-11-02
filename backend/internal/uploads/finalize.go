package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"lfu/internal/store"
)

func (m *Manager) verifyWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.queue:
			m.verify(id)
			m.queued.Delete(id)
			m.progress.Delete(id)
		}
	}
}

type manifest struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	StoredName   string `json:"stored_name"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	UploadedBy   string `json:"uploaded_by"`
	UserID       int64  `json:"user_id"`
	CreatedAt    string `json:"created_at"`
	CompletedAt  string `json:"completed_at"`
}

// verify re-hashes the received file and either finalizes it atomically into
// complete/ or quarantines it.
func (m *Manager) verify(id string) {
	ok, err := m.Store.ClaimVerifying(id)
	if err != nil || !ok {
		return
	}
	u, err := m.Store.UploadByID(id)
	if err != nil {
		return
	}
	src := filepath.Join(m.Incoming, id)
	sum, size, err := m.hashFile(id, src)
	if err != nil {
		m.Log.Error("verify: read failed", "id", id, "err", err)
		m.Store.FailVerify(id, "could not read the uploaded file: "+err.Error())
		return
	}

	if size != u.SizeBytes || sum != strings.ToLower(u.ClientSHA256) {
		msg := "checksum mismatch: the file on the server differs from the file you selected"
		if size != u.SizeBytes {
			msg = fmt.Sprintf("size mismatch: expected %d bytes, found %d", u.SizeBytes, size)
		}
		m.Log.Error("verify: mismatch", "id", id, "client", u.ClientSHA256, "server", sum, "size", size, "want", u.SizeBytes)
		qdir := filepath.Join(m.Quarantine, id)
		if err := os.MkdirAll(qdir, 0o750); err == nil {
			if err := os.Rename(src, filepath.Join(qdir, "data")); err == nil {
				os.Remove(src + ".info")
				m.Store.MarkFailedHash(id, sum, filepath.Join("quarantine", id, "data"), msg)
				return
			}
		}
		m.Store.FailVerify(id, "mismatch detected but quarantine failed")
		return
	}

	now := time.Now().UTC()
	dir := filepath.Join(m.Complete, now.Format("2006"), id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		m.Store.FailVerify(id, "cannot create destination: "+err.Error())
		return
	}
	m.chown(filepath.Dir(dir))
	m.chown(dir)
	stored := SafeName(u.OriginalName)
	dst := filepath.Join(dir, stored)

	if f, err := os.Open(src); err == nil { // flush file data before the rename makes it visible
		f.Sync()
		f.Close()
	}
	if err := os.Rename(src, dst); err != nil {
		m.Store.FailVerify(id, "cannot move file into place: "+err.Error())
		return
	}
	os.Chmod(dst, 0o640)
	m.chown(dst)
	os.Remove(src + ".info")

	mf := manifest{
		ID: id, OriginalName: u.OriginalName, StoredName: stored, SizeBytes: size, SHA256: sum,
		UploadedBy: u.OwnerName, UserID: u.UserID,
		CreatedAt: time.Unix(u.CreatedAt, 0).UTC().Format(time.RFC3339), CompletedAt: now.Format(time.RFC3339),
	}
	if b, err := json.MarshalIndent(mf, "", "  "); err == nil {
		mp := filepath.Join(dir, "manifest.json")
		if os.WriteFile(mp+".tmp", b, 0o640) == nil {
			os.Rename(mp+".tmp", mp)
			m.chown(mp)
		}
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	rel, _ := filepath.Rel(m.Cfg.UploadDir, dst)
	if err := m.Store.MarkComplete(id, sum, rel); err != nil {
		m.Log.Error("verify: mark complete", "id", id, "err", err)
		return
	}
	m.Log.Info("upload complete", "id", id, "path", rel, "sha256", sum)
}

func (m *Manager) chown(p string) {
	if m.Cfg.FileGID >= 0 {
		os.Chown(p, -1, m.Cfg.FileGID)
	}
}

func (m *Manager) hashFile(id, path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	var counter atomic.Int64
	m.progress.Store(id, &counter)
	h := sha256.New()
	buf := make([]byte, 4<<20)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			size += int64(n)
			counter.Store(size)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", 0, rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// reconcile re-queues work interrupted by a restart.
func (m *Manager) reconcile() {
	if err := m.Store.RequeueVerifying(); err != nil {
		m.Log.Error("reconcile", "err", err)
	}
	created, _ := m.Store.UploadsInState(store.StateCreated)
	for _, u := range created {
		if fi, err := os.Stat(filepath.Join(m.Incoming, u.ID)); err == nil && fi.Size() == u.SizeBytes {
			m.Store.MarkReceived(u.ID, u.SizeBytes) // finished just before a crash
		}
	}
	received, _ := m.Store.UploadsInState(store.StateReceived)
	for _, u := range received {
		m.maybeEnqueue(u.ID)
	}
}
