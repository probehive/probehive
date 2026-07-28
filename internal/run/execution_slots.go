package run

import (
	"context"
	"errors"
)

// ExecutionSlots bounds executions shared by scheduled, confirmation, and manual Runs.
// Sharing the same slots keeps the operator's configured concurrency ceiling authoritative
// regardless of which entry point requested the work.
type ExecutionSlots struct {
	slots chan struct{}
}

// NewExecutionSlots creates one shared execution bound.
func NewExecutionSlots(capacity int) (*ExecutionSlots, error) {
	if capacity < 1 {
		return nil, errors.New("execution concurrency must be positive")
	}
	return &ExecutionSlots{slots: make(chan struct{}, capacity)}, nil
}

// Acquire waits for one slot or for the caller to stop waiting.
func (slots *ExecutionSlots) Acquire(ctx context.Context) error {
	select {
	case slots.slots <- struct{}{}:
		return nil
	default:
	}
	// Once capacity is occupied, cancellation stops new work from queueing behind it.
	select {
	case slots.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire takes one slot without queueing.
func (slots *ExecutionSlots) TryAcquire() bool {
	select {
	case slots.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release returns one acquired slot.
func (slots *ExecutionSlots) Release() {
	<-slots.slots
}
