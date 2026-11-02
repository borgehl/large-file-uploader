package auth

import (
	"strings"
	"sync"
	"time"
)

// Limiter throttles failed logins per (ip, username) and per ip.
// After freeAttempts failures the delay doubles per failure up to maxDelay.
type Limiter struct {
	mu           sync.Mutex
	entries      map[string]*entry
	freeAttempts int
	baseDelay    time.Duration
	maxDelay     time.Duration
	ipFreeBudget int
	now          func() time.Time
}

type entry struct {
	fails   int
	blocked time.Time
	last    time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{
		entries:      map[string]*entry{},
		freeAttempts: 5,
		baseDelay:    time.Second,
		maxDelay:     15 * time.Minute,
		ipFreeBudget: 30,
		now:          time.Now,
	}
}

func keyUser(ip, user string) string { return "u|" + ip + "|" + strings.ToLower(user) }
func keyIP(ip string) string         { return "i|" + ip }

// Check returns how long the caller must wait before trying (0 = go ahead).
func (l *Limiter) Check(ip, user string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range []string{keyUser(ip, user), keyIP(ip)} {
		if e, ok := l.entries[k]; ok && e.blocked.After(l.now()) {
			if d := e.blocked.Sub(l.now()); d > wait {
				wait = d
			}
		}
	}
	return wait
}

func (l *Limiter) Fail(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bump(keyUser(ip, user), l.freeAttempts)
	l.bump(keyIP(ip), l.ipFreeBudget)
	if len(l.entries) > 10000 {
		l.gc()
	}
}

func (l *Limiter) bump(k string, free int) {
	e := l.entries[k]
	if e == nil {
		e = &entry{}
		l.entries[k] = e
	}
	e.fails++
	e.last = l.now()
	if e.fails >= free {
		d := l.baseDelay << min(e.fails-free, 20)
		if d > l.maxDelay || d <= 0 {
			d = l.maxDelay
		}
		e.blocked = l.now().Add(d)
	}
}

func (l *Limiter) Success(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, keyUser(ip, user))
}

func (l *Limiter) gc() {
	cutoff := l.now().Add(-time.Hour)
	for k, e := range l.entries {
		if e.last.Before(cutoff) && e.blocked.Before(l.now()) {
			delete(l.entries, k)
		}
	}
}
