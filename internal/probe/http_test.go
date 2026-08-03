package probe

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/check"
	"github.com/probehive/probehive/internal/outbound"
)

// Every test here uses a local listener and a stub resolver. Nothing reaches public DNS or the
// public internet, as the outbound-access policy requires of outbound tests.

var testInstant = time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

type fixedClock struct{ instant time.Time }

func (clock fixedClock) Now() time.Time { return clock.instant }

// stubResolver answers every name with the same addresses, which is how a test points a host
// name at a local listener without a name server.
type stubResolver struct{ addresses []netip.Addr }

func resolving(addresses ...string) stubResolver {
	parsed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		parsed = append(parsed, netip.MustParseAddr(address))
	}
	return stubResolver{addresses: parsed}
}

func (resolver stubResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, nil
}

func newExecutor(t *testing.T, settings Settings, resolver outbound.Resolver, ports ...uint16) *HTTPExecutor {
	t.Helper()
	policy, err := outbound.NewPolicy(outbound.Spec{
		Profile:      outbound.ProfilePrivate,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		AllowedPorts: ports,
	})
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	executor, err := NewHTTPExecutor(outbound.NewDialer(policy, resolver, 5*time.Second), settings, fixedClock{instant: testInstant})
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	return executor
}

func serverPort(t *testing.T, server *httptest.Server) uint16 {
	t.Helper()
	address, err := netip.ParseAddrPort(strings.TrimPrefix(strings.TrimPrefix(server.URL, "http://"), "https://"))
	if err != nil {
		t.Fatalf("server address %q: %v", server.URL, err)
	}
	return address.Port()
}

func configurationFor(url string) check.HTTPConfiguration {
	return check.HTTPConfiguration{
		URL:             url,
		Method:          check.DefaultMethod,
		TimeoutSeconds:  check.DefaultTimeoutSeconds,
		FollowRedirects: true,
		MaxRedirects:    check.DefaultMaxRedirects,
	}
}

func requireFailure(t *testing.T, observation Observation, outcome Outcome, code string) {
	t.Helper()
	if observation.Outcome != outcome {
		t.Fatalf("outcome = %q, want %q (failure %+v)", observation.Outcome, outcome, observation.Failure)
	}
	if observation.Failure == nil {
		t.Fatalf("failure is nil for outcome %q", outcome)
	}
	if observation.Failure.Code != code {
		t.Fatalf("failure code = %q, want %q", observation.Failure.Code, code)
	}
}

func TestExecutePassesAndMeasuresAnExpectedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte("hello"))
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	observation := executor.Execute(t.Context(), configurationFor(server.URL))

	if observation.Outcome != OutcomePassed || observation.Failure != nil {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if observation.HTTP == nil {
		t.Fatal("no HTTP result")
	}
	if observation.HTTP.StatusCode != http.StatusOK || observation.HTTP.Protocol != "HTTP/1.1" {
		t.Fatalf("status %d over %q", observation.HTTP.StatusCode, observation.HTTP.Protocol)
	}
	if observation.HTTP.BodyBytes != 5 || observation.HTTP.BodyTruncated {
		t.Fatalf("body = %d bytes, truncated %t", observation.HTTP.BodyBytes, observation.HTTP.BodyTruncated)
	}
	if observation.HTTP.RedirectCount != 0 || observation.HTTP.TLS != nil {
		t.Fatalf("redirects = %d, TLS = %+v", observation.HTTP.RedirectCount, observation.HTTP.TLS)
	}
	if !observation.StartedAt.Equal(testInstant) || !observation.FinishedAt.Equal(testInstant) {
		t.Fatalf("instants %s..%s do not come from the injected clock", observation.StartedAt, observation.FinishedAt)
	}
	if observation.Duration <= 0 {
		t.Fatal("duration is not measured monotonically")
	}
	if observation.Phases.Connect <= 0 || observation.Phases.FirstByte <= 0 || observation.Phases.TLS != 0 {
		t.Fatalf("phases = %+v", observation.Phases)
	}
}

