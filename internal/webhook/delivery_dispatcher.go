package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	DeliveryLeaseDuration = 30 * time.Second
	DeliveryBatchSize     = 4
	DeliveryConcurrency   = 4
	DeliveryTickInterval  = time.Second
)

type DeliveryDispatcherConfig struct {
	Store        DeliveryStore
	Clock        Clock
	UUIDs        IDGenerator
	Keyring      *Keyring
	Client       HTTPDoer
	RetryDelay   func(string, int) time.Duration
	Logger       *slog.Logger
	Lease        time.Duration
	BatchSize    int
	Concurrency  int
	TickInterval time.Duration
}

type DeliveryDispatcher struct {
	config   DeliveryDispatcherConfig
	executor *DeliveryExecutor
}

func NewDeliveryDispatcher(config DeliveryDispatcherConfig) (*DeliveryDispatcher, error) {
	if config.Store == nil || config.Clock == nil || config.UUIDs == nil ||
		config.Client == nil {
		return nil, errors.New(
			"Webhook delivery dispatcher requires store, clock, UUIDs, and HTTP client",
		)
	}
	if config.RetryDelay == nil {
		config.RetryDelay = func(string, int) time.Duration { return time.Second }
	}
	if config.Lease <= 0 {
		config.Lease = DeliveryLeaseDuration
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DeliveryBatchSize
	}
	if config.BatchSize > DeliveryBatchSize {
		return nil, fmt.Errorf("Webhook delivery batch size cannot exceed %d", DeliveryBatchSize)
	}
	if config.Concurrency <= 0 {
		config.Concurrency = DeliveryConcurrency
	}
	if config.Concurrency > DeliveryConcurrency {
		return nil, fmt.Errorf(
			"Webhook delivery concurrency cannot exceed %d", DeliveryConcurrency,
		)
	}
	if config.TickInterval <= 0 {
		config.TickInterval = DeliveryTickInterval
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}
	return &DeliveryDispatcher{
		config:   config,
		executor: NewDeliveryExecutor(config.Client, config.Keyring, config.Clock),
	}, nil
}

type DeliveryTickResult struct {
	Claimed   int
	Succeeded int
	Retried   int
	Failed    int
	Cancelled int
	Lost      int
}

func (dispatcher *DeliveryDispatcher) Serve(ctx context.Context) error {
	ticker := time.NewTicker(dispatcher.config.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result, err := dispatcher.Tick(ctx, dispatcher.config.Clock.Now().UTC())
			if err != nil {
				if ctx.Err() == nil {
					dispatcher.config.Logger.Error("Webhook delivery tick failed", "error", err)
				}
				continue
			}
			if result.Claimed != 0 {
				dispatcher.config.Logger.Info(
					"Webhook delivery tick",
					"claimed", result.Claimed,
					"succeeded", result.Succeeded,
					"retried", result.Retried,
					"failed", result.Failed,
					"cancelled", result.Cancelled,
					"lost", result.Lost,
				)
			}
		}
	}
}

func (dispatcher *DeliveryDispatcher) Tick(
	ctx context.Context, now time.Time,
) (DeliveryTickResult, error) {
	holder, err := dispatcher.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		return DeliveryTickResult{}, fmt.Errorf(
			"generate Webhook delivery lease holder: %w", err,
		)
	}
	claims, err := dispatcher.config.Store.Claim(
		ctx, holder, now, now.Add(dispatcher.config.Lease), dispatcher.config.BatchSize,
	)
	if err != nil {
		return DeliveryTickResult{}, fmt.Errorf("claim Webhook deliveries: %w", err)
	}

	result := DeliveryTickResult{Claimed: len(claims)}
	var mutex sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, dispatcher.config.Concurrency)
	for _, claim := range claims {
		claim := claim
		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			outcome := dispatcher.handle(ctx, claim)
			mutex.Lock()
			defer mutex.Unlock()
			switch outcome {
			case OutcomeSucceeded:
				result.Succeeded++
			case OutcomeCancelled:
				result.Cancelled++
			case "lost":
				result.Lost++
			case "retried":
				result.Retried++
			default:
				result.Failed++
			}
		}()
	}
	group.Wait()
	return result, nil
}

func (dispatcher *DeliveryDispatcher) handle(
	ctx context.Context, claim DeliveryClaim,
) string {
	execution := dispatcher.executor.Execute(ctx, claim)
	terminal := !execution.Retry || claim.Sequence >= MaxDeliveryAttempts
	var next *time.Time
	if execution.Retry && !terminal {
		value := execution.FinishedAt.Add(
			dispatcher.config.RetryDelay(claim.DeliveryID, int(claim.Sequence)),
		)
		next = &value
	}

	writeContext := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeContext, cancel = context.WithTimeout(
			context.WithoutCancel(ctx), 5*time.Second,
		)
		defer cancel()
	}
	err := dispatcher.config.Store.Complete(
		writeContext, claim, execution.AttemptUpdate, next, terminal,
	)
	if errors.Is(err, ErrDeliveryLeaseLost) {
		return "lost"
	}
	if err != nil {
		dispatcher.config.Logger.Error(
			"complete Webhook delivery attempt",
			"deliveryId", claim.DeliveryID,
			"sequence", claim.Sequence,
			"error", err,
		)
		return "lost"
	}
	if execution.Outcome == OutcomeSucceeded {
		return OutcomeSucceeded
	}
	if execution.Outcome == OutcomeCancelled {
		return OutcomeCancelled
	}
	if next != nil {
		return "retried"
	}
	return OutcomeFailed
}
