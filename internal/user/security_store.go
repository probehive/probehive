package user

import (
	"context"
	"errors"
	"time"
)

const SessionLifetime = 12 * time.Hour

// TokenHash is a SHA-256 digest of opaque token material. Raw browser tokens must never use this type.
type TokenHash [32]byte

// Session is a fixed-expiry server-side browser session. It stores only the cookie token hash.
type Session struct {
	TokenHash       TokenHash
	UserID          ID
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
}

// NewSession creates a session with the contractually fixed 12-hour lifetime.
func NewSession(tokenHash TokenHash, userID ID, authenticatedAt time.Time) (Session, error) {
	return RestoreSession(tokenHash, userID, authenticatedAt, authenticatedAt.Add(SessionLifetime))
}

// RestoreSession validates a Session loaded from persistence.
func RestoreSession(tokenHash TokenHash, userID ID, authenticatedAt, expiresAt time.Time) (Session, error) {
	if tokenHash == (TokenHash{}) {
		return Session{}, errors.New("a session requires a token hash")
	}
	if userID == "" {
		return Session{}, errors.New("a session requires a user id")
	}
	if !isUTC(authenticatedAt) || !isUTC(expiresAt) {
		return Session{}, errors.New("persisted timestamps must be UTC")
	}
	if !expiresAt.Equal(authenticatedAt.Add(SessionLifetime)) {
		return Session{}, errors.New("a session requires a fixed 12-hour lifetime")
	}
	return Session{
		TokenHash: tokenHash, UserID: userID,
		AuthenticatedAt: authenticatedAt, ExpiresAt: expiresAt,
	}, nil
}

// Expired reports whether the fixed server-side expiry has passed. It never renews the Session.
func (session Session) Expired(now time.Time) bool {
	return !now.Before(session.ExpiresAt)
}

// SessionStore persists fixed-expiry sessions by cryptographic token hash.
type SessionStore interface {
	Create(context.Context, Session) error
	FindByTokenHash(context.Context, TokenHash) (Session, bool, error)
	DeleteByTokenHash(context.Context, TokenHash) error
}

// AnonymousAntiforgeryKey is persistent HMAC-SHA-256 key material shared by API replicas.
// It signs anonymous pre-authentication tokens without persisting those tokens.
type AnonymousAntiforgeryKey [32]byte

// SessionAntiforgeryRecord contains only hashes of a cookie selector and request token.
// It is bound to exactly one authenticated session.
type SessionAntiforgeryRecord struct {
	SelectorHash     TokenHash
	RequestTokenHash TokenHash
	SessionTokenHash TokenHash
	ExpiresAt        time.Time
}

// NewSessionAntiforgeryRecord creates state bound to one authenticated session hash.
func NewSessionAntiforgeryRecord(selectorHash, requestTokenHash, sessionTokenHash TokenHash, expiresAt time.Time) (SessionAntiforgeryRecord, error) {
	if selectorHash == (TokenHash{}) || requestTokenHash == (TokenHash{}) {
		return SessionAntiforgeryRecord{}, errors.New("antiforgery state requires selector and request-token hashes")
	}
	if sessionTokenHash == (TokenHash{}) {
		return SessionAntiforgeryRecord{}, errors.New("authenticated antiforgery state requires a session-token hash")
	}
	if !isUTC(expiresAt) {
		return SessionAntiforgeryRecord{}, errors.New("persisted timestamps must be UTC")
	}
	return SessionAntiforgeryRecord{
		SelectorHash: selectorHash, RequestTokenHash: requestTokenHash,
		SessionTokenHash: sessionTokenHash, ExpiresAt: expiresAt,
	}, nil
}

// BoundTo reports whether the record belongs to the supplied authenticated session.
func (record SessionAntiforgeryRecord) BoundTo(sessionTokenHash TokenHash) bool {
	return record.SessionTokenHash == sessionTokenHash
}

// Expired reports whether antiforgery state is no longer usable.
func (record SessionAntiforgeryRecord) Expired(now time.Time) bool {
	return !now.Before(record.ExpiresAt)
}

// AnonymousAntiforgeryKeyStore persists one shared key for constant-space anonymous tokens.
type AnonymousAntiforgeryKeyStore interface {
	GetOrCreateAnonymousAntiforgeryKey(context.Context, AnonymousAntiforgeryKey) (AnonymousAntiforgeryKey, error)
	FindAnonymousAntiforgeryKey(context.Context) (AnonymousAntiforgeryKey, bool, error)
}

// SessionAntiforgeryStore persists session-bound state by hashed selector.
type SessionAntiforgeryStore interface {
	CreateSessionAntiforgery(context.Context, SessionAntiforgeryRecord) error
	FindSessionAntiforgeryBySelectorHash(context.Context, TokenHash) (SessionAntiforgeryRecord, bool, error)
	DeleteSessionAntiforgeryBySelectorHash(context.Context, TokenHash) error
}

// AntiforgeryStore combines anonymous key and authenticated synchronizer-token persistence.
type AntiforgeryStore interface {
	AnonymousAntiforgeryKeyStore
	SessionAntiforgeryStore
}
