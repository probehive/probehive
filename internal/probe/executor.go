package probe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/probehive/probehive/internal/check"
	"github.com/probehive/probehive/internal/outbound"
)

// Executor executes a stored check configuration by dispatching to the executor for its check
// type. It is the seam a worker calls: a Monitor Revision holds a check type, an integer
// schema version, and canonical configuration JSON (ADR 0014), and that is exactly what this
// takes.
//
// The configuration is validated again here rather than trusted. The API validates it when a
// revision is written, but a revision written by an older build, edited in the database, or
// restored from a backup has not passed the validator this build contains, and execution is
// the last place to notice (ADR 0020).
type Executor struct {
	httpExecutor *HTTPExecutor
}

// NewExecutor returns a dispatching executor over the check types this build can execute.
func NewExecutor(httpExecutor *HTTPExecutor) (*Executor, error) {
	if httpExecutor == nil {
		return nil, fmt.Errorf("probe: an executor for the 'http' check type is required")
	}
	return &Executor{httpExecutor: httpExecutor}, nil
}

// Supports reports whether this build can execute a check type.
func (executor *Executor) Supports(checkType string) bool {
	return checkType == executor.httpExecutor.CheckType()
}

// Execute validates a stored configuration and executes it. Like every execution here it
// returns an Observation rather than an error: a configuration this build cannot execute is
// an errored Run, which is a thing an operator can see, not a value a caller might drop.
//
// A rejected configuration reports the first stable validation code of ADR 0019 as its
// failure, because "the URL is missing" explains an errored Run and "invalid configuration"
// does not.
func (executor *Executor) Execute(ctx context.Context, checkType string, schemaVersion int, configuration json.RawMessage) Observation {
	if !executor.Supports(checkType) {
		return executor.immediate(failed(OutcomeErrored, CodeCheckTypeUnsupported, outbound.ClassPublic))
	}

	validated, _, failures := check.ValidateHTTP(schemaVersion, configuration)
	if len(failures) != 0 {
		return executor.immediate(failed(OutcomeErrored, failures[0][0], outbound.ClassPublic))
	}
	return executor.httpExecutor.Execute(ctx, validated)
}

// immediate stamps an Observation that never reached the network. Its instants are equal
// because rejecting a configuration takes no measurable time.
func (executor *Executor) immediate(observation Observation) Observation {
	now := executor.httpExecutor.clock.Now()
	observation.StartedAt, observation.FinishedAt = now, now
	return observation
}
