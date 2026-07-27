package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"
)

// stubResolver answers from a fixed list. Every dialer test resolves through one, so no test
// touches public DNS or the public internet.
type stubResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
	network   string
	host      string
}

func (resolver *stubResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	resolver.calls++
	resolver.network, resolver.host = network, host
	if resolver.err != nil {
		return nil, resolver.err
	}
	return resolver.addresses, nil
}

func resolving(addresses ...string) *stubResolver {
	parsed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		parsed = append(parsed, netip.MustParseAddr(address))
	}
	return &stubResolver{addresses: parsed}
}

// echoListener accepts one connection, echoes a byte, and reports how many connections it
// saw, which is how a test proves that a denied destination was never dialed.
type echoListener struct {
	listener net.Listener
	accepted chan struct{}
}

func newEchoListener(t *testing.T) *echoListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	echo := &echoListener{listener: listener, accepted: make(chan struct{}, 8)}
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			echo.accepted <- struct{}{}
			go func() {
				defer conn.Close()
				buffer := make([]byte, 1)
				if _, readErr := conn.Read(buffer); readErr == nil {
					conn.Write(buffer)
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return echo
}

func (echo *echoListener) port(t *testing.T) uint16 {
	t.Helper()
	address, err := netip.ParseAddrPort(echo.listener.Addr().String())
	if err != nil {
		t.Fatalf("listener address: %v", err)
	}
	return address.Port()
}

func (echo *echoListener) connectionCount() int { return len(echo.accepted) }

func exchangeByte(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := conn.Write([]byte{0x2a}); err != nil {
		t.Fatalf("write: %v", err)
	}
	buffer := make([]byte, 1)
	if _, err := conn.Read(buffer); err != nil {
		t.Fatalf("read: %v", err)
	}
	if buffer[0] != 0x2a {
		t.Fatalf("echoed byte = %#x", buffer[0])
	}
}

func TestDialerConnectsToTheAddressItValidated(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, port)
	resolver := resolving("127.0.0.1")
	dialer := NewDialer(policy, resolver, 5*time.Second)

	// The host name is deliberately unresolvable by the operating system. Reaching the
	// listener proves the connection went to the address that was checked, rather than to
	// whatever a second, unvalidated resolution might have returned.
	conn, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", port))
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	exchangeByte(t, conn)

	if resolver.calls != 1 || resolver.host != "service.invalid" || resolver.network != "ip" {
		t.Fatalf("resolver saw %d call(s) for %q on %q", resolver.calls, resolver.host, resolver.network)
	}
	if dialer.Policy().Profile() != ProfilePrivate {
		t.Fatalf("profile = %q", dialer.Policy().Profile())
	}
}

func TestDialerSkipsDeniedCandidatesAndKeepsTheAllowedOne(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, port)
	dialer := NewDialer(policy, resolving("169.254.169.254", "10.0.0.1", "127.0.0.1"), 5*time.Second)

	conn, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", port))
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	exchangeByte(t, conn)
}

func TestDialerFailsClosedWhenEveryCandidateIsDenied(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"10.0.0.0/8"}, port)
	dialer := NewDialer(policy, resolving("169.254.169.254", "127.0.0.1"), 5*time.Second)

	_, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", port))
	var outboundError *Error
	if !errors.As(err, &outboundError) || outboundError.Reason != ReasonAddressDenied {
		t.Fatalf("err = %v, want an address denial", err)
	}
	if outboundError.Class != ClassMetadata || outboundError.Host != "service.invalid" || outboundError.Port != port {
		t.Fatalf("denial = %+v, want the first denied candidate with its authority", outboundError)
	}
	if echo.connectionCount() != 0 {
		t.Fatalf("a denied destination was dialed %d time(s)", echo.connectionCount())
	}
}

func TestDialerReportsAnEmptyOrFailedResolution(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)

	empty := NewDialer(policy, &stubResolver{}, time.Second)
	_, err := empty.DialContext(t.Context(), "tcp", "service.invalid:443")
	if ReasonOf(err) != ReasonNoCandidate {
		t.Fatalf("empty answer reason = %q", ReasonOf(err))
	}

	failure := errors.New("no such host")
	broken := NewDialer(policy, &stubResolver{err: failure}, time.Second)
	_, err = broken.DialContext(t.Context(), "tcp", "service.invalid:443")
	if ReasonOf(err) != ReasonResolutionFailed || !errors.Is(err, failure) {
		t.Fatalf("resolution failure = %v (reason %q)", err, ReasonOf(err))
	}
}

func TestDialerAcceptsOnlyTCPNetworks(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)
	dialer := NewDialer(policy, resolving("127.0.0.1"), time.Second)
	for _, network := range []string{"udp", "udp4", "unix", "ip", "", "tcp5"} {
		_, err := dialer.DialContext(t.Context(), network, "service.invalid:443")
		if ReasonOf(err) != ReasonNetworkUnsupported {
			t.Errorf("network %q reason = %q, want %q", network, ReasonOf(err), ReasonNetworkUnsupported)
		}
	}
}

func TestDialerHonoursTheRequestedAddressFamily(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8", "::1/128"}, 443)
	dialer := NewDialer(policy, resolving("::1"), time.Second)

	// The candidate is allowed by the policy but is the wrong family for tcp4, so it is
	// dropped rather than dialed, and the attempt fails closed.
	_, err := dialer.DialContext(t.Context(), "tcp4", "service.invalid:443")
	if ReasonOf(err) != ReasonNoCandidate {
		t.Fatalf("reason = %q, want %q", ReasonOf(err), ReasonNoCandidate)
	}
}

func TestDialerDoesNotResolveAnIPLiteral(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, port)
	resolver := resolving("10.0.0.1")
	dialer := NewDialer(policy, resolver, 5*time.Second)

	conn, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	exchangeByte(t, conn)
	if resolver.calls != 0 {
		t.Fatalf("an IP literal was resolved %d time(s)", resolver.calls)
	}
}

func TestDialerBoundsHowManyCandidatesOneAnswerCanProduce(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, port)

	filler := make([]string, 0, maxCandidates+1)
	for index := 1; index <= maxCandidates; index++ {
		filler = append(filler, fmt.Sprintf("10.0.0.%d", index))
	}

	// The allowed address sits at the edge of the bound and is still considered.
	withinBound := append(append([]string{}, filler[:maxCandidates-1]...), "127.0.0.1")
	conn, err := NewDialer(policy, resolving(withinBound...), 5*time.Second).
		DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", port))
	if err != nil {
		t.Fatalf("DialContext within the bound: %v", err)
	}
	exchangeByte(t, conn)

	// One place beyond it, the answer is truncated and the attempt fails closed.
	beyondBound := append(append([]string{}, filler...), "127.0.0.1")
	_, err = NewDialer(policy, resolving(beyondBound...), 5*time.Second).
		DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", port))
	if ReasonOf(err) != ReasonAddressDenied {
		t.Fatalf("reason = %q, want the truncated answer to fail closed", ReasonOf(err))
	}
}

func TestDialerRejectsAConnectionToAnUnvalidatedAddress(t *testing.T) {
	t.Parallel()
	validated := newEchoListener(t)
	elsewhere := newEchoListener(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, validated.port(t), elsewhere.port(t))

	dialer := NewDialer(policy, resolving("127.0.0.1"), 5*time.Second)
	system := &net.Dialer{Timeout: 5 * time.Second}
	dialer.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return system.DialContext(ctx, network, elsewhere.listener.Addr().String())
	}

	conn, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("service.invalid:%d", validated.port(t)))
	if err == nil {
		conn.Close()
		t.Fatal("a connection to an address other than the validated one was returned")
	}
	if ReasonOf(err) != ReasonAddressMismatch {
		t.Fatalf("reason = %q, want %q", ReasonOf(err), ReasonAddressMismatch)
	}

	// The mismatched connection is closed rather than leaked, which the peer sees as EOF.
	select {
	case <-elsewhere.accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the substituted listener never accepted")
	}
}

func TestDialerRejectsANonTCPConnection(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)
	dialer := NewDialer(policy, resolving("127.0.0.1"), time.Second)
	dialer.dial = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		server.Close()
		return client, nil
	}
	_, err := dialer.DialContext(t.Context(), "tcp", "service.invalid:443")
	if ReasonOf(err) != ReasonAddressMismatch {
		t.Fatalf("reason = %q, want %q", ReasonOf(err), ReasonAddressMismatch)
	}
}

func TestDialerReportsAConnectFailureAsSuch(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)
	dialer := NewDialer(policy, resolving("127.0.0.1"), time.Second)
	refused := errors.New("connection refused")
	dialer.dial = func(context.Context, string, string) (net.Conn, error) { return nil, refused }

	_, err := dialer.DialContext(t.Context(), "tcp", "service.invalid:443")
	if ReasonOf(err) != ReasonConnectFailed || !errors.Is(err, refused) {
		t.Fatalf("err = %v (reason %q)", err, ReasonOf(err))
	}
}

func TestDialerAppliesThePolicyBeforeResolving(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)
	resolver := resolving("127.0.0.1")
	dialer := NewDialer(policy, resolver, time.Second)

	for _, address := range []string{"service.invalid:8080", "service.invalid:0", "0177.0.0.1:443", ":443"} {
		if _, err := dialer.DialContext(t.Context(), "tcp", address); err == nil {
			t.Errorf("DialContext(%q) was allowed", address)
		}
	}
	if resolver.calls != 0 {
		t.Fatalf("a rejected authority was resolved %d time(s)", resolver.calls)
	}
}

func TestDialerCarriesTheCallerContext(t *testing.T) {
	t.Parallel()
	echo := newEchoListener(t)
	port := echo.port(t)
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, port)
	dialer := NewDialer(policy, resolving("127.0.0.1"), 5*time.Second)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("service.invalid:%d", port))
	if err == nil {
		t.Fatal("a cancelled context produced a connection")
	}
	if ReasonOf(err) != ReasonConnectFailed {
		t.Fatalf("reason = %q, want %q", ReasonOf(err), ReasonConnectFailed)
	}
}
