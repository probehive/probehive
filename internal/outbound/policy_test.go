package outbound

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func managedPolicy(t *testing.T, ports ...uint16) Policy {
	t.Helper()
	policy, err := NewPolicy(Spec{Profile: ProfileManaged, AllowedPorts: ports})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

func privatePolicy(t *testing.T, allowed []string, ports ...uint16) Policy {
	t.Helper()
	prefixes := make([]netip.Prefix, 0, len(allowed))
	for _, entry := range allowed {
		prefixes = append(prefixes, netip.MustParsePrefix(entry))
	}
	policy, err := NewPolicy(Spec{Profile: ProfilePrivate, AllowedCIDRs: prefixes, AllowedPorts: ports})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

func TestProfileNamesAndTenantSelectability(t *testing.T) {
	t.Parallel()
	for _, name := range []ProfileName{ProfileManaged, ProfilePrivate, ProfileOperator} {
		parsed, err := ParseProfileName(string(name))
		if err != nil || parsed != name {
			t.Fatalf("ParseProfileName(%q) = %q, %v", name, parsed, err)
		}
	}
	if _, err := ParseProfileName("anything"); err == nil {
		t.Fatal("ParseProfileName accepted an unknown profile")
	}
	if !ProfileManaged.TenantSelectable() || !ProfilePrivate.TenantSelectable() {
		t.Fatal("managed and private must be tenant-selectable")
	}
	if ProfileOperator.TenantSelectable() {
		t.Fatal("the operator profile must never be tenant-selectable")
	}
}

func TestNewPolicyRejectsUnusableConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec Spec
	}{
		{"missing profile", Spec{}},
		{"unknown profile", Spec{Profile: ProfileName("open")}},
		{"managed with an allow list", Spec{
			Profile:      ProfileManaged,
			AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		}},
		{"invalid prefix", Spec{Profile: ProfilePrivate, AllowedCIDRs: []netip.Prefix{{}}}},
		{"mapped prefix", Spec{
			Profile:      ProfilePrivate,
			AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("::ffff:127.0.0.0/104")},
		}},
		{"port zero", Spec{Profile: ProfilePrivate, AllowedPorts: []uint16{0}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPolicy(test.spec); err == nil {
				t.Fatal("NewPolicy accepted unusable configuration")
			}
		})
	}
}

func TestPrivateProfileWithoutAnAllowListBehavesLikeManaged(t *testing.T) {
	t.Parallel()
	managed := managedPolicy(t)
	empty := privatePolicy(t, nil)
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "::1", "fc00::1", "203.0.113.5"} {
		addr := netip.MustParseAddr(address)
		_, managedErr := managed.CheckAddress(addr)
		_, emptyErr := empty.CheckAddress(addr)
		if (managedErr == nil) != (emptyErr == nil) {
			t.Fatalf("%s: managed error %v, empty private error %v", address, managedErr, emptyErr)
		}
	}
}

func TestZeroPolicyDeniesEverything(t *testing.T) {
	t.Parallel()
	var policy Policy
	if _, err := policy.CheckURL("https://example.test/"); ReasonOf(err) != ReasonPolicyUnconfigured {
		t.Fatalf("CheckURL reason = %q", ReasonOf(err))
	}
	if _, err := policy.CheckHostPort("example.test:443"); ReasonOf(err) != ReasonPolicyUnconfigured {
		t.Fatalf("CheckHostPort reason = %q", ReasonOf(err))
	}
	if _, err := policy.CheckAddress(netip.MustParseAddr("203.0.113.5")); ReasonOf(err) != ReasonPolicyUnconfigured {
		t.Fatalf("CheckAddress reason = %q", ReasonOf(err))
	}
	if err := policy.CheckPort(443); ReasonOf(err) != ReasonPolicyUnconfigured {
		t.Fatalf("CheckPort reason = %q", ReasonOf(err))
	}
}

