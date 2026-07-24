package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestCredentialLimiterBoundsHighCardinalityPartitions(t *testing.T) {
	now := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	limiter := newCredentialLimiter(1, func() time.Time { return now })

	for index := 0; index < maxCredentialPartitions; index++ {
		if !limiter.allow(fmt.Sprintf("192.0.2.%d", index)) {
			t.Fatalf("partition %d was unexpectedly limited", index)
		}
	}
	if got := len(limiter.entries); got != maxCredentialPartitions {
		t.Fatalf("partition count = %d, want %d", got, maxCredentialPartitions)
	}

	if !limiter.allow("2001:db8::1") {
		t.Fatal("first overflow attempt was unexpectedly limited")
	}
	if limiter.allow("2001:db8::2") {
		t.Fatal("distinct overflow partitions did not share the bounded window")
	}
	if got := len(limiter.entries); got != maxCredentialPartitions {
		t.Fatalf("partition count after overflow = %d, want %d", got, maxCredentialPartitions)
	}

	now = now.Add(credentialWindow)
	if !limiter.allow("198.51.100.1") {
		t.Fatal("fresh partition was limited after stale entries were cleaned")
	}
	if got := len(limiter.entries); got != 1 {
		t.Fatalf("partition count after cleanup = %d, want 1", got)
	}
}

func TestCredentialLimiterKeepsUnknownPartitionsInActiveOverflow(t *testing.T) {
	now := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	limiter := newCredentialLimiter(1, func() time.Time { return now })

	for index := 0; index < maxCredentialPartitions; index++ {
		if !limiter.allow(fmt.Sprintf("192.0.2.%d", index)) {
			t.Fatalf("partition %d was unexpectedly limited", index)
		}
	}

	now = now.Add(credentialWindow - time.Second)
	if !limiter.allow("2001:db8::1") {
		t.Fatal("first overflow attempt was unexpectedly limited")
	}
	if limiter.allow("2001:db8::2") {
		t.Fatal("overflow allowance was not exhausted")
	}

	// Expire and clean the normal partitions while the later-started overflow
	// window remains active.
	now = now.Add(time.Second)
	if limiter.allow("2001:db8::2") {
		t.Fatal("overflowed partition received a fresh allowance after normal cleanup")
	}
	if limiter.allow("2001:db8::3") {
		t.Fatal("new partition escaped the active overflow window after normal cleanup")
	}
	if got := len(limiter.entries); got != 0 {
		t.Fatalf("partition count during active overflow = %d, want 0", got)
	}

	// Admission to the normal table resumes only after the overflow window itself expires.
	now = now.Add(credentialWindow - time.Second)
	if !limiter.allow("2001:db8::2") {
		t.Fatal("partition remained limited after the overflow window expired")
	}
	if got := len(limiter.entries); got != 1 {
		t.Fatalf("partition count after overflow expiry = %d, want 1", got)
	}
}
