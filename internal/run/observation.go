package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Observation is the stored detail of one Run (ADR 0021), holding exactly what ADR 0024
// decided an execution may keep: codes, instants, and numbers.
//
// It restates the shape internal/probe produces rather than importing it, because a feature
// package stays standard-library-only (ADR 0025). The absence of a message, header, or body
// field is the point: an Observation with nowhere to put target-supplied text has nothing to
// redact, which is how ADR 0021's "redaction precedes persistence" holds by construction.
type Observation struct {
	// RunID and ScheduledFor are the Run this describes. ScheduledFor is repeated because it
	// is the partition key both tables share (ADR 0025).
	RunID        ID
	ScheduledFor time.Time
	// OrganizationID carries tenant identity explicitly (ADR 0009).
	OrganizationID string
	// FailureCode is the stable probe.* code, or the outbound.* denial reason, that explains
	// a non-passed outcome. It is empty for a passed Run.
	FailureCode string
	// FailureClass is the outbound address class when a denial named one, and is otherwise
	// empty.
	FailureClass string
	// Duration is the monotonically measured elapsed time of the execution.
	Duration time.Duration
	// Phases times the first hop. A phase that did not happen is zero.
	Phases Phases
	// HTTP is present when an HTTP response arrived, whatever the outcome.
	HTTP *HTTPDetail
}

// Phases holds the phase timings of one execution (ADR 0024).
type Phases struct {
	// Connect is establishing the first connection, including the resolution the outbound
	// dialer performs inside it.
	Connect time.Duration
	// TLS is the first TLS handshake.
	TLS time.Duration
	// FirstByte is from the start of execution to the first byte of the first response.
	FirstByte time.Duration
}

// HTTPDetail is the protocol detail of an HTTP execution. It holds no headers and no body;
// only the size the probe read and whether that size hit the ceiling survive.
type HTTPDetail struct {
	StatusCode    int
	Protocol      string
	RedirectCount int
	BodyBytes     int64
	BodyTruncated bool
	// TLS is present when the first hop completed a TLS handshake.
	TLS *TLSDetail
}

// TLSDetail is the negotiated TLS detail of the first hop.
type TLSDetail struct {
	Version     string
	CipherSuite string
	// CertificateExpiresAt is the leaf certificate's notAfter instant in UTC.
	CertificateExpiresAt time.Time
}

// Bounds on the small identifiers an Observation stores, so a stored row stays the fixed-size
// record ADR 0024 describes.
const (
	MaxCodeLength     = 100
	MaxProtocolLength = 20
	MaxTLSFieldLength = 64
)

// Validate rejects an Observation that could not have come from an execution.
func (value Observation) Validate() error {
	if value.RunID == "" || value.OrganizationID == "" {
		return errors.New("an Observation requires Run and Organization identity")
	}
	if !isUTC(value.ScheduledFor) {
		return errors.New("an Observation requires the UTC scheduled instant of its Run")
	}
	if value.Duration < 0 {
		return errors.New("an Observation duration cannot be negative")
	}
	if value.Phases.Connect < 0 || value.Phases.TLS < 0 || value.Phases.FirstByte < 0 {
		return errors.New("an Observation phase timing cannot be negative")
	}
	if len(value.FailureCode) > MaxCodeLength || len(value.FailureClass) > MaxCodeLength {
		return fmt.Errorf("an Observation code is at most %d bytes", MaxCodeLength)
	}
	if value.FailureClass != "" && value.FailureCode == "" {
		return errors.New("an outbound denial class requires the code that carried it")
	}
	if value.HTTP == nil {
		return nil
	}
	return value.HTTP.validate()
}

func (value HTTPDetail) validate() error {
	if value.StatusCode < 100 || value.StatusCode > 599 {
		return errors.New("an HTTP status code is between 100 and 599")
	}
	if len(value.Protocol) > MaxProtocolLength {
		return fmt.Errorf("an HTTP protocol name is at most %d bytes", MaxProtocolLength)
	}
	if value.RedirectCount < 0 {
		return errors.New("a redirect count cannot be negative")
	}
	if value.BodyBytes < 0 {
		return errors.New("a body size cannot be negative")
	}
	if value.TLS == nil {
		return nil
	}
	if len(value.TLS.Version) > MaxTLSFieldLength || len(value.TLS.CipherSuite) > MaxTLSFieldLength {
		return fmt.Errorf("a TLS detail field is at most %d bytes", MaxTLSFieldLength)
	}
	if !value.TLS.CertificateExpiresAt.IsZero() && !isUTC(value.TLS.CertificateExpiresAt) {
		return errors.New("a certificate expiry must be UTC")
	}
	return nil
}

// OutboxEntry is one effect that must follow a committed Run (ADR 0021). It is written in
// the same transaction as the Run and never emitted from inside it. Consumers are
// at-least-once and idempotent on ID.
//
// No topic is defined yet: incident evaluation and alert delivery are undecided, and naming
// an event before its consumer exists would publish a contract nothing agreed to (ADR 0025).
type OutboxEntry struct {
	ID             ID
	OrganizationID string
	// Topic names the effect. It is an opaque bounded identifier to this package; the
	// decision that introduces a consumer fixes its vocabulary.
	Topic string
	// Payload is a bounded JSON document. The bound exists because an outbox is a queue and
	// not a place for a provider response to come to rest.
	Payload json.RawMessage
	// CreatedAt is when the effect was recorded, in UTC.
	CreatedAt time.Time
}

// Bounds on an outbox entry.
const (
	MaxTopicLength     = 100
	MaxPayloadBytes    = 16 * 1024
	MaxOutboxBatchSize = 32
)

// Validate rejects an outbox entry that a consumer could not act on.
func (value OutboxEntry) Validate() error {
	if value.ID == "" || value.OrganizationID == "" {
		return errors.New("an outbox entry requires identity and Organization scope")
	}
	if value.Topic == "" || len(value.Topic) > MaxTopicLength {
		return fmt.Errorf("an outbox topic is 1 to %d bytes", MaxTopicLength)
	}
	if len(value.Payload) == 0 || !json.Valid(value.Payload) {
		return errors.New("an outbox entry requires a valid JSON payload")
	}
	if len(value.Payload) > MaxPayloadBytes {
		return fmt.Errorf("an outbox payload is at most %d bytes", MaxPayloadBytes)
	}
	if !isUTC(value.CreatedAt) {
		return errors.New("an outbox entry requires a UTC creation instant")
	}
	return nil
}
