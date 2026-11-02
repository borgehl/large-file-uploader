package uploads

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"lfu/internal/auth"
	"lfu/internal/httpx"
	"lfu/internal/store"
)

// StatusChecksumMismatch follows tus' "460 Checksum Mismatch".
const StatusChecksumMismatch = 460

const chunkHeader = "X-Chunk-SHA256"

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter } // keeps ResponseController working

// Files returns the handler for /files/ (tus). It must be wrapped in
// auth.Authenticate. It adds ownership checks and per-chunk SHA-256 verification
// in front of tusd; a chunk that fails verification never reaches the store, so
// the upload offset is unchanged and the client can simply resend it.
func (m *Manager) Files() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFrom(r.Context())
		if user == nil {
			httpx.Error(w, http.StatusUnauthorized, "authentication required")
			return
		}
		// Never trust a client-supplied identity header.
		r.Header.Del(userHeader)
		r.Header.Set(userHeader, strconv.FormatInt(user.ID, 10))

		if r.Method == http.MethodOptions {
			m.tusHandler.ServeHTTP(w, r)
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/files/"), "/")
		if id == "" {
			if r.Method != http.MethodPost {
				httpx.Error(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			m.tusHandler.ServeHTTP(w, r)
			return
		}
		if !idRE.MatchString(id) {
			httpx.Error(w, http.StatusNotFound, "upload not found")
			return
		}
		up, err := m.Store.UploadByID(id)
		if err != nil || (up.UserID != user.ID && user.Role != "admin") {
			httpx.Error(w, http.StatusNotFound, "upload not found") // do not reveal other users' uploads
			return
		}
		switch r.Method {
		case http.MethodHead:
			m.tusHandler.ServeHTTP(w, r)
		case http.MethodPatch:
			if up.UserID != user.ID {
				httpx.Error(w, http.StatusForbidden, "only the owner can upload data")
				return
			}
			if up.State != store.StateCreated {
				httpx.Error(w, http.StatusConflict, "upload is no longer accepting data")
				return
			}
			if !m.checkChunk(w, r) {
				return
			}
			sw := &statusWriter{ResponseWriter: w, status: 200}
			m.tusHandler.ServeHTTP(sw, r)
			if sw.status < 300 {
				m.Store.TouchUpload(id)
			}
		case http.MethodDelete:
			if up.State != store.StateCreated {
				httpx.Error(w, http.StatusConflict, "upload can no longer be cancelled")
				return
			}
			sw := &statusWriter{ResponseWriter: w, status: 200}
			m.tusHandler.ServeHTTP(sw, r)
			if sw.status < 300 {
				m.Store.SetState(id, store.StateAborted, "cancelled by user")
				m.removeIncoming(id)
			}
		default:
			httpx.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

// checkChunk buffers the PATCH body (bounded by the chunk size), verifies its
// SHA-256 against X-Chunk-SHA256 and replaces the body with the verified bytes.
func (m *Manager) checkChunk(w http.ResponseWriter, r *http.Request) bool {
	want := strings.ToLower(r.Header.Get(chunkHeader))
	if len(want) != 64 {
		httpx.Error(w, http.StatusBadRequest, "missing or malformed "+chunkHeader)
		return false
	}
	if _, err := hex.DecodeString(want); err != nil {
		httpx.Error(w, http.StatusBadRequest, "missing or malformed "+chunkHeader)
		return false
	}
	cl := r.ContentLength
	if cl < 0 {
		httpx.Error(w, http.StatusLengthRequired, "Content-Length required")
		return false
	}
	if cl > m.Cfg.MaxChunkBytes() {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "chunk too large")
		return false
	}
	buf := make([]byte, cl)
	if _, err := io.ReadFull(r.Body, buf); err != nil {
		httpx.Error(w, http.StatusBadRequest, "incomplete chunk body")
		return false
	}
	sum := sha256.Sum256(buf)
	if hex.EncodeToString(sum[:]) != want {
		httpx.Error(w, StatusChecksumMismatch, "chunk checksum mismatch, resend the chunk")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return true
}
