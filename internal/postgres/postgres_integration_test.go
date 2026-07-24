package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/user"
)

const foreignKeyViolation = "23503"

func TestMigrationsAreConcurrentSafeAndIdempotent(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	opened, err := Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := opened.Ping(t.Context()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	opened.Close()

	database := newIntegrationDatabase(t, false)
	start := make(chan struct{})
	errorsByRunner := make(chan error, 4)
	for range 4 {
		go func() {
			<-start
			errorsByRunner <- database.Migrate(t.Context())
		}()
	}
	close(start)
	for range 4 {
		if err := <-errorsByRunner; err != nil {
			t.Fatalf("concurrent Migrate() error = %v", err)
		}
	}
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatalf("idempotent Migrate() error = %v", err)
	}

	var migrationCount int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if migrationCount != 2 {
		t.Fatalf("schema migration count = %d, want 2", migrationCount)
	}

	for _, table := range []string{
		"organizations", "projects", "users", "monitors", "monitor_revisions",
		"sessions", "antiforgery_tokens", "anonymous_antiforgery_keys", "schema_migrations",
	} {
		if !relationExists(t, database, table) {
			t.Errorf("required table %q does not exist", table)
		}
	}
	for _, retired := range []string{"data_protection_keys", "__EFMigrationsHistory"} {
		if relationExists(t, database, retired) {
			t.Errorf("retired table %q unexpectedly exists", retired)
		}
	}
}

func TestFailedMigrationRollsBackOnlyItsSchemaAndVersionRecord(t *testing.T) {
	database := newIntegrationDatabase(t, false)
	failing := fstest.MapFS{
		"migrations/0001_marker.sql": {
			Data: []byte("CREATE TABLE migration_atomicity_marker (id integer PRIMARY KEY)"),
		},
		"migrations/0002_failure.sql": {
			Data: []byte("CREATE TABLE failed_migration_marker (id integer); SELECT * FROM relation_that_does_not_exist"),
		},
	}
	if err := runMigrations(t.Context(), database.pool, failing); err == nil {
		t.Fatal("runMigrations() error = nil, want failed migration")
	}
	if !relationExists(t, database, "migration_atomicity_marker") {
		t.Error("previously successful migration was rolled back")
	}
	if relationExists(t, database, "failed_migration_marker") {
		t.Error("schema change from failed migration was committed")
	}
	var versionCount int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&versionCount); err != nil {
		t.Fatalf("count migration versions: %v", err)
	}
	if versionCount != 1 {
		t.Fatalf("migration version count = %d, want only the successful version", versionCount)
	}
}

func TestOrganizationProvisioningRereadsDuplicateRaceWinner(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	delegate := database.Organizations()
	store := &synchronizedOrganizationStore{OrganizationStore: delegate, ready: make(chan struct{})}
	now := testTime()
	services := []*organization.Service{
		organization.NewService(store, fixedClock{now}, &sequenceUUIDs{values: []string{testUUID(1), testUUID(2)}}),
		organization.NewService(store, fixedClock{now}, &sequenceUUIDs{values: []string{testUUID(3), testUUID(4)}}),
	}

	start := make(chan struct{})
	results := make(chan organization.ProvisionResult, 2)
	errorsByCall := make(chan error, 2)
	for _, service := range services {
		go func(service *organization.Service) {
			<-start
			result, err := service.Provision(t.Context(), organization.ProvisionCommand{
				Slug: "race-tenant", DisplayName: "Race Tenant",
			})
			results <- result
			errorsByCall <- err
		}(service)
	}
	close(start)

	kinds := map[organization.ProvisionKind]int{}
	var winner organization.ID
	for range 2 {
		if err := <-errorsByCall; err != nil {
			t.Fatalf("Provision() error = %v", err)
		}
		result := <-results
		kinds[result.Kind]++
		if winner == "" {
			winner = result.Details.Organization.ID
		} else if result.Details.Organization.ID != winner {
			t.Errorf("race results returned different Organizations: %s and %s", winner, result.Details.Organization.ID)
		}
	}
	if kinds[organization.ProvisionCreated] != 1 || kinds[organization.ProvisionReplayed] != 1 {
		t.Fatalf("provision kinds = %#v, want one created and one replayed", kinds)
	}

	var organizationCount, projectCount int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM organizations").Scan(&organizationCount); err != nil {
		t.Fatalf("count Organizations: %v", err)
	}
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM projects").Scan(&projectCount); err != nil {
		t.Fatalf("count Projects: %v", err)
	}
	if organizationCount != 1 || projectCount != 1 {
		t.Fatalf("persisted Organizations/Projects = %d/%d, want 1/1", organizationCount, projectCount)
	}
}

