package outbound

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"
	"time"
)

// Resolver resolves a host name to candidate addresses.
//
// The resolver used for validation is operator-controlled and never tenant-selectable.
// A tenant-selected resolver is only ever the destination of a DNS check, where
// it is validated against the active profile like any other target - it never performs the
// resolution that decides whether a destination is allowed.
//
// *net.Resolver satisfies this interface, and a test can satisfy it without a name server.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// SystemResolver returns the host's own resolver. It is operator-controlled in the sense
// that the operator controls the machine's resolver configuration; an installation that
// wants an explicit, auditable answer path uses NewTrustedResolver instead.
func SystemResolver() Resolver { return net.DefaultResolver }

// NewTrustedResolver returns a Resolver that sends every query to one of the operator
// configured servers and to nothing else, whatever the host's resolver configuration says.
// Servers are tried in rotation so one unreachable server does not stop resolution.
//
// timeout bounds a single query connection; a value of zero or less leaves the connection
// bounded only by the caller's context.
func NewTrustedResolver(servers []netip.AddrPort, timeout time.Duration) (Resolver, error) {
	dial, err := serverDialer(servers, timeout)
	if err != nil {
		return nil, err
	}
	// PreferGo keeps resolution inside Go's own DNS client, which is what makes Dial
	// authoritative. StrictErrors stops a partial failure from being reported as an
	// answer with fewer addresses than the name really has.
	return &net.Resolver{PreferGo: true, StrictErrors: true, Dial: dial}, nil
}

// serverDialer builds the Dial function a trusted resolver uses. It ignores the address the
// resolver asks for: the server list comes from operator configuration, not from the host's
// resolv.conf, and substituting it here is what confines queries to reviewed servers.
func serverDialer(servers []netip.AddrPort, timeout time.Duration) (func(context.Context, string, string) (net.Conn, error), error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("outbound: at least one resolver server is required")
	}
	ordered := make([]netip.AddrPort, 0, len(servers))
	for _, server := range servers {
		if !server.IsValid() || server.Port() == 0 || server.Addr().Zone() != "" {
			return nil, fmt.Errorf("outbound: resolver server %q must be a plain address and port", server.String())
		}
		ordered = append(ordered, netip.AddrPortFrom(server.Addr().Unmap(), server.Port()))
	}

	dialer := &net.Dialer{Timeout: timeout}
	var attempts atomic.Uint64
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		start := attempts.Add(1) - 1
		var lastErr error
		for offset := range ordered {
			server := ordered[(start+uint64(offset))%uint64(len(ordered))]
			conn, err := dialer.DialContext(ctx, network, server.String())
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}, nil
}
