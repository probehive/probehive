// Package outbound owns the shared outbound-access policy and the validating dialer that is
// its single enforcement point. Every destination influenced by a tenant, a monitored
// target, or user-supplied configuration reaches the network through this package: probes,
// redirects, webhooks, and notification deliveries alike (ADR 0007, ADR 0020).
//
// The package is standard-library-only and deliberately protocol-blind. It decides where a
// connection may go and returns a connection bound to an address it validated; it never
// learns which protocol is spoken over that connection. A caller keeps the intended host
// name for the HTTP Host header and the TLS server name, because the dialer is given that
// name and resolves it itself rather than being handed an address to trust.
//
// Two entry points exist, and a redirect-following client needs both:
//
//	Policy.CheckURL    canonicalize and check one URL before it is requested, including
//	                   every redirect Location, without resolving a name.
//	Dialer.DialContext resolve, classify, and connect. Its signature is the one
//	                   net/http's Transport expects, so a probe cannot reach a socket
//	                   without passing the policy.
//
// internal/check validates stored configuration when it is written. This package validates
// every destination again when it is used, including destinations that never passed through
// configuration validation at all.
//
// Nothing here fails open. A zero Policy, an unknown network, an empty resolver answer, and
// an unclassifiable address all deny.
package outbound

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// MaxURLLength bounds a destination URL before it is parsed. It matches the check
// configuration bound and applies again here, because a redirect Location never passed
// through configuration validation.
const MaxURLLength = 2048

// maxHostLength is the DNS name limit in presentation form.
const maxHostLength = 253

// maxLabelLength is the DNS label limit.
const maxLabelLength = 63

// defaultAllowedPorts is the operator ceiling when configuration names no ports. It admits
// the two ports the only current check type uses.
var defaultAllowedPorts = []uint16{80, 443}

// ProfileName selects a built-in policy profile (ADR 0020).
type ProfileName string

const (
	// ProfileManaged denies every special-purpose range with no operator exceptions. It is
	// the profile for locations operated on behalf of tenants who do not control them.
	ProfileManaged ProfileName = "managed"
	// ProfilePrivate denies the same ranges until an operator opts specific CIDR ranges in.
	// It is the default for a self-hosted installation, where an empty allow list makes it
	// behave exactly like ProfileManaged.
	ProfilePrivate ProfileName = "private"
	// ProfileOperator is for operator-configured infrastructure integrations such as a
	// self-hosted identity provider. It is never reachable from tenant configuration.
	ProfileOperator ProfileName = "operator"
)

// TenantSelectable reports whether tenant-visible configuration may select the profile.
// ProfileOperator is deliberately excluded: an operator integration profile is chosen by
// the operator for a named integration, never implicitly by a Monitor.
func (name ProfileName) TenantSelectable() bool {
	return name == ProfileManaged || name == ProfilePrivate
}

// ParseProfileName converts operator configuration text into a profile name.
func ParseProfileName(value string) (ProfileName, error) {
	switch name := ProfileName(value); name {
	case ProfileManaged, ProfilePrivate, ProfileOperator:
		return name, nil
	}
	return "", fmt.Errorf("outbound: unknown policy profile %q", value)
}

// Spec is the operator configuration a Policy is built from. Its zero value is not usable;
// Profile is required.
type Spec struct {
	// Profile selects the built-in profile.
	Profile ProfileName
	// AllowedCIDRs opts specific special-purpose ranges back in and must be empty for
	// ProfileManaged. Metadata endpoints, transition ranges, multicast, reserved, discard,
	// and unspecified addresses stay denied whatever it contains.
	AllowedCIDRs []netip.Prefix
	// AllowedPorts is the operator ceiling on destination ports. An empty list means the
	// default set of 80 and 443. User configuration may be stricter but is checked against
	// this set, never merged into it.
	AllowedPorts []uint16
}

// Policy is an immutable resolved outbound-access policy. Copying one is safe; its internal
// tables are never written after construction. The zero Policy denies everything.
type Policy struct {
	profile ProfileName
	allowed []netip.Prefix
	ports   map[uint16]struct{}
}

// NewPolicy validates operator configuration and resolves it into a Policy. It reports
// configuration errors as ordinary errors: a misconfigured installation is an operator
// mistake, not an outbound denial.
func NewPolicy(spec Spec) (Policy, error) {
	profile, err := ParseProfileName(string(spec.Profile))
	if err != nil {
		return Policy{}, err
	}
	if profile == ProfileManaged && len(spec.AllowedCIDRs) != 0 {
		return Policy{}, fmt.Errorf("outbound: profile %q accepts no allowed CIDR ranges", ProfileManaged)
	}

	allowed := make([]netip.Prefix, 0, len(spec.AllowedCIDRs))
	for _, prefix := range spec.AllowedCIDRs {
		if !prefix.IsValid() {
			return Policy{}, fmt.Errorf("outbound: allowed CIDR range %q is not a valid prefix", prefix.String())
		}
		if prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
			return Policy{}, fmt.Errorf("outbound: allowed CIDR range %s must be a plain IPv4 or IPv6 prefix", prefix)
		}
		allowed = append(allowed, prefix.Masked())
	}

	configuredPorts := spec.AllowedPorts
	if len(configuredPorts) == 0 {
		configuredPorts = defaultAllowedPorts
	}
	ports := make(map[uint16]struct{}, len(configuredPorts))
	for _, port := range configuredPorts {
		if port == 0 {
			return Policy{}, fmt.Errorf("outbound: port 0 cannot be allowed")
		}
		ports[port] = struct{}{}
	}

	return Policy{profile: profile, allowed: allowed, ports: ports}, nil
}

