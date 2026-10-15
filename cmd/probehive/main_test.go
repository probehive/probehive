package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentValueUsesDirectOrFileValue(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		t.Setenv("PROBEHIVE_TEST_SECRET", "  direct-value  ")
		t.Setenv("PROBEHIVE_TEST_SECRET_FILE", "")

		value, err := environmentValue("PROBEHIVE_TEST_SECRET")
		if err != nil || value != "direct-value" {
			t.Fatalf("environmentValue() = %q, %v", value, err)
		}
	})

	t.Run("file", func(t *testing.T) {
		t.Setenv("PROBEHIVE_TEST_SECRET", "")
		secretPath := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(secretPath, []byte("file-value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PROBEHIVE_TEST_SECRET_FILE", secretPath)

		value, err := environmentValue("PROBEHIVE_TEST_SECRET")
		if err != nil || value != "file-value" {
			t.Fatalf("environmentValue() = %q, %v", value, err)
		}
	})
}

func TestEnvironmentValueRejectsAmbiguousOrInvalidFiles(t *testing.T) {
	t.Run("both sources", func(t *testing.T) {
		t.Setenv("PROBEHIVE_TEST_SECRET", "direct")
		t.Setenv("PROBEHIVE_TEST_SECRET_FILE", "secret")

		_, err := environmentValue("PROBEHIVE_TEST_SECRET")
		if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
			t.Fatalf("environmentValue() error = %v", err)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Setenv("PROBEHIVE_TEST_SECRET", "")
		t.Setenv("PROBEHIVE_TEST_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))

		_, err := environmentValue("PROBEHIVE_TEST_SECRET")
		if err == nil || !strings.Contains(err.Error(), "read PROBEHIVE_TEST_SECRET_FILE") {
			t.Fatalf("environmentValue() error = %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		t.Setenv("PROBEHIVE_TEST_SECRET", "")
		secretPath := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(secretPath, []byte(" \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PROBEHIVE_TEST_SECRET_FILE", secretPath)

		_, err := environmentValue("PROBEHIVE_TEST_SECRET")
		if err == nil || !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("environmentValue() error = %v", err)
		}
	})
}

func TestRequiredEnvironmentAcceptsFileAndRejectsMissingValue(t *testing.T) {
	t.Setenv("PROBEHIVE_TEST_REQUIRED", "")
	t.Setenv("PROBEHIVE_TEST_REQUIRED_FILE", "")

	_, err := requiredEnvironment("PROBEHIVE_TEST_REQUIRED")
	if err == nil ||
		!strings.Contains(err.Error(), "PROBEHIVE_TEST_REQUIRED_FILE is required") {
		t.Fatalf("requiredEnvironment() error = %v", err)
	}
}

func TestLaunchRejectsUnknownArguments(t *testing.T) {
	err := launch([]string{"bogus"}, nil)
	if err == nil || !strings.Contains(err.Error(), "usage: probehive") {
		t.Fatalf("launch error = %v", err)
	}
}

func TestResetPasswordRejectsMissingOrInvalidEmailWithoutDatabase(t *testing.T) {
	for _, arguments := range [][]string{nil, {"a", "b"}} {
		if err := resetPassword(arguments, nil); err == nil ||
			!strings.Contains(err.Error(), "usage: probehive reset-password") {
			t.Fatalf("resetPassword(%q) error = %v", arguments, err)
		}
	}
	err := resetPassword([]string{"not-an-email"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not a valid email") {
		t.Fatalf("invalid email error = %v", err)
	}
}

func TestReadReplacementPasswordReadsPipedLine(t *testing.T) {
	original := os.Stdin
	passwordFile := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordFile, []byte("replacement-password\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Open(passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = replacement
	defer func() {
		os.Stdin = original
		replacement.Close()
	}()
	value, err := readReplacementPassword()
	if err != nil || value != "replacement-password" {
		t.Fatalf("readReplacementPassword = %q, %v", value, err)
	}
}
