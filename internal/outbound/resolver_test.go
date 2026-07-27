package outbound

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestNewTrustedResolverRejectsUnusableServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		servers []netip.AddrPort
	}{
		{"none", nil},
		{"invalid", []netip.AddrPort{{}}},
		{"port zero", []netip.AddrPort{netip.MustParseAddrPort("10.0.0.53:0")}},
		{"zoned", []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("fe80::1%eth0"), 53)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewTrustedResolver(test.servers, time.Second); err == nil {
				t.Fatal("NewTrustedResolver accepted an unusable server list")
			}
		})
	}

	resolver, err := NewTrustedResolver([]netip.AddrPort{netip.MustParseAddrPort("10.0.0.53:53")}, time.Second)
	if err != nil || resolver == nil {
		t.Fatalf("NewTrustedResolver = %v, %v", resolver, err)
	}
}

func TestTrustedResolverQueriesOnlyTheConfiguredServers(t *testing.T) {
	t.Parallel()
	first := newEchoListener(t)
	second := newEchoListener(t)
	servers := []netip.AddrPort{
		netip.MustParseAddrPort(first.listener.Addr().String()),
		netip.MustParseAddrPort(second.listener.Addr().String()),
	}
	dial, err := serverDialer(servers, 5*time.Second)
	if err != nil {
		t.Fatalf("serverDialer: %v", err)
	}

	// The address the resolver asks for is the host's own resolver configuration. It is
	// ignored: every query goes to a server the operator reviewed.
	reached := make([]string, 0, 4)
	for attempt := 0; attempt < 4; attempt++ {
		conn, dialErr := dial(t.Context(), "tcp", "203.0.113.53:53")
		if dialErr != nil {
			t.Fatalf("dial %d: %v", attempt, dialErr)
		}
		reached = append(reached, conn.RemoteAddr().String())
		conn.Close()
	}

	for index, address := range reached {
		want := servers[index%len(servers)].String()
		if address != want {
			t.Fatalf("query %d reached %s, want %s", index, address, want)
		}
	}
}

func TestTrustedResolverFailsOverToTheNextServer(t *testing.T) {
	t.Parallel()
	unreachable := newEchoListener(t)
	unreachableAddress := netip.MustParseAddrPort(unreachable.listener.Addr().String())
	if err := unreachable.listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reachable := newEchoListener(t)

	dial, err := serverDialer([]netip.AddrPort{
		unreachableAddress,
		netip.MustParseAddrPort(reachable.listener.Addr().String()),
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("serverDialer: %v", err)
	}

	conn, err := dial(t.Context(), "tcp", "203.0.113.53:53")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if conn.RemoteAddr().String() != reachable.listener.Addr().String() {
		t.Fatalf("reached %s, want the second server", conn.RemoteAddr())
	}
}

func TestSystemResolverIsTheHostResolver(t *testing.T) {
	t.Parallel()
	if SystemResolver() != Resolver(net.DefaultResolver) {
		t.Fatal("SystemResolver must return the host resolver")
	}
}
