package clock

import (
	"testing"
	"time"
)

func TestSystemReturnsCurrentUTCInstant(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	got := (System{}).Now()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("System.Now() = %v, outside [%v, %v]", got, before, after)
	}
	_, offset := got.Zone()
	if offset != 0 || got.Location() != time.UTC {
		t.Fatalf("System.Now() = %v in %v with offset %d, want UTC", got, got.Location(), offset)
	}
}
