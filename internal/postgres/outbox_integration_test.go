package postgres

import (
	"testing"
	"time"
)

func TestOutboxClaimAcceptsNullGapFirstSeenAt(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, _ := seedTenant(t, database, 1300, "outbox-claim-tenant")
	now := testTime().Add(time.Hour)
	entryID := testUUID(1302)

	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO outbox_entries (
    id, organization_id, topic, payload, attempts, created_at, available_at
) VALUES ($1, $2, $3, $4, 0, $5, $5)`,
		entryID, string(organizationValue.ID), "test.recorded.v1", []byte(`{"test":true}`), now); err != nil {
		t.Fatalf("insert outbox entry: %v", err)
	}

	entries, err := database.Outbox().Claim(
		t.Context(), "test-holder", now, now.Add(time.Minute), 1)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Claim() returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ID != entryID || entry.Attempts != 1 {
		t.Fatalf("Claim() entry = %#v", entry)
	}
	if !entry.GapFirstSeenAt.IsZero() {
		t.Fatalf("Claim() GapFirstSeenAt = %v, want zero", entry.GapFirstSeenAt)
	}
}
