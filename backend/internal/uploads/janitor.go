package uploads

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// janitor expires abandoned partial uploads and removes orphaned files.
// Completed and quarantined files are never touched.
func (m *Manager) janitor(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	m.sweep()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.sweep()
		}
	}
}

func (m *Manager) sweep() {
	m.Store.PruneSessions(m.Cfg.SessionIdle)

	stale, err := m.Store.StaleCreated(time.Now().Add(-m.Cfg.PartialExpiry))
	if err != nil {
		m.Log.Error("janitor", "err", err)
	}
	for _, u := range stale {
		m.removeIncoming(u.ID)
		m.Store.SetState(u.ID, "expired", "abandoned upload expired")
		m.Log.Info("upload expired", "id", u.ID)
	}

	entries, err := os.ReadDir(m.Incoming)
	if err != nil {
		return
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".info")
		if !idRE.MatchString(id) {
			continue
		}
		if _, err := m.Store.UploadByID(id); err == nil {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(m.Incoming, e.Name()))
			m.Log.Info("removed orphan", "name", e.Name())
		}
	}
}

func (m *Manager) removeIncoming(id string) {
	os.Remove(filepath.Join(m.Incoming, id))
	os.Remove(filepath.Join(m.Incoming, id+".info"))
	os.Remove(filepath.Join(m.Locks, id+".lock"))
}
