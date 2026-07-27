package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/probehive/probehive/internal/run"
)

var _ run.MonitorSource = (*RunStore)(nil)

// MaxSchedulableMonitors bounds one scheduling read. An installation that exceeds it has
// outgrown a tick that lists every active Monitor, which ADR 0026 names as the trigger for
// introducing a due cursor; truncating silently would hide that by simply not running the
// Monitors past the bound.
const MaxSchedulableMonitors = 10000

// ListSchedulable returns every active Monitor with the revision a new Run would execute.
//
// It joins the latest revision rather than letting the scheduler ask for one per Monitor,
// because the scheduler's whole tick is one read and a Monitor without its configuration is
// not schedulable. An active Monitor always has a revision (ADR 0014), so the inner join
// excludes nothing that should have run.
func (store *RunStore) ListSchedulable(ctx context.Context) ([]run.Schedulable, error) {
	rows, err := store.pool.Query(ctx, `
SELECT monitors.organization_id, monitors.id, monitors.interval_seconds, monitors.updated_at,
       monitor_revisions.revision_number, monitor_revisions.check_type,
       monitor_revisions.check_schema_version, monitor_revisions.check_configuration
FROM monitors
JOIN monitor_revisions
  ON monitor_revisions.monitor_id = monitors.id
 AND monitor_revisions.organization_id = monitors.organization_id
 AND monitor_revisions.revision_number = monitors.latest_revision_number
WHERE monitors.state = 'active'
ORDER BY monitors.organization_id, monitors.id
LIMIT $1`, MaxSchedulableMonitors+1)
	if err != nil {
		return nil, fmt.Errorf("list schedulable Monitors: %w", err)
	}
	defer rows.Close()

	values := make([]run.Schedulable, 0)
	for rows.Next() {
		var (
			organizationID     string
			monitorID          string
			intervalSeconds    int
			updatedAt          time.Time
			revisionNumber     int
			checkType          string
			checkSchemaVersion int
			configuration      []byte
		)
		if err := rows.Scan(&organizationID, &monitorID, &intervalSeconds, &updatedAt, &revisionNumber,
			&checkType, &checkSchemaVersion, &configuration); err != nil {
			return nil, fmt.Errorf("scan schedulable Monitor: %w", err)
		}
		values = append(values, run.Schedulable{
			OrganizationID:     organizationID,
			MonitorID:          monitorID,
			RevisionNumber:     revisionNumber,
			CheckType:          checkType,
			CheckSchemaVersion: checkSchemaVersion,
			CheckConfiguration: append(json.RawMessage(nil), configuration...),
			Interval:           time.Duration(intervalSeconds) * time.Second,
			// updated_at moves when a Monitor is activated or gains a revision, so it is the
			// floor on how far back a misfire may be recorded (ADR 0026).
			NotBefore: updatedAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schedulable Monitors: %w", err)
	}
	if len(values) > MaxSchedulableMonitors {
		return nil, fmt.Errorf(
			"this installation has more than %d active Monitors, which the single-read scheduler does not support",
			MaxSchedulableMonitors)
	}
	return values, nil
}