// Profile reports the active profile.
func (policy Policy) Profile() ProfileName { return policy.profile }

// Target is a canonicalized destination authority that passed every check not requiring
// name resolution.
type Target struct {
	// Scheme is "http" or "https", and is empty for a target parsed from a bare authority.
	Scheme string
	// Host is the lower-case host name, or the canonical text of an IP literal.
	Host string
	// Port is the explicit port, or the scheme default when the URL omitted one.
	Port uint16
	// Addr is valid only when Host was an IP literal, in which case no name resolution
	// happens and this address has already been checked against the policy.
	Addr netip.Addr
}

// Address returns the authority in host:port form, suitable as a dial address.
func (target Target) Address() string {
	return net.JoinHostPort(target.Host, strconv.FormatUint(uint64(target.Port), 10))
}

// CheckURL canonicalizes one destination URL and applies every rule that does not require
// name resolution: input size, scheme, user information, host syntax, and the port ceiling.
// Call it for the initial request and again for every redirect Location before the redirect
// is followed, so a redirect cannot reach a destination the first hop could not.
//
// It does not resolve the host. Resolution and address classification happen in
// Dialer.DialContext, at the moment of connection, so nothing can change in between.
func (policy Policy) CheckURL(rawURL string) (Target, error) {
	if err := policy.configured(); err != nil {
		return Target{}, err
	}
	if len(rawURL) > MaxURLLength {
		return Target{}, &Error{Reason: ReasonURLTooLong}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Target{}, &Error{Reason: ReasonURLInvalid}
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return Target{}, &Error{Reason: ReasonURLNotAbsolute}
	}
	if scheme != "http" && scheme != "https" {
		return Target{}, &Error{Reason: ReasonSchemeUnsupported}
	}
	if parsed.User != nil {
		return Target{}, &Error{Reason: ReasonUserInfoPresent}
	}

	port := uint16(0)
	if text := parsed.Port(); text != "" {
		value, convertErr := strconv.ParseUint(text, 10, 16)
		if convertErr != nil || value == 0 {
			return Target{}, &Error{Reason: ReasonPortInvalid}
		}
		port = uint16(value)
	} else if scheme == "https" {
		port = 443
	} else {
		port = 80
	}

	target, err := policy.checkAuthority(parsed.Hostname(), port)
	if err != nil {
		return Target{}, err
	}
	target.Scheme = scheme
	return target, nil
}

// CheckHostPort applies the same authority rules to a bare "host:port" address, which is the
// form a transport hands to the dialer.
func (policy Policy) CheckHostPort(hostPort string) (Target, error) {
	if err := policy.configured(); err != nil {
		return Target{}, err
	}
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil {
		return Target{}, &Error{Reason: ReasonURLInvalid}
	}
	value, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || value == 0 {
		return Target{}, &Error{Reason: ReasonPortInvalid}
	}
	return policy.checkAuthority(host, uint16(value))
}

// CheckPort applies the operator port ceiling.
func (policy Policy) CheckPort(port uint16) error {
	if err := policy.configured(); err != nil {
		return err
	}
	if port == 0 {
		return &Error{Reason: ReasonPortInvalid}
	}
	if _, ok := policy.ports[port]; !ok {
		return &Error{Reason: ReasonPortDenied, Port: port}
	}
	return nil
}

func (policy Policy) checkAuthority(host string, port uint16) (Target, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return Target{}, &Error{Reason: ReasonHostMissing, Port: port}
	}
	if len(host) > maxHostLength {
		return Target{}, &Error{Reason: ReasonHostInvalid, Port: port}
	}
	if err := policy.CheckPort(port); err != nil {
		return Target{}, err
	}

	// An IP literal skips resolution, so its address is checked here and carried forward.
	if addr, err := netip.ParseAddr(host); err == nil {
		normalized, checkErr := policy.CheckAddress(addr)
		if checkErr != nil {
			return Target{}, withAuthority(checkErr, host, port)
		}
		return Target{Host: normalized.String(), Port: port, Addr: normalized}, nil
	}

	name := strings.ToLower(host)
	if !isDomainName(name) {
		return Target{}, &Error{Reason: ReasonHostInvalid, Host: name, Port: port}
	}
	return Target{Host: name, Port: port}, nil
}

func (policy Policy) configured() error {
	if policy.profile == "" || policy.ports == nil {
		return &Error{Reason: ReasonPolicyUnconfigured}
	}
	return nil
}

// isDomainName accepts the host names a destination may legitimately use. It rejects an
// all-digit final label, so a numeric form that netip.ParseAddr refused - a zero-padded,
// decimal, or hexadecimal IPv4 spelling, for example - cannot be smuggled past address
// classification by being handed to a resolver that parses it more liberally than Go does.
//
// Only ASCII is accepted, so an internationalized name must be supplied in its punycode
// form. That also rejects the characters a resolver might fold into a label separator, such
// as the ideographic full stop.
func isDomainName(name string) bool {
	if name == "" || len(name) > maxHostLength {
		return false
	}
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > maxLabelLength {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for index := 0; index < len(label); index++ {
			if !isHostCharacter(label[index]) {
				return false
			}
		}
	}
	return !isAllDigits(labels[len(labels)-1])
}

func isHostCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		character == '-' || character == '_'
}

func isAllDigits(label string) bool {
	for index := 0; index < len(label); index++ {
		if label[index] < '0' || label[index] > '9' {
			return false
		}
	}
	return true
}
