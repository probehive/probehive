// Package outbox owns bounded at-least-once dispatch for internal fact events.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"
)

const (
	DefaultLeaseDuration = 60 * time.Second
	DefaultBatchSize     = 32
	DefaultConcurrency   = 4
	DefaultTickInterval  = time.Second
	RetryBase            = time.Second
	RetryCap             = 5 * time.Minute
	MaxAttempts          = 12
	Retention            = 30 * 24 * time.Hour
	AggregateGapTimeout  = 15 * time.Minute

	CodeTopicUnknown         = "outbox.topic.unknown"
	CodePayloadInvalid       = "outbox.payload.invalid"
	CodeOrganizationMismatch = "outbox.organization.mismatch"
	CodeAggregateVersionGap  = "outbox.aggregate.versionGap"
	CodeHandlerFailed        = "outbox.handler.failed"
	CodeHandlerCancelled     = "outbox.handler.cancelled"
)

var ErrLeaseLost = errors.New("the outbox lease was lost")

type Entry struct {
	ID             string
	OrganizationID string
	Topic          string
	Payload        []byte
	Attempts       int
	CreatedAt      time.Time
	AvailableAt    time.Time
	LeaseHolder    string
	LeaseExpiresAt time.Time
	GapFirstSeenAt time.Time
}

type Store interface {
	Claim(context.Context, string, time.Time, time.Time, int) ([]Entry, error)
	Succeed(context.Context, Entry, string, time.Time) error
	Fail(context.Context, Entry, string, string, time.Time, time.Time, bool) error
	Cleanup(context.Context, time.Time) error
}

type Handler interface {
	Handle(context.Context, Entry) error
}

type HandlerFunc func(context.Context, Entry) error

func (function HandlerFunc) Handle(ctx context.Context, entry Entry) error {
	return function(ctx, entry)
}

type IDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

type Clock interface{ Now() time.Time }

type Config struct {
	Store        Store
	Handlers     map[string]Handler
	Clock        Clock
	UUIDs        IDGenerator
	Logger       *slog.Logger
	Lease        time.Duration
	BatchSize    int
	Concurrency  int
	TickInterval time.Duration
}

type Dispatcher struct {
	config Config
}

func New(config Config) (*Dispatcher, error) {
	if config.Store == nil || config.Clock == nil || config.UUIDs == nil {
		return nil, errors.New("outbox dispatcher requires a store, clock, and UUID generator")
	}
	if len(config.Handlers) == 0 {
		return nil, errors.New("outbox dispatcher requires at least one topic handler")
	}
	for topic, handler := range config.Handlers {
		if topic == "" || handler == nil {
			return nil, errors.New("outbox topic handlers require a topic and handler")
		}
	}
	if config.Lease <= 0 {
		config.Lease = DefaultLeaseDuration
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.BatchSize > DefaultBatchSize {
		return nil, fmt.Errorf("outbox batch size cannot exceed %d", DefaultBatchSize)
	}
	if config.Concurrency <= 0 {
		config.Concurrency = DefaultConcurrency
	}
	if config.Concurrency > DefaultConcurrency {
		return nil, fmt.Errorf("outbox concurrency cannot exceed %d", DefaultConcurrency)
	}
	if config.TickInterval <= 0 {
		config.TickInterval = DefaultTickInterval
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}
	return &Dispatcher{config: config}, nil
}

type TickResult struct {
	Claimed      int
	Succeeded    int
	Retried      int
	DeadLettered int
	Lost         int
}

func (dispatcher *Dispatcher) Serve(ctx context.Context) error {
	ticker := time.NewTicker(dispatcher.config.TickInterval)
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	defer cleanupTicker.Stop()
	dispatcher.cleanup(ctx, dispatcher.config.Clock.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-cleanupTicker.C:
			dispatcher.cleanup(ctx, dispatcher.config.Clock.Now().UTC())
		case <-ticker.C:
			result, err := dispatcher.Tick(ctx, dispatcher.config.Clock.Now().UTC())
			if err != nil {
				if ctx.Err() == nil {
					dispatcher.config.Logger.Error("outbox tick failed", "error", err)
				}
				continue
			}
			if result.Claimed != 0 {
				dispatcher.config.Logger.Info("outbox tick",
					"claimed", result.Claimed,
					"succeeded", result.Succeeded,
					"retried", result.Retried,
					"deadLettered", result.DeadLettered,
					"lost", result.Lost)
			}
		}
	}
}

func (dispatcher *Dispatcher) cleanup(ctx context.Context, now time.Time) {
	if err := dispatcher.config.Store.Cleanup(ctx, now.Add(-Retention)); err != nil && ctx.Err() == nil {
		dispatcher.config.Logger.Error("outbox cleanup failed", "error", err)
	}
}

