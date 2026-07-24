package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	credentialWindow        = time.Minute
	maxCredentialPartitions = 4096
)

type credentialLimiter struct {
	mu          sync.Mutex
	permits     int
	now         func() time.Time
	entries     map[string]credentialWindowState
	overflow    credentialWindowState
	nextCleanup time.Time
}

type credentialWindowState struct {
	started time.Time
	limiter *rate.Limiter
}

func newCredentialLimiter(permits int, now func() time.Time) *credentialLimiter {
	return &credentialLimiter{permits: permits, now: now, entries: make(map[string]credentialWindowState)}
}

func (limiter *credentialLimiter) allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := limiter.now()
	limiter.cleanup(now)
	entry, found := limiter.entries[key]
	if !found {
		overflow, active := limiter.currentWindow(limiter.overflow, now)
		// Once overflow begins, unknown keys stay in that window until it
		// expires, even if cleanup makes room in the normal table.
		if active || len(limiter.entries) >= maxCredentialPartitions {
			limiter.overflow = overflow
			return limiter.overflow.limiter.AllowN(now, 1)
		}
	}
	if !found || now.Sub(entry.started) >= credentialWindow || now.Before(entry.started) {
		entry, _ = limiter.currentWindow(credentialWindowState{}, now)
		limiter.entries[key] = entry
	}
	return entry.limiter.AllowN(now, 1)
}

func (limiter *credentialLimiter) currentWindow(
	entry credentialWindowState,
	now time.Time,
) (credentialWindowState, bool) {
	if entry.limiter != nil && now.Sub(entry.started) < credentialWindow && !now.Before(entry.started) {
		return entry, true
	}
	return credentialWindowState{
		started: now,
		// One token could refill only at the end of this window; replacing the
		// limiter then restores the complete fixed-window allowance.
		limiter: rate.NewLimiter(rate.Every(credentialWindow), limiter.permits),
	}, false
}

func (limiter *credentialLimiter) cleanup(now time.Time) {
	lastCleanup := limiter.nextCleanup.Add(-credentialWindow)
	if !limiter.nextCleanup.IsZero() && now.Before(limiter.nextCleanup) && !now.Before(lastCleanup) {
		return
	}
	for candidate, state := range limiter.entries {
		if now.Sub(state.started) >= credentialWindow || now.Before(state.started) {
			delete(limiter.entries, candidate)
		}
	}
	limiter.nextCleanup = now.Add(credentialWindow)
}

func credentialPartition(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if net.ParseIP(r.RemoteAddr) != nil {
		return r.RemoteAddr
	}
	return "unknown"
}