func TestExecuteSeparatesAnAssertionFailureFromAnExecutionFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	observation := executor.Execute(t.Context(), configurationFor(server.URL))
	requireFailure(t, observation, OutcomeFailed, CodeStatusUnexpected)
	if observation.HTTP == nil || observation.HTTP.StatusCode != http.StatusInternalServerError {
		t.Fatalf("HTTP result = %+v", observation.HTTP)
	}

	// The same response passes when the configuration expects it, which is what makes the
	// status assertion the target's answer rather than the executor's opinion.
	expected := configurationFor(server.URL)
	expected.ExpectedStatusCodes = []int{http.StatusInternalServerError}
	if observation := executor.Execute(t.Context(), expected); observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
}

func TestExecuteSendsTheConfiguredRequest(t *testing.T) {
	t.Parallel()
	received := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		received <- request.Clone(request.Context())
	}))
	t.Cleanup(server.Close)
	port := serverPort(t, server)
	executor := newExecutor(t, DefaultSettings(), resolving("127.0.0.1"), port)

	configuration := configurationFor(fmt.Sprintf("http://target.invalid:%d/status?deep=1", port))
	configuration.Method = "HEAD"
	configuration.Headers = []check.Header{{Name: "X-Probe", Value: "yes"}}
	if observation := executor.Execute(t.Context(), configuration); observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}

	request := <-received
	if request.Method != "HEAD" || request.URL.RequestURI() != "/status?deep=1" {
		t.Fatalf("request = %s %s", request.Method, request.URL.RequestURI())
	}
	// The connection went to the validated address while the intended host name stayed on
	// the request, as required for a policy-validated connection.
	if request.Host != fmt.Sprintf("target.invalid:%d", port) {
		t.Fatalf("Host = %q", request.Host)
	}
	if request.Header.Get("X-Probe") != "yes" || request.Header.Get("User-Agent") != DefaultUserAgent {
		t.Fatalf("headers = %v", request.Header)
	}
}

func TestExecuteLetsConfigurationReplaceTheUserAgent(t *testing.T) {
	t.Parallel()
	agents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		agents <- request.UserAgent()
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	configuration := configurationFor(server.URL)
	configuration.Headers = []check.Header{{Name: "User-Agent", Value: "Fleet/2"}}
	executor.Execute(t.Context(), configuration)

	if agent := <-agents; agent != "Fleet/2" {
		t.Fatalf("User-Agent = %q", agent)
	}
}

func TestExecuteFollowsRedirectsAndCountsThem(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/one":
			http.Redirect(writer, request, "/two", http.StatusFound)
		case "/two":
			http.Redirect(writer, request, "/three", http.StatusMovedPermanently)
		default:
			writer.Write([]byte("done"))
		}
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	observation := executor.Execute(t.Context(), configurationFor(server.URL+"/one"))

	if observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if observation.HTTP.RedirectCount != 2 || observation.HTTP.StatusCode != http.StatusOK {
		t.Fatalf("status %d after %d redirect(s)", observation.HTTP.StatusCode, observation.HTTP.RedirectCount)
	}
}

func TestExecuteAssertsTheRedirectItselfWhenFollowingIsDisabled(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Redirect(writer, request, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	configuration := configurationFor(server.URL)
	configuration.FollowRedirects = false
	observation := executor.Execute(t.Context(), configuration)

	requireFailure(t, observation, OutcomeFailed, CodeStatusUnexpected)
	if observation.HTTP.StatusCode != http.StatusFound || observation.HTTP.RedirectCount != 0 {
		t.Fatalf("status %d after %d redirect(s)", observation.HTTP.StatusCode, observation.HTTP.RedirectCount)
	}
	if count := requests.Load(); count != 1 {
		t.Fatalf("the server saw %d request(s)", count)
	}
}

func TestExecuteStopsWhenTheRedirectBudgetIsExhausted(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.Redirect(writer, request, "/again", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	configuration := configurationFor(server.URL)
	configuration.MaxRedirects = 2
	observation := executor.Execute(t.Context(), configuration)

	requireFailure(t, observation, OutcomeErrored, CodeRedirectBudgetExhausted)
	if count := requests.Load(); count != 3 {
		t.Fatalf("the server saw %d request(s), want the original and 2 redirects", count)
	}
}

func TestExecuteClampsTheRedirectBudgetToTheOperatorCeiling(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/again", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	settings := DefaultSettings()
	settings.MaxRedirects = 1
	executor := newExecutor(t, settings, nil, serverPort(t, server))

	// The stored configuration asks for the schema maximum; the operator ceiling wins.
	configuration := configurationFor(server.URL)
	configuration.MaxRedirects = check.MaxRedirects
	requireFailure(t, executor.Execute(t.Context(), configuration), OutcomeErrored, CodeRedirectBudgetExhausted)
}

func TestExecuteRevalidatesEveryRedirectAgainstThePolicy(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	// Port 80 is allowed so that the redirect is refused for its address rather than for
	// its port: the interesting denial is the metadata endpoint.
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server), 80)

	observation := executor.Execute(t.Context(), configurationFor(server.URL))

	requireFailure(t, observation, OutcomeErrored, string(outbound.ReasonAddressDenied))
	if observation.Failure.Class != outbound.ClassMetadata {
		t.Fatalf("class = %q, want %q", observation.Failure.Class, outbound.ClassMetadata)
	}
}

