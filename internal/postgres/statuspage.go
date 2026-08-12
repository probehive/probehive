package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/statuspage"
)

const statusPageOrganizationUniqueConstraint = "ux_status_pages_organization"

var _ statuspage.Store = (*StatusPageStore)(nil)

type StatusPageStore struct{ pool *pgxpool.Pool }

func (database *DB) StatusPages() *StatusPageStore { return &StatusPageStore{pool: database.pool} }

func (store *StatusPageStore) FindDraft(
	ctx context.Context, organizationID string,
) (statuspage.Draft, bool, error) {
	var (
		id        string
		title     string
		version   int64
		createdAt time.Time
		updatedAt time.Time
	)
	err := store.pool.QueryRow(ctx, `
SELECT id, title, version, created_at, updated_at
FROM status_pages
WHERE organization_id = $1`, organizationID).Scan(
		&id, &title, &version, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return statuspage.Draft{}, false, nil
	}
	if err != nil {
		return statuspage.Draft{}, false, fmt.Errorf("find status page draft: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
SELECT id, monitor_id, label, position
FROM status_page_components
WHERE organization_id = $1 AND status_page_id = $2
ORDER BY position`, organizationID, id)
	if err != nil {
		return statuspage.Draft{}, false, fmt.Errorf("list status components: %w", err)
	}
	defer rows.Close()
	components := make([]statuspage.Component, 0)
	for rows.Next() {
		var component statuspage.Component
		if err = rows.Scan(&component.ID, &component.MonitorID, &component.Label, &component.Position); err != nil {
			return statuspage.Draft{}, false, fmt.Errorf("scan status component: %w", err)
		}
		components = append(components, component)
	}
	if err = rows.Err(); err != nil {
		return statuspage.Draft{}, false, fmt.Errorf("list status components: %w", err)
	}
	draft, err := statuspage.RestoreDraft(
		statuspage.ID(id), organizationID, title, version,
		createdAt.UTC(), updatedAt.UTC(), components,
	)
	if err != nil {
		return statuspage.Draft{}, false, fmt.Errorf("restore status page draft: %w", err)
	}
	return draft, true, nil
}

func (store *StatusPageStore) ReplaceDraft(
	ctx context.Context, draft statuspage.Draft, expectedVersion int64,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin status page replacement: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var currentVersion int64
	err = transaction.QueryRow(ctx, `
SELECT version
FROM status_pages
WHERE organization_id = $1
FOR UPDATE`, draft.OrganizationID).Scan(&currentVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && expectedVersion != 0:
		return statuspage.ErrConcurrentUpdate
	case errors.Is(err, pgx.ErrNoRows):
		if _, err = transaction.Exec(ctx, `
INSERT INTO status_pages (id, organization_id, title, version, created_at, updated_at)
SELECT $1, organization.id, $3, $4, $5, $6
FROM organizations AS organization
WHERE organization.id = $2`,
			string(draft.ID), draft.OrganizationID, draft.Title, draft.Version,
			draft.CreatedAt.UTC(), draft.UpdatedAt.UTC(),
		); err != nil {
			return statusPageInsertError(err)
		}
	case err != nil:
		return fmt.Errorf("lock status page draft: %w", err)
	case currentVersion != expectedVersion:
		return statuspage.ErrConcurrentUpdate
	default:
		result, updateErr := transaction.Exec(ctx, `
UPDATE status_pages
SET title = $1, version = $2, updated_at = $3
WHERE organization_id = $4 AND version = $5`,
			draft.Title, draft.Version, draft.UpdatedAt.UTC(), draft.OrganizationID, expectedVersion,
		)
		if updateErr != nil {
			return fmt.Errorf("update status page draft: %w", updateErr)
		}
		if result.RowsAffected() != 1 {
			return statuspage.ErrConcurrentUpdate
		}
		if _, err = transaction.Exec(ctx, `
DELETE FROM status_page_components
WHERE organization_id = $1 AND status_page_id = $2`, draft.OrganizationID, string(draft.ID)); err != nil {
			return fmt.Errorf("replace status components: %w", err)
		}
	}

	for _, component := range draft.Components {
		result, insertErr := transaction.Exec(ctx, `
INSERT INTO status_page_components (
    id, organization_id, status_page_id, monitor_id, label, position
)
SELECT $1, $2, $3, monitor.id, $5, $6
FROM monitors AS monitor
WHERE monitor.id = $4
  AND monitor.organization_id = $2
  AND monitor.state <> 'archived'`,
			string(component.ID), draft.OrganizationID, string(draft.ID), component.MonitorID,
			component.Label, component.Position,
		)
		if insertErr != nil {
			return fmt.Errorf("insert status component: %w", insertErr)
		}
		if result.RowsAffected() != 1 {
			return statuspage.ErrMonitorUnavailable
		}
	}
	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit status page replacement: %w", err)
	}
	return nil
}

func statusPageInsertError(err error) error {
	if isConstraintViolation(err, uniqueViolation, statusPageOrganizationUniqueConstraint) {
		return statuspage.ErrConcurrentUpdate
	}
	return fmt.Errorf("insert status page draft: %w", err)
}
