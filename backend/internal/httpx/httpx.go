// Package httpx holds small HTTP helpers shared by handlers.
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func Error(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}

// Decode reads a bounded JSON body into v.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// ClientIP returns the client address. X-Real-IP is honoured only when the
// direct peer is inside one of the trusted proxy prefixes.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	for _, p := range trusted {
		if p.Contains(peer) {
			if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
				if a, err := netip.ParseAddr(xr); err == nil {
					return a.Unmap().String()
				}
			}
			break
		}
	}
	return peer.String()
}

// SameOrigin rejects cross-origin state-changing requests. A request without
// an Origin header (non-browser clients) is allowed; browsers always send one
// on cross-origin POST/PUT/PATCH/DELETE, and cookies are SameSite=Strict.
func SameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func IsSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
