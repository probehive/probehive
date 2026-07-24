// Package password implements local password hashing with Argon2id PHC strings.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/probehive/probehive/internal/user"
	"golang.org/x/crypto/argon2"
)

const (
	DefaultMemoryKiB   uint32 = 64 * 1024
	DefaultTime        uint32 = 3
	DefaultParallelism uint8  = 1
	DefaultSaltLength         = 16
	DefaultKeyLength   uint32 = 32

	minimumMemoryKiB   uint32 = 8 * 1024
	maximumMemoryKiB   uint32 = 256 * 1024
	minimumTime        uint32 = 1
	maximumTime        uint32 = 10
	minimumParallelism uint8  = 1
	maximumParallelism uint8  = 4
	minimumSaltLength         = 8
	maximumSaltLength         = 64
	minimumKeyLength   uint32 = 16
	maximumKeyLength   uint32 = 64
	maximumPHCLength          = 1024
)

var rawBase64 = base64.RawStdEncoding.Strict()

// Parameters control Argon2id resource cost and output sizes. Memory is in KiB.
type Parameters struct {
	MemoryKiB   uint32
	Time        uint32
	Parallelism uint8
	SaltLength  int
	KeyLength   uint32
}

// RecommendedParameters returns the reviewed password policy: 64 MiB, three passes,
// one lane, a 16-byte random salt, and a 32-byte derived key.
func RecommendedParameters() Parameters {
	return Parameters{
		MemoryKiB: DefaultMemoryKiB, Time: DefaultTime,
		Parallelism: DefaultParallelism, SaltLength: DefaultSaltLength,
		KeyLength: DefaultKeyLength,
	}
}

// Hasher encodes Argon2id hashes as versioned PHC strings.
type Hasher struct {
	parameters Parameters
	random     io.Reader
}

var _ user.PasswordHasher = (*Hasher)(nil)

