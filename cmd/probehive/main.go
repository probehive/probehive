// Command probehive runs the ProbeHive HTTP API and its database operations.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/check"
	"github.com/probehive/probehive/internal/clock"
	"github.com/probehive/probehive/internal/health"
	"github.com/probehive/probehive/internal/httpapi"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/password"
	"github.com/probehive/probehive/internal/postgres"
	"github.com/probehive/probehive/internal/run"
	"github.com/probehive/probehive/internal/user"
	"github.com/probehive/probehive/internal/uuidv7"
	"github.com/probehive/probehive/internal/webhook"
)

const (
	defaultHTTPAddress = ":8080"
	startupTimeout     = 30 * time.Second
	shutdownTimeout    = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := launch(os.Args[1:], logger); err != nil {
		logger.Error("ProbeHive stopped", "error", err)
		os.Exit(1)
	}
}

// launch is named around the run package rather than shadowing it: the composition root
// imports internal/run, and an entry point called run() would make that import unusable.
func launch(arguments []string, logger *slog.Logger) error {
	if len(arguments) != 0 {
		return errors.New("usage: probehive")
	}
	return serve(logger)
}

func serve(logger *slog.Logger) error {
	databaseURL, err := requiredEnvironment("PROBEHIVE_DATABASE_URL")
	if err != nil {
		return err
	}
	credentialAttempts, err := positiveEnvironmentInt("PROBEHIVE_CREDENTIAL_ATTEMPTS_PER_MINUTE", 10)
	if err != nil {
		return err
	}
	address := strings.TrimSpace(os.Getenv("PROBEHIVE_HTTP_ADDRESS"))
	if address == "" {
		address = defaultHTTPAddress
	}
	development := strings.EqualFold(strings.TrimSpace(os.Getenv("PROBEHIVE_ENVIRONMENT")), "Development")
	// Worker configuration is read before anything opens, so an installation with a
	// misconfigured outbound policy fails at startup rather than at its first check.
	workerConfiguration, err := readWorkerSettings()
	if err != nil {
		return err
	}
	webhookKeyring, err := webhook.ParseKeyring(os.Getenv("PROBEHIVE_WEBHOOK_KEYRING"))
	if err != nil {
		return err
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()
	database, err := postgres.Open(startupContext, databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Migrate(startupContext); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}

	systemClock := clock.System{}
	identifiers := uuidv7.New()
	users := user.NewService(database.Users(), password.New(), systemClock, identifiers)
	organizations := organization.NewService(database.Organizations(), systemClock, identifiers)
	monitors := monitor.NewService(database.Monitors(), check.NewCatalog(), systemClock, identifiers)
	runQueries := run.NewQueryService(database.Runs())
	healthService := health.NewService(database.Health(), systemClock, identifiers)
	incidentService := incident.NewService(database.Incidents(), systemClock, identifiers)
	alertService := alert.NewService(database.Alerts(), systemClock, identifiers)
	webhookService := webhook.NewService(
		database.Webhooks(), systemClock, identifiers, rand.Reader, webhookKeyring)
	if err := webhookService.Initialize(startupContext); err != nil {
		return fmt.Errorf("initialize Webhook secrets: %w", err)
	}

	var runtime *workerRuntime
	if workerConfiguration.enabled {
		runtime, err = newWorkerRuntime(
			database, workerConfiguration, systemClock, identifiers, logger)
		if err != nil {
			return err
		}
	}
	var manualRuns *run.ManualRunner
	if runtime != nil {
		manualRuns = runtime.manualRuns
	}
	handler, err := httpapi.New(httpapi.Config{
		Organizations:               organizations,
		Users:                       users,
		Monitors:                    monitors,
		Runs:                        runQueries,
		ManualRuns:                  manualRuns,
		MonitorHealth:               healthService,
		Incidents:                   incidentService,
		Alerts:                      alertService,
		Webhooks:                    webhookService,
		Sessions:                    database.Sessions(),
		Antiforgery:                 database.Antiforgery(),
		Clock:                       systemClock,
		Ready:                       database.Ping,
		Logger:                      logger,
		Development:                 development,
		CredentialAttemptsPerMinute: credentialAttempts,
		PublicOrigin:                strings.TrimSpace(os.Getenv("PROBEHIVE_PUBLIC_ORIGIN")),
	})
	if err != nil {
		return fmt.Errorf("compose HTTP API: %w", err)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      workerConfiguration.executionCeiling + 15*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The embedded worker shares this process (ADR 0020) and can be switched off so an
	// operator can run an API-only replica. Its goroutines stop on the same signal the HTTP
	// server does, and serve() waits for them before returning.
	var workers sync.WaitGroup
	if runtime != nil {
		dispatcher, err := newOutboxDispatcher(
			database, healthService, incidentService, alertService, runtime.confirmations,
			systemClock, identifiers, logger)
		if err != nil {
			return err
		}
		webhookDispatcher, err := newWebhookDeliveryDispatcher(
			database, webhookKeyring, runtime.webhookClient,
			systemClock, identifiers, logger)
		if err != nil {
			return err
		}
		workers.Add(5)
		go func() {
			defer workers.Done()
			serveMaintenance(stopContext, database.Runs(), workerConfiguration, systemClock, logger)
		}()
		go func() {
			defer workers.Done()
			if err := runtime.scheduler.Serve(stopContext); err != nil {
				logger.Error("scheduler stopped", "error", err)
			}
		}()
		go func() {
			defer workers.Done()
			if err := dispatcher.Serve(stopContext); err != nil {
				logger.Error("outbox dispatcher stopped", "error", err)
			}
		}()
		go func() {
			defer workers.Done()
			if err := webhookDispatcher.Serve(stopContext); err != nil {
				logger.Error("Webhook delivery dispatcher stopped", "error", err)
			}
		}()
		go func() {
			defer workers.Done()
			if err := healthService.ServeStaleness(
				stopContext, workerConfiguration.executionCeiling,
				workerConfiguration.tickInterval, func(err error) {
					logger.Error("health staleness sweep failed", "error", err)
				}); err != nil {
				logger.Error("health staleness worker stopped", "error", err)
			}
		}()
	} else {
		logger.Info("ProbeHive embedded worker is disabled")
	}
	defer workers.Wait()

	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("ProbeHive listening", "address", address, "development", development)
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-stopContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		logger.Info("ProbeHive stopped gracefully")
		return nil
	}
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func positiveEnvironmentInt(name string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100000 {
		return 0, fmt.Errorf("%s must be an integer from 1 through 100000", name)
	}
	return value, nil
}
