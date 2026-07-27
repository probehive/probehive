package outbound

import (
	"errors"
	"net/netip"
	"testing"
)

func TestClassifyFollowsTheSpecialPurposeRegistries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		address string
		want    Class
	}{
		{"203.0.113.5", ClassDocumentation},
		{"93.184.216.34", ClassPublic},
		{"2606:2800:220:1:248:1893:25c8:1946", ClassPublic},

		{"0.0.0.0", ClassUnspecified},
		{"0.1.2.3", ClassUnspecified},
		{"127.0.0.1", ClassLoopback},
		{"127.255.255.254", ClassLoopback},
		{"10.0.0.1", ClassPrivate},
		{"172.16.0.1", ClassPrivate},
		{"172.31.255.255", ClassPrivate},
		{"172.32.0.1", ClassPublic},
		{"192.168.1.1", ClassPrivate},
		{"100.64.0.1", ClassSharedAddressSpace},
		{"169.254.1.1", ClassLinkLocal},
		{"192.0.0.1", ClassProtocolAssignment},
		{"192.31.196.1", ClassProtocolAssignment},
		{"192.52.193.1", ClassProtocolAssignment},
		{"192.175.48.1", ClassProtocolAssignment},
		{"192.0.2.1", ClassDocumentation},
		{"198.51.100.1", ClassDocumentation},
		{"198.18.0.1", ClassBenchmark},
		{"198.19.255.255", ClassBenchmark},
		{"192.88.99.1", ClassTransition},
		{"224.0.0.1", ClassMulticast},
		{"239.255.255.255", ClassMulticast},
		{"240.0.0.1", ClassReserved},
		{"255.255.255.255", ClassReserved},

		{"::", ClassUnspecified},
		{"::1", ClassLoopback},
		{"fc00::1", ClassUniqueLocal},
		{"fd12:3456::1", ClassUniqueLocal},
		{"fe80::1", ClassLinkLocal},
		{"ff02::1", ClassMulticast},
		{"100::1", ClassDiscard},
		{"2001:db8::1", ClassDocumentation},
		{"3fff::1", ClassDocumentation},
		{"2001:2::1", ClassBenchmark},
		{"2001:1::1", ClassProtocolAssignment},
		{"2001:1::2", ClassProtocolAssignment},
		{"2001:3::1", ClassProtocolAssignment},
		{"2001:4:112::1", ClassProtocolAssignment},
		{"2001:20::1", ClassProtocolAssignment},
		{"2001:30::1", ClassProtocolAssignment},
		{"2001:10::1", ClassReserved},
		{"5f00::1", ClassProtocolAssignment},
		{"2620:4f:8000::1", ClassProtocolAssignment},

		// Transition ranges embed an IPv4 destination inside an IPv6 address.
		{"2002:7f00:1::", ClassTransition},
		{"2001::1", ClassTransition},
		{"64:ff9b::7f00:1", ClassTransition},
		{"64:ff9b:1::1", ClassTransition},

		// An IPv4-mapped address is judged as the IPv4 address it carries.
		{"::ffff:127.0.0.1", ClassLoopback},
		{"::ffff:10.0.0.1", ClassPrivate},
		{"::ffff:93.184.216.34", ClassPublic},

		{"169.254.169.254", ClassMetadata},
		{"169.254.169.253", ClassMetadata},
		{"169.254.169.123", ClassMetadata},
		{"169.254.170.2", ClassMetadata},
		{"168.63.129.16", ClassMetadata},
		{"100.100.100.200", ClassMetadata},
		{"192.0.0.192", ClassMetadata},
		{"fd00:ec2::254", ClassMetadata},
		{"fd00:ec2::253", ClassMetadata},
		{"fd00:ec2::123", ClassMetadata},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			t.Parallel()
			if got := Classify(netip.MustParseAddr(test.address)); got != test.want {
				t.Fatalf("Classify(%s) = %q, want %q", test.address, got, test.want)
			}
		})
	}

	if got := Classify(netip.Addr{}); got != ClassUnspecified {
		t.Fatalf("Classify(invalid) = %q", got)
	}
	if got := Classify(netip.MustParseAddr("fe80::1%eth0")); got != ClassZoned {
		t.Fatalf("Classify(zoned) = %q", got)
	}
}

func TestManagedProfileDeniesEverySpecialPurposeAddress(t *testing.T) {
	t.Parallel()
	policy := managedPolicy(t)
	denied := []string{
		"0.0.0.0", "127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "100.64.0.1",
		"169.254.1.1", "192.0.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "198.18.0.1",
		"192.88.99.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "fc00::1", "fe80::1", "ff02::1", "100::1", "2001:db8::1", "3fff::1",
		"2001:2::1", "2001:10::1", "2002:7f00:1::", "2001::1", "64:ff9b::7f00:1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "169.254.169.254", "168.63.129.16",
	}
	for _, address := range denied {
		if _, err := policy.CheckAddress(netip.MustParseAddr(address)); err == nil {
			t.Errorf("the managed profile allowed %s", address)
		}
	}

	allowed := []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946", "1.1.1.1"}
	for _, address := range allowed {
		checked, err := policy.CheckAddress(netip.MustParseAddr(address))
		if err != nil {
			t.Errorf("the managed profile denied public address %s: %v", address, err)
			continue
		}
		if checked.String() != address {
			t.Errorf("CheckAddress(%s) = %s", address, checked)
		}
	}
}

