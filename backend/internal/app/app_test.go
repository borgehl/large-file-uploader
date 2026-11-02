package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lfu/internal/app"
	"lfu/internal/auth"
	"lfu/internal/config"
	"lfu/internal/store"
	"lfu/internal/uploads"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	cfg *config.Config
	st  *store.Store
	dir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		UploadDir: filepath.Join(dir, "uploads"), DBPath: ":memory:",
		MaxUploadBytes: 1 << 20, ReserveBytes: 0, ChunkSizeMiB: 1,
		RootUsername: "root", RootPassword: "correct horse battery",
		SessionIdle: time.Hour, SessionMax: 24 * time.Hour, PartialExpiry: time.Hour,
		VerifyWorkers: 1, InsecureCookie: true, FileGID: -1,
	}
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := auth.NewService(st, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.BootstrapRoot(); err != nil {
		t.Fatal(err)
	}
	m, err := uploads.NewManager(cfg, st, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	srv := httptest.NewServer(app.NewHandler(a, m))
	t.Cleanup(func() { srv.Close(); cancel(); st.Close() })
	return &env{t: t, srv: srv, cfg: cfg, st: st, dir: dir}
}

type client struct {
	e *env
	c *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *client) do(method, path string, body []byte, hdr map[string]string) (*http.Response, []byte) {
	c.e.t.Helper()
	req, _ := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (c *client) jsonReq(method, path string, v any) (*http.Response, map[string]any) {
	c.e.t.Helper()
	var b []byte
	if v != nil {
		b, _ = json.Marshal(v)
	}
	resp, raw := c.do(method, path, b, map[string]string{"Content-Type": "application/json"})
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp, out
}

func (c *client) login(user, pw string) *http.Response {
	r, _ := c.jsonReq("POST", "/api/auth/login", map[string]string{"username": user, "password": pw})
	return r
}

// adminClient logs in as root and completes the forced password change.
func (e *env) adminClient() *client {
	c := e.client()
	if r := c.login("root", "correct horse battery"); r.StatusCode != 200 {
		e.t.Fatalf("root login: %d", r.StatusCode)
	}
	r, _ := c.jsonReq("POST", "/api/auth/password", map[string]string{"current_password": "correct horse battery", "new_password": "a brand new passphrase"})
	if r.StatusCode != 200 {
		e.t.Fatalf("root password change: %d", r.StatusCode)
	}
	return c
}

func (e *env) newUser(admin *client, name string) *client {
	r, body := admin.jsonReq("POST", "/api/admin/users", map[string]string{"username": name, "password": "temporary passphrase 1"})
	if r.StatusCode != 201 {
		e.t.Fatalf("create user: %d %v", r.StatusCode, body)
	}
	c := e.client()
	if r := c.login(name, "temporary passphrase 1"); r.StatusCode != 200 {
		e.t.Fatalf("login %s: %d", name, r.StatusCode)
	}
	if r, _ := c.jsonReq("POST", "/api/auth/password", map[string]string{"current_password": "temporary passphrase 1", "new_password": name + " permanent passphrase"}); r.StatusCode != 200 {
		e.t.Fatalf("pw change: %d", r.StatusCode)
	}
	return c
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func meta(name string) string {
	return "filename " + base64.StdEncoding.EncodeToString([]byte(name))
}

// tusCreate creates an upload and returns its id (the last path element of Location).
func (c *client) tusCreate(size int, name string) (string, *http.Response) {
	resp, _ := c.do("POST", "/files/", nil, map[string]string{
		"Tus-Resumable": "1.0.0", "Upload-Length": strconv.Itoa(size), "Upload-Metadata": meta(name),
	})
	loc := resp.Header.Get("Location")
	return loc[strings.LastIndex(loc, "/")+1:], resp
}

func (c *client) tusPatch(id string, offset int, chunk []byte, hash string) *http.Response {
	resp, _ := c.do("PATCH", "/files/"+id, chunk, map[string]string{
		"Tus-Resumable": "1.0.0", "Upload-Offset": strconv.Itoa(offset),
		"Content-Type": "application/offset+octet-stream", "X-Chunk-SHA256": hash,
	})
	return resp
}

func (c *client) offset(id string) int {
	resp, _ := c.do("HEAD", "/files/"+id, nil, map[string]string{"Tus-Resumable": "1.0.0"})
	n, _ := strconv.Atoi(resp.Header.Get("Upload-Offset"))
	return n
}

func (c *client) waitState(id, want string) map[string]any {
	c.e.t.Helper()
	for i := 0; i < 200; i++ {
		_, v := c.jsonReq("GET", "/api/uploads/"+id, nil)
		if v["state"] == want {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	_, v := c.jsonReq("GET", "/api/uploads/"+id, nil)
	c.e.t.Fatalf("upload %s never reached %s: %v", id, want, v)
	return nil
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func TestUnauthenticatedRejected(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for _, p := range []string{"/api/auth/me", "/api/uploads", "/api/config", "/api/admin/users", "/files/"} {
		method := "GET"
		hdr := map[string]string{}
		if p == "/files/" {
			method = "POST"
			hdr["Tus-Resumable"] = "1.0.0"
			hdr["Upload-Length"] = "10"
		}
		if r, _ := c.do(method, p, nil, hdr); r.StatusCode != 401 {
			t.Errorf("%s %s = %d, want 401", method, p, r.StatusCode)
		}
	}
	if r, _ := c.do("GET", "/api/healthz", nil, nil); r.StatusCode != 200 {
		t.Errorf("healthz = %d", r.StatusCode)
	}
}

func TestRootMustChangePassword(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	if r := c.login("root", "correct horse battery"); r.StatusCode != 200 {
		t.Fatalf("login %d", r.StatusCode)
	}
	if r, b := c.jsonReq("GET", "/api/uploads", nil); r.StatusCode != 403 || b["error"] != "password_change_required" {
		t.Fatalf("expected forced change, got %d %v", r.StatusCode, b)
	}
	if r, _ := c.jsonReq("GET", "/api/auth/me", nil); r.StatusCode != 200 {
		t.Fatalf("me should work: %d", r.StatusCode)
	}
	if r, _ := c.jsonReq("POST", "/api/auth/password", map[string]string{"current_password": "correct horse battery", "new_password": "short"}); r.StatusCode != 400 {
		t.Fatalf("short password accepted: %d", r.StatusCode)
	}
	if r, _ := c.jsonReq("POST", "/api/auth/password", map[string]string{"current_password": "correct horse battery", "new_password": "a brand new passphrase"}); r.StatusCode != 200 {
		t.Fatalf("change failed: %d", r.StatusCode)
	}
	if r, _ := c.jsonReq("GET", "/api/uploads", nil); r.StatusCode != 200 {
		t.Fatalf("after change: %d", r.StatusCode)
	}
}

func TestLoginBackoffAndCookieFlags(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for i := 0; i < 5; i++ {
		if r := c.login("root", "wrong password!!"); r.StatusCode != 401 {
			t.Fatalf("attempt %d = %d", i, r.StatusCode)
		}
	}
	r := c.login("root", "wrong password!!")
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", r.StatusCode)
	}
	// Even the right password is refused while blocked.
	if r := c.login("root", "correct horse battery"); r.StatusCode != 429 {
		t.Fatalf("blocked login should be 429, got %d", r.StatusCode)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	e := newEnv(t)
	e.cfg.InsecureCookie = false // secure by default
	c := e.client()
	r := c.login("root", "correct horse battery")
	var ck *http.Cookie
	for _, x := range r.Cookies() {
		if x.Name == auth.CookieName {
			ck = x
		}
	}
	if ck == nil || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteStrictMode {
		t.Fatalf("bad cookie: %+v", ck)
	}
}

func TestCrossOriginRefused(t *testing.T) {
	e := newEnv(t)
	c := e.adminClient()
	r, _ := c.do("POST", "/api/auth/logout", nil, map[string]string{"Origin": "https://evil.example"})
	if r.StatusCode != 403 {
		t.Fatalf("cross-origin POST = %d", r.StatusCode)
	}
	r, _ = c.do("POST", "/api/auth/login", []byte(`{}`), map[string]string{"Origin": "https://evil.example"})
	if r.StatusCode != 403 {
		t.Fatalf("cross-origin login = %d", r.StatusCode)
	}
}

func TestUploadHappyPathAndFinalize(t *testing.T) {
	e := newEnv(t)
	admin := e.adminClient()
	u := e.newUser(admin, "alice")

	data := payload(300_000)
	id, resp := u.tusCreate(len(data), "../../evil/My Report.bin")
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Location"), e.srv.URL+"/files/") {
		t.Errorf("Location = %q", resp.Header.Get("Location"))
	}
	off := 0
	for off < len(data) {
		end := min(off+100_000, len(data))
		r := u.tusPatch(id, off, data[off:end], sum(data[off:end]))
		if r.StatusCode != 204 {
			t.Fatalf("patch at %d = %d", off, r.StatusCode)
		}
		off = end
	}
	// Digest arrives after the upload finished.
	time.Sleep(50 * time.Millisecond)
	v := u.waitState(id, "received")
	if v["offset"].(float64) != float64(len(data)) {
		t.Errorf("offset %v", v["offset"])
	}
	if r, b := u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": sum(data)}); r.StatusCode != 200 {
		t.Fatalf("sha submit = %d %v", r.StatusCode, b)
	}
	v = u.waitState(id, "complete")
	if v["server_sha256"] != sum(data) {
		t.Errorf("server hash %v", v["server_sha256"])
	}

	// File landed under complete/<year>/<id>/ with a sanitized name and a manifest; nothing escaped.
	matches, _ := filepath.Glob(filepath.Join(e.cfg.UploadDir, "complete", "*", id, "*"))
	var names []string
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	if len(names) != 2 {
		t.Fatalf("files in destination: %v", names)
	}
	got, err := os.ReadFile(filepath.Join(filepath.Dir(matches[0]), "My Report.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("final file wrong: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.UploadDir, "incoming", id)); !os.IsNotExist(err) {
		t.Errorf("incoming file still present")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "evil")); !os.IsNotExist(err) {
		t.Errorf("path traversal created a directory")
	}
	var mf map[string]any
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(matches[0]), "manifest.json"))
	json.Unmarshal(b, &mf)
	if mf["sha256"] != sum(data) || mf["uploaded_by"] != "alice" {
		t.Errorf("manifest %v", mf)
	}
}

func TestDigestBeforeUploadEnds(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(e.adminClient(), "bob")
	data := payload(50_000)
	id, _ := u.tusCreate(len(data), "a.bin")
	if r, _ := u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": strings.ToUpper(sum(data))}); r.StatusCode != 200 {
		t.Fatalf("early digest = %d", r.StatusCode)
	}
	if r, _ := u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": strings.Repeat("0", 64)}); r.StatusCode != 409 {
		t.Fatalf("changed digest should conflict, got %d", r.StatusCode)
	}
	if r := u.tusPatch(id, 0, data, sum(data)); r.StatusCode != 204 {
		t.Fatalf("patch = %d", r.StatusCode)
	}
	u.waitState(id, "complete")
}

