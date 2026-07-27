package outbound

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

// Reason is a stable outbound-policy failure identifier. A caller may record a reason in an
// Observation and a client may translate it; the English text beside it is documentation and
// may be reworded freely (ADR 0019).
type Reason string

const (
	// ReasonPolicyUnconfigured is returned by a zero Policy. It exists so that forgetting to
	// build a policy denies rather than permits.
	ReasonPolicyUnconfigured Reason = "outbound.policy.unconfigured"

	ReasonURLTooLong        Reason = "outbound.url.tooLong"
	ReasonURLInvalid        Reason = "outbound.url.invalid"
	ReasonURLNotAbsolute    Reason = "outbound.url.notAbsolute"
	ReasonSchemeUnsupported Reason = "outbound.url.scheme"
	ReasonUserInfoPresent   Reason = "outbound.url.userInfo"

	ReasonHostMissing Reason = "outbound.host.missing"
	ReasonHostInvalid Reason = "outbound.host.invalid"
	ReasonPortInvalid Reason = "outbound.port.invalid"
	ReasonPortDenied  Reason = "outbound.port.denied"

	ReasonNetworkUnsupported Reason = "outbound.network.unsupported"
	ReasonResolutionFailed   Reason = "outbound.resolution.failed"
	ReasonNoCandidate        Reason = "outbound.resolution.empty"

	// ReasonAddressDenied carries the Class that denied the address.
	ReasonAddressDenied Reason = "outbound.address.denied"
	// ReasonAddressMismatch means a connection was established to an address other than the
	// one that was validated. It should be unreachable and is checked anyway, because the
	// whole guarantee of this package is that those two addresses are the same.
	ReasonAddressMismatch Reason = "outbound.address.mismatch"
	ReasonConnectFailed   Reason = "outbound.connect.failed"
)

// Error is an outbound-policy failure. Its fields are deliberately narrow: a reason, the
// destination authority, and the address involved. It never carries a URL path, query,
// fragment, or user information, so an Error is safe to log, return in an Observation, and
// show to an operator.
type Error struct {
	// Reason is the stable identifier of what failed.
	Reason Reason
	// Class is set when Reason is ReasonAddressDenied and names why the address was denied.
	Class Class
	// Host is the destination host name or IP literal, when one was known.
	Host string
	// Port is the destination port, when one was known.
	Port uint16
	// Address is the specific candidate involved, when one was known.
	Address netip.Addr

	cause error
}

func (outboundError *Error) Error() string {
	var message strings.Builder
	message.WriteString("outbound: ")
	message.WriteString(string(outboundError.Reason))
	if outboundError.Class != ClassPublic {
		message.WriteString(" (")
		message.WriteString(string(outboundError.Class))
		message.WriteString(")")
	}
	if outboundError.Host != "" {
		message.WriteString(" for ")
		message.WriteString(outboundError.Host)
		if outboundError.Port != 0 {
			message.WriteString(":")
			message.WriteString(strconv.FormatUint(uint64(outboundError.Port), 10))
		}
	}
	if outboundError.Address.IsValid() {
		message.WriteString(" address ")
		message.WriteString(outboundError.Address.String())
	}
	if outboundError.cause != nil {
		message.WriteString(": ")
		message.WriteString(outboundError.cause.Error())
	}
	return message.String()
}

func (outboundError *Error) Unwrap() error { return outboundError.cause }

// ReasonOf reports the stable reason an error carries, or an empty Reason for an error this
// package did not produce.
func ReasonOf(err error) Reason {
	var outboundError *Error
	if errors.As(err, &outboundError) {
		return outboundError.Reason
	}
	return ""
}

// withAuthority fills in the destination an error was produced for. An address check knows
// the address but not the name it came from, and the name is what an operator recognizes.
func withAuthority(err error, host string, port uint16) error {
	var outboundError *Error
	if !errors.As(err, &outboundError) {
		return err
	}
	annotated := *outboundError
	if annotated.Host == "" {
		annotated.Host = host
	}
	if annotated.Port == 0 {
		annotated.Port = port
	}
	return &annotated
}