func TestCheckURLAcceptsAndCanonicalizesDestinations(t *testing.T) {
	t.Parallel()
	policy := managedPolicy(t, 80, 443, 8443)
	tests := []struct {
		name   string
		rawURL string
		want   Target
	}{
		{"scheme default port", "http://Example.Test/health?token=abc", Target{Scheme: "http", Host: "example.test", Port: 80}},
		{"tls default port", "https://example.test/", Target{Scheme: "https", Host: "example.test", Port: 443}},
		{"explicit port", "HTTPS://Example.TEST:8443/deep/path", Target{Scheme: "https", Host: "example.test", Port: 8443}},
		{"trailing dot", "https://example.test./", Target{Scheme: "https", Host: "example.test", Port: 443}},
		{"underscore label", "https://my_service.example.test/", Target{Scheme: "https", Host: "my_service.example.test", Port: 443}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target, err := policy.CheckURL(test.rawURL)
			if err != nil {
				t.Fatalf("CheckURL: %v", err)
			}
			if target.Scheme != test.want.Scheme || target.Host != test.want.Host || target.Port != test.want.Port {
				t.Fatalf("target = %+v, want %+v", target, test.want)
			}
			if target.Addr.IsValid() {
				t.Fatalf("a name target carries an address: %s", target.Addr)
			}
		})
	}
}

func TestCheckURLRejectsDestinationsBeforeResolution(t *testing.T) {
	t.Parallel()
	policy := managedPolicy(t)
	tests := []struct {
		name   string
		rawURL string
		want   Reason
	}{
		{"too long", "https://example.test/" + strings.Repeat("a", MaxURLLength), ReasonURLTooLong},
		{"unparsable", "https://exa mple.test/\x7f", ReasonURLInvalid},
		{"relative", "/health", ReasonURLNotAbsolute},
		{"file scheme", "file:///etc/passwd", ReasonSchemeUnsupported},
		{"gopher scheme", "gopher://example.test/", ReasonSchemeUnsupported},
		{"user information", "https://user:secret@example.test/", ReasonUserInfoPresent},
		{"missing host", "https:///health", ReasonHostMissing},
		{"denied port", "https://example.test:9200/", ReasonPortDenied},
		{"port zero", "https://example.test:0/", ReasonPortInvalid},
		{"host too long", "https://" + strings.Repeat("a.", 130) + "test/", ReasonHostInvalid},
		{"empty label", "https://example..test/", ReasonHostInvalid},
		{"leading hyphen", "https://-example.test/", ReasonHostInvalid},
		{"numeric final label", "https://0177.0.0.1/", ReasonHostInvalid},
		{"hexadecimal address form", "https://0x7f.0.0.1/", ReasonHostInvalid},
		{"decimal address form", "https://2130706433/", ReasonHostInvalid},
		{"unicode host", "https://exämple.test/", ReasonHostInvalid},
		{"ideographic full stop", "https://example.test。evil.test/", ReasonHostInvalid},
		{"opaque authority", "https:example.test/health", ReasonHostMissing},
		{"scheme-relative", "//example.test/health", ReasonURLNotAbsolute},
		{"loopback literal", "https://127.0.0.1/", ReasonAddressDenied},
		{"mapped loopback literal", "https://[::ffff:127.0.0.1]/", ReasonAddressDenied},
		{"mapped loopback in hexadecimal", "https://[::ffff:7f00:1]/", ReasonAddressDenied},
		{"trailing dot literal", "https://127.0.0.1./", ReasonAddressDenied},
		{"metadata literal", "http://169.254.169.254/latest/meta-data/", ReasonAddressDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target, err := policy.CheckURL(test.rawURL)
			if got := ReasonOf(err); got != test.want {
				t.Fatalf("reason = %q, want %q (target %+v)", got, test.want, target)
			}
		})
	}
}