func TestBadChunkHashRejectedThenResume(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(e.adminClient(), "carol")
	data := payload(200_000)
	id, _ := u.tusCreate(len(data), "c.bin")

	if r := u.tusPatch(id, 0, data[:100_000], sum(data[:100_000])); r.StatusCode != 204 {
		t.Fatalf("first chunk = %d", r.StatusCode)
	}
	// corrupted in transit: hash is for different bytes
	if r := u.tusPatch(id, 100_000, data[100_000:], sum(data[:10])); r.StatusCode != 460 {
		t.Fatalf("bad hash = %d, want 460", r.StatusCode)
	}
	if r := u.tusPatch(id, 100_000, data[100_000:], ""); r.StatusCode != 400 {
		t.Fatalf("missing hash = %d, want 400", r.StatusCode)
	}
	if n := u.offset(id); n != 100_000 {
		t.Fatalf("offset after rejected chunk = %d, want 100000", n)
	}
	// "reconnect": a brand new session resumes from the server offset
	if r := u.tusPatch(id, 100_000, data[100_000:], sum(data[100_000:])); r.StatusCode != 204 {
		t.Fatalf("resume = %d", r.StatusCode)
	}
	u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": sum(data)})
	u.waitState(id, "complete")
}

func TestWholeFileHashMismatchQuarantines(t *testing.T) {
	e := newEnv(t)
	admin := e.adminClient()
	u := e.newUser(admin, "dave")
	data := payload(80_000)
	id, _ := u.tusCreate(len(data), "d.bin")
	u.tusPatch(id, 0, data, sum(data))
	// client claims the original file had a different digest (file changed / corrupted before send)
	u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": sum([]byte("something else"))})
	v := u.waitState(id, "failed_hash")
	if !strings.Contains(v["error"].(string), "mismatch") {
		t.Errorf("error %v", v["error"])
	}
	if _, err := os.Stat(filepath.Join(e.cfg.UploadDir, "quarantine", id, "data")); err != nil {
		t.Errorf("not quarantined: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(e.cfg.UploadDir, "complete", "*", id)); len(m) != 0 {
		t.Errorf("mismatched file must not be in complete/")
	}
	// only admins can discard quarantined data
	if r, _ := u.jsonReq("DELETE", "/api/uploads/"+id, nil); r.StatusCode != 403 {
		t.Errorf("user delete quarantine = %d", r.StatusCode)
	}
	if r, _ := admin.jsonReq("DELETE", "/api/uploads/"+id, nil); r.StatusCode != 200 {
		t.Errorf("admin delete quarantine = %d", r.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.UploadDir, "quarantine", id)); !os.IsNotExist(err) {
		t.Errorf("quarantine not removed")
	}
}

func TestOnDiskCorruptionDetected(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(e.adminClient(), "erin")
	data := payload(60_000)
	id, _ := u.tusCreate(len(data), "e.bin")
	// Receive the data but hold back the digest, then flip a byte on disk.
	u.tusPatch(id, 0, data, sum(data))
	u.waitState(id, "received")
	p := filepath.Join(e.cfg.UploadDir, "incoming", id)
	b, _ := os.ReadFile(p)
	b[1234] ^= 0xFF
	os.WriteFile(p, b, 0o640)
	u.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": sum(data)})
	u.waitState(id, "failed_hash")
}

func TestOwnershipIsolation(t *testing.T) {
	e := newEnv(t)
	admin := e.adminClient()
	a := e.newUser(admin, "frank")
	b := e.newUser(admin, "grace")
	data := payload(10_000)
	id, _ := a.tusCreate(len(data), "f.bin")

	for _, tc := range []struct{ name, method, path string }{
		{"HEAD", "HEAD", "/files/" + id}, {"PATCH", "PATCH", "/files/" + id}, {"DELETE", "DELETE", "/files/" + id},
		{"api get", "GET", "/api/uploads/" + id}, {"api delete", "DELETE", "/api/uploads/" + id},
	} {
		r, _ := b.do(tc.method, tc.path, nil, map[string]string{"Tus-Resumable": "1.0.0", "Upload-Offset": "0", "X-Chunk-SHA256": sum(nil), "Content-Type": "application/offset+octet-stream"})
		if r.StatusCode != 404 {
			t.Errorf("%s by other user = %d, want 404", tc.name, r.StatusCode)
		}
	}
	if r, _ := b.jsonReq("POST", "/api/uploads/"+id+"/sha256", map[string]string{"sha256": sum(data)}); r.StatusCode != 404 {
		t.Errorf("digest by other user = %d", r.StatusCode)
	}
	// list shows only own; admin ?all=1 sees everything
	resp, raw := b.do("GET", "/api/uploads", nil, nil)
	if resp.StatusCode != 200 || strings.Contains(string(raw), id) {
		t.Errorf("other user's list leaks upload: %s", raw)
	}
	_, raw = admin.do("GET", "/api/uploads?all=1", nil, nil)
	if !strings.Contains(string(raw), id) {
		t.Errorf("admin all=1 missing upload")
	}
	// admin can read but not write someone else's upload
	if r, _ := admin.do("HEAD", "/files/"+id, nil, map[string]string{"Tus-Resumable": "1.0.0"}); r.StatusCode != 200 {
		t.Errorf("admin HEAD = %d", r.StatusCode)
	}
	if r := admin.tusPatch(id, 0, data, sum(data)); r.StatusCode != 403 {
		t.Errorf("admin PATCH = %d, want 403", r.StatusCode)
	}
}

func TestSizeLimitAndSpoofedUserHeader(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(e.adminClient(), "heidi")
	limit := int(e.cfg.MaxUploadBytes)
	if _, r := u.tusCreate(limit, "big.bin"); r.StatusCode != 413 {
		t.Errorf("exactly at limit = %d, want 413", r.StatusCode)
	}
	if _, r := u.tusCreate(limit+1, "big.bin"); r.StatusCode != 413 {
		t.Errorf("over limit = %d, want 413", r.StatusCode)
	}
	if _, r := u.tusCreate(limit-1, "ok.bin"); r.StatusCode != 201 {
		t.Errorf("limit-1 = %d, want 201", r.StatusCode)
	}
	if _, r := u.tusCreate(0, "zero.bin"); r.StatusCode == 201 {
		t.Errorf("zero-length upload accepted")
	}
	// A client-supplied identity header must not let anyone act as user 1 (root).
	resp, _ := u.do("POST", "/files/", nil, map[string]string{
		"Tus-Resumable": "1.0.0", "Upload-Length": "10", "Upload-Metadata": meta("x"), "X-Lfu-User-Id": "1",
	})
	loc := resp.Header.Get("Location")
	row, _ := e.st.UploadByID(loc[strings.LastIndex(loc, "/")+1:])
	if row == nil || row.OwnerName != "heidi" {
		t.Errorf("owner = %+v, spoofed header was trusted", row)
	}
}

func TestAdminUserManagement(t *testing.T) {
	e := newEnv(t)
	admin := e.adminClient()
	u := e.newUser(admin, "ivan")
	if r, _ := u.jsonReq("GET", "/api/admin/users", nil); r.StatusCode != 403 {
		t.Errorf("non-admin list users = %d", r.StatusCode)
	}
	if r, _ := admin.jsonReq("POST", "/api/admin/users", map[string]string{"username": "ivan"}); r.StatusCode != 409 {
		t.Errorf("duplicate user = %d", r.StatusCode)
	}
	if r, _ := admin.jsonReq("POST", "/api/admin/users", map[string]string{"username": "../x"}); r.StatusCode != 400 {
		t.Errorf("bad username = %d", r.StatusCode)
	}
	users, _ := e.st.ListUsers()
	var ivan, root *store.User
	for _, x := range users {
		if x.Username == "ivan" {
			ivan = x
		}
		if x.IsRoot {
			root = x
		}
	}
	// root is protected
	if r, _ := admin.jsonReq("DELETE", "/api/admin/users/"+strconv.FormatInt(root.ID, 10), nil); r.StatusCode != 403 {
		t.Errorf("delete root = %d", r.StatusCode)
	}
	// generated temp password on reset forces a change, and disabling kills sessions
	r, body := admin.jsonReq("PATCH", "/api/admin/users/"+strconv.FormatInt(ivan.ID, 10), map[string]any{"reset_password": true})
	if r.StatusCode != 200 || body["temp_password"] == nil {
		t.Fatalf("reset = %d %v", r.StatusCode, body)
	}
	if r, _ := u.jsonReq("GET", "/api/auth/me", nil); r.StatusCode != 401 {
		t.Errorf("session survived reset: %d", r.StatusCode)
	}
	c2 := e.client()
	if r := c2.login("ivan", body["temp_password"].(string)); r.StatusCode != 200 {
		t.Fatalf("login with temp password = %d", r.StatusCode)
	}
	if r, b := c2.jsonReq("GET", "/api/uploads", nil); r.StatusCode != 403 || b["error"] != "password_change_required" {
		t.Errorf("temp password must force change: %d %v", r.StatusCode, b)
	}
	admin.jsonReq("PATCH", "/api/admin/users/"+strconv.FormatInt(ivan.ID, 10), map[string]any{"disabled": true})
	if r, _ := c2.jsonReq("GET", "/api/auth/me", nil); r.StatusCode != 401 {
		t.Errorf("disabled user kept session: %d", r.StatusCode)
	}
	if r := e.client().login("ivan", body["temp_password"].(string)); r.StatusCode != 401 {
		t.Errorf("disabled user can log in: %d", r.StatusCode)
	}
}

func TestCancelRemovesPartial(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(e.adminClient(), "judy")
	data := payload(40_000)
	id, _ := u.tusCreate(len(data), "j.bin")
	u.tusPatch(id, 0, data[:10_000], sum(data[:10_000]))
	if r, _ := u.do("DELETE", "/files/"+id, nil, map[string]string{"Tus-Resumable": "1.0.0"}); r.StatusCode != 204 {
		t.Fatalf("tus delete = %d", r.StatusCode)
	}
	u.waitState(id, "aborted")
	if _, err := os.Stat(filepath.Join(e.cfg.UploadDir, "incoming", id)); !os.IsNotExist(err) {
		t.Errorf("partial file remains")
	}
	if r := u.tusPatch(id, 10_000, data[10_000:], sum(data[10_000:])); r.StatusCode == 204 {
		t.Errorf("aborted upload accepted data")
	}
}
