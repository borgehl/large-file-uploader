package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"lfu/internal/config"
	"lfu/internal/httpx"
	"lfu/internal/store"
)

const CookieName = "lfu_session"

type ctxKey int

const (
	userKey ctxKey = iota
	sessKey
)

type Service struct {
	Store   *store.Store
	Cfg     *config.Config
	Limiter *Limiter
	Log     *slog.Logger

	dummyHash string
	touchGap  int64 // seconds between session last_seen writes
}

func NewService(st *store.Store, cfg *config.Config, log *slog.Logger) (*Service, error) {
	d, err := HashPassword("dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	return &Service{Store: st, Cfg: cfg, Limiter: NewLimiter(), Log: log, dummyHash: d, touchGap: 60}, nil
}

// UserFrom returns the authenticated user placed in the context by Authenticate.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey).(*store.User)
	return u
}

func sessionHashFrom(ctx context.Context) string {
	s, _ := ctx.Value(sessKey).(string)
	return s
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// lookup resolves the request cookie to a live session and an enabled user.
func (s *Service) lookup(r *http.Request) (*store.User, string, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return nil, "", store.ErrNotFound
	}
	h := hashToken(c.Value)
	sess, err := s.Store.SessionByHash(h)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().Unix()
	if sess.ExpiresAt <= now || sess.LastSeenAt+int64(s.Cfg.SessionIdle.Seconds()) <= now {
		s.Store.DeleteSession(h)
		return nil, "", store.ErrNotFound
	}
	u, err := s.Store.UserByID(sess.UserID)
	if err != nil || u.Disabled {
		s.Store.DeleteSession(h)
		return nil, "", store.ErrNotFound
	}
	if now-sess.LastSeenAt >= s.touchGap {
		s.Store.TouchSession(h, now)
	}
	return u, h, nil
}

// allowedWhilePasswordChangeRequired lists the only routes usable before the
// forced password change.
func allowedWhilePasswordChangeRequired(path string) bool {
	switch path {
	case "/api/auth/me", "/api/auth/password", "/api/auth/logout":
		return true
	}
	return false
}

// Authenticate guards next: a valid session is required, cross-origin
// state-changing requests are refused, and users who must change their
// password are confined to the change-password routes.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.IsSafeMethod(r.Method) && !httpx.SameOrigin(r) {
			httpx.Error(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		u, h, err := s.lookup(r)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if u.MustChangePassword && !allowedWhilePasswordChangeRequired(r.URL.Path) {
			httpx.Error(w, http.StatusForbidden, "password_change_required")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessKey, h)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil || u.Role != "admin" {
			httpx.Error(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) setCookie(w http.ResponseWriter, tok string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: !s.Cfg.InsecureCookie, SameSite: http.SameSiteStrictMode,
	})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !httpx.SameOrigin(r) {
		httpx.Error(w, http.StatusForbidden, "cross-origin request refused")
		return
	}
	var req loginReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	ip := httpx.ClientIP(r, s.Cfg.TrustedProxies)
	if wait := s.Limiter.Check(ip, req.Username); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		httpx.Error(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	if len(req.Password) > MaxPasswordLen || len(req.Username) > 100 {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.Store.UserByName(req.Username)
	hash := s.dummyHash
	if err == nil {
		hash = u.PasswordHash
	}
	ok, rehash, verr := VerifyPassword(req.Password, hash)
	if err != nil || verr != nil || !ok || u.Disabled {
		s.Limiter.Fail(ip, req.Username)
		s.Log.Warn("login failed", "user", req.Username, "ip", ip)
		httpx.Error(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.Limiter.Success(ip, req.Username)
	if rehash {
		if nh, err := HashPassword(req.Password); err == nil {
			s.Store.SetPassword(u.ID, nh, u.MustChangePassword)
		}
	}
	tok, err := newToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.Store.CreateSession(hashToken(tok), u.ID, s.Cfg.SessionMax, ip, r.UserAgent()); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Store.TouchLogin(u.ID)
	s.setCookie(w, tok, int(s.Cfg.SessionMax.Seconds()))
	s.Log.Info("login", "user", u.Username, "ip", ip)
	httpx.JSON(w, http.StatusOK, u)
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	s.Store.DeleteSession(sessionHashFrom(r.Context()))
	s.setCookie(w, "", -1)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, UserFrom(r.Context()))
}

type changePwReq struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

func (s *Service) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePwReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	u := UserFrom(r.Context())
	if len(req.Current) > MaxPasswordLen {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ok, _, err := VerifyPassword(req.Current, u.PasswordHash)
	if err != nil || !ok {
		httpx.Error(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	if err := ValidatePassword(req.New); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.New == req.Current {
		httpx.Error(w, http.StatusBadRequest, "new password must differ from the current one")
		return
	}
	h, err := HashPassword(req.New)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.Store.SetPassword(u.ID, h, false); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Store.DeleteUserSessions(u.ID, sessionHashFrom(r.Context()))
	s.Log.Info("password changed", "user", u.Username)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// BootstrapRoot creates the root admin from ROOT_USERNAME/ROOT_PASSWORD when
// the users table is empty. The password is stored hashed and must be changed
// at first login.
func (s *Service) BootstrapRoot() error {
	n, err := s.Store.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if s.Cfg.RootUsername == "" || s.Cfg.RootPassword == "" {
		return errors.New("no users exist: set ROOT_USERNAME and ROOT_PASSWORD for first start")
	}
	if err := ValidatePassword(s.Cfg.RootPassword); err != nil {
		return errors.New("ROOT_PASSWORD: " + err.Error())
	}
	h, err := HashPassword(s.Cfg.RootPassword)
	if err != nil {
		return err
	}
	if _, err := s.Store.CreateUser(s.Cfg.RootUsername, h, "admin", true, true); err != nil {
		return err
	}
	s.Log.Warn("root account created; remove ROOT_PASSWORD from the environment", "user", s.Cfg.RootUsername)
	return nil
}
