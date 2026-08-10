package run

import (
	"encoding/json"
	"errors"
	"time"
)

const TopicRunRecordedV1 = "run.recorded.v1"

const postgresTimePrecision = time.Microsecond

type RunRecordedV1 struct {
	EventID          string    `json:"eventId"`
	OrganizationID   string    `json:"organizationId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType"`
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int       `json:"aggregateVersion"`
	RunID            string    `json:"runId"`
	MonitorID        string    `json:"monitorId"`
	RevisionNumber   int       `json:"revisionNumber"`
	Location         string    `json:"location"`
	ScheduledFor     time.Time `json:"scheduledFor"`
	Kind             string    `json:"kind"`
	Outcome          string    `json:"outcome"`
}

func NewRunRecordedEntry(id ID, value Run, occurredAt time.Time) (OutboxEntry, error) {
	if id == "" {
		return OutboxEntry{}, errors.New("a Run recorded event requires an identifier")
	}
	if value.InFlight() {
		return OutboxEntry{}, errors.New("a Run recorded event requires a terminal Run")
	}
	if !isUTC(occurredAt) {
		return OutboxEntry{}, errors.New("a Run recorded event requires a UTC occurrence time")
	}
	event := RunRecordedV1{
		EventID: string(id), OrganizationID: value.Slot.OrganizationID,
		OccurredAt: occurredAt, AggregateType: "run", AggregateID: string(value.ID),
		AggregateVersion: 1, RunID: string(value.ID), MonitorID: value.Slot.MonitorID,
		RevisionNumber: value.Slot.RevisionNumber, Location: value.Slot.Location,
		ScheduledFor: canonicalInstant(value.Slot.ScheduledFor), Kind: string(value.Kind), Outcome: string(value.Outcome),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return OutboxEntry{}, err
	}
	entry := OutboxEntry{
		ID: id, OrganizationID: value.Slot.OrganizationID, Topic: TopicRunRecordedV1,
		Payload: payload, CreatedAt: occurredAt,
	}
	if err := entry.Validate(); err != nil {
		return OutboxEntry{}, err
	}
	return entry, nil
}

// PostgreSQL timestamptz stores microseconds; event timestamps compared with persisted Run rows
// must use the same precision.
func canonicalInstant(value time.Time) time.Time {
	return value.UTC().Truncate(postgresTimePrecision)
}
