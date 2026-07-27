package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/probehive/probehive/internal/check"
	"github.com/probehive/probehive/internal/outbound"
)

// DefaultUserAgent identifies the probe to the target it requests. A monitoring request that
// announces itself is one a target's operator can recognize in a log and allow deliberately.
// Check configuration may replace it.
const DefaultUserAgent = "ProbeHive"

// Built-in operator ceilings. They apply when configuration names no value, because an
// executor that reads a missing ceiling as "unbounded" fails open (ADR 0024).
const (
	// DefaultMaxTimeout matches the largest timeout check configuration may request, so the
	// built-in ceiling constrains nothing an operator did not already allow.
	DefaultMaxTimeout = 60 * time.Second
	// DefaultMaxRedirects matches the check schema bound for the same reason.
	DefaultMaxRedirects = 10
	// DefaultMaxResponseBytes bounds the body one response may make the probe read.
	DefaultMaxResponseBytes = 1 << 20
	// DefaultMaxResponseHeaderBytes bounds the response headers the transport will accept.
	DefaultMaxResponseHeaderBytes = 64 << 10
	// DefaultMaxTLSHandshake bounds the handshake phase within the total execution budget.
	DefaultMaxTLSHandshake = 10 * time.Second
)

// Settings are the operator ceilings and trust material an HTTP executor applies. Every
// ceiling here is a maximum: check configuration may ask for less and never for more.
//
// A zero or negative value means the field was not configured and the built-in default
// applies. There is deliberately no way to express a ceiling of zero: a global "follow no
// redirects" or "read no body" setting is not a current requirement, and the per-Monitor
// values already express it for the Monitors that want it.
type Settings struct {
	// MaxTimeout bounds one whole execution, including redirects and the body read.
	MaxTimeout time.Duration
	// MaxRedirects bounds how many redirects one execution may follow.
	MaxRedirects int
	// MaxResponseBytes bounds the body bytes read from one response. The body is discarded
	// after it is counted; reaching this ceiling truncates rather than fails.
	MaxResponseBytes int64
	// MaxResponseHeaderBytes bounds the response headers the transport accepts.
	MaxResponseHeaderBytes int64
	// MaxTLSHandshake bounds the TLS handshake phase. The effective value is never larger
	// than the execution timeout.
	MaxTLSHandshake time.Duration
	// UserAgent is sent when check configuration sets no User-Agent header.
	UserAgent string
	// RootCAs are the certificate authorities trusted in addition to nothing else when set,
	// and the host's roots when nil. It exists so an installation with an internal
	// certificate authority can monitor its own TLS endpoints. There is deliberately no
	// setting that skips verification (ADR 0024).
	RootCAs *x509.CertPool
}

// DefaultSettings returns the built-in ceilings.
func DefaultSettings() Settings {
	return Settings{
		MaxTimeout:             DefaultMaxTimeout,
		MaxRedirects:           DefaultMaxRedirects,
		MaxResponseBytes:       DefaultMaxResponseBytes,
		MaxResponseHeaderBytes: DefaultMaxResponseHeaderBytes,
		MaxTLSHandshake:        DefaultMaxTLSHandshake,
		UserAgent:              DefaultUserAgent,
	}
}

func (settings Settings) normalized() Settings {
	defaults := DefaultSettings()
	if settings.MaxTimeout <= 0 {
		settings.MaxTimeout = defaults.MaxTimeout
	}
	if settings.MaxRedirects <= 0 {
		settings.MaxRedirects = defaults.MaxRedirects
	}
	if settings.MaxResponseBytes <= 0 {
		settings.MaxResponseBytes = defaults.MaxResponseBytes
	}
	if settings.MaxResponseHeaderBytes <= 0 {
		settings.MaxResponseHeaderBytes = defaults.MaxResponseHeaderBytes
	}
	if settings.MaxTLSHandshake <= 0 {
		settings.MaxTLSHandshake = defaults.MaxTLSHandshake
	}
	if settings.UserAgent == "" {
		settings.UserAgent = defaults.UserAgent
	}
	return settings
}

// HTTPExecutor executes the 'http' check type. It is safe for concurrent use: every execution
// builds its own transport and shares only the dialer, the ceilings, and the clock.
type HTTPExecutor struct {
	dialer   *outbound.Dialer
	settings Settings
	clock    Clock
}

// NewHTTPExecutor returns an executor enforcing settings over dialer. A missing dialer or
// clock is an operator mistake reported as an ordinary error, not something to substitute a
// default for: defaulting the dialer would mean defaulting the outbound policy.
func NewHTTPExecutor(dialer *outbound.Dialer, settings Settings, clock Clock) (*HTTPExecutor, error) {
	if dialer == nil {
		return nil, fmt.Errorf("probe: an outbound dialer is required")
	}
	if clock == nil {
		return nil, fmt.Errorf("probe: a clock is required")
	}
	return &HTTPExecutor{dialer: dialer, settings: settings.normalized(), clock: clock}, nil
}

// CheckType reports the check type this executor executes.
func (*HTTPExecutor) CheckType() string { return check.HTTPCheckType }

