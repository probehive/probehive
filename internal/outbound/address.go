package outbound

import "net/netip"

// Class names why an address is not ordinary public unicast. It is a stable value: a caller
// may record it in an Observation or translate it for display.
type Class string

const (
	// ClassPublic is the empty class carried by an ordinary globally routable address.
	ClassPublic Class = ""

	ClassUnspecified        Class = "unspecified"
	ClassLoopback           Class = "loopback"
	ClassPrivate            Class = "private"
	ClassLinkLocal          Class = "linkLocal"
	ClassUniqueLocal        Class = "uniqueLocal"
	ClassSharedAddressSpace Class = "sharedAddressSpace"
	ClassProtocolAssignment Class = "protocolAssignment"
	ClassDocumentation      Class = "documentation"
	ClassBenchmark          Class = "benchmark"
	ClassMulticast          Class = "multicast"
	ClassReserved           Class = "reserved"
	ClassDiscard            Class = "discard"
	// ClassTransition covers the IPv4-in-IPv6 transition ranges. They embed an IPv4
	// address inside an IPv6 one, which would let a denied IPv4 destination be reached
	// through an address that classifies as IPv6, so they are denied outright.
	ClassTransition Class = "transition"
	// ClassMetadata is a cloud instance-metadata endpoint. It is denied by every profile
	// and no operator allow list can reinstate it.
	ClassMetadata Class = "metadata"
	// ClassZoned is an address carrying an interface zone. A scoped address is never a
	// legitimate monitoring destination and its scope cannot be validated here.
	ClassZoned Class = "zoned"
)

// metadataAddresses are the cloud instance-metadata endpoints denied by every profile
// (ADR 0007). Most sit inside a link-local or private range and would be denied anyway; they
// are listed explicitly because an operator allow list may legitimately open those ranges on
// a private location, and reaching metadata must not come along with that.
//
// 168.63.129.16 is globally routable in appearance but is Azure's host agent, reachable only
// from inside a virtual network, so it is denied like any other metadata endpoint.
var metadataAddresses = map[netip.Addr]struct{}{
	netip.MustParseAddr("169.254.169.254"): {}, // AWS, Azure, GCP, DigitalOcean, OpenStack, Hetzner
	netip.MustParseAddr("169.254.169.253"): {}, // AWS VPC DNS
	netip.MustParseAddr("169.254.169.123"): {}, // AWS time sync
	netip.MustParseAddr("169.254.170.2"):   {}, // AWS ECS task metadata
	netip.MustParseAddr("168.63.129.16"):   {}, // Azure host agent
	netip.MustParseAddr("100.100.100.200"): {}, // Alibaba Cloud
	netip.MustParseAddr("192.0.0.192"):     {}, // Oracle Cloud legacy metadata
	netip.MustParseAddr("fd00:ec2::254"):   {}, // AWS instance metadata over IPv6
	netip.MustParseAddr("fd00:ec2::253"):   {}, // AWS VPC DNS over IPv6
	netip.MustParseAddr("fd00:ec2::123"):   {}, // AWS time sync over IPv6
}

type prefixClass struct {
	prefix netip.Prefix
	class  Class
}

