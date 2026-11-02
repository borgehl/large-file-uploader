package uploads

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lfu/internal/auth"
	"lfu/internal/httpx"
	"lfu/internal/store"
)

var sha256RE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type uploadView struct {
	*store.Upload
	Offset        int64  `json:"offset"`
	VerifiedBytes *int64 `json:"verified_bytes,omitempty"`
	StoredPath    string `json:"stored_path,omitempty"`
}

func (m *Manager) view(u *store.Upload) uploadView {
	v := uploadView{Upload: u, Offset: u.BytesReceived}
	switch u.State {
	case store.StateCreated:
		if fi, err := os.Stat(filepath.Join(m.Incoming, u.ID)); err == nil {
			v.Offset = fi.Size()
		}
	case store.StateVerifying:
		if n, ok := m.verifyProgress(u.ID); ok {
			v.VerifiedBytes = &n
		}
	case store.StateComplete:
		v.StoredPath = u.FinalPath
	}
	return v
}

func (m *Manager) Config(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"max_upload_bytes":  m.Cfg.MaxUploadBytes,
		"chunk_size_bytes":  int64(m.Cfg.ChunkSizeMiB) << 20,
		"chunk_header_name": chunkHeader,
	})
}

func (m *Manager) List(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	all := r.URL.Query().Get("all") == "1" && u.Role == "admin"
	rows, err := m.Store.ListUploads(u.ID, all)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]uploadView, 0, len(rows))
	for _, r := range rows {
		out = append(out, m.view(r))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// load fetches an upload the caller may see (owner or admin); 404 otherwise.
func (m *Manager) load(w http.ResponseWriter, r *http.Request) (*store.Upload, *store.User) {
	user := auth.UserFrom(r.Context())
	id := r.PathValue("id")
	if !idRE.MatchString(id) {
		httpx.Error(w, http.StatusNotFound, "upload not found")
		return nil, nil
	}
	up, err := m.Store.UploadByID(id)
	if err != nil || (up.UserID != user.ID && user.Role != "admin") {
		httpx.Error(w, http.StatusNotFound, "upload not found")
		return nil, nil
	}
	return up, user
}

func (m *Manager) Get(w http.ResponseWriter, r *http.Request) {
	up, _ := m.load(w, r)
	if up == nil {
		return
	}
	httpx.JSON(w, http.StatusOK, m.view(up))
}

type shaReq struct {
	SHA256 string `json:"sha256"`
}

// SubmitSHA256 records the browser-computed whole-file digest.
func (m *Manager) SubmitSHA256(w http.ResponseWriter, r *http.Request) {
	up, user := m.load(w, r)
	if up == nil {
		return
	}
	if up.UserID != user.ID {
		httpx.Error(w, http.StatusForbidden, "only the owner can submit the digest")
		return
	}
	var req shaReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !sha256RE.MatchString(req.SHA256) {
		httpx.Error(w, http.StatusBadRequest, "sha256 must be 64 hex characters")
		return
	}
	if up.State != store.StateCreated && up.State != store.StateReceived && up.State != store.StateVerifying && up.State != store.StateComplete {
		httpx.Error(w, http.StatusConflict, "upload is not active")
		return
	}
	digest := strings.ToLower(req.SHA256)
	if err := m.Store.SetClientSHA256(up.ID, digest); err != nil {
		if err == store.ErrConflict {
			httpx.Error(w, http.StatusConflict, "a different digest was already submitted")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	m.maybeEnqueue(up.ID)
	fresh, _ := m.Store.UploadByID(up.ID)
	httpx.JSON(w, http.StatusOK, m.view(fresh))
}

// Delete cancels an active upload (owner/admin) or discards a quarantined one (admin).
func (m *Manager) Delete(w http.ResponseWriter, r *http.Request) {
	up, user := m.load(w, r)
	if up == nil {
		return
	}
	switch up.State {
	case store.StateCreated:
		m.removeIncoming(up.ID)
		m.Store.SetState(up.ID, store.StateAborted, "cancelled by user")
	case store.StateFailedHash:
		if user.Role != "admin" {
			httpx.Error(w, http.StatusForbidden, "admin only")
			return
		}
		os.RemoveAll(filepath.Join(m.Quarantine, up.ID))
		m.Store.DeleteUpload(up.ID)
	default:
		httpx.Error(w, http.StatusConflict, "this upload cannot be deleted")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