// Execute performs one HTTP check and reports what happened. It never returns an error: an
// unreachable target is a measurement rather than a failure to measure (ADR 0024).
//
// The configuration is the validated one from internal/check. Its timeout and redirect budget
// are clamped to the operator ceilings again here, so a stored revision that predates a
// tightened ceiling cannot exceed it (ADR 0020).
func (executor *HTTPExecutor) Execute(ctx context.Context, configuration check.HTTPConfiguration) Observation {
	startedAt := executor.clock.Now()
	began := time.Now()
	trace := &executionTrace{began: began}

	observe := func(observation Observation) Observation {
		observation.StartedAt = startedAt
		observation.FinishedAt = executor.clock.Now()
		observation.Duration = time.Since(began)
		observation.Phases = trace.phases()
		if observation.HTTP != nil {
			observation.HTTP.RedirectCount = trace.followedRedirects()
			observation.HTTP.TLS = trace.tlsResult()
		}
		return observation
	}

	// The destination is checked before a request exists, so an obviously denied target
	// costs no connection. The dialer checks it again when it dials, which is what makes
	// this a cheap first answer rather than the enforcement point.
	policy := executor.dialer.Policy()
	if _, err := policy.CheckURL(configuration.URL); err != nil {
		return observe(classify(ctx, err))
	}

	timeout := executor.effectiveTimeout(configuration.TimeoutSeconds)
	executionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(executionCtx, configuration.Method, configuration.URL, nil)
	if err != nil {
		return observe(failed(OutcomeErrored, CodeRequestInvalid, outbound.ClassPublic))
	}
	applyHeaders(request, configuration.Headers, executor.settings.UserAgent)
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace.clientTrace()))

	transport := executor.transport(trace, timeout)
	// Closing the transport's connections here is what keeps reuse inside one execution.
	// A connection validated for this execution is never inherited by the next one.
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport:     transport,
		CheckRedirect: redirectChecker(policy, configuration.FollowRedirects, executor.effectiveRedirects(configuration.MaxRedirects), trace),
		// No cookie jar: nothing a target sets survives a hop, let alone a Run.
		Jar: nil,
	}

	response, err := client.Do(request)
	if err != nil {
		// A CheckRedirect failure returns the previous response with its body already
		// closed. Closing it twice is harmless; leaking it is not.
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return observe(classify(ctx, err))
	}
	defer response.Body.Close()

	result := &HTTPResult{StatusCode: response.StatusCode, Protocol: response.Proto}
	result.BodyBytes, result.BodyTruncated, err = executor.readBody(response.Body)
	if err != nil {
		observation := classify(ctx, err)
		observation.HTTP = result
		return observe(observation)
	}

	if !configuration.AcceptsStatus(response.StatusCode) {
		return observe(Observation{
			Outcome: OutcomeFailed,
			Failure: &Failure{Code: CodeStatusUnexpected},
			HTTP:    result,
		})
	}
	return observe(Observation{Outcome: OutcomePassed, HTTP: result})
}

// effectiveTimeout is the stricter of the configured timeout and the operator ceiling.
func (executor *HTTPExecutor) effectiveTimeout(configuredSeconds int) time.Duration {
	configured := time.Duration(configuredSeconds) * time.Second
	if configured <= 0 {
		return executor.settings.MaxTimeout
	}
	return min(configured, executor.settings.MaxTimeout)
}

// effectiveRedirects is the stricter of the configured budget and the operator ceiling. A
// configured budget of zero is a real answer here: it means no redirect may be followed.
func (executor *HTTPExecutor) effectiveRedirects(configured int) int {
	if configured < 0 {
		return 0
	}
	return min(configured, executor.settings.MaxRedirects)
}

func (executor *HTTPExecutor) transport(trace *executionTrace, timeout time.Duration) *http.Transport {
	return &http.Transport{
		// Proxy is nil deliberately rather than by omission. The package default reads
		// proxy environment variables, which would send every request to an address the
		// outbound policy never validated - the enforcement point inverted (ADR 0024).
		Proxy:       nil,
		DialContext: trace.dial(executor.dialer.DialContext),
		// The transport does its own TLS over the connection the dialer returns, so the
		// server name comes from the URL and the connection is still the validated one.
		ForceAttemptHTTP2:      true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: executor.settings.RootCAs},
		TLSHandshakeTimeout:    min(executor.settings.MaxTLSHandshake, timeout),
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: executor.settings.MaxResponseHeaderBytes,
	}
}

// readBody counts the body and discards it. It reads one byte past the ceiling so that a
// response exactly at the ceiling is not reported as truncated.
func (executor *HTTPExecutor) readBody(body io.Reader) (int64, bool, error) {
	ceiling := executor.settings.MaxResponseBytes
	read, err := io.Copy(io.Discard, io.LimitReader(body, ceiling+1))
	if err != nil {
		return read, false, err
	}
	if read > ceiling {
		return ceiling, true, nil
	}
	return read, false, nil
}

func applyHeaders(request *http.Request, headers []check.Header, userAgent string) {
	// Configuration headers cannot name Host, Authorization, Cookie, Content-Length, or
	// Transfer-Encoding: internal/check rejects those when the revision is written.
	for _, header := range headers {
		request.Header.Set(header.Name, header.Value)
	}
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", userAgent)
	}
}

