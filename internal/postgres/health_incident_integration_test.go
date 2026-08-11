package postgres

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/health"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/maintenance"
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
		bytes.NewReader(sequenceBytes(128)), webhookKeyring,
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
	prepared, err := webhookService.PrepareRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 2,
	})
	if err != nil || prepared.Kind != webhook.RotationPrepared {
		t.Fatalf("PrepareRotation(Webhook) = %#v, %v", prepared, err)
	}
	activated, err := webhookService.ActivateRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 3,
	})
	if err != nil || activated.Kind != webhook.RotationUpdated {
		t.Fatalf("ActivateRotation(Webhook) = %#v, %v", activated, err)
	}
	blockedRetirement, err := webhookService.RetireRotation(
		t.Context(), webhook.RotationCommand{
			OrganizationID:  string(organizationValue.ID),
			IntegrationID:   createdWebhook.Integration.ID,
			ExpectedVersion: 4,
		},
	)
	if err != nil || blockedRetirement.Kind != webhook.RotationConflict ||
		blockedRetirement.Code != webhook.RetiringSecretInUseCode {
		t.Fatalf("RetireRotation(in-use Webhook secret) = %#v, %v", blockedRetirement, err)
	}
	deliveryStore := database.Webhooks()
	firstClaimAt := base.Add(11 * time.Second)
	claims, err := deliveryStore.Claim(
		t.Context(), testUUID(1251), firstClaimAt,
		firstClaimAt.Add(webhook.DeliveryLeaseDuration), webhook.DeliveryBatchSize,
	)
	if err != nil || len(claims) != 1 {
		t.Fatalf("Claim(first Webhook delivery) = %#v, %v", claims, err)
	}
	firstClaim := claims[0]
	if firstClaim.DeliveryID != deliveryID || firstClaim.Sequence != 1 ||
		firstClaim.AlertID != routedAlertID ||
		firstClaim.IntegrationID != routedIntegrationID ||
		firstClaim.Envelope.KeyID != "test" ||
		len(firstClaim.Envelope.Nonce) != 12 ||
		len(firstClaim.Envelope.Ciphertext) < 16 {
		t.Fatalf("first Webhook delivery claim = %#v", firstClaim)
	}
	var durableOutcome string
	var durableFinishedAt *time.Time
	if err := database.pool.QueryRow(t.Context(), `
SELECT outcome, finished_at
FROM webhook_delivery_attempts
WHERE delivery_id=$1 AND sequence=1`, deliveryID).Scan(
		&durableOutcome, &durableFinishedAt,
	); err != nil {
		t.Fatalf("load durable in-progress Webhook attempt: %v", err)
	}
	if durableOutcome != webhook.OutcomeInProgress || durableFinishedAt != nil {
		t.Fatalf("durable attempt = %q/%v", durableOutcome, durableFinishedAt)
	}

	reclaimAt := firstClaim.LeaseExpiresAt.Add(time.Second)
	claims, err = deliveryStore.Claim(
		t.Context(), testUUID(1252), reclaimAt,
		reclaimAt.Add(webhook.DeliveryLeaseDuration), webhook.DeliveryBatchSize,
	)
	if err != nil || len(claims) != 1 || claims[0].Sequence != 2 {
		t.Fatalf("Claim(expired Webhook delivery) = %#v, %v", claims, err)
	}
	secondClaim := claims[0]
	retryStatus := http.StatusServiceUnavailable
	retryFinishedAt := reclaimAt.Add(time.Second)
	nextAvailableAt := reclaimAt.Add(2 * time.Second)
	if err := deliveryStore.Complete(
		t.Context(), secondClaim, webhook.AttemptUpdate{
			Outcome: webhook.OutcomeFailed, FinishedAt: retryFinishedAt,
			HTTPStatus: &retryStatus, FailureCode: webhook.FailureCodeHTTPRetryable,
		}, &nextAvailableAt, false,
	); err != nil {
		t.Fatalf("Complete(retryable Webhook delivery) error = %v", err)
	}

	claims, err = deliveryStore.Claim(
		t.Context(), testUUID(1253), nextAvailableAt,
		nextAvailableAt.Add(webhook.DeliveryLeaseDuration), webhook.DeliveryBatchSize,
	)
	if err != nil || len(claims) != 1 || claims[0].Sequence != 3 {
		t.Fatalf("Claim(retried Webhook delivery) = %#v, %v", claims, err)
	}
	successStatus := http.StatusNoContent
	successFinishedAt := nextAvailableAt.Add(time.Second)
	if err := deliveryStore.Complete(
		t.Context(), claims[0], webhook.AttemptUpdate{
			Outcome: webhook.OutcomeSucceeded, FinishedAt: successFinishedAt,
			HTTPStatus: &successStatus,
		}, nil, true,
	); err != nil {
		t.Fatalf("Complete(successful Webhook delivery) error = %v", err)
	}
	if after, err := deliveryStore.Claim(
		t.Context(), testUUID(1254), successFinishedAt.Add(time.Minute),
		successFinishedAt.Add(time.Minute+webhook.DeliveryLeaseDuration),
		webhook.DeliveryBatchSize,
	); err != nil || len(after) != 0 {
		t.Fatalf("Claim(completed Webhook delivery) = %#v, %v", after, err)
	}
	retired, err := webhookService.RetireRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 4,
	})
	if err != nil || retired.Kind != webhook.RotationUpdated ||
		retired.Integration.Version != 5 {
		t.Fatalf("RetireRotation(completed Webhook secret) = %#v, %v", retired, err)
	}

	audits, found, err := deliveryStore.ListAudit(
		t.Context(), webhook.DeliveryScope{
			OrganizationID: string(organizationValue.ID),
			ProjectID:      string(project.ID),
			MonitorID:      string(monitorValue.ID),
			AlertID:        routedAlertID,
		},
	)
	if err != nil || !found || len(audits) != 1 ||
		len(audits[0].Attempts) != 3 {
		t.Fatalf("ListAudit() = %#v, found %v, error %v", audits, found, err)
	}
	if audits[0].Attempts[0].Outcome != webhook.OutcomeFailed ||
		audits[0].Attempts[0].FailureCode != webhook.FailureCodeOutcomeUncertain ||
		audits[0].Attempts[0].HTTPStatus != nil ||
		audits[0].Attempts[1].Outcome != webhook.OutcomeFailed ||
		audits[0].Attempts[1].HTTPStatus == nil ||
		*audits[0].Attempts[1].HTTPStatus != retryStatus ||
		audits[0].Attempts[2].Outcome != webhook.OutcomeSucceeded ||
		audits[0].Attempts[2].HTTPStatus == nil ||
		*audits[0].Attempts[2].HTTPStatus != successStatus {
		t.Fatalf("Webhook delivery audit attempts = %#v", audits[0].Attempts)
	}
	_, wrongScopeFound, err := deliveryStore.ListAudit(
		t.Context(), webhook.DeliveryScope{
			OrganizationID: string(organizationValue.ID),
			ProjectID:      testUUID(1299),
			MonitorID:      string(monitorValue.ID),
			AlertID:        routedAlertID,
		},
	)
	if err != nil || wrongScopeFound {
		t.Fatalf("wrong-scope ListAudit() found/error = %v/%v", wrongScopeFound, err)
	}

	disabled := false
	disabledWebhook, err := webhookService.SetEnabled(t.Context(), webhook.StateCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: 5, Enabled: &disabled,
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
	reenabled := true
	reenabledWebhook, err := webhookService.SetEnabled(t.Context(), webhook.StateCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   createdWebhook.Integration.ID,
		ExpectedVersion: disabledWebhook.Integration.Version,
		Enabled:         &reenabled,
	})
	if err != nil || reenabledWebhook.Kind != webhook.StateUpdated ||
		!reenabledWebhook.Integration.Enabled {
		t.Fatalf("SetEnabled(Webhook true) = %#v, %v", reenabledWebhook, err)
	}

	maintenanceStartsAt := base.Add(time.Minute + 5*time.Second)
	maintenanceService := maintenance.NewService(
		database.Maintenance(), fixedClock{base.Add(30 * time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1260)}},
	)
	maintenanceResult, err := maintenanceService.Create(t.Context(), maintenance.CreateCommand{
		Scope: maintenance.Scope{
			OrganizationID: scope.OrganizationID,
			ProjectID:      scope.ProjectID,
			MonitorID:      scope.MonitorID,
		},
		StartsAt: maintenanceStartsAt,
		EndsAt:   base.Add(2 * time.Minute),
	})
	if err != nil || maintenanceResult.Kind != maintenance.CreateCreated {
		t.Fatalf("Create(event-time maintenance) = %#v, %v", maintenanceResult, err)
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
	maintenanceService = maintenance.NewService(
		database.Maintenance(),
		fixedClock{base.Add(time.Minute + 8*time.Second + 500*time.Millisecond)},
		&sequenceUUIDs{},
	)
	cancelledMaintenance, err := maintenanceService.Cancel(
		t.Context(), maintenanceResult.Window.Scope(), maintenanceResult.Window.ID,
	)
	if err != nil || cancelledMaintenance.Kind != maintenance.CancelCancelled ||
		cancelledMaintenance.Window.CancelledAt == nil {
		t.Fatalf("Cancel(event-time maintenance) = %#v, %v", cancelledMaintenance, err)
	}
	if resolved.State != incident.StateResolved || resolved.Version != 3 ||
		len(resolved.Timeline) != 3 || resolved.Timeline[2].ID != resolvedTimelineID ||
		resolved.Timeline[2].Counts == nil || resolved.Timeline[2].Counts.Passing != 1 {
		t.Fatalf("resolved Incident = %#v", resolved)
	}

	alertService = alert.NewService(
		database.Alerts(), fixedClock{base.Add(time.Minute + 9*time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1244), testUUID(1261)}},
	)
	if err := alertService.HandleIncidentTransition(
		t.Context(), resolvedAlertEventID, string(organizationValue.ID),
		loadOutboxPayload(t, database, resolvedAlertEventID),
	); err != nil {
		t.Fatalf("HandleIncidentTransition(resolved) error = %v", err)
	}
	replayService := alert.NewService(
		database.Alerts(), fixedClock{base.Add(time.Minute + 10*time.Second)},
		&sequenceUUIDs{values: []string{testUUID(1262)}},
	)
	if err := replayService.HandleIncidentTransition(
		t.Context(), resolvedAlertEventID, string(organizationValue.ID),
		loadOutboxPayload(t, database, resolvedAlertEventID),
	); err != nil {
		t.Fatalf("redelivered resolved Alert event error = %v", err)
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
	if deliveryCount != 2 {
		t.Fatalf("Webhook route count after maintained resolved Alert = %d", deliveryCount)
	}
	resolvedAudits, found, err := deliveryStore.ListAudit(
		t.Context(), webhook.DeliveryScope{
			OrganizationID: string(organizationValue.ID),
			ProjectID:      string(project.ID),
			MonitorID:      string(monitorValue.ID),
			AlertID:        testUUID(1244),
		},
	)
	if err != nil || !found || len(resolvedAudits) != 1 ||
		resolvedAudits[0].SuppressionReason != webhook.SuppressionReasonMaintenance ||
		resolvedAudits[0].MaintenanceWindowID != string(maintenanceResult.Window.ID) ||
		len(resolvedAudits[0].Attempts) != 0 {
		t.Fatalf("maintained Webhook delivery audit = %#v, found %v, error %v", resolvedAudits, found, err)
	}
	claimAt := base.Add(3 * time.Minute)
	claims, err = deliveryStore.Claim(
		t.Context(), testUUID(1263), claimAt,
		claimAt.Add(webhook.DeliveryLeaseDuration), webhook.DeliveryBatchSize,
	)
	if err != nil || len(claims) != 0 {
		t.Fatalf("Claim(suppressed Webhook delivery) = %#v, %v", claims, err)
	}
	var suppressedAttemptCount int
	if err := database.pool.QueryRow(t.Context(), `
SELECT count(*) FROM webhook_delivery_attempts WHERE alert_id=$1`, testUUID(1244)).Scan(
		&suppressedAttemptCount,
	); err != nil || suppressedAttemptCount != 0 {
		t.Fatalf("suppressed Webhook attempt count/error = %d/%v", suppressedAttemptCount, err)
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