func TestExecuteRefusesADeniedDestinationWithoutConnecting(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	port := serverPort(t, server)
	// The name resolves to a metadata endpoint. The listener exists only to prove that
	// nothing reached it.
	executor := newExecutor(t, DefaultSettings(), resolving("169.254.169.254"), port)

	observation := executor.Execute(t.Context(), configurationFor(fmt.Sprintf("http://target.invalid:%d/", port)))

	requireFailure(t, observation, OutcomeErrored, string(outbound.ReasonAddressDenied))
	if observation.Failure.Class != outbound.ClassMetadata {
		t.Fatalf("class = %q, want %q", observation.Failure.Class, outbound.ClassMetadata)
	}
	if observation.HTTP != nil || observation.Phases.Connect != 0 {
		t.Fatalf("HTTP = %+v, phases = %+v", observation.HTTP, observation.Phases)
	}
	if count := requests.Load(); count != 0 {
		t.Fatalf("the listener saw %d request(s)", count)
	}
}

func TestExecuteRefusesADeniedPortBeforeResolving(t *testing.T) {
	t.Parallel()
	executor := newExecutor(t, DefaultSettings(), nil, 443)

	observation := executor.Execute(t.Context(), configurationFor("http://example.test:8080/"))

	requireFailure(t, observation, OutcomeErrored, string(outbound.ReasonPortDenied))
}

func TestExecuteTruncatesAnOversizedBodyInsteadOfFailing(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write(make([]byte, 4096))
	}))
	t.Cleanup(server.Close)
	settings := DefaultSettings()
	settings.MaxResponseBytes = 1024
	executor := newExecutor(t, settings, nil, serverPort(t, server))

	observation := executor.Execute(t.Context(), configurationFor(server.URL))

	if observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if observation.HTTP.BodyBytes != 1024 || !observation.HTTP.BodyTruncated {
		t.Fatalf("body = %d bytes, truncated %t", observation.HTTP.BodyBytes, observation.HTTP.BodyTruncated)
	}
}

func TestExecuteTimesOutAtTheOperatorCeiling(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	settings := DefaultSettings()
	settings.MaxTimeout = 200 * time.Millisecond
	executor := newExecutor(t, settings, nil, serverPort(t, server))

	// The stored configuration asks for 30 seconds; the operator ceiling decides.
	observation := executor.Execute(t.Context(), configurationFor(server.URL))

	requireFailure(t, observation, OutcomeTimedOut, CodeTimeout)
	if observation.Duration >= 5*time.Second {
		t.Fatalf("duration = %s, want the effective ceiling", observation.Duration)
	}
}

func TestExecuteReportsCallerCancellationAsCancelled(t *testing.T) {
	t.Parallel()
	var once sync.Once
	reached := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(reached) })
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-reached
		cancel()
	}()
	defer cancel()

	requireFailure(t, executor.Execute(ctx, configurationFor(server.URL)), OutcomeCancelled, CodeCancelled)
}

func TestExecuteRecordsTLSDetailOverAVerifiedConnection(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	settings := DefaultSettings()
	settings.RootCAs = roots
	executor := newExecutor(t, settings, nil, serverPort(t, server))

	observation := executor.Execute(t.Context(), configurationFor(server.URL))

	if observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if observation.HTTP.TLS == nil {
		t.Fatal("no TLS detail")
	}
	if !strings.HasPrefix(observation.HTTP.TLS.Version, "TLS ") || observation.HTTP.TLS.CipherSuite == "" {
		t.Fatalf("TLS = %+v", observation.HTTP.TLS)
	}
	if !observation.HTTP.TLS.CertificateExpiresAt.Equal(server.Certificate().NotAfter.UTC()) {
		t.Fatalf("certificate expiry = %s, want %s", observation.HTTP.TLS.CertificateExpiresAt, server.Certificate().NotAfter.UTC())
	}
	if observation.Phases.TLS <= 0 {
		t.Fatalf("phases = %+v", observation.Phases)
	}
}

func TestExecuteRejectsAnUntrustedCertificate(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	// No roots are supplied, so the test server's certificate is untrusted. There is no
	// setting that would skip the verification.
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	requireFailure(t, executor.Execute(t.Context(), configurationFor(server.URL)), OutcomeErrored, CodeCertificateInvalid)
}

func TestExecuteIgnoresProxyEnvironment(t *testing.T) {
	// No t.Parallel: this test sets environment variables.
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	// A proxy that would swallow the request if the transport read the environment. The
	// address is not in the policy either, so a proxied request could not even be dialed.
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:3128")
	t.Setenv("HTTPS_PROXY", "http://192.0.2.1:3128")
	executor := newExecutor(t, DefaultSettings(), nil, serverPort(t, server))

	if observation := executor.Execute(t.Context(), configurationFor(server.URL)); observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if count := requests.Load(); count != 1 {
		t.Fatalf("the server saw %d request(s)", count)
	}
}

func TestExecuteReportsAConnectionFailureAsErrored(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	port := serverPort(t, server)
	server.Close()
	executor := newExecutor(t, DefaultSettings(), nil, port)

	observation := executor.Execute(t.Context(), configurationFor(fmt.Sprintf("http://127.0.0.1:%d/", port)))

	requireFailure(t, observation, OutcomeErrored, string(outbound.ReasonConnectFailed))
}

func TestNewHTTPExecutorRequiresItsCollaborators(t *testing.T) {
	t.Parallel()
	if _, err := NewHTTPExecutor(nil, DefaultSettings(), fixedClock{instant: testInstant}); err == nil {
		t.Fatal("a missing dialer was accepted")
	}
	policy, err := outbound.NewPolicy(outbound.Spec{Profile: outbound.ProfileManaged})
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	if _, err := NewHTTPExecutor(outbound.NewDialer(policy, nil, time.Second), DefaultSettings(), nil); err == nil {
		t.Fatal("a missing clock was accepted")
	}
}

func TestSettingsFallBackToBuiltInCeilings(t *testing.T) {
	t.Parallel()
	// A zero ceiling means unconfigured, never unbounded.
	settings := Settings{}.normalized()
	if settings != DefaultSettings() {
		t.Fatalf("normalized zero settings = %+v", settings)
	}

	stricter := Settings{MaxTimeout: time.Second, MaxRedirects: 1, MaxResponseBytes: 16, MaxResponseHeaderBytes: 32, MaxTLSHandshake: time.Second, UserAgent: "Fleet/2"}
	if stricter.normalized() != stricter {
		t.Fatalf("normalized configured settings = %+v", stricter.normalized())
	}
}

func TestEffectiveLimitsTakeTheStricterValue(t *testing.T) {
	t.Parallel()
	settings := DefaultSettings()
	settings.MaxTimeout = 5 * time.Second
	settings.MaxRedirects = 3
	executor := newExecutor(t, settings, nil, 443)

	for _, testCase := range []struct {
		name      string
		seconds   int
		redirects int
		timeout   time.Duration
		budget    int
	}{
		{"user is stricter", 2, 1, 2 * time.Second, 1},
		{"operator is stricter", 60, 10, 5 * time.Second, 3},
		{"user asks for nothing", 0, 0, 5 * time.Second, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if timeout := executor.effectiveTimeout(testCase.seconds); timeout != testCase.timeout {
				t.Fatalf("timeout = %s, want %s", timeout, testCase.timeout)
			}
			if budget := executor.effectiveRedirects(testCase.redirects); budget != testCase.budget {
				t.Fatalf("redirect budget = %d, want %d", budget, testCase.budget)
			}
		})
	}
}
