package outbound

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// maxCandidates bounds how many resolved addresses one attempt will consider, so a hostile or
// broken name server cannot turn a single connection into an unbounded series of dials.
const maxCandidates = 16

// Dialer is the point where policy stops being advice. It resolves through the operator
// configured resolver, classifies every candidate, and connects to a validated address -
// binding the connection to the address that was checked, so nothing re-resolves between the
// decision and the connect.
//
// Its DialContext signature is the one net/http's Transport expects. A protocol package
// hands it to a transport and thereby has no code path to a socket that skips the policy.
// Each redirect hop and each address-family fallback enters DialContext again, which is what
// makes revalidation automatic rather than remembered.
//
// A Dialer performs one connection attempt at a time, trying allowed candidates in the order
// the resolver returned them. There is no concurrent address-family race: a deterministic
// order is worth more here than a few saved milliseconds.
type Dialer struct {
	policy   Policy
	resolver Resolver
	dial     func(ctx context.Context, network, address string) (net.Conn, error)
}

// NewDialer returns a Dialer enforcing policy. A nil resolver means SystemResolver.
// connectTimeout bounds one connection attempt; zero or less leaves attempts bounded only by
// the caller's context.
func NewDialer(policy Policy, resolver Resolver, connectTimeout time.Duration) *Dialer {
	if resolver == nil {
		resolver = SystemResolver()
	}
	system := &net.Dialer{Timeout: connectTimeout}
	return &Dialer{policy: policy, resolver: resolver, dial: system.DialContext}
}

// Policy reports the policy this Dialer enforces.
func (dialer *Dialer) Policy() Policy { return dialer.policy }

// DialContext validates and connects. It accepts only the TCP networks: a new protocol
// category needs its own threat model before it gets a connection from here.
func (dialer *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	lookupNetwork, err := lookupNetworkFor(network)
	if err != nil {
		return nil, err
	}
	target, err := dialer.policy.CheckHostPort(address)
	if err != nil {
		return nil, err
	}

	candidates, err := dialer.candidates(ctx, lookupNetwork, target)
	if err != nil {
		return nil, err
	}

	allowed := make([]netip.Addr, 0, len(candidates))
	var denial error
	for _, candidate := range candidates {
		checked, checkErr := dialer.policy.CheckAddress(candidate)
		if checkErr != nil {
			if denial == nil {
				denial = withAuthority(checkErr, target.Host, target.Port)
			}
			continue
		}
		if !matchesNetwork(lookupNetwork, checked) {
			continue
		}
		allowed = append(allowed, checked)
	}
	if len(allowed) == 0 {
		// Fail closed. A denied candidate is reported as the denial it was, never as a
		// resolution failure, and no unvalidated address is tried as a fallback.
		if denial != nil {
			return nil, denial
		}
		return nil, &Error{Reason: ReasonNoCandidate, Host: target.Host, Port: target.Port}
	}

	var lastErr error
	for _, addr := range allowed {
		conn, dialErr := dialer.connect(ctx, network, addr, target.Port)
		if dialErr == nil {
			return conn, nil
		}
		// A policy failure at connect time means the connection did not land where it was
		// validated. That is never retried against the next candidate: it is reported as
		// the violation it is.
		var policyError *Error
		if errors.As(dialErr, &policyError) {
			return nil, withAuthority(dialErr, target.Host, target.Port)
		}
		lastErr = dialErr
	}
	return nil, &Error{Reason: ReasonConnectFailed, Host: target.Host, Port: target.Port, cause: lastErr}
}

// candidates returns the addresses to consider. An IP literal is already checked by
// CheckHostPort and is never resolved.
func (dialer *Dialer) candidates(ctx context.Context, lookupNetwork string, target Target) ([]netip.Addr, error) {
	if target.Addr.IsValid() {
		return []netip.Addr{target.Addr}, nil
	}
	resolved, err := dialer.resolver.LookupNetIP(ctx, lookupNetwork, target.Host)
	if err != nil {
		return nil, &Error{Reason: ReasonResolutionFailed, Host: target.Host, Port: target.Port, cause: err}
	}
	if len(resolved) == 0 {
		return nil, &Error{Reason: ReasonNoCandidate, Host: target.Host, Port: target.Port}
	}
	if len(resolved) > maxCandidates {
		resolved = resolved[:maxCandidates]
	}
	return resolved, nil
}

// connect dials one validated address and confirms the connection landed on it.
func (dialer *Dialer) connect(ctx context.Context, network string, addr netip.Addr, port uint16) (net.Conn, error) {
	endpoint := netip.AddrPortFrom(addr, port)
	conn, err := dialer.dial(ctx, network, endpoint.String())
	if err != nil {
		return nil, err
	}
	if !connectedTo(conn, endpoint) {
		conn.Close()
		return nil, &Error{Reason: ReasonAddressMismatch, Port: port, Address: addr}
	}
	return conn, nil
}

func connectedTo(conn net.Conn, endpoint netip.AddrPort) bool {
	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return false
	}
	actual := remote.AddrPort()
	return actual.Addr().Unmap().WithZone("") == endpoint.Addr() && actual.Port() == endpoint.Port()
}

func lookupNetworkFor(network string) (string, error) {
	switch network {
	case "tcp":
		return "ip", nil
	case "tcp4":
		return "ip4", nil
	case "tcp6":
		return "ip6", nil
	}
	return "", &Error{Reason: ReasonNetworkUnsupported}
}

func matchesNetwork(lookupNetwork string, addr netip.Addr) bool {
	switch lookupNetwork {
	case "ip4":
		return addr.Is4()
	case "ip6":
		return addr.Is6()
	}
	return true
}
