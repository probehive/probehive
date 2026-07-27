// Package probe executes checks. It turns a validated check configuration plus the outbound
// dialer of internal/outbound into an Observation (ADR 0020, ADR 0024).
//
// The split from internal/check is deliberate: a check type is defined and validated in one
// package that stays standard-library-only, and executed in another that may use protocol
// clients. The two are joined by the check type identifier and integer schema version of
// ADR 0014, and execution revalidates the stored configuration rather than trusting it.
//
// This package imports no persistence, HTTP-API, or composition package, and nothing imports
// it back. It receives its ceilings; it does not read configuration.
//
// Execution never returns an error. A destination that was denied, a target that did not
// answer, and a target that answered wrongly are all measurements, and each is reported as an
// Observation with the outcome it earned.
package probe

import (
	"time"

	"github.com/probehive/probehive/internal/outbound"
)

// Clock supplies domain-relevant time.
type Clock interface{ Now() time.Time }

// Outcome is what one execution amounted to. It matches the Run outcomes of ADR 0021 minus
// OutcomeSkipped, which belongs to the scheduler: an executor that ran cannot have skipped.
type Outcome string

const (
	// OutcomePassed means the target answered and every assertion accepted the answer.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed means the target answered and an assertion rejected the answer. It is
	// the only outcome that says something about the target rather than about reaching it.
	OutcomeFailed Outcome = "failed"
	// OutcomeErrored means execution itself failed: a denied destination, a resolution or
	// connection failure, a TLS failure, or an unusable stored configuration.
	OutcomeErrored Outcome = "errored"
	// OutcomeTimedOut means the effective execution deadline expired.
	OutcomeTimedOut Outcome = "timedout"
	// OutcomeCancelled means the caller's context was cancelled, which is what a graceful
	// shutdown or a lost lease looks like from here.
	OutcomeCancelled Outcome = "cancelled"
)

// Stable failure codes. A code is contract under ADR 0019 and ADR 0024; the English text
// beside it is documentation and may be reworded freely. An outbound denial is reported under
// its own outbound.* reason rather than being flattened into one of these.
const (
	// CodeCheckTypeUnsupported means this build has no executor for the check type. It is
	// reachable only from a stored revision written by a build that had one.
	CodeCheckTypeUnsupported = "probe.checkType.unsupported"
	// CodeStatusUnexpected means the response status was outside the expected set.
	CodeStatusUnexpected = "probe.http.status.unexpected"
	// CodeRedirectBudgetExhausted means following redirects ran past the effective budget.
	// A configuration that follows no redirects at all asserts the redirect response
	// instead and never reports this.
	CodeRedirectBudgetExhausted = "probe.http.redirect.tooMany"
	// CodeRequestInvalid means the stored configuration could not be turned into a request,
	// which a method or URL the validator would have rejected causes.
	CodeRequestInvalid = "probe.http.request.invalid"
	// CodeCertificateInvalid means the target's certificate chain failed verification.
	// It is distinguished from a generic transport failure because an expired or misissued
	// certificate is a thing operators monitor for.
	CodeCertificateInvalid = "probe.tls.certificateInvalid"
	// CodeTransportFailed is the residual: the connection or the exchange over it failed
	// for a reason with no more specific code.
	CodeTransportFailed = "probe.transport.failed"
	// CodeTimeout accompanies OutcomeTimedOut.
	CodeTimeout = "probe.execution.timeout"
	// CodeCancelled accompanies OutcomeCancelled.
	CodeCancelled = "probe.execution.cancelled"
)

// Failure explains a non-passed Observation.
//
// It is a code and, for an outbound denial, the class that denied the address. There is
// deliberately no message field: an Observation that carries no free text and no
// target-supplied bytes has nothing to redact, which is how ADR 0021's requirement that
// redaction precede persistence is met by construction rather than by a step a later change
// could forget (ADR 0024).
type Failure struct {
	// Code is the stable identifier of what failed. It is a probe.* code, or the outbound.*
	// reason of ADR 0023 when the outbound policy refused the destination.
	Code string
	// Class is set when an outbound address denial named one, and reports why the address
	// was denied.
	Class outbound.Class
}

// Observation is the bounded detail of one execution (ADR 0021). Organization identity, Run
// identity, Probe Location, and persistence belong to the caller; this is the measurement.
type Observation struct {
	// Outcome is what the execution amounted to.
	Outcome Outcome
	// Failure explains a non-passed outcome and is nil when Outcome is OutcomePassed.
	Failure *Failure
	// StartedAt and FinishedAt are the wall-clock record, read from the injected clock.
	StartedAt  time.Time
	FinishedAt time.Time
	// Duration is the elapsed time, measured monotonically. It, not the difference between
	// the two instants, is the latency: a clock step must not become a latency spike.
	Duration time.Duration
	// Phases times the parts of the first hop. A later redirect hop may reuse a connection,
	// so recording its zero would read as an anomaly rather than as reuse (ADR 0024).
	Phases Phases
	// HTTP is present when a response arrived, whatever the outcome.
	HTTP *HTTPResult
}

// Phases holds the phase timings of one execution. A phase that did not happen is zero:
// no TLS handshake on a plaintext request, no first byte when the connection failed.
type Phases struct {
	// Connect is establishing the first connection, including the resolution the outbound
	// dialer performs inside it (ADR 0024).
	Connect time.Duration
	// TLS is the first TLS handshake.
	TLS time.Duration
	// FirstByte is from the start of execution to the first byte of the first response.
	FirstByte time.Duration
}

// HTTPResult is the protocol detail of an HTTP execution. It holds no response headers and no
// response body: the body is read to the operator ceiling and discarded, and only its size
// survives.
type HTTPResult struct {
	// StatusCode is the status of the response that was asserted, which is the final hop
	// when redirects were followed.
	StatusCode int
	// Protocol is the negotiated protocol as reported by the response, such as "HTTP/1.1".
	Protocol string
	// RedirectCount is how many redirects were followed.
	RedirectCount int
	// BodyBytes is how many body bytes the probe read, after any transparent decompression,
	// bounded by the operator ceiling.
	BodyBytes int64
	// BodyTruncated reports that the body reached the ceiling and the rest was not read, so
	// BodyBytes is a floor rather than the response size.
	BodyTruncated bool
	// TLS is present when the first hop completed a TLS handshake.
	TLS *TLSResult
}

// TLSResult is the negotiated TLS detail of the first hop.
type TLSResult struct {
	// Version is the negotiated protocol version, such as "TLS 1.3".
	Version string
	// CipherSuite is the negotiated cipher suite name.
	CipherSuite string
	// CertificateExpiresAt is the leaf certificate's notAfter instant in UTC, which is the
	// value an expiry assertion will eventually be written against.
	CertificateExpiresAt time.Time
}

// failed builds a non-passed Observation for an outcome that has no protocol detail.
func failed(outcome Outcome, code string, class outbound.Class) Observation {
	return Observation{Outcome: outcome, Failure: &Failure{Code: code, Class: class}}
}
