package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const wrappingKeySize = 32

var (
	ErrKeyringUnavailable   = errors.New("Webhook signing-secret keyring is unavailable")
	ErrSecretAuthentication = errors.New("Webhook signing-secret ciphertext authentication failed")
)

type WrappingKey struct {
	ID  string
	Key []byte
}

type Envelope struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
}

type Keyring struct {
	active string
	keys   map[string][]byte
}

func ParseKeyring(value string) (*Keyring, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	entries := strings.Split(value, ",")
	keys := make([]WrappingKey, 0, len(entries))
	for _, entry := range entries {
		id, encoded, found := strings.Cut(entry, ":")
		if !found || !validKeyID(id) || encoded == "" {
			return nil, errors.New("PROBEHIVE_WEBHOOK_KEYRING entries must be keyId:base64url")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(decoded) != wrappingKeySize {
			return nil, fmt.Errorf("PROBEHIVE_WEBHOOK_KEYRING key %q must be 32 unpadded base64url bytes", id)
		}
		keys = append(keys, WrappingKey{ID: id, Key: decoded})
	}
	return NewKeyring(keys)
}

func NewKeyring(entries []WrappingKey) (*Keyring, error) {
	if len(entries) == 0 {
		return nil, errors.New("a Webhook keyring requires at least one key")
	}
	keys := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if !validKeyID(entry.ID) {
			return nil, fmt.Errorf("invalid Webhook wrapping-key id %q", entry.ID)
		}
		if len(entry.Key) != wrappingKeySize {
			return nil, fmt.Errorf("Webhook wrapping key %q must contain 32 bytes", entry.ID)
		}
		if _, exists := keys[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate Webhook wrapping-key id %q", entry.ID)
		}
		keys[entry.ID] = append([]byte(nil), entry.Key...)
	}
	return &Keyring{active: entries[0].ID, keys: keys}, nil
}

func (keyring *Keyring) ActiveKeyID() string {
	if keyring == nil {
		return ""
	}
	return keyring.active
}

func (keyring *Keyring) Seal(plaintext, associatedData []byte, random io.Reader) (Envelope, error) {
	if keyring == nil || random == nil {
		return Envelope{}, ErrKeyringUnavailable
	}
	block, err := aes.NewCipher(keyring.keys[keyring.active])
	if err != nil {
		return Envelope{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return Envelope{}, fmt.Errorf("generate Webhook secret nonce: %w", err)
	}
	return Envelope{
		KeyID:      keyring.active,
		Nonce:      nonce,
		Ciphertext: aead.Seal(nil, nonce, plaintext, associatedData),
	}, nil
}

func (keyring *Keyring) Open(envelope Envelope, associatedData []byte) ([]byte, error) {
	if keyring == nil {
		return nil, ErrKeyringUnavailable
	}
	key, exists := keyring.keys[envelope.KeyID]
	if !exists {
		return nil, fmt.Errorf("%w: missing key %q", ErrKeyringUnavailable, envelope.KeyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(envelope.Nonce) != aead.NonceSize() || len(envelope.Ciphertext) < aead.Overhead() {
		return nil, ErrSecretAuthentication
	}
	plaintext, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, associatedData)
	if err != nil {
		return nil, ErrSecretAuthentication
	}
	return plaintext, nil
}

func validKeyID(value string) bool {
	if len(value) < 1 || len(value) > 32 {
		return false
	}
	for index := range value {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			(index > 0 && (character == '-' || character == '_' || character == '.')) {
			continue
		}
		return false
	}
	return true
}
