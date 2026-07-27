package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/outbound"
	"github.com/probehive/probehive/internal/postgres"
	"github.com/probehive/probehive/internal/probe"
	"github.com/probehive/probehive/internal/run"
)

// Worker defaults. Every one of them is an operator ceiling or floor that ADR 0020 requires
// to be configuration rather than code; the values here are what an installation that
// configures nothing gets.
const (
	defaultProbeLocation    = run.DefaultLocation
	defaultOutboundProfile  = string(outbound.ProfilePrivate)
	defaultResolverTimeout  = 5 * time.Second
	defaultConnectTimeout   = 10 * time.Second
	defaultExecutionCeiling = 60 * time.Second
	defaultMinimumInterval  = 30 * time.Second
	partitionLookahead      = run.DefaultPartitionsAhead
	maintenanceInterval     = 6 * time.Hour
)

// probeExecutor adapts internal/probe to the scheduler's executor port.
//
// This is the only place probe.Observation becomes run.Observation. ADR 0025 keeps the two
// shapes separate because a feature package imports no protocol client and a protocol package
// imports no persistence; the translation belongs to the composition that owns both, and a
// change to either shape that is not mirrored here fails to compile.
type probeExecutor struct {
	executor *probe.Executor
}

func (adapter probeExecutor) Execute(
	ctx context.Context,
	checkType string,
	schemaVersion int,
	configuration json.RawMessage,
) run.Execution {
	observation := adapter.executor.Execute(ctx, checkType, schemaVersion, configuration)

	stored := run.Observation{
		Duration: observation.Duration,
		Phases: run.Phases{
			Connect:   observation.Phases.Connect,
			TLS:       observation.Phases.TLS,
			FirstByte: observation.Phases.FirstByte,
		},
	}
	if observation.Failure != nil {
		stored.FailureCode = observation.Failure.Code
		stored.FailureClass = string(observation.Failure.Class)
	}
	if observation.HTTP != nil {
		detail := run.HTTPDetail{
			StatusCode:    observation.HTTP.StatusCode,
			Protocol:      observation.HTTP.Protocol,
			RedirectCount: observation.HTTP.RedirectCount,
			BodyBytes:     observation.HTTP.BodyBytes,
			BodyTruncated: observation.HTTP.BodyTruncated,
		}
		if observation.HTTP.TLS != nil {
			detail.TLS = &run.TLSDetail{
				Version:              observation.HTTP.TLS.Version,
				CipherSuite:          observation.HTTP.TLS.CipherSuite,
				CertificateExpiresAt: observation.HTTP.TLS.CertificateExpiresAt,
			}
		}
		stored.HTTP = &detail
	}
	return run.Execution{
		Outcome:     run.Outcome(observation.Outcome),
		StartedAt:   observation.StartedAt,
		FinishedAt:  observation.FinishedAt,
		Observation: stored,
	}
}

// workerSettings is the operator configuration the embedded worker reads from the environment.
type workerSettings struct {
	enabled          bool
	location         string
	minimumInterval  time.Duration
	executionCeiling time.Duration
	concurrency      int
	tickInterval     time.Duration
	retention        run.Retention
	outbound         outbound.Spec
	resolvers        []netip.AddrPort
	resolverTimeout  time.Duration
	connectTimeout   time.Duration
	rootCAs          *x509.CertPool
	probeSettings    probe.Settings
}

// readWorkerSettings turns environment configuration into validated worker settings. Every
// failure here is an operator mistake reported before anything starts, because a worker that
// silently falls back to a default outbound profile is a worker that fails open.
func readWorkerSettings() (workerSettings, error) {
	settings := workerSettings{enabled: true}

	enabled, err := environmentBool("PROBEHIVE_WORKER_ENABLED", true)
	if err != nil {
		return workerSettings{}, err
	}
	settings.enabled = enabled

	settings.location = strings.TrimSpace(os.Getenv("PROBEHIVE_PROBE_LOCATION"))
	if settings.location == "" {
		settings.location = defaultProbeLocation
	}
	if len(settings.location) > run.MaxLocationLength {
		return workerSettings{}, fmt.Errorf(
			"PROBEHIVE_PROBE_LOCATION must be at most %d bytes", run.MaxLocationLength)
	}

	settings.minimumInterval, err = environmentSeconds("PROBEHIVE_MINIMUM_INTERVAL_SECONDS", defaultMinimumInterval)
	if err != nil {
		return workerSettings{}, err
	}
	// The operator floor may be raised but never lowered past the platform minimum, which is
	// what keeps an installation from configuring a frequency the Monitor validator rejects
	// (ADR 0026).
	if settings.minimumInterval < time.Duration(monitor.MinIntervalSeconds)*time.Second {
		return workerSettings{}, fmt.Errorf(
			"PROBEHIVE_MINIMUM_INTERVAL_SECONDS cannot go below the platform minimum of %d seconds",
			monitor.MinIntervalSeconds)
	}
	settings.executionCeiling, err = environmentSeconds("PROBEHIVE_EXECUTION_CEILING_SECONDS", defaultExecutionCeiling)
	if err != nil {
		return workerSettings{}, err
	}
	settings.tickInterval, err = environmentSeconds("PROBEHIVE_SCHEDULER_TICK_SECONDS", run.DefaultTickInterval)
	if err != nil {
		return workerSettings{}, err
	}
	settings.concurrency, err = positiveEnvironmentInt("PROBEHIVE_WORKER_CONCURRENCY", run.DefaultConcurrency)
	if err != nil {
		return workerSettings{}, err
	}
	retentionDays, err := positiveEnvironmentInt("PROBEHIVE_RETENTION_DAYS", run.DefaultRetentionDays)
	if err != nil {
		return workerSettings{}, err
	}
	settings.retention, err = run.NewRetention(retentionDays)
	if err != nil {
		return workerSettings{}, fmt.Errorf("PROBEHIVE_RETENTION_DAYS: %w", err)
	}

	if settings.outbound, err = readOutboundSpec(); err != nil {
		return workerSettings{}, err
	}
	if settings.resolvers, err = readResolvers(); err != nil {
		return workerSettings{}, err
	}
	settings.resolverTimeout, err = environmentSeconds("PROBEHIVE_RESOLVER_TIMEOUT_SECONDS", defaultResolverTimeout)
	if err != nil {
		return workerSettings{}, err
	}
	settings.connectTimeout, err = environmentSeconds("PROBEHIVE_CONNECT_TIMEOUT_SECONDS", defaultConnectTimeout)
	if err != nil {
		return workerSettings{}, err
	}
	if settings.rootCAs, err = readRootCAs(); err != nil {
		return workerSettings{}, err
	}

	settings.probeSettings = probe.DefaultSettings()
	settings.probeSettings.MaxTimeout = settings.executionCeiling
	settings.probeSettings.RootCAs = settings.rootCAs
	return settings, nil
}

func readOutboundSpec() (outbound.Spec, error) {
	profile := strings.TrimSpace(os.Getenv("PROBEHIVE_OUTBOUND_PROFILE"))
	if profile == "" {
		profile = defaultOutboundProfile
	}
	name, err := outbound.ParseProfileName(profile)
	if err != nil {
		return outbound.Spec{}, fmt.Errorf("PROBEHIVE_OUTBOUND_PROFILE: %w", err)
	}
	if name == outbound.ProfileOperator {
		return outbound.Spec{}, fmt.Errorf(
			"PROBEHIVE_OUTBOUND_PROFILE cannot be %q: it is for operator integrations and is never reachable from tenant configuration",
			outbound.ProfileOperator)
	}

	spec := outbound.Spec{Profile: name}
	for _, entry := range splitList(os.Getenv("PROBEHIVE_OUTBOUND_ALLOWED_CIDRS")) {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return outbound.Spec{}, fmt.Errorf("PROBEHIVE_OUTBOUND_ALLOWED_CIDRS: %q is not a CIDR prefix", entry)
		}
		spec.AllowedCIDRs = append(spec.AllowedCIDRs, prefix)
	}
	for _, entry := range splitList(os.Getenv("PROBEHIVE_OUTBOUND_ALLOWED_PORTS")) {
		port, err := strconv.ParseUint(entry, 10, 16)
		if err != nil || port == 0 {
			return outbound.Spec{}, fmt.Errorf("PROBEHIVE_OUTBOUND_ALLOWED_PORTS: %q is not a port", entry)
		}
		spec.AllowedPorts = append(spec.AllowedPorts, uint16(port))
	}
	return spec, nil
}

func readResolvers() ([]netip.AddrPort, error) {
	entries := splitList(os.Getenv("PROBEHIVE_OUTBOUND_RESOLVERS"))
	resolvers := make([]netip.AddrPort, 0, len(entries))
	for _, entry := range entries {
		address, err := netip.ParseAddrPort(entry)
		if err != nil {
			return nil, fmt.Errorf("PROBEHIVE_OUTBOUND_RESOLVERS: %q is not an address:port", entry)
		}
		resolvers = append(resolvers, address)
	}
	return resolvers, nil
}

// readRootCAs loads an installation's internal certificate authorities. They are added to a
// pool that replaces the host's roots, so an operator who names a file gets exactly the trust
// they configured. There is no setting that skips verification (ADR 0024).
func readRootCAs() (*x509.CertPool, error) {
	path := strings.TrimSpace(os.Getenv("PROBEHIVE_PROBE_ROOT_CA_FILE"))
	if path == "" {
		return nil, nil
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("PROBEHIVE_PROBE_ROOT_CA_FILE: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("PROBEHIVE_PROBE_ROOT_CA_FILE: %s contains no PEM certificates", path)
	}
	return pool, nil
}

// newScheduler composes the outbound policy, the probe executor, and the Run store into the
// embedded scheduler of ADR 0020.
func newScheduler(
	database *postgres.DB,
	settings workerSettings,
	systemClock run.Clock,
	identifiers run.IDGenerator,
	logger *slog.Logger,
) (*run.Scheduler, error) {
	policy, err := outbound.NewPolicy(settings.outbound)
	if err != nil {
		return nil, fmt.Errorf("compose outbound policy: %w", err)
	}
	// Named resolvers confine every query to reviewed servers; without them the host's own
	// resolver is used, which is operator-controlled in the weaker sense that the operator
	// controls the machine. Neither is tenant-selectable, which is what ADR 0007 requires.
	// Refusing to start without an explicit list would make the API unavailable over a
	// worker setting, so the fallback is the documented one rather than a failure.
	resolver := outbound.SystemResolver()
	if len(settings.resolvers) != 0 {
		trusted, err := outbound.NewTrustedResolver(settings.resolvers, settings.resolverTimeout)
		if err != nil {
			return nil, fmt.Errorf("compose outbound resolver: %w", err)
		}
		resolver = trusted
	} else {
		logger.Warn("no outbound resolvers are configured; using the host resolver",
			"setting", "PROBEHIVE_OUTBOUND_RESOLVERS")
	}
	dialer := outbound.NewDialer(policy, resolver, settings.connectTimeout)

	httpExecutor, err := probe.NewHTTPExecutor(dialer, settings.probeSettings, systemClock)
	if err != nil {
		return nil, fmt.Errorf("compose HTTP executor: %w", err)
	}
	executor, err := probe.NewExecutor(httpExecutor)
	if err != nil {
		return nil, fmt.Errorf("compose check executor: %w", err)
	}

	runs := database.Runs()
	return run.NewScheduler(run.SchedulerConfig{
		Source:           runs,
		Store:            runs,
		Executor:         probeExecutor{executor: executor},
		Clock:            systemClock,
		UUIDs:            identifiers,
		Logger:           logger,
		Location:         settings.location,
		MinimumInterval:  settings.minimumInterval,
		ExecutionCeiling: settings.executionCeiling,
		TickInterval:     settings.tickInterval,
		Concurrency:      settings.concurrency,
	})
}

// serveMaintenance creates partitions ahead of time and drops the ones that have aged out.
//
// ADR 0021 makes this a required operational component rather than an optimization: there is
// no default partition, so an installation whose maintenance never runs eventually fails to
// insert a Run. It runs once at startup so a fresh installation can store its first Run
// before the first interval elapses.
func serveMaintenance(
	ctx context.Context,
	store *postgres.RunStore,
	settings workerSettings,
	systemClock run.Clock,
	logger *slog.Logger,
) {
	maintain := func() {
		now := systemClock.Now().UTC()
		created, err := store.EnsurePartitions(ctx, now, partitionLookahead)
		if err != nil {
			logger.Error("cannot create Run partitions", "error", err)
			return
		}
		if len(created) != 0 {
			logger.Info("created Run partitions", "partitions", created)
		}
		dropped, err := store.DropExpiredPartitions(ctx, settings.retention, now)
		if err != nil {
			logger.Error("cannot expire Run partitions", "error", err)
			return
		}
		if len(dropped) != 0 {
			logger.Info("expired Run partitions", "partitions", dropped, "retentionDays", settings.retention.Days)
		}
	}

	maintain()
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintain()
		}
	}
}

func environmentBool(name string, defaultValue bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func environmentSeconds(name string, defaultValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 86400 {
		return 0, fmt.Errorf("%s must be an integer number of seconds from 1 through 86400", name)
	}
	return time.Duration(value) * time.Second, nil
}

func splitList(raw string) []string {
	entries := make([]string, 0)
	for _, entry := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(entry)
		if trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}
