package uuidv7

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"
)

func TestGeneratorProducesRFC9562Version7FromInjectedTimestamp(t *testing.T) {
	t.Parallel()
	random := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	generator, err := NewWithReader(bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 24, 12, 34, 56, 789_999_999, time.FixedZone("offset", 8*60*60))
	encoded, err := generator.NewUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 36 || encoded[8] != '-' || encoded[13] != '-' || encoded[18] != '-' || encoded[23] != '-' {
		t.Fatalf("non-canonical UUID: %q", encoded)
	}
	value := decodeUUID(t, encoded)
	milliseconds := int64(0)
	for _, octet := range value[:6] {
		milliseconds = milliseconds<<8 | int64(octet)
	}
	if milliseconds != at.UnixMilli() {
		t.Fatalf("timestamp = %d, want %d", milliseconds, at.UnixMilli())
	}
	if value[6]>>4 != 7 {
		t.Fatalf("version nibble = %x", value[6]>>4)
	}
	if value[8]>>6 != 2 {
		t.Fatalf("variant bits = %b", value[8]>>6)
	}
	if value[6]&0x0f != random[0]&0x0f || value[7] != random[1] || value[8]&0x3f != random[2]&0x3f || !bytes.Equal(value[9:], random[3:]) {
		t.Fatalf("random bits were not preserved: %x", value[6:])
	}
}

func TestGeneratorRejectsTimestampRangeAndRandomFailures(t *testing.T) {
	t.Parallel()
	generator, _ := NewWithReader(bytes.NewReader(make([]byte, 10)))
	if _, err := generator.NewUUIDv7(time.UnixMilli(-1)); err == nil {
		t.Fatal("negative Unix timestamp accepted")
	}
	if _, err := generator.NewUUIDv7(time.UnixMilli(1 << 48)); err == nil {
		t.Fatal("timestamp beyond 48 bits accepted")
	}
	sentinel := errors.New("random failed")
	failing, _ := NewWithReader(errorReader{err: sentinel})
	if _, err := failing.NewUUIDv7(time.UnixMilli(0)); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped sentinel", err)
	}
	short, _ := NewWithReader(io.LimitReader(bytes.NewReader(make([]byte, 10)), 9))
	if _, err := short.NewUUIDv7(time.UnixMilli(0)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short-reader error = %v", err)
	}
	if _, err := NewWithReader(nil); err == nil {
		t.Fatal("nil random source accepted")
	}
	if _, err := (Generator{}).NewUUIDv7(time.UnixMilli(0)); err == nil {
		t.Fatal("zero Generator accepted")
	}
}

func TestCryptoGeneratorProducesDistinctIdentifiersAtOneInstant(t *testing.T) {
	t.Parallel()
	generator := New()
	at := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	first, err := generator.NewUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.NewUUIDv7(at)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two UUIDv7 values unexpectedly matched: %s", first)
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func decodeUUID(t *testing.T, encoded string) []byte {
	t.Helper()
	compact := encoded[:8] + encoded[9:13] + encoded[14:18] + encoded[19:23] + encoded[24:]
	value, err := hex.DecodeString(compact)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
