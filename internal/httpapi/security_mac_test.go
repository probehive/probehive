package httpapi

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/user"
)

func TestAnonymousAntiforgeryMACExpiryAndIntegrity(t *testing.T) {
	t.Parallel()
	issuedAt := time.Date(2026, 7, 24, 1, 2, 3, 456, time.UTC)
	var key user.AnonymousAntiforgeryKey
	key[0] = 1
	selector, request := signedAnonymousAntiforgery(key, issuedAt, 7)

	if !validAnonymousAntiforgery(key, selector, request, issuedAt) {
		t.Fatal("fresh anonymous antiforgery token was rejected")
	}
	if !validAnonymousAntiforgery(key, selector, request, issuedAt.Add(user.SessionLifetime-time.Nanosecond)) {
		t.Fatal("anonymous antiforgery token expired before its fixed lifetime")
	}
	if validAnonymousAntiforgery(key, selector, request, issuedAt.Add(user.SessionLifetime)) {
		t.Fatal("anonymous antiforgery token remained valid at its expiry boundary")
	}

	tamperedRequest := append([]byte(nil), request...)
	tamperedRequest[0] ^= 1
	if validAnonymousAntiforgery(key, selector, tamperedRequest, issuedAt) {
		t.Fatal("tampered anonymous antiforgery request token was accepted")
	}
	tamperedSelector := append([]byte(nil), selector...)
	tamperedSelector[anonymousTimestampBytes] ^= 1
	if validAnonymousAntiforgery(key, tamperedSelector, request, issuedAt) {
		t.Fatal("tampered anonymous antiforgery selector was accepted")
	}

	futureSelector, futureRequest := signedAnonymousAntiforgery(key, issuedAt.Add(anonymousClockSkew+time.Nanosecond), 9)
	if validAnonymousAntiforgery(key, futureSelector, futureRequest, issuedAt) {
		t.Fatal("anonymous antiforgery token beyond the replica clock-skew bound was accepted")
	}
}

func TestAnonymousAntiforgeryIssuanceUsesConstantStoreSpace(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	client := &http.Client{}
	for range 32 {
		response := environment.request(t, client, http.MethodGet, "/api/v1/auth/antiforgery", "", "", "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("antiforgery status = %d, body %s", response.StatusCode, response.Body)
		}
		var token api.AntiforgeryTokenResponse
		if err := json.Unmarshal(response.Body, &token); err != nil || token.RequestToken == "" {
			t.Fatalf("antiforgery response = %#v, error %v", token, err)
		}
	}

	environment.antiforgery.mu.Lock()
	defer environment.antiforgery.mu.Unlock()
	if !environment.antiforgery.anonymousKeySet || environment.antiforgery.anonymousKeyCreations != 1 {
		t.Fatalf("anonymous key initialized %d times, set = %v", environment.antiforgery.anonymousKeyCreations, environment.antiforgery.anonymousKeySet)
	}
	if len(environment.antiforgery.entries) != 0 {
		t.Fatalf("anonymous issuance persisted %d per-token records", len(environment.antiforgery.entries))
	}
}

func TestAnonymousAntiforgeryWorksAcrossServersWithSharedKeyStore(t *testing.T) {
	shared := newMemoryAntiforgeryStore()
	first := newTestEnvironment(t, true, 0, func(config *Config) { config.Antiforgery = shared })
	second := newTestEnvironment(t, true, 0, func(config *Config) { config.Antiforgery = shared })

	issued := first.request(t, &http.Client{}, http.MethodGet, "/api/v1/auth/antiforgery", "", "", "")
	var token api.AntiforgeryTokenResponse
	if err := json.Unmarshal(issued.Body, &token); err != nil {
		t.Fatal(err)
	}
	cookie := findCookie(t, issued, antiforgeryCookieName)
	setup := second.request(
		t, &http.Client{}, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		"", token.RequestToken, cookie,
	)
	if setup.StatusCode != http.StatusCreated {
		t.Fatalf("cross-server setup status = %d, body %s", setup.StatusCode, setup.Body)
	}
}

func TestAuthenticatedAntiforgeryIssuanceReplacesSessionRecord(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	client := &http.Client{}
	issued := environment.request(t, client, http.MethodGet, "/api/v1/auth/antiforgery", "", "", "")
	var anonymous api.AntiforgeryTokenResponse
	if err := json.Unmarshal(issued.Body, &anonymous); err != nil {
		t.Fatal(err)
	}
	setup := environment.request(
		t, client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		"", anonymous.RequestToken, findCookie(t, issued, antiforgeryCookieName),
	)
	sessionCookie := findCookie(t, setup, sessionCookieName)
	for range 16 {
		response := environment.request(
			t, client, http.MethodGet, "/api/v1/auth/antiforgery", "", "", "", sessionCookie,
		)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("authenticated antiforgery status = %d, body %s", response.StatusCode, response.Body)
		}
	}

	environment.antiforgery.mu.Lock()
	defer environment.antiforgery.mu.Unlock()
	if len(environment.antiforgery.entries) != 1 {
		t.Fatalf("authenticated issuance retained %d records for one session", len(environment.antiforgery.entries))
	}
}

func signedAnonymousAntiforgery(key user.AnonymousAntiforgeryKey, issuedAt time.Time, nonce byte) ([]byte, []byte) {
	selector := make([]byte, tokenBytes)
	binary.BigEndian.PutUint64(selector[:anonymousTimestampBytes], uint64(issuedAt.UnixNano()))
	for index := anonymousTimestampBytes; index < len(selector); index++ {
		selector[index] = nonce + byte(index-anonymousTimestampBytes)
	}
	request := anonymousAntiforgeryMAC(key, selector)
	return selector, request[:]
}
