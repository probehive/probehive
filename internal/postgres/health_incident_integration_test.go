package postgres

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/health"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/run"
	"github.com/probehive/probehive/internal/webhook"
)

func TestHealthConfirmationAndIncidentLifecycle(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 1200, "health-incident-tenant")
	monitorValue := seedMonitor(t, database, 1205, organizationValue, project, testTime())
	monitorValue = appendTestRevision(
		t, database, monitorValue, 1, "{\"url\":\"https://health.example.test\"}")
	activateMonitor(t, database, &monitorValue)

	webhookKeyring, err := webhook.NewKeyring([]webhook.WrappingKey{{
		ID: "test", Key: bytes.Repeat([]byte{9}, 32),
	}})
	if err != nil {
		t.Fatal(err)
	}
	webhookService := webhook.NewService(
		database.Webhooks(), fixedClock{value: testTime()},
		&sequenceUUIDs{values: []string{testUUID(1249)}},
		bytes.NewReader(sequenceBytes(64)), webhookKeyring,
	)
	createdWebhook, err := webhookService.Create(t.Context(), webhook.CreateCommand{
		OrganizationID: string(organizationValue.ID),
		Name:           "Incident receiver", DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil || createdWebhook.Kind != webhook.CreateCreated {
		t.Fatalf("Create(Webhook) = %#v, %v", createdWebhook, err)
	}
	enabled := true
	enabledWebhook, err := webhookService.SetEnabled(t.Context(), webhook.StateCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 1, Enabled: &enabled,
	})
	if err != nil || enabledWebhook.Kind != webhook.StateUpdated {
		t.Fatalf("SetEnabled(Webhook) = %#v, %v", enabledWebhook, err)
	}

	base := testTime().Add(time.Hour)
	if _, err := database.Runs().EnsurePartitions(
		t.Context(), base, run.DefaultPartitionsAhead); err != nil {
		t.Fatalf("EnsurePartitions() error = %v", err)
	}

	failureEvent := completeHealthTestRun(
		t, database, run.KindScheduled, nil,
		1206, 1207, string(organizationValue.ID), string(monitorValue.ID),
		base, run.OutcomeFailed, "probe.http.status.unexpected")
	failureIDs := health.ProcessIDs{
		CandidateID: testUUID(1208), ConfirmationEventID: testUUID(1209),
		TransitionID: testUUID(1210), TransitionEventID: testUUID(1211),
	}
	if err := database.Health().ProcessRunRecorded(
		t.Context(), failureEvent, failureIDs, base.Add(3*time.Second)); err != nil {
		t.Fatalf("ProcessRunRecorded(failure) error = %v", err)
	}
	scope := health.Scope{
		OrganizationID: string(organizationValue.ID),
		ProjectID:      string(project.ID),
		MonitorID:      string(monitorValue.ID),
	}
	degraded, found, err := database.Health().GetHealth(t.Context(), scope)
	if err != nil || !found {
		t.Fatalf("GetHealth(degraded) found/error = %v/%v", found, err)
	}
	if degraded.State != health.StateDegraded || degraded.Candidate == nil ||
		degraded.Candidate.ID != failureIDs.CandidateID {
		t.Fatalf("degraded health = %#v", degraded)
	}

	failureConfirmationCause := &run.ConfirmationCause{
		CandidateID:            degraded.Candidate.ID,
		TriggeringRunID:        run.ID(degraded.Candidate.TriggeringRunID),
		TriggeringScheduledFor: degraded.Candidate.TriggeringScheduledFor,
		CausationEventID:       failureIDs.ConfirmationEventID,
		PolicyVersion:          health.PolicyVersion,
	}
	confirmationEvent := completeHealthTestRun(
		t, database, run.KindConfirmation, failureConfirmationCause,
		1212, 1213, string(organizationValue.ID), string(monitorValue.ID),
		base.Add(3*time.Second), run.OutcomeFailed, "probe.http.status.unexpected")
	confirmationIDs := health.ProcessIDs{
		CandidateID: testUUID(1214), ConfirmationEventID: testUUID(1215),
		TransitionID: testUUID(1216), TransitionEventID: testUUID(1217),
	}
	if err := database.Health().ProcessRunRecorded(
		t.Context(), confirmationEvent, confirmationIDs, base.Add(6*time.Second)); err != nil {
		t.Fatalf("ProcessRunRecorded(failure confirmation) error = %v", err)
	}
	down, _, err := database.Health().GetHealth(t.Context(), scope)
	if err != nil || down.State != health.StateDown || down.Candidate != nil {
		t.Fatalf("down health/error = %#v/%v", down, err)
	}
	var linkedRunID string
	if err := database.pool.QueryRow(t.Context(),
		"SELECT confirmation_run_id FROM health_candidates WHERE id=$1 AND organization_id=$2",
		failureIDs.CandidateID, string(organizationValue.ID)).Scan(&linkedRunID); err != nil {
		t.Fatalf("load linked confirmation Run: %v", err)
	}
	if linkedRunID != confirmationEvent.RunID {
		t.Fatalf("linked confirmation Run = %s, want %s", linkedRunID, confirmationEvent.RunID)
	}

	incidentStore := database.Incidents()
	firstTransition := loadIncidentTransitionEvent(t, database, failureIDs.TransitionEventID)
	secondTransition := loadIncidentTransitionEvent(t, database, confirmationIDs.TransitionEventID)
	if err := incidentStore.ProcessHealthTransition(
		t.Context(), firstTransition,
		incident.ProcessIDs{
			IncidentID: testUUID(1218), TimelineID: testUUID(1219),
			AlertEventID: testUUID(1246),
		},
		base.Add(7*time.Second)); err != nil {
		t.Fatalf("ProcessHealthTransition(degraded) error = %v", err)
	}
	incidentID, openedTimelineID, openedAlertEventID := testUUID(1220), testUUID(1221), testUUID(1241)
	if err := incidentStore.ProcessHealthTransition(
		t.Context(), secondTransition,
		incident.ProcessIDs{
			IncidentID: incidentID, TimelineID: openedTimelineID,
			AlertEventID: openedAlertEventID,
		},
		base.Add(8*time.Second)); err != nil {
		t.Fatalf("ProcessHealthTransition(down) error = %v", err)
	}

	incidentScope := incident.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      scope.MonitorID,
	}
	opened, found, err := incidentStore.GetIncident(t.Context(), incidentScope, incidentID)
	if err != nil || !found {
		t.Fatalf("GetIncident(opened) found/error = %v/%v", found, err)
	}
	if opened.State != incident.StateOpen || len(opened.Timeline) != 1 ||
		opened.Timeline[0].Counts == nil || opened.Timeline[0].Counts.Failing != 1 {
		t.Fatalf("opened Incident = %#v", opened)
	}

	alertService := alert.NewService(
		database.Alerts(), fixedClock{base.Add(9 * time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1242), testUUID(1250)}},
	)
	if err := alertService.HandleIncidentTransition(
		t.Context(), openedAlertEventID, string(organizationValue.ID),
		loadOutboxPayload(t, database, openedAlertEventID),
	); err != nil {
		t.Fatalf("HandleIncidentTransition(opened) error = %v", err)
	}
	alertService = alert.NewService(
		database.Alerts(), fixedClock{base.Add(10 * time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1245)}},
	)
	if err := alertService.HandleIncidentTransition(
		t.Context(), openedAlertEventID, string(organizationValue.ID),
		loadOutboxPayload(t, database, openedAlertEventID),
	); err != nil {
		t.Fatalf("redelivered opened Alert event error = %v", err)
	}
	alertScope := alert.Scope{
		OrganizationID: scope.OrganizationID, ProjectID: scope.ProjectID, MonitorID: scope.MonitorID,
	}
	alertPage, found, err := alertService.List(
		t.Context(), alertScope, alert.ListQuery{PageSize: 50},
	)
	if err != nil || !found || len(alertPage.Alerts) != 1 ||
		alertPage.Alerts[0].Kind != alert.KindIncidentOpened {
		t.Fatalf("opened Alert page = %#v, found %v, error %v", alertPage, found, err)
	}

	var deliveryID, routedAlertID, routedIntegrationID string
	var integrationVersion, secretVersion int64
	var routedAt time.Time
	if err := database.pool.QueryRow(t.Context(), `
SELECT id, alert_id, integration_id, integration_version, secret_version, routed_at
FROM webhook_deliveries
WHERE organization_id=$1`, string(organizationValue.ID)).Scan(
		&deliveryID, &routedAlertID, &routedIntegrationID,
		&integrationVersion, &secretVersion, &routedAt,
	); err != nil {
		t.Fatalf("load opened Alert Webhook route: %v", err)
	}
	if deliveryID != testUUID(1250) || routedAlertID != testUUID(1242) ||
		routedIntegrationID != createdWebhook.Integration.ID ||
		integrationVersion != 2 || secretVersion != 1 ||
		!routedAt.Equal(base.Add(9*time.Second)) {
		t.Fatalf(
			"Webhook route = %s/%s/%s/%d/%d/%v",
			deliveryID, routedAlertID, routedIntegrationID,
			integrationVersion, secretVersion, routedAt,
		)
	}
	disabled := false
	disabledWebhook, err := webhookService.SetEnabled(t.Context(), webhook.StateCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 2, Enabled: &disabled,
	})
	if err != nil || disabledWebhook.Kind != webhook.StateUpdated {
		t.Fatalf("SetEnabled(Webhook false) = %#v, %v", disabledWebhook, err)
	}
	actorID := seedAdministrator(t, database)
	acknowledged, found, err := incidentStore.AcknowledgeIncident(
		t.Context(), incidentScope, incidentID, actorID, testUUID(1222), base.Add(9*time.Second))
	if err != nil || !found || acknowledged.State != incident.StateAcknowledged {
		t.Fatalf("AcknowledgeIncident() = %#v, found %v, error %v", acknowledged, found, err)
	}

	recoveryEvent := completeHealthTestRun(
		t, database, run.KindScheduled, nil,
		1223, 1224, string(organizationValue.ID), string(monitorValue.ID),
		base.Add(time.Minute), run.OutcomePassed, "")
	recoveryIDs := health.ProcessIDs{
		CandidateID: testUUID(1225), ConfirmationEventID: testUUID(1226),
		TransitionID: testUUID(1227), TransitionEventID: testUUID(1228),
	}
	if err := database.Health().ProcessRunRecorded(
		t.Context(), recoveryEvent, recoveryIDs, base.Add(time.Minute+3*time.Second)); err != nil {
		t.Fatalf("ProcessRunRecorded(recovery) error = %v", err)
	}
	recovering, _, err := database.Health().GetHealth(t.Context(), scope)
	if err != nil || recovering.State != health.StateDegraded || recovering.Candidate == nil {
		t.Fatalf("recovering health/error = %#v/%v", recovering, err)
	}

	recoveryCause := &run.ConfirmationCause{
		CandidateID:            recovering.Candidate.ID,
		TriggeringRunID:        run.ID(recovering.Candidate.TriggeringRunID),
		TriggeringScheduledFor: recovering.Candidate.TriggeringScheduledFor,
		CausationEventID:       recoveryIDs.ConfirmationEventID,
		PolicyVersion:          health.PolicyVersion,
	}
	recoveryConfirmation := completeHealthTestRun(
		t, database, run.KindConfirmation, recoveryCause,
		1229, 1230, string(organizationValue.ID), string(monitorValue.ID),
		base.Add(time.Minute+3*time.Second), run.OutcomePassed, "")
	recoveredIDs := health.ProcessIDs{
		CandidateID: testUUID(1231), ConfirmationEventID: testUUID(1232),
		TransitionID: testUUID(1233), TransitionEventID: testUUID(1234),
	}
	if err := database.Health().ProcessRunRecorded(
		t.Context(), recoveryConfirmation, recoveredIDs, base.Add(time.Minute+6*time.Second)); err != nil {
		t.Fatalf("ProcessRunRecorded(recovery confirmation) error = %v", err)
	}

	thirdTransition := loadIncidentTransitionEvent(t, database, recoveryIDs.TransitionEventID)
	fourthTransition := loadIncidentTransitionEvent(t, database, recoveredIDs.TransitionEventID)
	if err := incidentStore.ProcessHealthTransition(
		t.Context(), thirdTransition,
		incident.ProcessIDs{
			IncidentID: testUUID(1235), TimelineID: testUUID(1236),
			AlertEventID: testUUID(1247),
		},
		base.Add(time.Minute+7*time.Second)); err != nil {
		t.Fatalf("ProcessHealthTransition(recovering) error = %v", err)
	}
	resolvedTimelineID, resolvedAlertEventID := testUUID(1237), testUUID(1243)
	if err := incidentStore.ProcessHealthTransition(
		t.Context(), fourthTransition,
		incident.ProcessIDs{
			IncidentID: testUUID(1238), TimelineID: resolvedTimelineID,
			AlertEventID: resolvedAlertEventID,
		},
		base.Add(time.Minute+8*time.Second)); err != nil {
		t.Fatalf("ProcessHealthTransition(healthy) error = %v", err)
	}
	if err := incidentStore.ProcessHealthTransition(
		t.Context(), fourthTransition,
		incident.ProcessIDs{
			IncidentID: testUUID(1239), TimelineID: testUUID(1240),
			AlertEventID: testUUID(1248),
		},
		base.Add(time.Minute+9*time.Second)); err != nil {
		t.Fatalf("redelivered healthy transition error = %v", err)
	}

	resolved, found, err := incidentStore.GetIncident(t.Context(), incidentScope, incidentID)
	if err != nil || !found {
		t.Fatalf("GetIncident(resolved) found/error = %v/%v", found, err)
	}
	if resolved.State != incident.StateResolved || resolved.Version != 3 ||
		len(resolved.Timeline) != 3 || resolved.Timeline[2].ID != resolvedTimelineID ||
		resolved.Timeline[2].Counts == nil || resolved.Timeline[2].Counts.Passing != 1 {
		t.Fatalf("resolved Incident = %#v", resolved)
	}

	alertService = alert.NewService(
		database.Alerts(), fixedClock{base.Add(time.Minute + 9*time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1244)}},
	)
	if err := alertService.HandleIncidentTransition(
		t.Context(), resolvedAlertEventID, string(organizationValue.ID),
		loadOutboxPayload(t, database, resolvedAlertEventID),
	); err != nil {
		t.Fatalf("HandleIncidentTransition(resolved) error = %v", err)
	}
	alertPage, found, err = alertService.List(
		t.Context(), alertScope, alert.ListQuery{PageSize: 50},
	)
	if err != nil || !found || len(alertPage.Alerts) != 2 ||
		alertPage.Alerts[0].Kind != alert.KindIncidentResolved ||
		alertPage.Alerts[1].Kind != alert.KindIncidentOpened {
		t.Fatalf("resolved Alert page = %#v, found %v, error %v", alertPage, found, err)
	}
	var deliveryCount int
	if err := database.pool.QueryRow(t.Context(), `
SELECT count(*) FROM webhook_deliveries WHERE organization_id=$1`,
		string(organizationValue.ID)).Scan(&deliveryCount); err != nil {
		t.Fatalf("count Webhook routes: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("Webhook route count after disabled resolved Alert = %d", deliveryCount)
	}
}

