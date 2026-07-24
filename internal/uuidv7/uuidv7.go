// Package uuidv7 generates RFC 9562 UUID version 7 identifiers.
package uuidv7

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const maxUnixMilliseconds = int64(1<<48 - 1)

// Generator combines a caller-supplied timestamp with cryptographically random bits.
type Generator struct {
	random io.Reader
}

// New returns a Generator backed by crypto/rand.Reader.
func New() Generator { return Generator{random: rand.Reader} }

// NewWithReader returns a Generator with an injectable cryptographic random source.
// The alternate source exists for deterministic tests and must be cryptographically secure in production.
func NewWithReader(random io.Reader) (Generator, error) {
	if random == nil {
		return Generator{}, errors.New("UUIDv7 requires a random source")
	}
	return Generator{random: random}, nil
}

// NewUUIDv7 returns a lowercase canonical RFC 9562 UUIDv7 string for the supplied instant.
func (generator Generator) NewUUIDv7(at time.Time) (string, error) {
	if generator.random == nil {
		return "", errors.New("UUIDv7 Generator is not initialized")
	}
	milliseconds := at.UnixMilli()
	if milliseconds < 0 || milliseconds > maxUnixMilliseconds {
		return "", fmt.Errorf("UUIDv7 timestamp is outside the 48-bit Unix millisecond range")
	}

	var value [16]byte
	if _, err := io.ReadFull(generator.random, value[6:]); err != nil {
		return "", fmt.Errorf("read UUIDv7 randomness: %w", err)
	}
	for index := 5; index >= 0; index-- {
		value[index] = byte(milliseconds)
		milliseconds >>= 8
	}
	value[6] = value[6]&0x0f | 0x70
	value[8] = value[8]&0x3f | 0x80

	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:]), nil
}
