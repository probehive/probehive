package password

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/probehive/probehive/internal/user"
)

func TestRecommendedParameters(t *testing.T) {
	parameters := RecommendedParameters()
	if parameters.MemoryKiB != 64*1024 || parameters.Time != 3 || parameters.Parallelism != 1 || parameters.SaltLength != 16 || parameters.KeyLength != 32 {
		t.Fatalf("RecommendedParameters = %#v", parameters)
	}
}

func TestHashUsesArgon2idPHCAndFreshInjectedSalt(t *testing.T) {
	parameters := testParameters()
	salt := bytes.Repeat([]byte{0x5a}, parameters.SaltLength)
	hasher, err := NewWithParameters(parameters, bytes.NewReader(salt))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "$argon2id$v=19$m=8192,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$"
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatalf("PHC = %q, want prefix %q", encoded, prefix)
	}
	parts := strings.Split(encoded, "$")
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != int(parameters.KeyLength) {
		t.Fatalf("derived key length = %d, error %v", len(key), err)
	}
}

func TestVerifySuccessFailureAndConstantLengthTamper(t *testing.T) {
	parameters := testParameters()
	hasher, _ := NewWithParameters(parameters, bytes.NewReader(bytes.Repeat([]byte{1}, parameters.SaltLength)))
	encoded, err := hasher.Hash("password")
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := hasher.Verify(encoded, "password"); err != nil || outcome != user.PasswordVerified {
		t.Fatalf("correct Verify = %v, %v", outcome, err)
	}
	if outcome, err := hasher.Verify(encoded, "wrong"); err != nil || outcome != user.PasswordFailed {
		t.Fatalf("wrong Verify = %v, %v", outcome, err)
	}
	parts := strings.Split(encoded, "$")
	key, _ := base64.RawStdEncoding.DecodeString(parts[5])
	key[0] ^= 0xff
	parts[5] = base64.RawStdEncoding.EncodeToString(key)
	if outcome, err := hasher.Verify(strings.Join(parts, "$"), "password"); err != nil || outcome != user.PasswordFailed {
		t.Fatalf("tampered Verify = %v, %v", outcome, err)
	}
}

func TestVerifyRequestsRehashWhenAnyPolicyParameterDiffers(t *testing.T) {
	oldParameters := testParameters()
	oldHasher, _ := NewWithParameters(oldParameters, bytes.NewReader(bytes.Repeat([]byte{2}, oldParameters.SaltLength)))
	encoded, err := oldHasher.Hash("password")
	if err != nil {
		t.Fatal(err)
	}
	current := New()
	outcome, err := current.Verify(encoded, "password")
	if err != nil || outcome != user.PasswordVerifiedRehashNeeded {
		t.Fatalf("Verify = %v, %v; want rehash", outcome, err)
	}
}

func TestParserRejectsMalformedAndResourceAmplifyingPHCBeforeDerivation(t *testing.T) {
	validSalt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	validKey := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	valid := func(cost, salt, key string) string {
		return "$argon2id$v=19$" + cost + "$" + salt + "$" + key
	}
	tests := []string{
		"",
		"$argon2i$v=19$m=65536,t=3,p=1$" + validSalt + "$" + validKey,
		"$argon2id$v=18$m=65536,t=3,p=1$" + validSalt + "$" + validKey,
		valid("m=65536,t=3", validSalt, validKey),
		valid("m=65536,t=3,t=3", validSalt, validKey),
		valid("m=65536,t=3,x=1", validSalt, validKey),
		valid("m=262145,t=3,p=1", validSalt, validKey),
		valid("m=65536,t=0,p=1", validSalt, validKey),
		valid("m=65536,t=11,p=1", validSalt, validKey),
		valid("m=65536,t=3,p=0", validSalt, validKey),
		valid("m=65536,t=3,p=5", validSalt, validKey),
		valid("m=65536,t=3,p=1", "%%%", validKey),
		valid("m=65536,t=3,p=1", base64.RawStdEncoding.EncodeToString(make([]byte, 7)), validKey),
		valid("m=65536,t=3,p=1", validSalt, base64.RawStdEncoding.EncodeToString(make([]byte, 15))),
		valid("m=65536,t=3,p=1", validSalt+"=", validKey),
		strings.Repeat("x", maximumPHCLength+1),
	}
	hasher := New()
	for index, encoded := range tests {
		outcome, err := hasher.Verify(encoded, "password")
		if err == nil || outcome != user.PasswordFailed {
			t.Errorf("case %d: outcome = %v, error %v", index, outcome, err)
		}
	}
}

func TestConstructionAndSaltReadErrors(t *testing.T) {
	if _, err := NewWithParameters(Parameters{}, bytes.NewReader(nil)); err == nil {
		t.Fatal("invalid parameters accepted")
	}
	if _, err := NewWithParameters(RecommendedParameters(), nil); err == nil {
		t.Fatal("nil random source accepted")
	}
	sentinel := errors.New("random failed")
	hasher, err := NewWithParameters(testParameters(), errorReader{err: sentinel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hasher.Hash("password"); !errors.Is(err, sentinel) {
		t.Fatalf("Hash error = %v, want wrapped sentinel", err)
	}
	short, _ := NewWithParameters(testParameters(), io.LimitReader(bytes.NewReader(make([]byte, 16)), 15))
	if _, err = short.Hash("password"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short salt error = %v", err)
	}
	if _, err = (*Hasher)(nil).Hash("password"); err == nil {
		t.Fatal("nil Hasher accepted")
	}
}

func testParameters() Parameters {
	return Parameters{MemoryKiB: minimumMemoryKiB, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }
