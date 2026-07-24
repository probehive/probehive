package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/user"
)

const (
	sessionCookieName        = "probehive.session"
	antiforgeryCookieName    = "probehive.antiforgery"
	antiforgeryHeaderName    = "X-ProbeHive-Antiforgery"
	tokenBytes               = 32
	anonymousTimestampBytes  = 8
	anonymousClockSkew       = time.Minute
	anonymousMACPurpose      = "probehive/anonymous-antiforgery/v1\x00"
	antiforgeryInvalidTitle  = "Antiforgery token missing or invalid"
	antiforgeryInvalidDetail = "Unsafe requests require the antiforgery request token in the custom header; obtain it from GET /api/v1/auth/antiforgery."
	originRejectedTitle      = "Browser origin rejected"
	originRejectedDetail     = "The Origin or Referer of this request does not match the request authority."
)

type authenticatedSession struct {
	account user.User
	record  user.Session
}

func (server *Server) loadSession(r *http.Request) (*authenticatedSession, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return nil, nil
	}
	if err != nil {
		return nil, nil
	}
	raw, ok := decodeOpaqueToken(cookie.Value)
	if !ok {
		return nil, nil
	}
	hash := hashToken(raw)
	record, found, err := server.sessions.FindByTokenHash(r.Context(), hash)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if record.Expired(server.clock.Now().UTC()) {
		if err := server.sessions.DeleteByTokenHash(r.Context(), hash); err != nil {
			return nil, err
		}
		return nil, nil
	}
	account, found, err := server.users.Get(r.Context(), record.UserID)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := server.sessions.DeleteByTokenHash(r.Context(), hash); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return &authenticatedSession{account: account, record: record}, nil
}

func (server *Server) requireAuthentication(w http.ResponseWriter, r *http.Request) (*authenticatedSession, bool) {
	principal, err := server.loadSession(r)
	if err != nil {
		server.internalError(w, r, "load session", err)
		return nil, false
	}
	if principal == nil {
		writeStatusProblem(w, http.StatusUnauthorized)
		return nil, false
	}
	return principal, true
}

func (server *Server) requireAdministrator(w http.ResponseWriter, r *http.Request) (*authenticatedSession, bool) {
	principal, ok := server.requireAuthentication(w, r)
	if !ok {
		return nil, false
	}
	if principal.account.Role != user.AdministratorRole {
		writeStatusProblem(w, http.StatusForbidden)
		return nil, false
	}
	return principal, true
}

func (server *Server) protectUnsafe(w http.ResponseWriter, r *http.Request, administrator bool) (*authenticatedSession, bool) {
	var principal *authenticatedSession
	var ok bool
	if administrator {
		principal, ok = server.requireAdministrator(w, r)
	} else {
		principal, ok = server.requireAuthentication(w, r)
	}
	if !ok {
		return nil, false
	}
	if !server.acceptableBrowserOrigin(r) {
		writeProblem(w, http.StatusForbidden, originRejectedTitle, originRejectedDetail)
		return nil, false
	}
	valid, err := server.validAntiforgery(r, principal)
	if err != nil {
		server.internalError(w, r, "validate antiforgery state", err)
		return nil, false
	}
	if !valid {
		writeProblem(w, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)
		return nil, false
	}
	return principal, true
}

func (server *Server) prepareCredentialUnsafe(w http.ResponseWriter, r *http.Request) (*authenticatedSession, bool) {
	principal, err := server.loadSession(r)
	if err != nil {
		server.internalError(w, r, "load optional session", err)
		return nil, false
	}
	if !server.credentials.allow(credentialPartition(r)) {
		writeStatusProblem(w, http.StatusTooManyRequests)
		return nil, false
	}
	if !server.acceptableBrowserOrigin(r) {
		writeProblem(w, http.StatusForbidden, originRejectedTitle, originRejectedDetail)
		return nil, false
	}
	valid, err := server.validAntiforgery(r, principal)
	if err != nil {
		server.internalError(w, r, "validate antiforgery state", err)
		return nil, false
	}
	if !valid {
		writeProblem(w, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)
		return nil, false
	}
	return principal, true
}

