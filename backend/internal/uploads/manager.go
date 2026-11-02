// Package uploads wires tusd (resumable uploads) to authentication, per-chunk
// integrity checks, whole-file SHA-256 verification and atomic finalization.
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"

	"lfu/internal/config"
	"lfu/internal/store"
)

var idRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

const userHeader = "X-Lfu-User-Id" // set by Files() after authentication; client values are discarded

type Manager struct {
	Cfg   *config.Config
	Store *store.Store
	Log   *slog.Logger

	Incoming, Complete, Quarantine, Locks string

	tus        *handler.Handler
	tusHandler http.Handler // tus with the /files/ prefix stripped
	queue      chan string

	createMu sync.Mutex
	queued   sync.Map // id -> struct{}: queued or being verified
	progress sync.Map // id -> *atomic.Int64: bytes hashed so far
}

func NewManager(cfg *config.Config, st *store.Store, log *slog.Logger) (*Manager, error) {
	m := &Manager{
		Cfg: cfg, Store: st, Log: log,
		Incoming:   filepath.Join(cfg.UploadDir, "incoming"),
		Complete:   filepath.Join(cfg.UploadDir, "complete"),
		Quarantine: filepath.Join(cfg.UploadDir, "quarantine"),
		Locks:      filepath.Join(cfg.UploadDir, "locks"),
		queue:      make(chan string, 1024),
	}
	for _, d := range []string{cfg.UploadDir, m.Incoming, m.Complete, m.Quarantine, m.Locks} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}

	if cfg.FileGID >= 0 { // let the host group traverse into complete/
		os.Chown(m.Complete, -1, cfg.FileGID)
	}

	composer := handler.NewStoreComposer()
	filestore.New(m.Incoming).UseIn(composer)
	filelocker.New(m.Locks).UseIn(composer)

	h, err := handler.NewHandler(handler.Config{
		BasePath:                "/files/",
		StoreComposer:           composer,
		MaxSize:                 cfg.MaxUploadBytes - 1, // files must be strictly smaller than the limit
		NotifyCompleteUploads:   true,
		RespectForwardedHeaders: true,
		DisableDownload:         true,
		Cors:                    &handler.CorsConfig{Disable: true},
		PreUploadCreateCallback: m.preCreate,
	})
	if err != nil {
		return nil, fmt.Errorf("tusd: %w", err)
	}
	m.tus = h
	m.tusHandler = http.StripPrefix("/files/", h)
	return m, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func freeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// preCreate validates a new upload, reserves disk and records ownership.
func (m *Manager) preCreate(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	var none handler.FileInfoChanges
	uid := hook.HTTPRequest.Header.Get(userHeader)
	var userID int64
	if _, err := fmt.Sscanf(uid, "%d", &userID); err != nil || userID == 0 {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_UNAUTHENTICATED", "authentication required", 401)
	}
	user, err := m.Store.UserByID(userID)
	if err != nil || user.Disabled {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_UNAUTHENTICATED", "authentication required", 401)
	}
	size := hook.Upload.Size
	if hook.Upload.SizeIsDeferred || size <= 0 {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_SIZE_REQUIRED", "upload size must be known and greater than zero", 400)
	}
	if size >= m.Cfg.MaxUploadBytes {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_TOO_LARGE",
			fmt.Sprintf("file must be smaller than %d bytes", m.Cfg.MaxUploadBytes), 413)
	}
	if hook.Upload.IsPartial || hook.Upload.IsFinal {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_CONCAT", "concatenation is not supported", 400)
	}
	name := SafeName(hook.Upload.MetaData["filename"])

	m.createMu.Lock()
	defer m.createMu.Unlock()
	if err := m.checkDisk(size); err != nil {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_NO_SPACE", err.Error(), 507)
	}
	id, err := newID()
	if err != nil {
		return handler.HTTPResponse{}, none, err
	}
	if err := m.Store.CreateUpload(id, user.ID, user.Username, name, size); err != nil {
		return handler.HTTPResponse{}, none, err
	}
	m.Log.Info("upload created", "id", id, "user", user.Username, "size", size)
	return handler.HTTPResponse{}, handler.FileInfoChanges{
		ID:       id,
		MetaData: handler.MetaData{"filename": name},
	}, nil
}

// checkDisk requires room for the new upload plus the reserve, after
// accounting for the not-yet-written remainder of uploads in flight.
func (m *Manager) checkDisk(size int64) error {
	free, err := freeBytes(m.Cfg.UploadDir)
	if err != nil {
		return fmt.Errorf("cannot determine free disk space")
	}
	inflight, err := m.Store.InFlightUploads()
	if err != nil {
		return err
	}
	var pending int64
	for _, u := range inflight {
		written := int64(0)
		if fi, err := os.Stat(filepath.Join(m.Incoming, u.ID)); err == nil {
			written = fi.Size()
		}
		if rem := u.SizeBytes - written; rem > 0 {
			pending += rem
		}
	}
	if free-pending < size+m.Cfg.ReserveBytes {
		return fmt.Errorf("not enough free disk space on the server for this file")
	}
	return nil
}

// Start launches background workers; they stop when ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	go m.consumeComplete(ctx)
	for i := 0; i < m.Cfg.VerifyWorkers; i++ {
		go m.verifyWorker(ctx)
	}
	m.reconcile()
	go m.janitor(ctx)
}

func (m *Manager) consumeComplete(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-m.tus.CompleteUploads:
			id := ev.Upload.ID
			if _, err := m.Store.MarkReceived(id, ev.Upload.Size); err != nil {
				m.Log.Error("mark received", "id", id, "err", err)
				continue
			}
			m.maybeEnqueue(id)
		}
	}
}

// maybeEnqueue queues verification once the file is complete and the client
// digest is known. Safe to call from either side in any order.
func (m *Manager) maybeEnqueue(id string) {
	u, err := m.Store.UploadByID(id)
	if err != nil || u.State != store.StateReceived || u.ClientSHA256 == "" {
		return
	}
	if _, loaded := m.queued.LoadOrStore(id, struct{}{}); loaded {
		return
	}
	go func() { m.queue <- id }()
}

func (m *Manager) verifyProgress(id string) (int64, bool) {
	if v, ok := m.progress.Load(id); ok {
		return v.(*atomic.Int64).Load(), true
	}
	return 0, false
}