func (dispatcher *Dispatcher) Tick(ctx context.Context, now time.Time) (TickResult, error) {
	holder, err := dispatcher.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		return TickResult{}, fmt.Errorf("generate outbox holder: %w", err)
	}
	entries, err := dispatcher.config.Store.Claim(
		ctx, holder, now, now.Add(dispatcher.config.Lease), dispatcher.config.BatchSize)
	if err != nil {
		return TickResult{}, fmt.Errorf("claim outbox entries: %w", err)
	}

	result := TickResult{Claimed: len(entries)}
	var mutex sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, dispatcher.config.Concurrency)
	for _, entry := range entries {
		entry := entry
		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			outcome := dispatcher.handle(ctx, entry, holder, now)
			mutex.Lock()
			defer mutex.Unlock()
			switch outcome {
			case handledSucceeded:
				result.Succeeded++
			case handledRetried:
				result.Retried++
			case handledDeadLettered:
				result.DeadLettered++
			case handledLost:
				result.Lost++
			}
		}()
	}
	group.Wait()
	return result, nil
}

type handledOutcome uint8

const (
	handledSucceeded handledOutcome = iota + 1
	handledRetried
	handledDeadLettered
	handledLost
)

func (dispatcher *Dispatcher) handle(
	ctx context.Context, entry Entry, holder string, now time.Time,
) handledOutcome {
	handler, known := dispatcher.config.Handlers[entry.Topic]
	code := ""
	permanent := false
	var handleErr error
	if !known {
		code, permanent = CodeTopicUnknown, true
	} else {
		handleErr = handler.Handle(ctx, entry)
		if handleErr != nil {
			var failure *Failure
			switch {
			case errors.As(handleErr, &failure):
				code, permanent = failure.Code, failure.Permanent
			case ctx.Err() != nil:
				code = CodeHandlerCancelled
			default:
				code = CodeHandlerFailed
			}
		}
	}
	if code == "" {
		err := dispatcher.config.Store.Succeed(ctx, entry, holder, now)
		if errors.Is(err, ErrLeaseLost) {
			return handledLost
		}
		if err != nil {
			dispatcher.config.Logger.Error("complete outbox entry",
				"topic", entry.Topic, "error", err)
			return handledLost
		}
		return handledSucceeded
	}

	dead := permanent
	if code == CodeAggregateVersionGap {
		firstSeen := entry.GapFirstSeenAt
		if firstSeen.IsZero() {
			firstSeen = now
		}
		dead = !now.Before(firstSeen.Add(AggregateGapTimeout))
	} else if entry.Attempts >= MaxAttempts {
		dead = true
	}
	next := now.Add(RetryDelay(entry.ID, entry.Attempts))
	if code == CodeAggregateVersionGap && !dead {
		deadline := now.Add(AggregateGapTimeout)
		if !entry.GapFirstSeenAt.IsZero() {
			deadline = entry.GapFirstSeenAt.Add(AggregateGapTimeout)
		}
		if next.After(deadline) {
			next = deadline
		}
	}
	if err := dispatcher.config.Store.Fail(ctx, entry, holder, code, next, now, dead); err != nil {
		if !errors.Is(err, ErrLeaseLost) {
			dispatcher.config.Logger.Error("record outbox failure",
				"topic", entry.Topic, "failureCode", code, "error", err)
		}
		return handledLost
	}
	if dead {
		dispatcher.config.Logger.Error("outbox entry dead-lettered",
			"eventId", entry.ID, "topic", entry.Topic,
			"failureCode", code, "attempt", entry.Attempts)
		return handledDeadLettered
	}
	if handleErr != nil && !permanent {
		dispatcher.config.Logger.Warn("outbox handler failed",
			"topic", entry.Topic, "failureCode", code, "attempt", entry.Attempts)
	}
	return handledRetried
}

type Failure struct {
	Code      string
	Permanent bool
}

func (failure *Failure) Error() string {
	return failure.Code
}

func Permanent(code string) error {
	return &Failure{Code: code, Permanent: true}
}

func Transient(code string) error {
	return &Failure{Code: code}
}

func RetryDelay(id string, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := RetryBase
	for index := 1; index < attempt && delay < RetryCap; index++ {
		if delay > RetryCap/2 {
			delay = RetryCap
			break
		}
		delay *= 2
	}
	if delay >= RetryCap {
		return RetryCap
	}
	room := RetryCap - delay
	jitterBound := delay / 5
	if jitterBound > room {
		jitterBound = room
	}
	if jitterBound <= 0 {
		return delay
	}
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%s:%d", id, attempt)
	jitter := time.Duration(hash.Sum64() % uint64(jitterBound+1))
	return delay + jitter
}