func TestAnAllowListReinstatesOnlyOverridableClasses(t *testing.T) {
	t.Parallel()
	// An operator who allows everything still cannot reach a metadata endpoint, a
	// transition range, multicast, reserved, discard, or unspecified space.
	policy := privatePolicy(t, []string{"0.0.0.0/0", "::/0"})

	reinstated := map[string]Class{
		"127.0.0.1":   ClassLoopback,
		"10.0.0.1":    ClassPrivate,
		"169.254.1.1": ClassLinkLocal,
		"100.64.0.1":  ClassSharedAddressSpace,
		"192.0.2.1":   ClassDocumentation,
		"198.18.0.1":  ClassBenchmark,
		"192.0.0.1":   ClassProtocolAssignment,
		"::1":         ClassLoopback,
		"fc00::1":     ClassUniqueLocal,
		"fe80::1":     ClassLinkLocal,
		"2001:db8::1": ClassDocumentation,
	}
	for address, class := range reinstated {
		if _, err := policy.CheckAddress(netip.MustParseAddr(address)); err != nil {
			t.Errorf("an allow list did not reinstate %s (%s): %v", address, class, err)
		}
	}

	refused := map[string]Class{
		"169.254.169.254": ClassMetadata,
		"fd00:ec2::254":   ClassMetadata,
		"168.63.129.16":   ClassMetadata,
		"100.100.100.200": ClassMetadata,
		"192.0.0.192":     ClassMetadata,
		"2002:7f00:1::":   ClassTransition,
		"2001::1":         ClassTransition,
		"64:ff9b::7f00:1": ClassTransition,
		"192.88.99.1":     ClassTransition,
		"224.0.0.1":       ClassMulticast,
		"ff02::1":         ClassMulticast,
		"240.0.0.1":       ClassReserved,
		"2001:10::1":      ClassReserved,
		"100::1":          ClassDiscard,
		"0.0.0.0":         ClassUnspecified,
		"::":              ClassUnspecified,
	}
	for address, class := range refused {
		_, err := policy.CheckAddress(netip.MustParseAddr(address))
		var outboundError *Error
		if !errors.As(err, &outboundError) {
			t.Errorf("an allow list reinstated %s, which no allow list may reinstate", address)
			continue
		}
		if outboundError.Reason != ReasonAddressDenied || outboundError.Class != class {
			t.Errorf("CheckAddress(%s) = %q/%q, want %q/%q", address, outboundError.Reason, outboundError.Class, ReasonAddressDenied, class)
		}
	}
}

func TestAnAllowListReinstatesOnlyTheRangesItNames(t *testing.T) {
	t.Parallel()
	policy := privatePolicy(t, []string{"10.10.0.0/16", "fd12:3456::/32"})
	for _, address := range []string{"10.10.0.1", "10.10.255.254", "fd12:3456::1"} {
		if _, err := policy.CheckAddress(netip.MustParseAddr(address)); err != nil {
			t.Errorf("the allow list did not admit %s: %v", address, err)
		}
	}
	for _, address := range []string{"10.11.0.1", "127.0.0.1", "192.168.1.1", "fd99::1", "::1"} {
		if _, err := policy.CheckAddress(netip.MustParseAddr(address)); err == nil {
			t.Errorf("the allow list admitted %s, which it does not name", address)
		}
	}
	// A mapped form of an allowed address is unmapped first, so it is admitted as itself.
	checked, err := policy.CheckAddress(netip.MustParseAddr("::ffff:10.10.0.1"))
	if err != nil || checked.String() != "10.10.0.1" {
		t.Fatalf("CheckAddress(mapped) = %s, %v", checked, err)
	}
}

func TestOperatorProfileTakesAnAllowListAndStillDeniesMetadata(t *testing.T) {
	t.Parallel()
	policy, err := NewPolicy(Spec{
		Profile:      ProfileOperator,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		AllowedPorts: []uint16{8443},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if policy.Profile() != ProfileOperator {
		t.Fatalf("profile = %q", policy.Profile())
	}
	if _, err := policy.CheckAddress(netip.MustParseAddr("10.1.2.3")); err != nil {
		t.Fatalf("the operator profile denied its own allowed range: %v", err)
	}
	if _, err := policy.CheckAddress(netip.MustParseAddr("169.254.169.254")); err == nil {
		t.Fatal("the operator profile reached a metadata endpoint")
	}
}
