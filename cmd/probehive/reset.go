package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/probehive/probehive/internal/clock"
	"github.com/probehive/probehive/internal/password"
	"github.com/probehive/probehive/internal/postgres"
	"github.com/probehive/probehive/internal/user"
	"github.com/probehive/probehive/internal/uuidv7"
)

// resetPassword replaces one local user's password directly in the database. It is
// the recovery path for a forgotten password: whoever can run it already controls
// the database credentials, so no email verification or old-password proof exists.
// Existing sessions keep working; only the next login uses the new password.
func resetPassword(arguments []string, logger *slog.Logger) error {
	if len(arguments) != 1 {
		return errors.New("usage: probehive reset-password <email>")
	}
	email, validEmail := user.NormalizeEmail(arguments[0])
	if !validEmail {
		return fmt.Errorf("%q is not a valid email address", arguments[0])
	}
	databaseURL, err := requiredEnvironment("PROBEHIVE_DATABASE_URL")
	if err != nil {
		return err
	}

	connectContext, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	database, err := postgres.Open(connectContext, databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	// The account is resolved before prompting so a mistyped email costs nothing.
	_, found, err := database.Users().FindByEmail(connectContext, email)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no local user has email %s", email)
	}

	replacement, err := readReplacementPassword()
	if err != nil {
		return err
	}
	users := user.NewService(database.Users(), password.New(), clock.System{}, uuidv7.New())
	result, err := users.ResetPassword(connectContext, user.ResetPasswordCommand{
		Email: email, Password: replacement,
	})
	if err != nil {
		return err
	}
	switch result.Kind {
	case user.ResetPasswordReset:
		logger.Info("Password reset", "email", result.User.Email)
		return nil
	case user.ResetPasswordInvalid:
		messages := make([]string, 0, len(result.Failures))
		for _, failure := range result.Failures {
			messages = append(messages, failure.Message)
		}
		return fmt.Errorf("invalid replacement password: %s", strings.Join(messages, "; "))
	case user.ResetPasswordNotFound:
		return fmt.Errorf("no local user has email %s", email)
	default:
		return errors.New("unexpected reset-password result")
	}
}

// readReplacementPassword prompts twice without echo when stdin is a terminal and
// reads one line when stdin is piped, so automation can supply the password.
func readReplacementPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", readErr)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	first, err := promptPassword("New password: ")
	if err != nil {
		return "", err
	}
	second, err := promptPassword("Confirm password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the passwords do not match")
	}
	return first, nil
}

// promptPassword reads one secret from the terminal without echoing it.
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(secret), nil
}
