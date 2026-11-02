// Package app assembles the HTTP server.
package app

import (
	"net/http"

	"lfu/internal/auth"
	"lfu/internal/httpx"
	"lfu/internal/uploads"
)

func NewHandler(a *auth.Service, m *uploads.Manager) http.Handler {
	mux := http.NewServeMux()
	prot := a.Authenticate
	admin := func(h http.HandlerFunc) http.Handler { return prot(auth.RequireAdmin(h)) }

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/auth/login", a.Login)
	mux.Handle("POST /api/auth/logout", prot(http.HandlerFunc(a.Logout)))
	mux.Handle("GET /api/auth/me", prot(http.HandlerFunc(a.Me)))
	mux.Handle("POST /api/auth/password", prot(http.HandlerFunc(a.ChangePassword)))

	mux.Handle("GET /api/admin/users", admin(a.AdminListUsers))
	mux.Handle("POST /api/admin/users", admin(a.AdminCreateUser))
	mux.Handle("PATCH /api/admin/users/{id}", admin(a.AdminPatchUser))
	mux.Handle("DELETE /api/admin/users/{id}", admin(a.AdminDeleteUser))

	mux.Handle("GET /api/config", prot(http.HandlerFunc(m.Config)))
	mux.Handle("GET /api/uploads", prot(http.HandlerFunc(m.List)))
	mux.Handle("GET /api/uploads/{id}", prot(http.HandlerFunc(m.Get)))
	mux.Handle("POST /api/uploads/{id}/sha256", prot(http.HandlerFunc(m.SubmitSHA256)))
	mux.Handle("DELETE /api/uploads/{id}", prot(http.HandlerFunc(m.Delete)))

	mux.Handle("/files/", prot(m.Files()))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not found")
	})
	return secureHeaders(mux)
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