func TestCheckURLKeepsTheValidatedAddressOfALiteralHost(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 80)
	target, err := policy.CheckURL("http://127.0.0.1/health")
	if err != nil {
		t.Fatalf("CheckURL: %v", err)
	}
	if !target.Addr.IsValid() || target.Addr.String() != "127.0.0.1" || target.Address() != "127.0.0.1:80" {
		t.Fatalf("target = %+v", target)
	}

	mapped, err := policy.CheckURL("http://[::ffff:127.0.0.1]/health")
	if err != nil {
		t.Fatalf("CheckURL mapped: %v", err)
	}
	if mapped.Addr.String() != "127.0.0.1" || mapped.Host != "127.0.0.1" {
		t.Fatalf("mapped target = %+v, want the unmapped IPv4 form", mapped)
	}
}

func TestCheckHostPortMatchesTheURLRules(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"127.0.0.0/8"}, 443)
	target, err := policy.CheckHostPort("Example.Test:443")
	if err != nil || target.Host != "example.test" || target.Port != 443 || target.Scheme != "" {
		t.Fatalf("target = %+v, err = %v", target, err)
	}

	tests := []struct {
		name    string
		address string
		want    Reason
	}{
		{"no port", "example.test", ReasonURLInvalid},
		{"port zero", "example.test:0", ReasonPortInvalid},
		{"non-numeric port", "example.test:https", ReasonPortInvalid},
		{"denied port", "example.test:8080", ReasonPortDenied},
		{"empty host", ":443", ReasonHostMissing},
		{"denied address", "10.0.0.1:443", ReasonAddressDenied},
		{"zoned address", "fe80::1%eth0:443", ReasonURLInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := policy.CheckHostPort(test.address)
			if got := ReasonOf(err); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestZonedLiteralIsDenied(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"fe80::/10"}, 443)
	_, err := policy.CheckHostPort("[fe80::1%eth0]:443")
	if ReasonOf(err) != ReasonAddressDenied {
		t.Fatalf("reason = %q", ReasonOf(err))
	}
	var outboundError *Error
	if !errors.As(err, &outboundError) || outboundError.Class != ClassZoned {
		t.Fatalf("error %v does not carry class %q", err, ClassZoned)
	}
}

func TestPortCeilingIgnoresConfigurationOrder(t *testing.T) {
	t.Parallel()
	policy := managedPolicy(t, 8443)
	if err := policy.CheckPort(8443); err != nil {
		t.Fatalf("CheckPort(8443): %v", err)
	}
	for _, port := range []uint16{0, 80, 443, 22, 65535} {
		if err := policy.CheckPort(port); err == nil {
			t.Fatalf("CheckPort(%d) allowed a port the operator did not list", port)
		}
	}
	if err := managedPolicy(t).CheckPort(443); err != nil {
		t.Fatalf("the default port set must contain 443: %v", err)
	}
	if err := managedPolicy(t).CheckPort(8080); err == nil {
		t.Fatal("the default port set must not contain 8080")
	}
}

func TestErrorMessagesCarryNoURLDetail(t *testing.T) {
	t.Parallel()
	policy := managedPolicy(t)

	_, credentials := policy.CheckURL("http://probe:s3cret@example.test/admin?token=abcdef")
	if credentials == nil {
		t.Fatal("user information was accepted")
	}
	if message := credentials.Error(); strings.Contains(message, "s3cret") || strings.Contains(message, "abcdef") || strings.Contains(message, "/admin") {
		t.Fatalf("message leaks URL detail: %s", message)
	}

	_, metadata := policy.CheckURL("http://169.254.169.254/latest/meta-data/iam?token=abcdef")
	if metadata == nil {
		t.Fatal("a metadata destination was accepted")
	}
	message := metadata.Error()
	if strings.Contains(message, "meta-data") || strings.Contains(message, "abcdef") {
		t.Fatalf("message leaks URL detail: %s", message)
	}
	for _, want := range []string{string(ReasonAddressDenied), string(ClassMetadata), "169.254.169.254", ":80"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q omits %q", message, want)
		}
	}
	if ReasonOf(nil) != "" || ReasonOf(errStub{}) != "" {
		t.Fatal("ReasonOf must be empty for an error this package did not produce")
	}
}

type errStub struct{}

func (errStub) Error() string { return "unrelated" }
