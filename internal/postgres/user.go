package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/user"
)

var (
	_ user.Store            = (*UserStore)(nil)
	_ user.SessionStore     = (*SessionStore)(nil)
	_ user.AntiforgeryStore = (*AntiforgeryStore)(nil)
)

const firstAdministratorLockKey int64 = 7355608013

// UserStore persists local users.
type UserStore struct {
	pool *pgxpool.Pool
}

// Users returns the local-user persistence adapter.
func (database *DB) Users() *UserStore {
	return &UserStore{pool: database.pool}
}

// AnyUsersExist reports whether setup has already created any local user.
func (store *UserStore) AnyUsersExist(ctx context.Context) (bool, error) {
	var exists bool
	if err := store.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users)").Scan(&exists); err != nil {
		return false, fmt.Errorf("check for users: %w", err)
	}
	return exists, nil
}

// FindByID loads a local user so a persisted session can restore its principal.
func (store *UserStore) FindByID(ctx context.Context, id user.ID) (user.User, bool, error) {
	return scanUser(store.pool.QueryRow(ctx, `
SELECT id, email, display_name, role, password_hash, created_at
FROM users
WHERE id = $1`, string(id)))
}

// FindByEmail loads a local user by normalized email.
func (store *UserStore) FindByEmail(ctx context.Context, email string) (user.User, bool, error) {
	return scanUser(store.pool.QueryRow(ctx, `
SELECT id, email, display_name, role, password_hash, created_at
FROM users
WHERE email = $1`, email))
}

// CreateFirstAdministrator serializes bootstrap and inserts exactly one user.
func (store *UserStore) CreateFirstAdministrator(ctx context.Context, account user.User) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin first-administrator creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", firstAdministratorLockKey); err != nil {
		return fmt.Errorf("acquire first-administrator advisory lock: %w", err)
	}
	var exists bool
	if err := transaction.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users)").Scan(&exists); err != nil {
		return fmt.Errorf("re-check for users after first-administrator lock: %w", err)
	}
	if exists {
		return user.ErrSetupAlreadyCompleted
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO users (id, email, display_name, role, password_hash, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`, string(account.ID), account.Email, account.DisplayName,
		account.Role, account.PasswordHash, account.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("insert first administrator: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit first-administrator creation: %w", err)
	}
	return nil
}

// UpdatePasswordHash atomically replaces one user's password hash.
func (store *UserStore) UpdatePasswordHash(ctx context.Context, id user.ID, passwordHash string) error {
	result, err := store.pool.Exec(ctx, `
UPDATE users
SET password_hash = $2
WHERE id = $1`, string(id), passwordHash)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("update password hash: user %s does not exist", id)
	}
	return nil
}

func scanUser(row rowScanner) (user.User, bool, error) {
	var (
		id           string
		email        string
		displayName  string
		role         string
		passwordHash string
		createdAt    time.Time
	)
	if err := row.Scan(&id, &email, &displayName, &role, &passwordHash, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, false, nil
		}
		return user.User{}, false, fmt.Errorf("scan user: %w", err)
	}
	account, err := user.NewUser(user.ID(id), email, displayName, role, passwordHash, createdAt.UTC())
	if err != nil {
		return user.User{}, false, fmt.Errorf("restore user: %w", err)
	}
	return account, true, nil
}

// SessionStore persists fixed-expiry sessions by SHA-256 token hash.
type SessionStore struct {
	pool *pgxpool.Pool
}

// Sessions returns the server-side session persistence adapter.
func (database *DB) Sessions() *SessionStore {
	return &SessionStore{pool: database.pool}
}

// Create inserts a server-side session without persisting raw token material.
func (store *SessionStore) Create(ctx context.Context, session user.Session) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin session creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `
DELETE FROM sessions
WHERE expires_at <= $1`, session.AuthenticatedAt.UTC()); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO sessions (token_hash, user_id, authenticated_at, expires_at)
VALUES ($1, $2, $3, $4)`, session.TokenHash[:], string(session.UserID),
		session.AuthenticatedAt.UTC(), session.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit session creation: %w", err)
	}
	return nil
}

// FindByTokenHash loads a session without extending its fixed expiry.
func (store *SessionStore) FindByTokenHash(ctx context.Context, tokenHash user.TokenHash) (user.Session, bool, error) {
	var (
		storedHash      []byte
		userID          string
		authenticatedAt time.Time
		expiresAt       time.Time
	)
	if err := store.pool.QueryRow(ctx, `
SELECT token_hash, user_id, authenticated_at, expires_at
FROM sessions
WHERE token_hash = $1`, tokenHash[:]).Scan(&storedHash, &userID, &authenticatedAt, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.Session{}, false, nil
		}
		return user.Session{}, false, fmt.Errorf("scan session: %w", err)
	}
	restoredHash, err := restoreTokenHash(storedHash)
	if err != nil {
		return user.Session{}, false, fmt.Errorf("restore session token hash: %w", err)
	}
	session, err := user.RestoreSession(restoredHash, user.ID(userID), authenticatedAt.UTC(), expiresAt.UTC())
	if err != nil {
		return user.Session{}, false, fmt.Errorf("restore session: %w", err)
	}
	return session, true, nil
}

// DeleteByTokenHash invalidates a server-side session.
func (store *SessionStore) DeleteByTokenHash(ctx context.Context, tokenHash user.TokenHash) error {
	if _, err := store.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash[:]); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// AntiforgeryStore persists one anonymous MAC key and session-bound synchronizer-token state.
type AntiforgeryStore struct {
	pool *pgxpool.Pool
}

// Antiforgery returns the antiforgery persistence adapter.
func (database *DB) Antiforgery() *AntiforgeryStore {
	return &AntiforgeryStore{pool: database.pool}
}