// New returns a Hasher using reviewed parameters and crypto/rand.Reader.
func New() *Hasher {
	hasher, err := NewWithParameters(RecommendedParameters(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return hasher
}

// NewWithParameters constructs a Hasher with bounded parameters and an injected salt source.
func NewWithParameters(parameters Parameters, random io.Reader) (*Hasher, error) {
	if random == nil {
		return nil, errors.New("password hashing requires a random source")
	}
	if err := validateParameters(parameters); err != nil {
		return nil, err
	}
	return &Hasher{parameters: parameters, random: random}, nil
}

// Hash derives and PHC-encodes an Argon2id password hash with a fresh random salt.
func (hasher *Hasher) Hash(password string) (string, error) {
	if hasher == nil || hasher.random == nil {
		return "", errors.New("password Hasher is not initialized")
	}
	salt := make([]byte, hasher.parameters.SaltLength)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key := argon2.IDKey(
		[]byte(password), salt,
		hasher.parameters.Time, hasher.parameters.MemoryKiB,
		hasher.parameters.Parallelism, hasher.parameters.KeyLength,
	)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		hasher.parameters.MemoryKiB,
		hasher.parameters.Time,
		hasher.parameters.Parallelism,
		rawBase64.EncodeToString(salt),
		rawBase64.EncodeToString(key),
	), nil
}

// Verify parses bounded PHC parameters, derives a candidate key, and compares it in constant time.
func (hasher *Hasher) Verify(encoded, password string) (user.PasswordVerification, error) {
	if hasher == nil {
		return user.PasswordFailed, errors.New("password Hasher is not initialized")
	}
	parsed, err := parsePHC(encoded)
	if err != nil {
		return user.PasswordFailed, err
	}
	candidate := argon2.IDKey(
		[]byte(password), parsed.salt,
		parsed.parameters.Time, parsed.parameters.MemoryKiB,
		parsed.parameters.Parallelism, parsed.parameters.KeyLength,
	)
	if subtle.ConstantTimeCompare(candidate, parsed.key) != 1 {
		return user.PasswordFailed, nil
	}
	if parsed.parameters != hasher.parameters {
		return user.PasswordVerifiedRehashNeeded, nil
	}
	return user.PasswordVerified, nil
}

type parsedPHC struct {
	parameters Parameters
	salt       []byte
	key        []byte
}

func parsePHC(encoded string) (parsedPHC, error) {
	if len(encoded) == 0 || len(encoded) > maximumPHCLength {
		return parsedPHC{}, errors.New("invalid Argon2id PHC length")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return parsedPHC{}, errors.New("invalid Argon2id PHC envelope")
	}
	memory, iterations, parallelism, err := parseCostParameters(parts[3])
	if err != nil {
		return parsedPHC{}, err
	}
	if memory < minimumMemoryKiB || memory > maximumMemoryKiB ||
		iterations < minimumTime || iterations > maximumTime ||
		parallelism < minimumParallelism || parallelism > maximumParallelism ||
		memory < 8*uint32(parallelism) {
		return parsedPHC{}, errors.New("Argon2id PHC cost is outside supported bounds")
	}
	salt, err := rawBase64.DecodeString(parts[4])
	if err != nil {
		return parsedPHC{}, errors.New("invalid Argon2id PHC salt encoding")
	}
	key, err := rawBase64.DecodeString(parts[5])
	if err != nil {
		return parsedPHC{}, errors.New("invalid Argon2id PHC key encoding")
	}
	parameters := Parameters{
		MemoryKiB: memory, Time: iterations, Parallelism: parallelism,
		SaltLength: len(salt), KeyLength: uint32(len(key)),
	}
	if err = validateParameters(parameters); err != nil {
		return parsedPHC{}, fmt.Errorf("invalid Argon2id PHC parameters: %w", err)
	}
	return parsedPHC{parameters: parameters, salt: salt, key: key}, nil
}

func parseCostParameters(encoded string) (uint32, uint32, uint8, error) {
	parts := strings.Split(encoded, ",")
	if len(parts) != 3 {
		return 0, 0, 0, errors.New("invalid Argon2id PHC cost parameters")
	}
	values := make(map[string]uint64, 3)
	for _, part := range parts {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 || (pair[0] != "m" && pair[0] != "t" && pair[0] != "p") {
			return 0, 0, 0, errors.New("invalid Argon2id PHC cost parameter")
		}
		if _, duplicate := values[pair[0]]; duplicate || pair[1] == "" || pair[1][0] == '+' || pair[1][0] == '-' {
			return 0, 0, 0, errors.New("invalid Argon2id PHC cost parameter")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return 0, 0, 0, errors.New("invalid Argon2id PHC cost parameter")
		}
		values[pair[0]] = value
	}
	if _, ok := values["m"]; !ok {
		return 0, 0, 0, errors.New("missing Argon2id memory cost")
	}
	if _, ok := values["t"]; !ok {
		return 0, 0, 0, errors.New("missing Argon2id time cost")
	}
	if _, ok := values["p"]; !ok || values["p"] > 255 {
		return 0, 0, 0, errors.New("missing or invalid Argon2id parallelism")
	}
	return uint32(values["m"]), uint32(values["t"]), uint8(values["p"]), nil
}

func validateParameters(parameters Parameters) error {
	if parameters.MemoryKiB < minimumMemoryKiB || parameters.MemoryKiB > maximumMemoryKiB || parameters.MemoryKiB < 8*uint32(parameters.Parallelism) {
		return fmt.Errorf("memory must be between %d and %d KiB", minimumMemoryKiB, maximumMemoryKiB)
	}
	if parameters.Time < minimumTime || parameters.Time > maximumTime {
		return fmt.Errorf("time must be between %d and %d", minimumTime, maximumTime)
	}
	if parameters.Parallelism < minimumParallelism || parameters.Parallelism > maximumParallelism {
		return fmt.Errorf("parallelism must be between %d and %d", minimumParallelism, maximumParallelism)
	}
	if parameters.SaltLength < minimumSaltLength || parameters.SaltLength > maximumSaltLength {
		return fmt.Errorf("salt length must be between %d and %d bytes", minimumSaltLength, maximumSaltLength)
	}
	if parameters.KeyLength < minimumKeyLength || parameters.KeyLength > maximumKeyLength {
		return fmt.Errorf("key length must be between %d and %d bytes", minimumKeyLength, maximumKeyLength)
	}
	return nil
}