func TestDefaultProjectPartialUniqueIndex(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, _ := seedTenant(t, database, 80, "default-project-tenant")
	now := testTime().Add(time.Minute)

	_, err := database.pool.Exec(t.Context(), `
INSERT INTO projects (id, organization_id, name, is_default, created_at)
VALUES ($1, $2, $3, $4, $5)`,
		testUUID(82), string(organizationValue.ID), "Second Default", true, now)
	requireConstraint(t, err, uniqueViolation, "ux_projects_organization_default")

	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO projects (id, organization_id, name, is_default, created_at)
VALUES ($1, $2, $3, $4, $5)`,
		testUUID(83), string(organizationValue.ID), "Additional Project", false, now); err != nil {
		t.Fatalf("insert non-default Project: %v", err)
	}

	var projectCount, defaultCount int
	if err := database.pool.QueryRow(t.Context(), `
SELECT count(*), count(*) FILTER (WHERE is_default)
FROM projects
WHERE organization_id = $1`, string(organizationValue.ID)).Scan(&projectCount, &defaultCount); err != nil {
		t.Fatalf("count Projects by default status: %v", err)
	}
	if projectCount != 2 || defaultCount != 1 {
		t.Fatalf("persisted Projects/defaults = %d/%d, want 2/1", projectCount, defaultCount)
	}
}

func TestOrganizationProvisioningRollsBackWhenDefaultProjectInsertFails(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	_, existingProject := seedTenant(t, database, 90, "existing-tenant")
	now := testTime()
	const slug = "rollback-tenant"
	candidateID := organization.ID(testUUID(92))
	service := organization.NewService(
		database.Organizations(),
		fixedClock{now},
		&sequenceUUIDs{values: []string{string(candidateID), string(existingProject.ID)}},
	)

	_, err := service.Provision(t.Context(), organization.ProvisionCommand{
		Slug: slug, DisplayName: "Rollback Tenant",
	})
	requireConstraint(t, err, uniqueViolation, "pk_projects")

	_, found, err := database.Organizations().FindByID(t.Context(), candidateID)
	if err != nil || found {
		t.Fatalf("FindByID() after rollback found/error = %v/%v, want false/nil", found, err)
	}
	_, found, err = database.Organizations().FindBySlug(t.Context(), slug)
	if err != nil || found {
		t.Fatalf("FindBySlug() after rollback found/error = %v/%v, want false/nil", found, err)
	}

	var organizationCount, projectCount int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM organizations").Scan(&organizationCount); err != nil {
		t.Fatalf("count Organizations after rollback: %v", err)
	}
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM projects").Scan(&projectCount); err != nil {
		t.Fatalf("count Projects after rollback: %v", err)
	}
	if organizationCount != 1 || projectCount != 1 {
		t.Fatalf("persisted Organizations/Projects after rollback = %d/%d, want 1/1", organizationCount, projectCount)
	}
}

func TestCompositeTenantForeignKeysRejectCrossTenantRows(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationA, projectA := seedTenant(t, database, 10, "tenant-a")
	organizationB, projectB := seedTenant(t, database, 20, "tenant-b")
	now := testTime()

	wrongProject, err := monitor.NewMonitor(
		monitor.ID(testUUID(30)), string(organizationA.ID), string(projectB.ID), "Wrong Project", "http", now,
	)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	err = database.Monitors().CreateMonitor(t.Context(), wrongProject)
	requireConstraint(t, err, foreignKeyViolation, "fk_monitors_projects")

	monitorA := seedMonitor(t, database, 31, organizationA, projectA, now)
	_ = seedMonitor(t, database, 32, organizationB, projectB, now)
	_, err = database.pool.Exec(t.Context(), `
INSERT INTO monitor_revisions (
    id, monitor_id, organization_id, revision_number, check_type,
    check_schema_version, check_configuration, created_at
) VALUES ($1, $2, $3, 1, 'http', 1, '{}'::jsonb, $4)`,
		testUUID(33), string(monitorA.ID), string(organizationB.ID), now)
	requireConstraint(t, err, foreignKeyViolation, "fk_monitor_revisions_monitors")
}

func TestFirstAdministratorContentionCreatesExactlyOneUser(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Users()
	now := testTime()
	accounts := []user.User{
		mustUser(t, 40, "first@example.com", now),
		mustUser(t, 41, "second@example.com", now),
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, account := range accounts {
		go func(account user.User) {
			<-start
			results <- store.CreateFirstAdministrator(t.Context(), account)
		}(account)
	}
	close(start)

	created := 0
	alreadyCompleted := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			created++
		case errors.Is(err, user.ErrSetupAlreadyCompleted):
			alreadyCompleted++
		default:
			t.Fatalf("CreateFirstAdministrator() error = %v", err)
		}
	}
	if created != 1 || alreadyCompleted != 1 {
		t.Fatalf("race results created/already-completed = %d/%d, want 1/1", created, alreadyCompleted)
	}
	var count int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
}

func TestSessionStoreRoundTripAndDeletion(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	account := mustUser(t, 100, "roundtrip@example.com", testTime())
	if err := database.Users().CreateFirstAdministrator(t.Context(), account); err != nil {
		t.Fatalf("CreateFirstAdministrator() error = %v", err)
	}

	tokenHash := user.TokenHash(sha256.Sum256([]byte("session roundtrip token")))
	want, err := user.NewSession(tokenHash, account.ID, testTime().Add(17*time.Minute))
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	store := database.Sessions()
	if err := store.Create(t.Context(), want); err != nil {
		t.Fatalf("SessionStore.Create() error = %v", err)
	}

	got, found, err := store.FindByTokenHash(t.Context(), tokenHash)
	if err != nil || !found {
		t.Fatalf("FindByTokenHash() found/error = %v/%v, want true/nil", found, err)
	}
	if got.TokenHash != want.TokenHash || got.UserID != want.UserID ||
		!got.AuthenticatedAt.Equal(want.AuthenticatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("FindByTokenHash() = %#v, want %#v", got, want)
	}

	if err := store.DeleteByTokenHash(t.Context(), tokenHash); err != nil {
		t.Fatalf("DeleteByTokenHash() error = %v", err)
	}
	_, found, err = store.FindByTokenHash(t.Context(), tokenHash)
	if err != nil || found {
		t.Fatalf("FindByTokenHash() after delete found/error = %v/%v, want false/nil", found, err)
	}
}

func TestSessionStoreCreateCleansExpiredSessionsAtomically(t *testing.T) {
	t.Run("successful insert removes expired sessions", func(t *testing.T) {
		database := newIntegrationDatabase(t, true)
		account := mustUser(t, 101, "cleanup@example.com", testTime())
		if err := database.Users().CreateFirstAdministrator(t.Context(), account); err != nil {
			t.Fatalf("CreateFirstAdministrator() error = %v", err)
		}

		store := database.Sessions()
		now := testTime()
		expiredHash := user.TokenHash(sha256.Sum256([]byte("expired session")))
		expired, err := user.NewSession(expiredHash, account.ID, now.Add(-user.SessionLifetime-time.Minute))
		if err != nil {
			t.Fatalf("NewSession() expired error = %v", err)
		}
		if err := store.Create(t.Context(), expired); err != nil {
			t.Fatalf("SessionStore.Create() expired error = %v", err)
		}

		currentHash := user.TokenHash(sha256.Sum256([]byte("current session")))
		current, err := user.NewSession(currentHash, account.ID, now)
		if err != nil {
			t.Fatalf("NewSession() current error = %v", err)
		}
		if err := store.Create(t.Context(), current); err != nil {
			t.Fatalf("SessionStore.Create() current error = %v", err)
		}

		_, found, err := store.FindByTokenHash(t.Context(), expiredHash)
		if err != nil || found {
			t.Fatalf("expired session after successful Create() found/error = %v/%v, want false/nil", found, err)
		}
		_, found, err = store.FindByTokenHash(t.Context(), currentHash)
		if err != nil || !found {
			t.Fatalf("current session after successful Create() found/error = %v/%v, want true/nil", found, err)
		}
	})

	t.Run("failed insert rolls cleanup back", func(t *testing.T) {
		database := newIntegrationDatabase(t, true)
		account := mustUser(t, 102, "rollback-cleanup@example.com", testTime())
		if err := database.Users().CreateFirstAdministrator(t.Context(), account); err != nil {
			t.Fatalf("CreateFirstAdministrator() error = %v", err)
		}

		store := database.Sessions()
		now := testTime()
		duplicateHash := user.TokenHash(sha256.Sum256([]byte("duplicate session")))
		duplicate, err := user.NewSession(duplicateHash, account.ID, now)
		if err != nil {
			t.Fatalf("NewSession() duplicate error = %v", err)
		}
		if err := store.Create(t.Context(), duplicate); err != nil {
			t.Fatalf("SessionStore.Create() initial duplicate error = %v", err)
		}

		expiredHash := user.TokenHash(sha256.Sum256([]byte("rollback expired session")))
		expired, err := user.NewSession(expiredHash, account.ID, now.Add(-user.SessionLifetime-time.Minute))
		if err != nil {
			t.Fatalf("NewSession() expired error = %v", err)
		}
		if err := store.Create(t.Context(), expired); err != nil {
			t.Fatalf("SessionStore.Create() expired error = %v", err)
		}

		err = store.Create(t.Context(), duplicate)
		requireConstraint(t, err, uniqueViolation, "pk_sessions")
		_, found, err := store.FindByTokenHash(t.Context(), expiredHash)
		if err != nil || !found {
			t.Fatalf("expired session after rolled-back Create() found/error = %v/%v, want true/nil", found, err)
		}
	})
}

func TestAnonymousAntiforgeryKeyIsCreatedOnceAndStable(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	store := database.Antiforgery()
	firstCandidate := user.AnonymousAntiforgeryKey(sha256.Sum256([]byte("first anonymous antiforgery key")))
	secondCandidate := user.AnonymousAntiforgeryKey(sha256.Sum256([]byte("second anonymous antiforgery key")))

	first, err := store.GetOrCreateAnonymousAntiforgeryKey(t.Context(), firstCandidate)
	if err != nil {
		t.Fatalf("first GetOrCreateAnonymousAntiforgeryKey() error = %v", err)
	}
	if first != firstCandidate {
		t.Fatalf("first GetOrCreateAnonymousAntiforgeryKey() returned unexpected key")
	}
	second, err := store.GetOrCreateAnonymousAntiforgeryKey(t.Context(), secondCandidate)
	if err != nil {
		t.Fatalf("second GetOrCreateAnonymousAntiforgeryKey() error = %v", err)
	}
	if second != first {
		t.Fatal("second GetOrCreateAnonymousAntiforgeryKey() replaced the singleton key")
	}

	loaded, found, err := store.FindAnonymousAntiforgeryKey(t.Context())
	if err != nil || !found {
		t.Fatalf("FindAnonymousAntiforgeryKey() found/error = %v/%v, want true/nil", found, err)
	}
	if loaded != first {
		t.Fatal("FindAnonymousAntiforgeryKey() did not return the initialized key")
	}
	var count int
	if err := database.pool.QueryRow(t.Context(), "SELECT count(*) FROM anonymous_antiforgery_keys").Scan(&count); err != nil {
		t.Fatalf("count anonymous antiforgery keys: %v", err)
	}
	if count != 1 {
		t.Fatalf("anonymous antiforgery key count = %d, want 1", count)
	}
}

func TestSessionAndAntiforgeryPersistenceStoresOnlyHashes(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	account := mustUser(t, 50, "session@example.com", testTime())
	if err := database.Users().CreateFirstAdministrator(t.Context(), account); err != nil {
		t.Fatalf("CreateFirstAdministrator() error = %v", err)
	}

	rawSession := bytes.Repeat([]byte{0x41}, 32)
	sessionHash := user.TokenHash(sha256.Sum256(rawSession))
	session, err := user.NewSession(sessionHash, account.ID, testTime())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := database.Sessions().Create(t.Context(), session); err != nil {
		t.Fatalf("SessionStore.Create() error = %v", err)
	}
	restoredAccount, found, err := database.Users().FindByID(t.Context(), session.UserID)
	if err != nil || !found {
		t.Fatalf("FindByID() found/error = %v/%v, want true/nil", found, err)
	}
	if restoredAccount.ID != account.ID || restoredAccount.Role != user.AdministratorRole {
		t.Fatalf("FindByID() = %#v, want persisted Administrator", restoredAccount)
	}

	var storedSessionHash []byte
	if err := database.pool.QueryRow(t.Context(), "SELECT token_hash FROM sessions").Scan(&storedSessionHash); err != nil {
		t.Fatalf("read stored session hash: %v", err)
	}
	if !bytes.Equal(storedSessionHash, sessionHash[:]) || bytes.Equal(storedSessionHash, rawSession) {
		t.Fatal("session persistence did not retain exactly the SHA-256 digest")
	}

	rawSelector := bytes.Repeat([]byte{0x42}, 32)
	rawRequest := bytes.Repeat([]byte{0x43}, 32)
	selectorHash := user.TokenHash(sha256.Sum256(rawSelector))
	requestHash := user.TokenHash(sha256.Sum256(rawRequest))
	record, err := user.NewSessionAntiforgeryRecord(
		selectorHash, requestHash, sessionHash, testTime().Add(user.SessionLifetime),
	)
	if err != nil {
		t.Fatalf("NewSessionAntiforgeryRecord() error = %v", err)
	}
	if err := database.Antiforgery().CreateSessionAntiforgery(t.Context(), record); err != nil {
		t.Fatalf("AntiforgeryStore.Create() error = %v", err)
	}
	loaded, found, err := database.Antiforgery().FindSessionAntiforgeryBySelectorHash(t.Context(), selectorHash)
	if err != nil || !found {
		t.Fatalf("FindBySelectorHash() found/error = %v/%v, want true/nil", found, err)
	}
	if !loaded.BoundTo(sessionHash) || loaded.RequestTokenHash != requestHash {
		t.Fatal("loaded antiforgery state lost its request hash or session binding")
	}

	var storedSelectorHash, storedRequestHash []byte
	if err := database.pool.QueryRow(t.Context(), `
SELECT selector_hash, request_token_hash FROM antiforgery_tokens`).Scan(&storedSelectorHash, &storedRequestHash); err != nil {
		t.Fatalf("read stored antiforgery hashes: %v", err)
	}
	if !bytes.Equal(storedSelectorHash, selectorHash[:]) || bytes.Equal(storedSelectorHash, rawSelector) ||
		!bytes.Equal(storedRequestHash, requestHash[:]) || bytes.Equal(storedRequestHash, rawRequest) {
		t.Fatal("antiforgery persistence did not retain exactly the SHA-256 digests")
	}

	if err := database.Sessions().DeleteByTokenHash(t.Context(), sessionHash); err != nil {
		t.Fatalf("DeleteByTokenHash() error = %v", err)
	}
	_, found, err = database.Antiforgery().FindSessionAntiforgeryBySelectorHash(t.Context(), selectorHash)
	if err != nil || found {
		t.Fatalf("cascaded antiforgery lookup found/error = %v/%v, want false/nil", found, err)
	}
}

func TestMonitorOrderingXminAndRevisionRace(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 60, "monitor-tenant")
	now := testTime()
	second := seedMonitor(t, database, 62, organizationValue, project, now)
	first := seedMonitor(t, database, 61, organizationValue, project, now)

	store := database.Monitors()
	listed, err := store.ListMonitors(t.Context(), string(organizationValue.ID), string(project.ID))
	if err != nil {
		t.Fatalf("ListMonitors() error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("ListMonitors() order = %#v, want UUID tie-break order", monitorIDs(listed))
	}
	_, found, err := store.FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: testUUID(999), ProjectID: string(project.ID), MonitorID: first.ID,
	})
	if err != nil || found {
		t.Fatalf("cross-tenant FindMonitor() found/error = %v/%v, want false/nil", found, err)
	}

	loaded, found, err := store.FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID), MonitorID: first.ID,
	})
	if err != nil || !found {
		t.Fatalf("FindMonitor() found/error = %v/%v, want true/nil", found, err)
	}
	stale := loaded
	if err := loaded.Rename("Winner", now.Add(time.Minute)); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := store.UpdateMonitor(t.Context(), loaded, stale.Version); err != nil {
		t.Fatalf("UpdateMonitor() error = %v", err)
	}
	if err := stale.Rename("Loser", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("stale Rename() error = %v", err)
	}
	if err := store.UpdateMonitor(t.Context(), stale, stale.Version); !errors.Is(err, monitor.ErrConcurrentUpdate) {
		t.Fatalf("stale UpdateMonitor() error = %v, want ErrConcurrentUpdate", err)
	}

	scope := monitor.Scope{
		OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID), MonitorID: second.ID,
	}
	left, found, err := store.FindMonitor(t.Context(), scope)
	if err != nil || !found {
		t.Fatalf("FindMonitor() before revision race found/error = %v/%v", found, err)
	}
	right := left
	revisionTime := now.Add(3 * time.Minute)
	leftRevision := mustRevision(t, 70, left, 1, revisionTime)
	rightRevision := mustRevision(t, 71, right, 1, revisionTime)
	if err := left.RecordRevision(1, revisionTime); err != nil {
		t.Fatalf("left RecordRevision() error = %v", err)
	}
	if err := right.RecordRevision(1, revisionTime); err != nil {
		t.Fatalf("right RecordRevision() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- store.AppendRevision(t.Context(), left, leftRevision, left.Version)
	}()
	go func() {
		<-start
		results <- store.AppendRevision(t.Context(), right, rightRevision, right.Version)
	}()
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, monitor.ErrConcurrentUpdate):
			conflicts++
		default:
			t.Fatalf("AppendRevision() race error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("revision race success/conflict = %d/%d, want 1/1", successes, conflicts)
	}

	revisions, err := store.ListRevisions(t.Context(), string(organizationValue.ID), second.ID)
	if err != nil {
		t.Fatalf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 1 || revisions[0].RevisionNumber != 1 {
		t.Fatalf("persisted revisions = %#v, want exactly revision 1", revisionNumbers(revisions))
	}
	current, found, err := store.FindMonitor(t.Context(), scope)
	if err != nil || !found || current.LatestRevisionNumber != 1 {
		t.Fatalf("Monitor after revision race found/error/latest = %v/%v/%d", found, err, current.LatestRevisionNumber)
	}

	backstop := seedMonitor(t, database, 63, organizationValue, project, now.Add(4*time.Minute))
	backstopScope := monitor.Scope{
		OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID), MonitorID: backstop.ID,
	}
	backstopLoaded, found, err := store.FindMonitor(t.Context(), backstopScope)
	if err != nil || !found {
		t.Fatalf("FindMonitor() before unique-index backstop found/error = %v/%v", found, err)
	}
	backstopTime := now.Add(5 * time.Minute)
	preexisting := mustRevision(t, 72, backstopLoaded, 1, backstopTime)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO monitor_revisions (
    id, monitor_id, organization_id, revision_number, check_type,
    check_schema_version, check_configuration, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		string(preexisting.ID), string(preexisting.MonitorID), preexisting.OrganizationID,
		preexisting.RevisionNumber, preexisting.CheckType, preexisting.CheckSchemaVersion,
		string(preexisting.CheckConfiguration), preexisting.CreatedAt); err != nil {
		t.Fatalf("insert preexisting revision for unique-index backstop: %v", err)
	}

	mutated := backstopLoaded
	if err := mutated.RecordRevision(1, backstopTime); err != nil {
		t.Fatalf("RecordRevision() for unique-index backstop error = %v", err)
	}
	contender := mustRevision(t, 73, backstopLoaded, 1, backstopTime)
	if err := store.AppendRevision(
		t.Context(), mutated, contender, backstopLoaded.Version,
	); !errors.Is(err, monitor.ErrConcurrentUpdate) {
		t.Fatalf("unique-index AppendRevision() error = %v, want ErrConcurrentUpdate", err)
	}
	afterBackstop, found, err := store.FindMonitor(t.Context(), backstopScope)
	if err != nil || !found {
		t.Fatalf("FindMonitor() after unique-index backstop found/error = %v/%v", found, err)
	}
	if afterBackstop.LatestRevisionNumber != 0 {
		t.Fatalf("Monitor counter after rolled-back unique conflict = %d, want 0", afterBackstop.LatestRevisionNumber)
	}
}

func TestRevisionQueriesDenyWrongOrganization(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationA, projectA := seedTenant(t, database, 110, "revision-tenant-a")
	organizationB, _ := seedTenant(t, database, 120, "revision-tenant-b")
	now := testTime()
	created := seedMonitor(t, database, 130, organizationA, projectA, now)
	store := database.Monitors()
	scope := monitor.Scope{
		OrganizationID: string(organizationA.ID),
		ProjectID:      string(projectA.ID),
		MonitorID:      created.ID,
	}
	persisted, found, err := store.FindMonitor(t.Context(), scope)
	if err != nil || !found {
		t.Fatalf("FindMonitor() found/error = %v/%v, want true/nil", found, err)
	}

	revisionTime := now.Add(time.Minute)
	revision := mustRevision(t, 131, persisted, 1, revisionTime)
	expectedVersion := persisted.Version
	if err := persisted.RecordRevision(1, revisionTime); err != nil {
		t.Fatalf("RecordRevision() error = %v", err)
	}
	if err := store.AppendRevision(t.Context(), persisted, revision, expectedVersion); err != nil {
		t.Fatalf("AppendRevision() error = %v", err)
	}

	loaded, found, err := store.FindRevision(t.Context(), string(organizationA.ID), created.ID, 1)
	if err != nil || !found || loaded.ID != revision.ID {
		t.Fatalf("same-Organization FindRevision() found/error/id = %v/%v/%s", found, err, loaded.ID)
	}
	_, found, err = store.FindRevision(t.Context(), string(organizationB.ID), created.ID, 1)
	if err != nil || found {
		t.Fatalf("wrong-Organization FindRevision() found/error = %v/%v, want false/nil", found, err)
	}

	revisions, err := store.ListRevisions(t.Context(), string(organizationB.ID), created.ID)
	if err != nil {
		t.Fatalf("wrong-Organization ListRevisions() error = %v", err)
	}
	if len(revisions) != 0 {
		t.Fatalf("wrong-Organization ListRevisions() = %#v, want empty", revisionNumbers(revisions))
	}
}

func newIntegrationDatabase(t *testing.T, migrate bool) *DB {
	t.Helper()
	databaseURL := integrationDatabaseURL(t)
	baseConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PROBEHIVE_TEST_DATABASE_URL: %v", err)
	}
	basePool, err := pgxpool.NewWithConfig(t.Context(), baseConfig)
	if err != nil {
		t.Fatalf("open integration-test base pool: %v", err)
	}
	if err := basePool.Ping(t.Context()); err != nil {
		basePool.Close()
		t.Fatalf("ping integration-test database: %v", err)
	}

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		basePool.Close()
		t.Fatalf("generate integration-test schema name: %v", err)
	}
	schema := "probehive_test_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := basePool.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		basePool.Close()
		t.Fatalf("create integration-test schema: %v", err)
	}

	testConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		basePool.Close()
		t.Fatalf("parse integration-test pool config: %v", err)
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schema
	testConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	testPool, err := pgxpool.NewWithConfig(t.Context(), testConfig)
	if err != nil {
		basePool.Close()
		t.Fatalf("open integration-test schema pool: %v", err)
	}
	database := &DB{pool: testPool}
	t.Cleanup(func() {
		database.Close()
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := basePool.Exec(cleanupContext, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop integration-test schema: %v", err)
		}
		basePool.Close()
	})
	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("ping integration-test schema pool: %v", err)
	}
	if migrate {
		if err := database.Migrate(t.Context()); err != nil {
			t.Fatalf("migrate integration-test schema: %v", err)
		}
	}
	return database
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	value := os.Getenv("PROBEHIVE_TEST_DATABASE_URL")
	if value == "" {
		t.Skip("PROBEHIVE_TEST_DATABASE_URL is not set")
	}
	return value
}

func relationExists(t *testing.T, database *DB, name string) bool {
	t.Helper()
	var relation *string
	if err := database.pool.QueryRow(t.Context(), "SELECT to_regclass($1)::text", name).Scan(&relation); err != nil {
		t.Fatalf("check relation %q: %v", name, err)
	}
	return relation != nil
}

type synchronizedOrganizationStore struct {
	*OrganizationStore
	mu     sync.Mutex
	misses int
	ready  chan struct{}
}

func (store *synchronizedOrganizationStore) FindBySlug(ctx context.Context, slug string) (organization.Organization, bool, error) {
	value, found, err := store.OrganizationStore.FindBySlug(ctx, slug)
	if err != nil || found {
		return value, found, err
	}
	store.mu.Lock()
	store.misses++
	if store.misses == 2 {
		close(store.ready)
	}
	ready := store.ready
	store.mu.Unlock()
	select {
	case <-ready:
		return value, false, nil
	case <-ctx.Done():
		return organization.Organization{}, false, ctx.Err()
	}
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type sequenceUUIDs struct {
	mu     sync.Mutex
	values []string
}

func (generator *sequenceUUIDs) NewUUIDv7(time.Time) (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	if len(generator.values) == 0 {
		return "", errors.New("test UUID sequence exhausted")
	}
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func seedTenant(t *testing.T, database *DB, offset int, slug string) (organization.Organization, organization.Project) {
	t.Helper()
	now := testTime()
	organizationValue, err := organization.NewOrganization(
		organization.ID(testUUID(offset)), slug, "Test Tenant", now,
	)
	if err != nil {
		t.Fatalf("NewOrganization() error = %v", err)
	}
	project, err := organization.NewDefaultProject(
		organization.ProjectID(testUUID(offset+1)), organizationValue.ID, now,
	)
	if err != nil {
		t.Fatalf("NewDefaultProject() error = %v", err)
	}
	if err := database.Organizations().Create(t.Context(), organizationValue, project); err != nil {
		t.Fatalf("OrganizationStore.Create() error = %v", err)
	}
	return organizationValue, project
}

func seedMonitor(
	t *testing.T,
	database *DB,
	id int,
	organizationValue organization.Organization,
	project organization.Project,
	createdAt time.Time,
) monitor.Monitor {
	t.Helper()
	value, err := monitor.NewMonitor(
		monitor.ID(testUUID(id)), string(organizationValue.ID), string(project.ID),
		fmt.Sprintf("Monitor %d", id), "http", createdAt,
	)
	if err != nil {
		t.Fatalf("NewMonitor() error = %v", err)
	}
	if err := database.Monitors().CreateMonitor(t.Context(), value); err != nil {
		t.Fatalf("MonitorStore.CreateMonitor() error = %v", err)
	}
	return value
}

func mustUser(t *testing.T, id int, email string, createdAt time.Time) user.User {
	t.Helper()
	value, err := user.NewUser(
		user.ID(testUUID(id)), email, "Administrator", user.AdministratorRole,
		"$argon2id$test-only-hash", createdAt,
	)
	if err != nil {
		t.Fatalf("NewUser() error = %v", err)
	}
	return value
}

func mustRevision(t *testing.T, id int, value monitor.Monitor, number int, createdAt time.Time) monitor.Revision {
	t.Helper()
	revision, err := monitor.NewRevision(
		monitor.RevisionID(testUUID(id)), value.ID, value.OrganizationID, number,
		value.CheckType, 1, json.RawMessage(`{"url":"https://example.test"}`), createdAt,
	)
	if err != nil {
		t.Fatalf("NewRevision() error = %v", err)
	}
	return revision
}

func requireConstraint(t *testing.T, err error, sqlState, constraint string) {
	t.Helper()
	if !isConstraintViolation(err, sqlState, constraint) {
		t.Fatalf("error = %v, want PostgreSQL %s constraint %q", err, sqlState, constraint)
	}
}

func monitorIDs(values []monitor.Monitor) []monitor.ID {
	ids := make([]monitor.ID, len(values))
	for index, value := range values {
		ids[index] = value.ID
	}
	return ids
}

func revisionNumbers(values []monitor.Revision) []int {
	numbers := make([]int, len(values))
	for index, value := range values {
		numbers[index] = value.RevisionNumber
	}
	return numbers
}

func testTime() time.Time {
	return time.Date(2026, time.July, 24, 10, 30, 0, 0, time.UTC)
}

func testUUID(value int) string {
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", value)
}