// GetOrCreateAnonymousAntiforgeryKey atomically initializes the singleton key across replicas.
func (store *AntiforgeryStore) GetOrCreateAnonymousAntiforgeryKey(ctx context.Context, candidate user.AnonymousAntiforgeryKey) (user.AnonymousAntiforgeryKey, error) {
	if _, err := store.pool.Exec(ctx, `
INSERT INTO anonymous_antiforgery_keys (id, key_material)
VALUES (1, $1)
ON CONFLICT (id) DO NOTHING`, candidate[:]); err != nil {
		return user.AnonymousAntiforgeryKey{}, fmt.Errorf("initialize anonymous antiforgery key: %w", err)
	}
	key, found, err := store.FindAnonymousAntiforgeryKey(ctx)
	if err != nil {
		return user.AnonymousAntiforgeryKey{}, err
	}
	if !found {
		return user.AnonymousAntiforgeryKey{}, errors.New("anonymous antiforgery key was not initialized")
	}
	return key, nil
}

// FindAnonymousAntiforgeryKey loads the singleton key shared by all API replicas.
func (store *AntiforgeryStore) FindAnonymousAntiforgeryKey(ctx context.Context) (user.AnonymousAntiforgeryKey, bool, error) {
	var material []byte
	if err := store.pool.QueryRow(ctx, `
SELECT key_material
FROM anonymous_antiforgery_keys
WHERE id = 1`).Scan(&material); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.AnonymousAntiforgeryKey{}, false, nil
		}
		return user.AnonymousAntiforgeryKey{}, false, fmt.Errorf("scan anonymous antiforgery key: %w", err)
	}
	key, err := restoreAnonymousAntiforgeryKey(material)
	if err != nil {
		return user.AnonymousAntiforgeryKey{}, false, fmt.Errorf("restore anonymous antiforgery key: %w", err)
	}
	return key, true, nil
}

// CreateSessionAntiforgery stores only hashes of the cookie selector and request token.
func (store *AntiforgeryStore) CreateSessionAntiforgery(ctx context.Context, record user.SessionAntiforgeryRecord) error {
	if _, err := store.pool.Exec(ctx, `
INSERT INTO antiforgery_tokens (selector_hash, request_token_hash, session_token_hash, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (session_token_hash) DO UPDATE SET
    selector_hash = EXCLUDED.selector_hash,
    request_token_hash = EXCLUDED.request_token_hash,
    expires_at = EXCLUDED.expires_at`, record.SelectorHash[:], record.RequestTokenHash[:], record.SessionTokenHash[:], record.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("insert antiforgery state: %w", err)
	}
	return nil
}

// FindSessionAntiforgeryBySelectorHash loads session-bound state by its cookie selector hash.
func (store *AntiforgeryStore) FindSessionAntiforgeryBySelectorHash(ctx context.Context, selectorHash user.TokenHash) (user.SessionAntiforgeryRecord, bool, error) {
	var (
		storedSelector []byte
		requestHash    []byte
		sessionHash    []byte
		expiresAt      time.Time
	)
	if err := store.pool.QueryRow(ctx, `
SELECT selector_hash, request_token_hash, session_token_hash, expires_at
FROM antiforgery_tokens
WHERE selector_hash = $1`, selectorHash[:]).Scan(&storedSelector, &requestHash, &sessionHash, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.SessionAntiforgeryRecord{}, false, nil
		}
		return user.SessionAntiforgeryRecord{}, false, fmt.Errorf("scan antiforgery state: %w", err)
	}

	selector, err := restoreTokenHash(storedSelector)
	if err != nil {
		return user.SessionAntiforgeryRecord{}, false, fmt.Errorf("restore antiforgery selector hash: %w", err)
	}
	request, err := restoreTokenHash(requestHash)
	if err != nil {
		return user.SessionAntiforgeryRecord{}, false, fmt.Errorf("restore antiforgery request hash: %w", err)
	}
	binding, err := restoreTokenHash(sessionHash)
	if err != nil {
		return user.SessionAntiforgeryRecord{}, false, fmt.Errorf("restore antiforgery session hash: %w", err)
	}
	record, err := user.NewSessionAntiforgeryRecord(selector, request, binding, expiresAt.UTC())
	if err != nil {
		return user.SessionAntiforgeryRecord{}, false, fmt.Errorf("restore session antiforgery state: %w", err)
	}
	return record, true, nil
}

// DeleteSessionAntiforgeryBySelectorHash invalidates session-bound antiforgery state.
func (store *AntiforgeryStore) DeleteSessionAntiforgeryBySelectorHash(ctx context.Context, selectorHash user.TokenHash) error {
	if _, err := store.pool.Exec(ctx, "DELETE FROM antiforgery_tokens WHERE selector_hash = $1", selectorHash[:]); err != nil {
		return fmt.Errorf("delete antiforgery state: %w", err)
	}
	return nil
}

func restoreTokenHash(value []byte) (user.TokenHash, error) {
	if len(value) != len(user.TokenHash{}) {
		return user.TokenHash{}, fmt.Errorf("expected 32 bytes, found %d", len(value))
	}
	var result user.TokenHash
	copy(result[:], value)
	return result, nil
}

func restoreAnonymousAntiforgeryKey(value []byte) (user.AnonymousAntiforgeryKey, error) {
	if len(value) != len(user.AnonymousAntiforgeryKey{}) {
		return user.AnonymousAntiforgeryKey{}, fmt.Errorf("expected 32 bytes, found %d", len(value))
	}
	var result user.AnonymousAntiforgeryKey
	copy(result[:], value)
	return result, nil
}
