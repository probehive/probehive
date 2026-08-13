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
		id                   string
		title                string
		version              int64
		createdAt            time.Time
		updatedAt            time.Time
		publicationTokenHash []byte
		publishedAt          *time.Time
	)
	err := store.pool.QueryRow(ctx, `
SELECT id, title, version, created_at, updated_at, publication_token_hash, published_at
FROM status_pages
WHERE organization_id = $1`, organizationID).Scan(
		&id, &title, &version, &createdAt, &updatedAt, &publicationTokenHash, &publishedAt,
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
	var draft statuspage.Draft
	if publicationTokenHash == nil {
		draft, err = statuspage.RestoreDraft(
			statuspage.ID(id), organizationID, title, version,
			createdAt.UTC(), updatedAt.UTC(), components,
		)
	} else {
		if len(publicationTokenHash) != 32 || publishedAt == nil {
			return statuspage.Draft{}, false, errors.New("restore status page draft: invalid publication state")
		}
		var tokenHash statuspage.TokenHash
		copy(tokenHash[:], publicationTokenHash)
		draft, err = statuspage.RestorePublishedDraft(
			statuspage.ID(id), organizationID, title, version,
			createdAt.UTC(), updatedAt.UTC(), components,
			statuspage.Publication{TokenHash: tokenHash, PublishedAt: publishedAt.UTC()},
		)
	}
	if err != nil {
		return statuspage.Draft{}, false, fmt.Errorf("restore status page draft: %w", err)
	}
	return draft, true, nil
}

func (store *StatusPageStore) Publish(
	ctx context.Context, organizationID string, publication statuspage.Publication,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin status page publication: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var published bool
	err = transaction.QueryRow(ctx, `
SELECT publication_token_hash IS NOT NULL
FROM status_pages
WHERE organization_id = $1
FOR UPDATE`, organizationID).Scan(&published)
	if errors.Is(err, pgx.ErrNoRows) {
		return statuspage.ErrDraftMissing
	}
	if err != nil {
		return fmt.Errorf("lock status page publication: %w", err)
	}
	if published {
		return statuspage.ErrAlreadyPublished
	}
	if _, err = transaction.Exec(ctx, `
UPDATE status_pages
SET publication_token_hash = $1, published_at = $2
WHERE organization_id = $3`,
		publication.TokenHash[:], publication.PublishedAt.UTC(), organizationID); err != nil {
		return fmt.Errorf("publish status page: %w", err)
	}
	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit status page publication: %w", err)
	}
	return nil
}

func (store *StatusPageStore) Revoke(ctx context.Context, organizationID string) error {
	if _, err := store.pool.Exec(ctx, `
UPDATE status_pages
SET publication_token_hash = NULL, published_at = NULL
WHERE organization_id = $1`, organizationID); err != nil {
		return fmt.Errorf("revoke status page: %w", err)
	}
	return nil
}

func (store *StatusPageStore) FindPublicPage(
	ctx context.Context, tokenHash statuspage.TokenHash, now time.Time,
) (statuspage.PublicPage, bool, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("begin public status page read: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var (
		pageID         string
		organizationID string
		title          string
	)
	err = transaction.QueryRow(ctx, `
SELECT id, organization_id, title
FROM status_pages
WHERE publication_token_hash = $1
FOR SHARE`, tokenHash[:]).Scan(&pageID, &organizationID, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return statuspage.PublicPage{}, false, nil
	}
	if err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("find public status page: %w", err)
	}
	rows, err := transaction.Query(ctx, `
SELECT component.label,
       CASE WHEN monitor.state = 'active' THEN COALESCE(health.state, 'unknown') ELSE 'unknown' END,
       CASE WHEN monitor.state = 'active' THEN COALESCE(health.updated_at, monitor.created_at) ELSE monitor.updated_at END,
       monitor.state = 'active' AND EXISTS (
           SELECT 1 FROM maintenance_windows AS maintenance_window
           WHERE maintenance_window.organization_id = component.organization_id
             AND maintenance_window.monitor_id = component.monitor_id
             AND maintenance_window.cancelled_at IS NULL
             AND maintenance_window.starts_at <= $3
             AND maintenance_window.ends_at > $3
       )
FROM status_page_components AS component
JOIN monitors AS monitor
  ON monitor.id = component.monitor_id
 AND monitor.organization_id = component.organization_id
LEFT JOIN monitor_health AS health
  ON health.monitor_id = monitor.id
 AND health.organization_id = monitor.organization_id
WHERE component.status_page_id = $1 AND component.organization_id = $2
ORDER BY component.position`, pageID, organizationID, now.UTC())
	if err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("list public status components: %w", err)
	}
	defer rows.Close()
	components := make([]statuspage.PublicComponent, 0)
	for rows.Next() {
		var component statuspage.PublicComponent
		if err = rows.Scan(&component.Label, &component.State, &component.UpdatedAt, &component.Maintenance); err != nil {
			return statuspage.PublicPage{}, false, fmt.Errorf("scan public status component: %w", err)
		}
		component.UpdatedAt = component.UpdatedAt.UTC()
		components = append(components, component)
	}
	if err = rows.Err(); err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("list public status components: %w", err)
	}
	rows.Close()
	page, err := statuspage.RestorePublicPage(title, components)
	if err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("restore public status page: %w", err)
	}
	if err = transaction.Commit(ctx); err != nil {
		return statuspage.PublicPage{}, false, fmt.Errorf("commit public status page read: %w", err)
	}
	return page, true, nil
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