func (server *Server) createSession(w http.ResponseWriter, r *http.Request, account user.User) error {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if raw, ok := decodeOpaqueToken(cookie.Value); ok {
			if err := server.sessions.DeleteByTokenHash(r.Context(), hashToken(raw)); err != nil {
				return err
			}
		}
	}
	raw, encoded, err := server.newOpaqueToken()
	if err != nil {
		return err
	}
	now := server.clock.Now().UTC()
	record, err := user.NewSession(hashToken(raw), account.ID, now)
	if err != nil {
		return err
	}
	if err := server.sessions.Create(r.Context(), record); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: encoded, Path: "/", HttpOnly: true,
		Secure: server.secureCookie(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (server *Server) expireSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: server.secureCookie(r), SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0).UTC(),
	})
}

func (server *Server) issueAntiforgery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	principal, err := server.loadSession(r)
	if err != nil {
		server.internalError(w, r, "load optional session", err)
		return
	}
	now := server.clock.Now().UTC()
	var selectorRaw, requestRaw []byte
	var selector, requestToken string
	if principal == nil {
		selectorRaw, selector, requestToken, err = server.newAnonymousAntiforgery(r.Context(), now)
		if err != nil {
			server.internalError(w, r, "generate anonymous antiforgery token", err)
			return
		}
	} else {
		selectorRaw, selector, err = server.newOpaqueToken()
		if err == nil {
			requestRaw, requestToken, err = server.newOpaqueToken()
		}
		if err != nil {
			server.internalError(w, r, "generate session antiforgery token", err)
			return
		}
	}
	if cookie, cookieErr := r.Cookie(antiforgeryCookieName); cookieErr == nil {
		if oldRaw, ok := decodeOpaqueToken(cookie.Value); ok {
			if err := server.antiforgery.DeleteSessionAntiforgeryBySelectorHash(r.Context(), hashToken(oldRaw)); err != nil {
				server.internalError(w, r, "rotate antiforgery state", err)
				return
			}
		}
	}
	if principal != nil {
		record, recordErr := user.NewSessionAntiforgeryRecord(
			hashToken(selectorRaw), hashToken(requestRaw), principal.record.TokenHash, principal.record.ExpiresAt,
		)
		if recordErr != nil {
			server.internalError(w, r, "construct antiforgery state", recordErr)
			return
		}
		if err := server.antiforgery.CreateSessionAntiforgery(r.Context(), record); err != nil {
			server.internalError(w, r, "persist antiforgery state", err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: antiforgeryCookieName, Value: selector, Path: "/", HttpOnly: true,
		Secure: server.secureCookie(r), SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, api.AntiforgeryTokenResponse{
		HeaderName: antiforgeryHeaderName, RequestToken: requestToken,
	})
}

func (server *Server) validAntiforgery(r *http.Request, principal *authenticatedSession) (bool, error) {
	cookie, err := r.Cookie(antiforgeryCookieName)
	if err != nil {
		return false, nil
	}
	selectorRaw, ok := decodeOpaqueToken(cookie.Value)
	if !ok {
		return false, nil
	}
	headerValues := r.Header.Values(antiforgeryHeaderName)
	if len(headerValues) != 1 {
		return false, nil
	}
	requestRaw, ok := decodeOpaqueToken(headerValues[0])
	if !ok {
		return false, nil
	}
	if principal == nil {
		key, found, err := server.antiforgery.FindAnonymousAntiforgeryKey(r.Context())
		if err != nil || !found {
			return false, err
		}
		return validAnonymousAntiforgery(key, selectorRaw, requestRaw, server.clock.Now().UTC()), nil
	}

	record, found, err := server.antiforgery.FindSessionAntiforgeryBySelectorHash(r.Context(), hashToken(selectorRaw))
	if err != nil || !found {
		return false, err
	}
	if record.Expired(server.clock.Now().UTC()) {
		if err := server.antiforgery.DeleteSessionAntiforgeryBySelectorHash(r.Context(), record.SelectorHash); err != nil {
			return false, err
		}
		return false, nil
	}
	requestHash := hashToken(requestRaw)
	if subtle.ConstantTimeCompare(requestHash[:], record.RequestTokenHash[:]) != 1 {
		return false, nil
	}
	return subtle.ConstantTimeCompare(principal.record.TokenHash[:], record.SessionTokenHash[:]) == 1, nil
}

func (server *Server) newAnonymousAntiforgery(ctx context.Context, issuedAt time.Time) ([]byte, string, string, error) {
	var candidate user.AnonymousAntiforgeryKey
	if _, err := io.ReadFull(server.random, candidate[:]); err != nil {
		return nil, "", "", err
	}
	key, err := server.antiforgery.GetOrCreateAnonymousAntiforgeryKey(ctx, candidate)
	if err != nil {
		return nil, "", "", err
	}

	timestamp := issuedAt.UnixNano()
	if !time.Unix(0, timestamp).UTC().Equal(issuedAt) {
		return nil, "", "", errors.New("anonymous antiforgery issuance time is outside the encoded range")
	}
	selectorRaw := make([]byte, tokenBytes)
	binary.BigEndian.PutUint64(selectorRaw[:anonymousTimestampBytes], uint64(timestamp))
	if _, err := io.ReadFull(server.random, selectorRaw[anonymousTimestampBytes:]); err != nil {
		return nil, "", "", err
	}
	requestRaw := anonymousAntiforgeryMAC(key, selectorRaw)
	return selectorRaw, base64.RawURLEncoding.EncodeToString(selectorRaw),
		base64.RawURLEncoding.EncodeToString(requestRaw[:]), nil
}

func validAnonymousAntiforgery(key user.AnonymousAntiforgeryKey, selectorRaw, requestRaw []byte, now time.Time) bool {
	if len(selectorRaw) != tokenBytes || len(requestRaw) != sha256.Size {
		return false
	}
	expected := anonymousAntiforgeryMAC(key, selectorRaw)
	if !hmac.Equal(requestRaw, expected[:]) {
		return false
	}

	timestamp := int64(binary.BigEndian.Uint64(selectorRaw[:anonymousTimestampBytes]))
	issuedAt := time.Unix(0, timestamp).UTC()
	if issuedAt.After(now.Add(anonymousClockSkew)) {
		return false
	}
	return now.Before(issuedAt.Add(user.SessionLifetime))
}

func anonymousAntiforgeryMAC(key user.AnonymousAntiforgeryKey, selectorRaw []byte) [sha256.Size]byte {
	mac := hmac.New(sha256.New, key[:])
	_, _ = io.WriteString(mac, anonymousMACPurpose)
	_, _ = mac.Write(selectorRaw)
	var result [sha256.Size]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func (server *Server) newOpaqueToken() ([]byte, string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := io.ReadFull(server.random, raw); err != nil {
		return nil, "", err
	}
	return raw, base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeOpaqueToken(encoded string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != tokenBytes || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, false
	}
	return raw, true
}

func hashToken(raw []byte) user.TokenHash { return sha256.Sum256(raw) }

func (server *Server) secureCookie(r *http.Request) bool {
	return !server.development || r.TLS != nil
}

func (server *Server) acceptableBrowserOrigin(r *http.Request) bool {
	if origins := r.Header.Values("Origin"); len(origins) != 0 {
		if len(origins) != 1 {
			return false
		}
		if origins[0] != "" {
			return strings.EqualFold(origins[0], server.requestOrigin(r))
		}
	}
	if referers := r.Header.Values("Referer"); len(referers) != 0 {
		if len(referers) != 1 {
			return false
		}
		if referers[0] == "" {
			return true
		}
		referer, err := url.Parse(referers[0])
		if err != nil || !referer.IsAbs() || referer.Host == "" || referer.User != nil {
			return false
		}
		return strings.EqualFold(referer.Scheme+"://"+referer.Host, server.requestOrigin(r))
	}
	return true
}

func (server *Server) requestOrigin(r *http.Request) string {
	if server.publicOrigin != "" {
		return server.publicOrigin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func normalizePublicOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("public origin must be an absolute http or https origin without path, query, fragment, or user information")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
