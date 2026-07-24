// Package user owns local users, credential verification, sessions, and antiforgery persistence ports.
package user

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

const (
	// AdministratorRole is the only current instance role.
	AdministratorRole = "Administrator"
	// EmailValidationMessage is the exact first-administrator email validation message.
	EmailValidationMessage = "An email address contains one '@' with non-empty sides, no whitespace, and at most 254 characters."
	// DisplayNameValidationMessage is the exact first-administrator display-name validation message.
	DisplayNameValidationMessage = "A display name is 1 to 100 characters after trimming."
	// PasswordValidationMessage is the exact first-administrator password validation message.
	PasswordValidationMessage = "A password is 12 to 128 characters."
	// SetupAlreadyCompletedTitle is the public setup conflict title.
	SetupAlreadyCompletedTitle = "Setup already completed"
	// SetupAlreadyCompletedDetail is the public setup conflict detail.
	SetupAlreadyCompletedDetail = "The instance already has at least one user; sign in instead."
	// InvalidCredentialsTitle is the generic login failure title.
	InvalidCredentialsTitle = "Invalid credentials"
	// InvalidCredentialsDetail is the generic login failure detail.
	InvalidCredentialsDetail = "The email and password combination did not match a local account."
)

// ID identifies a local user.
type ID string

// User is an instance-scoped local account. PasswordHash is never serialized to clients.
type User struct {
	ID           ID
	Email        string
	DisplayName  string
	Role         string
	PasswordHash string
	CreatedAt    time.Time
}

// NewUser creates or restores a user while enforcing local-account invariants.
func NewUser(id ID, email, displayName, role, passwordHash string, createdAt time.Time) (User, error) {
	if id == "" {
		return User{}, errors.New("a user requires an id")
	}
	if normalized, ok := NormalizeEmail(email); !ok || normalized != email {
		return User{}, errors.New("invalid normalized email")
	}
	if normalized, ok := NormalizeDisplayName(displayName); !ok || normalized != displayName {
		return User{}, errors.New("invalid user display name")
	}
	if role != AdministratorRole {
		return User{}, errors.New("unknown instance role")
	}
	if strings.TrimSpace(passwordHash) == "" {
		return User{}, errors.New("a user requires a non-empty password hash")
	}
	if !isUTC(createdAt) {
		return User{}, errors.New("persisted timestamps must be UTC")
	}
	return User{
		ID: id, Email: email, DisplayName: displayName, Role: role,
		PasswordHash: passwordHash, CreatedAt: createdAt,
	}, nil
}

// ReplacePasswordHash applies a rehash-on-success result.
func (value *User) ReplacePasswordHash(passwordHash string) error {
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("a user requires a non-empty password hash")
	}
	value.PasswordHash = passwordHash
	return nil
}

// ValidationFailure is one field-level use-case validation failure.
type ValidationFailure struct {
	Field   string
	Message string
}

// NormalizeEmail trims and invariant-lowercases a login email before validation.
func NormalizeEmail(candidate string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(candidate))
	length := utf16Length(normalized)
	if length < 1 || length > 254 {
		return "", false
	}
	at := -1
	for index, character := range normalized {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", false
		}
		if character == '@' {
			if at >= 0 {
				return "", false
			}
			at = index
		}
	}
	if at < 1 || at >= len(normalized)-1 {
		return "", false
	}
	return normalized, true
}

// NormalizeDisplayName trims Unicode surrounding whitespace and enforces 100 UTF-16 code units.
func NormalizeDisplayName(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := utf16Length(normalized)
	if length < 1 || length > 100 {
		return "", false
	}
	return normalized, true
}

// ValidPassword reports whether a password contains 12 to 128 UTF-16 code units.
// It deliberately performs no trimming or normalization.
func ValidPassword(password string) bool {
	length := utf16Length(password)
	return length >= 12 && length <= 128
}

func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