// redirectBudgetError ends a redirect chain that ran past its effective budget. It is a
// distinct type so the outcome is the budget rather than whatever the last hop returned.
type redirectBudgetError struct{}

func (*redirectBudgetError) Error() string { return "probe: redirect budget exhausted" }

// redirectChecker re-enters the policy for every hop. net/http would otherwise follow a
// Location header to a destination only the first hop was validated against.
func redirectChecker(policy outbound.Policy, follow bool, budget int, trace *executionTrace) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if !follow {
			// The redirect response is the answer, and the status assertion judges it.
			return http.ErrUseLastResponse
		}
		if len(via) > budget {
			return &redirectBudgetError{}
		}
		if _, err := policy.CheckURL(request.URL.String()); err != nil {
			return err
		}
		trace.recordRedirect(len(via))
		return nil
	}
}

// classify turns an execution error into the outcome it earned. Order matters: a deadline
// that expires during a dial arrives wrapped in an outbound connect failure, and it is a
// timeout rather than a policy result.
func classify(ctx context.Context, err error) Observation {
	if errors.Is(ctx.Err(), context.Canceled) {
		return failed(OutcomeCancelled, CodeCancelled, outbound.ClassPublic)
	}
	if errors.Is(err, context.Canceled) {
		return failed(OutcomeCancelled, CodeCancelled, outbound.ClassPublic)
	}
	if errors.Is(err, context.DeadlineExceeded) || timedOut(err) {
		return failed(OutcomeTimedOut, CodeTimeout, outbound.ClassPublic)
	}

	var outboundError *outbound.Error
	if errors.As(err, &outboundError) {
		return failed(OutcomeErrored, string(outboundError.Reason), outboundError.Class)
	}
	var budgetError *redirectBudgetError
	if errors.As(err, &budgetError) {
		return failed(OutcomeErrored, CodeRedirectBudgetExhausted, outbound.ClassPublic)
	}
	var verificationError *tls.CertificateVerificationError
	if errors.As(err, &verificationError) {
		return failed(OutcomeErrored, CodeCertificateInvalid, outbound.ClassPublic)
	}
	return failed(OutcomeErrored, CodeTransportFailed, outbound.ClassPublic)
}

func timedOut(err error) bool {
	var netError net.Error
	return errors.As(err, &netError) && netError.Timeout()
}

// executionTrace collects phase timings. Its callbacks run on the transport's goroutines, so
// every field is guarded; first value wins, because a later redirect hop may reuse a
// connection and report a zero that would read as an anomaly (ADR 0024).
type executionTrace struct {
	mutex           sync.Mutex
	began           time.Time
	connect         time.Duration
	hasConnect      bool
	tlsBegan        time.Time
	tlsDuration     time.Duration
	tlsState        tls.ConnectionState
	hasTLS          bool
	firstByte       time.Duration
	hasFirstByte    bool
	redirectsFollow int
}

// dial wraps the outbound dialer to time it. Resolution happens inside that dialer, so the
// measurement covers resolution and connection together.
func (trace *executionTrace) dial(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		began := time.Now()
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		trace.mutex.Lock()
		if !trace.hasConnect {
			trace.connect, trace.hasConnect = time.Since(began), true
		}
		trace.mutex.Unlock()
		return conn, nil
	}
}

func (trace *executionTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		TLSHandshakeStart:    trace.startTLS,
		TLSHandshakeDone:     trace.finishTLS,
		GotFirstResponseByte: trace.recordFirstByte,
	}
}

func (trace *executionTrace) startTLS() {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if !trace.hasTLS {
		trace.tlsBegan = time.Now()
	}
}

func (trace *executionTrace) finishTLS(state tls.ConnectionState, err error) {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if err != nil || trace.hasTLS || trace.tlsBegan.IsZero() {
		return
	}
	trace.tlsDuration, trace.tlsState, trace.hasTLS = time.Since(trace.tlsBegan), state, true
}

func (trace *executionTrace) recordFirstByte() {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if !trace.hasFirstByte {
		trace.firstByte, trace.hasFirstByte = time.Since(trace.began), true
	}
}

func (trace *executionTrace) recordRedirect(followed int) {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if followed > trace.redirectsFollow {
		trace.redirectsFollow = followed
	}
}

func (trace *executionTrace) followedRedirects() int {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	return trace.redirectsFollow
}

func (trace *executionTrace) phases() Phases {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	return Phases{Connect: trace.connect, TLS: trace.tlsDuration, FirstByte: trace.firstByte}
}

func (trace *executionTrace) tlsResult() *TLSResult {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if !trace.hasTLS {
		return nil
	}
	result := &TLSResult{
		Version:     tls.VersionName(trace.tlsState.Version),
		CipherSuite: tls.CipherSuiteName(trace.tlsState.CipherSuite),
	}
	if len(trace.tlsState.PeerCertificates) != 0 {
		result.CertificateExpiresAt = trace.tlsState.PeerCertificates[0].NotAfter.UTC()
	}
	return result
}