// specialPurposePrefixes follows the IANA IPv4 and IPv6 Special-Purpose Address Registries.
// Order is irrelevant: classify takes the longest matching prefix, so a specific entry always
// beats the wider range containing it.
//
// ::ffff:0:0/96 is absent on purpose. An IPv4-mapped address is unmapped before
// classification, so it is judged as the IPv4 address it carries.
var specialPurposePrefixes = []prefixClass{
	{netip.MustParsePrefix("0.0.0.0/8"), ClassUnspecified},
	{netip.MustParsePrefix("10.0.0.0/8"), ClassPrivate},
	{netip.MustParsePrefix("100.64.0.0/10"), ClassSharedAddressSpace},
	{netip.MustParsePrefix("127.0.0.0/8"), ClassLoopback},
	{netip.MustParsePrefix("169.254.0.0/16"), ClassLinkLocal},
	{netip.MustParsePrefix("172.16.0.0/12"), ClassPrivate},
	{netip.MustParsePrefix("192.0.0.0/24"), ClassProtocolAssignment},
	{netip.MustParsePrefix("192.0.2.0/24"), ClassDocumentation},
	{netip.MustParsePrefix("192.31.196.0/24"), ClassProtocolAssignment},
	{netip.MustParsePrefix("192.52.193.0/24"), ClassProtocolAssignment},
	{netip.MustParsePrefix("192.88.99.0/24"), ClassTransition},
	{netip.MustParsePrefix("192.168.0.0/16"), ClassPrivate},
	{netip.MustParsePrefix("192.175.48.0/24"), ClassProtocolAssignment},
	{netip.MustParsePrefix("198.18.0.0/15"), ClassBenchmark},
	{netip.MustParsePrefix("198.51.100.0/24"), ClassDocumentation},
	{netip.MustParsePrefix("203.0.113.0/24"), ClassDocumentation},
	{netip.MustParsePrefix("224.0.0.0/4"), ClassMulticast},
	{netip.MustParsePrefix("240.0.0.0/4"), ClassReserved},

	{netip.MustParsePrefix("::/128"), ClassUnspecified},
	{netip.MustParsePrefix("::1/128"), ClassLoopback},
	{netip.MustParsePrefix("64:ff9b::/96"), ClassTransition},
	{netip.MustParsePrefix("64:ff9b:1::/48"), ClassTransition},
	{netip.MustParsePrefix("100::/64"), ClassDiscard},
	{netip.MustParsePrefix("2001::/32"), ClassTransition},
	{netip.MustParsePrefix("2001:1::1/128"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:1::2/128"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:2::/48"), ClassBenchmark},
	{netip.MustParsePrefix("2001:3::/32"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:4:112::/48"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:10::/28"), ClassReserved},
	{netip.MustParsePrefix("2001:20::/28"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:30::/28"), ClassProtocolAssignment},
	{netip.MustParsePrefix("2001:db8::/32"), ClassDocumentation},
	{netip.MustParsePrefix("2002::/16"), ClassTransition},
	{netip.MustParsePrefix("2620:4f:8000::/48"), ClassProtocolAssignment},
	{netip.MustParsePrefix("3fff::/20"), ClassDocumentation},
	{netip.MustParsePrefix("5f00::/16"), ClassProtocolAssignment},
	{netip.MustParsePrefix("fc00::/7"), ClassUniqueLocal},
	{netip.MustParsePrefix("fe80::/10"), ClassLinkLocal},
	{netip.MustParsePrefix("ff00::/8"), ClassMulticast},
}

// overridableClasses are the classes an operator allow list may reinstate on a private or
// operator profile. They are the ranges a real network is built from. The classes left out -
// metadata, transition, multicast, reserved, discard, unspecified, and zoned - are never a
// legitimate destination for a check, and allowing them back would either reopen address
// classification through a side door or dial something that cannot answer.
var overridableClasses = map[Class]struct{}{
	ClassLoopback:           {},
	ClassPrivate:            {},
	ClassLinkLocal:          {},
	ClassUniqueLocal:        {},
	ClassSharedAddressSpace: {},
	ClassProtocolAssignment: {},
	ClassDocumentation:      {},
	ClassBenchmark:          {},
}

// CheckAddress applies the profile to one candidate address and returns the normalized form
// to dial. It is the only place an address becomes allowed, and it is called for every
// candidate of every connection attempt, including each redirect hop.
func (policy Policy) CheckAddress(addr netip.Addr) (netip.Addr, error) {
	if err := policy.configured(); err != nil {
		return netip.Addr{}, err
	}
	if !addr.IsValid() {
		return netip.Addr{}, &Error{Reason: ReasonAddressDenied, Class: ClassUnspecified}
	}
	if addr.Zone() != "" {
		return netip.Addr{}, &Error{Reason: ReasonAddressDenied, Class: ClassZoned, Address: addr.WithZone("")}
	}

	normalized := addr.Unmap()
	class := classify(normalized)
	if class == ClassPublic {
		return normalized, nil
	}
	if _, overridable := overridableClasses[class]; overridable && policy.allowsAddress(normalized) {
		return normalized, nil
	}
	return netip.Addr{}, &Error{Reason: ReasonAddressDenied, Class: class, Address: normalized}
}

// Classify reports the class of an address without consulting a profile. It is exported for
// diagnostics and tests; an allow decision always goes through CheckAddress.
func Classify(addr netip.Addr) Class {
	if !addr.IsValid() {
		return ClassUnspecified
	}
	if addr.Zone() != "" {
		return ClassZoned
	}
	return classify(addr.Unmap())
}

func classify(addr netip.Addr) Class {
	if _, metadata := metadataAddresses[addr]; metadata {
		return ClassMetadata
	}
	class := ClassPublic
	longest := -1
	for _, entry := range specialPurposePrefixes {
		if entry.prefix.Bits() > longest && entry.prefix.Contains(addr) {
			class, longest = entry.class, entry.prefix.Bits()
		}
	}
	return class
}

func (policy Policy) allowsAddress(addr netip.Addr) bool {
	for _, prefix := range policy.allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