func completeHealthTestRun(
	t *testing.T,
	database *DB,
	kind run.Kind,
	cause *run.ConfirmationCause,
	runID, eventID int,
	organizationID, monitorID string,
	scheduledFor time.Time,
	outcome run.Outcome,
	failureCode string,
) health.RunRecordedV1 {
	t.Helper()
	slot := run.Slot{
		OrganizationID: organizationID, MonitorID: monitorID, RevisionNumber: 1,
		Location: "embedded", ScheduledFor: scheduledFor,
	}
	holder := testUUID(runID + 100)
	var claimed run.Run
	var err error
	if cause == nil {
		claimed, err = run.Claim(run.ID(testUUID(runID)), slot, kind, holder, scheduledFor.Add(time.Minute))
	} else {
		claimed, err = run.ClaimConfirmation(
			run.ID(testUUID(runID)), slot, *cause, holder, scheduledFor.Add(time.Minute))
	}
	if err != nil {
		t.Fatalf("build claimed Run: %v", err)
	}
	claimed, err = database.Runs().ClaimSlot(t.Context(), claimed, scheduledFor)
	if err != nil {
		t.Fatalf("ClaimSlot() error = %v", err)
	}
	completed := claimed
	if err := completed.Complete(
		outcome, scheduledFor.Add(time.Second), scheduledFor.Add(2*time.Second)); err != nil {
		t.Fatalf("Run.Complete() error = %v", err)
	}
	status := 200
	if failureCode != "" {
		status = 500
	}
	observation := run.Observation{
		RunID: completed.ID, ScheduledFor: scheduledFor, OrganizationID: organizationID,
		FailureCode: failureCode, Duration: time.Second,
		HTTP: &run.HTTPDetail{StatusCode: status, Protocol: "HTTP/1.1"},
	}
	entry, err := run.NewRunRecordedEntry(
		run.ID(testUUID(eventID)), completed, completed.FinishedAt)
	if err != nil {
		t.Fatalf("NewRunRecordedEntry() error = %v", err)
	}
	if err := database.Runs().Complete(
		t.Context(), completed, holder, observation, []run.OutboxEntry{entry}); err != nil {
		t.Fatalf("RunStore.Complete() error = %v", err)
	}
	var event health.RunRecordedV1
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		t.Fatalf("unmarshal run.recorded.v1: %v", err)
	}
	return event
}

func loadIncidentTransitionEvent(
	t *testing.T, database *DB, eventID string,
) incident.HealthTransitionedV1 {
	t.Helper()
	var payload []byte
	if err := database.pool.QueryRow(t.Context(),
		"SELECT payload FROM outbox_entries WHERE id=$1", eventID).Scan(&payload); err != nil {
		t.Fatalf("load health.transitioned.v1 %s: %v", eventID, err)
	}
	var event incident.HealthTransitionedV1
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("unmarshal health.transitioned.v1: %v", err)
	}
	return event
}

func loadOutboxPayload(t *testing.T, database *DB, eventID string) []byte {
	t.Helper()
	var payload []byte
	if err := database.pool.QueryRow(
		t.Context(), "SELECT payload FROM outbox_entries WHERE id=$1", eventID,
	).Scan(&payload); err != nil {
		t.Fatalf("load outbox event %s: %v", eventID, err)
	}
	return payload
}
