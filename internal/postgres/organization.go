package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/organization"
)

var _ organization.Store = (*OrganizationStore)(nil)

const organizationSlugUniqueIndex = "ux_organizations_slug"

// OrganizationStore persists Organizations and Projects.
type OrganizationStore struct {
	pool *pgxpool.Pool
}

// Organizations returns the Organization persistence adapter.
func (database *DB) Organizations() *OrganizationStore {
	return &OrganizationStore{pool: database.pool}
}

func (store *OrganizationStore) FindByID(ctx context.Context, id organization.ID) (organization.Organization, bool, error) {
	return scanOrganization(store.pool.QueryRow(ctx, `
SELECT id, slug, display_name, created_at
FROM organizations
WHERE id = $1`, string(id)))
}

func (store *OrganizationStore) FindBySlug(ctx context.Context, slug string) (organization.Organization, bool, error) {
	return scanOrganization(store.pool.QueryRow(ctx, `
SELECT id, slug, display_name, created_at
FROM organizations
WHERE slug = $1`, slug))
}

func (store *OrganizationStore) FindDefaultProject(ctx context.Context, organizationID organization.ID) (organization.Project, bool, error) {
	return scanProject(store.pool.QueryRow(ctx, `
SELECT id, organization_id, name, is_default, created_at
FROM projects
WHERE organization_id = $1 AND is_default`, string(organizationID)))
}

func (store *OrganizationStore) FindProject(ctx context.Context, organizationID organization.ID, projectID organization.ProjectID) (organization.Project, bool, error) {
	return scanProject(store.pool.QueryRow(ctx, `
SELECT id, organization_id, name, is_default, created_at
FROM projects
WHERE id = $1 AND organization_id = $2`, string(projectID), string(organizationID)))
}

func (store *OrganizationStore) Create(ctx context.Context, value organization.Organization, defaultProject organization.Project) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Organization creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `
INSERT INTO organizations (id, slug, display_name, created_at)
VALUES ($1, $2, $3, $4)`, string(value.ID), value.Slug, value.DisplayName, value.CreatedAt.UTC()); err != nil {
		if isConstraintViolation(err, uniqueViolation, organizationSlugUniqueIndex) {
			return organization.ErrDuplicateSlug
		}
		return fmt.Errorf("insert Organization: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO projects (id, organization_id, name, is_default, created_at)
VALUES ($1, $2, $3, $4, $5)`, string(defaultProject.ID), string(defaultProject.OrganizationID), defaultProject.Name,
		defaultProject.IsDefault, defaultProject.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("insert default Project: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		if isConstraintViolation(err, uniqueViolation, organizationSlugUniqueIndex) {
			return organization.ErrDuplicateSlug
		}
		return fmt.Errorf("commit Organization creation: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanOrganization(row rowScanner) (organization.Organization, bool, error) {
	var (
		id          string
		slug        string
		displayName string
		createdAt   time.Time
	)
	if err := row.Scan(&id, &slug, &displayName, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return organization.Organization{}, false, nil
		}
		return organization.Organization{}, false, fmt.Errorf("scan Organization: %w", err)
	}
	value, err := organization.NewOrganization(organization.ID(id), slug, displayName, createdAt.UTC())
	if err != nil {
		return organization.Organization{}, false, fmt.Errorf("restore Organization: %w", err)
	}
	return value, true, nil
}

func scanProject(row rowScanner) (organization.Project, bool, error) {
	var (
		id             string
		organizationID string
		name           string
		isDefault      bool
		createdAt      time.Time
	)
	if err := row.Scan(&id, &organizationID, &name, &isDefault, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return organization.Project{}, false, nil
		}
		return organization.Project{}, false, fmt.Errorf("scan Project: %w", err)
	}
	return organization.Project{
		ID: organization.ProjectID(id), OrganizationID: organization.ID(organizationID),
		Name: name, IsDefault: isDefault, CreatedAt: createdAt.UTC(),
	}, true, nil
}
