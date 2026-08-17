# Backend Contract

Status: working implementation specification for the unreleased pre-release candidate.

This document defines the observable backend behavior of the current candidate. It is
maintained with the v1 API, check validation, PostgreSQL adapters and migrations, API
tests, React client, Playwright journeys, and current architecture baseline. The web
application and browser journeys are contract
consumers and remain synchronized with this specification.

The architecture baseline remains normative. Where the current implementation
and this document differ, this document calls out the gap rather than turning it
into a compatibility promise.

## 1. Common HTTP and JSON Rules

- The versioned API root is `/api/v1`. There is no unversioned API contract.
- Requests and successful structured responses use JSON. Response property names
  are camelCase exactly as listed below.
- Identifiers are JSON strings containing UUIDs. New Organization, Project, User,
  Monitor, and Monitor Revision identifiers are time-ordered UUID version 7 values.
- Persisted and returned timestamps are UTC. They are JSON ISO 8601 strings; clients
  must not depend on a particular fractional-second precision.
- Request object property-name matching is case-insensitive and unknown envelope
  properties are ignored. Strict rejection applies only inside
  `checkConfiguration`, as specified in section 8.
- A missing or JSON `null` string field reaches use-case validation and produces the
  same field-level `400` response as an invalid string. A JSON value of the wrong
  primitive type or malformed JSON is a framework-level `400`.
- Unpaginated collections are returned as bare JSON arrays, not wrapper objects. An
  existing empty Project or Monitor has an empty array, not `null` and not `404`.
  Bounded Run and Incident collections use their documented page response objects.
- Canonical route parameters are UUID strings. A malformed UUID, non-decimal revision
  number, or revision number outside the positive signed 32-bit range does not match
  and returns `404`.
- All unsafe `/api/v1` methods are subject to authorization, Origin/Referer checking,
  and antiforgery in the ordering described in sections 4 and 5.

### Problem Details

Errors use `Content-Type: application/problem+json`. The ordinary shape is:

```json
{
  "type": "a standards problem type URI",
  "title": "Human-readable summary",
  "status": 409,
  "detail": "Human-readable detail"
}
```

Non-validation problems carry a stable `code` beside `title`:

```json
{
  "type": "a standards problem type URI",
  "title": "Invalid credentials",
  "status": 401,
  "code": "user.credentials.invalid",
  "detail": "Human-readable detail"
}
```

Validation errors use this extension, one coded entry per failure:

```json
{
  "type": "a standards problem type URI",
  "title": "One or more validation errors occurred.",
  "status": 400,
  "errors": {
    "field.path": [
      { "code": "organization.slug.invalid", "message": "One message, in validation order." }
    ]
  }
}
```

**Prose is not contract; codes are.** Every `title`, `detail`, and validation
`message` in this document is the current English text and may be reworded without a
compatibility event. Clients must never match on it. What is contract is the `code`:
its spelling, its meaning, the field path it appears under, and the accompanying HTTP
status. A code's meaning never changes once published; a new rule gets a new code, and
adding a code to an existing field is compatible. Clients localize from the code and
fall back to `message` for a code they do not recognize.

Clients treat `type`, `title`, `status`, `code`, `detail`, and `errors` as optional and
ignore unknown properties. The contract requires the media type, numeric status, the
codes listed in this document, and the validation error map. A server may add a
standards URI or request-id extension.

Bare authorization and routing statuses are also Problem Details and also carry codes:
unauthenticated is `401` `auth.unauthorized`, authenticated but unauthorized is `403`
`auth.forbidden`, missing resources are `404` `resource.notFound`, a rejected method is
`405` `request.methodNotAllowed`, and exhausted rate limits are `429`
`request.rateLimited`. A malformed body is `400` `request.malformed` and an unexpected
server failure is `500` `server.internalError`. Authentication never redirects to an
HTML login or access-denied page.

The remaining transport codes are `request.antiforgery.invalid` for a missing or invalid
antiforgery token and `request.origin.rejected` for a browser-origin mismatch.

## 2. Wire Types

All response fields below are required and non-null unless explicitly marked nullable.
Nullable fields are still present in the JSON object: `nextCursor`, `outcome`, `startedAt`,
`finishedAt`, `leaseExpiresAt`, `confirmation`, `http`, `tls`, `certificateExpiresAt`,
health evidence pointers, a health `candidate`, maintenance cancellation, Incident
acknowledgement/resolution pointers, and acknowledgement-only timeline evidence serialize
as JSON `null` when absent.

| Type | Exact JSON fields |
| --- | --- |
| `SetupStatusResponse` | `setupComplete: boolean` |
| `SetupResponse` | `user: UserResponse`, `organization: OrganizationResponse` |
| `AntiforgeryTokenResponse` | `headerName: string`, `requestToken: string` |
| `SessionResponse` | `userId: UUID string`, `email: string`, `displayName: string`, `role: string` |
| `UserResponse` | `id: UUID string`, `email: string`, `displayName: string`, `role: string`, `createdAt: UTC timestamp string` |
| `ProjectResponse` | `id: UUID string`, `organizationId: UUID string`, `name: string`, `isDefault: boolean`, `createdAt: UTC timestamp string` |
| `OrganizationResponse` | `id: UUID string`, `slug: string`, `displayName: string`, `createdAt: UTC timestamp string`, `defaultProject: ProjectResponse` |
| `OrganizationOverviewResponse` | `organizationId: UUID string`; nullable `monitors`, `health`, `incidents`, `integrations`, and `statusPage` summaries; `capabilities: OrganizationOverviewCapabilities` |
| `OrganizationOverviewMonitorCounts` | non-negative `total`, `draft`, `active`, `paused`, `archived` integers covering every Monitor |
| `OrganizationOverviewHealthCounts` | non-negative `notEvaluated`, `unknown`, `healthy`, `degraded`, and `down` integers covering Active Monitors only |
| `OrganizationOverviewIncidentSummary` | non-negative `active`, `open`, `acknowledged`; at most five `activePreview` rows; `activePreviewTruncated: boolean` |
| `OrganizationOverviewActiveIncident` | Incident, Project, and Monitor UUIDs; `monitorName`; active `state` `open` or `acknowledged`; `updatedAt: UTC timestamp string` |
| `OrganizationOverviewIntegrationCounts` | non-negative `total` and `enabled` integers; Administrator-only |
| `OrganizationOverviewStatusPageState` | `configured: boolean`, `published: boolean`; Administrator-only |
| `OrganizationOverviewCapabilities` | presentation hints `manageOrganization`, `manageIntegrations`, `manageStatusPage`; authorization remains server-side |
| `MonitorResponse` | `id`, `organizationId`, `projectId` as UUID strings; `name`, `checkType`, `state` as strings; `intervalSeconds`, `latestRevisionNumber` as integers; `createdAt`, `updatedAt` as UTC timestamp strings |
| `MonitorRevisionResponse` | `id`, `monitorId` as UUID strings; `revisionNumber: integer`; `checkType: string`; `checkSchemaVersion: integer`; `checkConfiguration: JSON value`; `createdAt: UTC timestamp string` |
| `MaintenanceWindowResponse` | `id`, `organizationId`, `projectId`, `monitorId` as UUID strings; `startsAt`, `endsAt`, `createdAt` as UTC timestamp strings; `status` exactly `upcoming`, `active`, `ended`, or `cancelled`; `cancelledAt: nullable UTC timestamp string` |
| `StatusPageDraftResponse` | `id`, `organizationId` as UUID strings; `title`; positive `version`; `components: StatusComponentResponse[]` in deterministic position order; `publication: nullable StatusPagePublicationResponse`; `createdAt`, `updatedAt` as UTC timestamp strings |
| `StatusComponentResponse` | component `id`, selected `monitorId` as distinct UUID strings; operator-chosen `label`; zero-based `position` |
| `StatusPagePublicationResponse` | `publishedAt: UTC timestamp string`; never a token or URL |
| `PublishStatusPageResponse` | one-time `publicUrl`; `publishedAt: UTC timestamp string` |
| `PublicStatusPageResponse` | `title`; `components: PublicStatusComponentResponse[]` in draft order |
| `PublicStatusComponentResponse` | operator `label`; `state` exactly `unknown`, `healthy`, `degraded`, or `down`; `updatedAt: UTC timestamp string`; `maintenance: boolean` |
| `MonitorHealthResponse` | scoped Organization, Project, and Monitor UUIDs; `state`, `stableState`, `policyVersion`; `version`; nullable source revision, Run, cohort, candidate, and determinate-finish pointers; `counts: HealthCountsResponse`; transition/update timestamps |
| `HealthCountsResponse` | `configured`, `eligible`, `responding`, `passing`, `failing`, `locationFault`, `indeterminate`, and `missing` non-negative integers |
| `RunPageResponse` | `items: RunResponse[]`; `nextCursor: nullable opaque string` |
| `RunResponse` | `id`, `organizationId`, `projectId`, `monitorId` as UUID strings; `revisionNumber: integer`; `location`, `kind` as strings; `scheduledFor: UTC timestamp string`; `outcome: nullable string`; `startedAt`, `finishedAt`, `leaseExpiresAt` as nullable UTC timestamp strings; `confirmation: nullable ConfirmationCauseResponse` |
| `ConfirmationCauseResponse` | candidate, triggering Run, and causation-event UUIDs; triggering slot timestamp; policy version |
| `ObservationResponse` | `runId`, `organizationId` as UUID strings; `scheduledFor: UTC timestamp string`; `failureCode`, `failureClass` as strings; `durationMicroseconds: integer`; `phases: ObservationPhasesResponse`; `http: nullable HTTPObservationResponse` |
| `ObservationPhasesResponse` | `connectMicroseconds`, `tlsMicroseconds`, `firstByteMicroseconds` as integers |
| `HTTPObservationResponse` | `statusCode`, `redirectCount`, `bodyBytes` as integers; `protocol: string`; `bodyTruncated: boolean`; `tls: nullable TLSObservationResponse` |
| `TLSObservationResponse` | `version`, `cipherSuite` as strings; `certificateExpiresAt: nullable UTC timestamp string` |
| `IncidentPageResponse` | `items: IncidentResponse[]`; `nextCursor: nullable opaque string` |
| `IncidentResponse` | scoped UUIDs; lifecycle `state` and `version`; opening transition; nullable acknowledgement and resolution identity/timestamps; created/updated timestamps; `timeline: IncidentTimelineResponse[]` |
| `IncidentTimelineResponse` | timeline UUID, Incident version and kind; nullable health transition, actor, health states, policy, causal Run, slot, and counts; occurrence timestamp |
| `AlertPageResponse` | `items: AlertResponse[]`; `nextCursor: nullable opaque string` |
| `AlertResponse` | Alert, Organization, Project, Monitor, and source Incident UUIDs; positive source Incident version; kind `incident.opened` or `incident.resolved`; source occurrence and projection creation timestamps |
| `AlertDeliveryPageResponse` | `items: AlertDeliveryResponse[]`, at most five point-in-time routes |
| `AlertDeliveryResponse` | stable delivery and Integration UUIDs; channel `webhook`; positive Integration and signing-secret versions; route timestamp; `attempts: DeliveryAttemptResponse[]` ordered by sequence and limited to five |
| `DeliveryAttemptResponse` | positive sequence; start and nullable finish timestamps; outcome `inProgress`, `succeeded`, `failed`, or `cancelled`; nullable HTTP status and stable allowlisted failure code |
| `WebhookIntegrationResponse` | Integration and Organization UUIDs; name; Administrator-visible HTTPS destination URL; `enabled`; positive Integration and active-secret versions; nullable pending- and retiring-secret versions; creation/update timestamps |
| `CreateWebhookIntegrationResponse` | `integration: WebhookIntegrationResponse`; `signingSecret: string` returned exactly once |
| `PrepareWebhookSigningSecretResponse` | `integration: WebhookIntegrationResponse`; positive `secretVersion`; `signingSecret: string` returned exactly once |

Request shapes are:

| Type | Exact JSON fields |
| --- | --- |
| `CreateFirstAdministratorRequest` | `email`, `displayName`, `password` (nullable strings at decoding boundary; required by validation) |
| `LoginRequest` | `email`, `password` (nullable strings at decoding boundary) |
| `CreateOrganizationRequest` | `slug`, `displayName` (nullable strings at decoding boundary; required by validation) |
| `CreateWebhookIntegrationRequest` | `name`, `destinationUrl` (nullable strings at decoding boundary; required by validation) |
| `WebhookIntegrationStateRequest` | `enabled: boolean`, `version: positive integer` for optimistic concurrency |
| `WebhookIntegrationVersionRequest` | `version: positive integer`, the current Integration version for optimistic concurrency |
| `CreateMonitorRequest` | `name`, `checkType` strings |
| `RenameOrganizationRequest` | `displayName` (nullable string at decoding boundary; required by validation) |
| `RenameMonitorRequest` | `name` string |
| `ChangeMonitorStateRequest` | `state` string |
| `ChangeMonitorIntervalRequest` | `intervalSeconds: integer` |
| `CreateMonitorRevisionRequest` | `checkSchemaVersion: integer`, `checkConfiguration: JSON value` |
| `CreateMaintenanceWindowRequest` | `startsAt`, `endsAt` (nullable timestamp strings at decoding boundary; required by validation and restricted to an explicit zero UTC offset) |
| `ReplaceStatusPageDraftRequest` | `title` nullable string at decoding boundary; `version` non-negative integer (`0` creates); `components: ReplaceStatusComponentInput[]` in desired order |
| `ReplaceStatusComponentInput` | `monitorId`, `label` nullable strings at decoding boundary; required by validation |

Current Organization role strings are exactly `Administrator` and `Viewer`. Monitor state strings are
exactly `draft`, `active`, `paused`, and `archived`. The only supported check type is
exactly lowercase `http`.

## 3. Endpoint Matrix

`Anonymous` means no session is required. `Authenticated` means any valid session.
`Instance admin` means a session carrying the instance role `Administrator`.
A permission such as `monitor.write` means the caller must be a member of the
Organization in the route whose role carries that permission; a non-member gets `404`
and a member with an insufficient role gets `403` (section 4). `Unsafe` means the
antiforgery and origin rules in section 5 also apply.

| Method and path | Access | Success | Other application results |
| --- | --- | --- | --- |
| `GET /api/v1/setup/status` | Anonymous | `200 SetupStatusResponse` | none |
| `POST /api/v1/setup/admin` | Anonymous, unsafe, credential rate limit | `201 SetupResponse`, provisions the installation Organization, signs in and sets a new session cookie; no `Location` | `400` validation; `409` completed; `429` |
| `GET /api/v1/auth/antiforgery` | Anonymous | `200 AntiforgeryTokenResponse`, stores antiforgery cookie | none |
| `POST /api/v1/auth/login` | Anonymous, unsafe, credential rate limit | `200 SessionResponse`, sets a fresh session cookie | `401` generic invalid credentials; `429` |
| `POST /api/v1/auth/logout` | Authenticated, unsafe | `204` empty body, invalidates session and expires cookie | `401` |
| `GET /api/v1/auth/session` | Authenticated | `200 SessionResponse` | `401` |
| `GET /api/v1/organizations` | Authenticated | `200 OrganizationResponse[]` of the caller's memberships in creation order, UUID as tie-breaker; `[]` when none | `401` |
| `POST /api/v1/organizations` | Instance admin, unsafe | first create: `201 OrganizationResponse` and `Location: /api/v1/organizations/{id}`; identical replay: `200 OrganizationResponse` without creating state | `400`, `409`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}` | `organization.read` | `200 OrganizationResponse` | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/overview` | `organization.read` plus permission-aware summaries | `200 OrganizationOverviewResponse` | `404`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/name` | `organization.write`, unsafe | `200 OrganizationResponse` with the new display name and an unchanged slug | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/webhook-integrations` | `integration.manage` | `200 WebhookIntegrationResponse[]` in creation order; never returns signing-secret material | `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/webhook-integrations` | `integration.manage`, unsafe | `201 CreateWebhookIntegrationResponse`; creates a disabled Integration and returns its signing secret once | `400`, `404`, `409`, `503` without an operator keyring, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/state` | `integration.manage`, unsafe | `200 WebhookIntegrationResponse`; enables or disables future Alert routing, with no version bump for an already-current desired state | `400`, `404`, `409` for stale version or the five-enabled limit, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/prepare` | `integration.manage`, unsafe | `201 PrepareWebhookSigningSecretResponse`; keeps the current secret active and returns the pending secret once | `400`, `404`, `409`, `503` without an operator keyring, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/activate` | `integration.manage`, unsafe | `200 WebhookIntegrationResponse`; activates the pending secret and marks the former active secret retiring | `400`, `404`, `409`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/retire` | `integration.manage`, unsafe | `200 WebhookIntegrationResponse`; clears the retiring secret's ciphertext and retains audit metadata | `400`, `404`, `409` while unfinished deliveries still reference the secret, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | `monitor.write`, unsafe | `201 MonitorResponse` and canonical monitor `Location` | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/status-page/draft` | `statusPage.write` | `200 StatusPageDraftResponse`; `204` before configuration exists | `404`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/status-page/draft` | `statusPage.write`, unsafe | `200 StatusPageDraftResponse`; whole-draft replacement keeps array order | `400` invalid or unavailable Monitor; `409` stale version; `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/status-page/publication` | `statusPage.write`, unsafe | `201 PublishStatusPageResponse`; publishes the existing draft and reveals the URL once | `409` no draft or already published; `404`, `401`, `403` |
| `DELETE /api/v1/organizations/{organizationId}/status-page/publication` | `statusPage.write`, unsafe | `204`; idempotently revokes anonymous access | `404`, `401`, `403` |
| `GET /api/v1/status-pages/{publicationToken}` | Anonymous, public-status rate limit | `200 PublicStatusPageResponse`; `Cache-Control: no-store` | byte-identical `404` for invalid, missing, or revoked token; `429` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | `monitor.read` | `200 MonitorResponse[]` in creation order, UUID as tie-breaker | `404` if the Project is not in the Organization; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}` | `monitor.read` | `200 MonitorResponse` | `404`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name` | `monitor.write`, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state` | `monitor.write`, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/interval` | `monitor.write`, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | `monitor.write`, unsafe | `201 MonitorRevisionResponse` and canonical revision `Location` | `400`, `404`, `409`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | `monitor.read` | `200 MonitorRevisionResponse[]` in ascending revision number | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}` | `monitor.read` | `200 MonitorRevisionResponse` | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/maintenance-windows` | `maintenance.read` | `200 MaintenanceWindowResponse[]` for current, upcoming, and cancelled-but-not-ended windows in ascending `(startsAt, id)` order | `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/maintenance-windows` | `maintenance.write`, unsafe | `201 MaintenanceWindowResponse` and canonical maintenance-window `Location` | `400`, `404`; `409` on overlap; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/maintenance-windows/{maintenanceWindowId}` | `maintenance.read` | `200 MaintenanceWindowResponse` | `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/maintenance-windows/{maintenanceWindowId}/cancel` | `maintenance.write`, unsafe | `200 MaintenanceWindowResponse`; repeat cancellation is idempotent | `404`; `409` after the window ended or on an unresolved concurrent update; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/health` | `monitor.read` | `200 MonitorHealthResponse` | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents` | `incident.read` | `200 IncidentPageResponse`, newest first by `(createdAt, id)` | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/alerts` | `alert.read` | `200 AlertPageResponse`, newest first by `(occurredAt, id)` | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/alerts/{alertId}/deliveries` | `alert.read` | `200 AlertDeliveryPageResponse` with point-in-time routes and append-only attempts | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents/{incidentId}` | `incident.read` | `200 IncidentResponse` with timeline | `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents/{incidentId}/acknowledge` | `incident.write`, unsafe | `200 IncidentResponse`; repeat acknowledgement is idempotent | `400`, `404`, `409` when resolved, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs` | `monitor.read` | `200 RunPageResponse`, newest first by `(scheduledFor, id)` | `400`, `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs` | `monitor.write`, unsafe | `201 RunResponse` for the completed manual Run and canonical Run `Location` | `400`, `404`; `409` without a revision; `429` when execution capacity is occupied; `503` on an API-only process; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs/{runId}` | `monitor.read` | `200 RunResponse` | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs/{runId}/observation` | `monitor.read` | `200 ObservationResponse` for a completed, non-skipped Run | `404`, `401`, `403` |

The canonical revision `Location` ends in `/revisions/{revisionNumber}`. All monitor
and Run lookups include Organization, Project, and Monitor scope. A real identifier
presented through the wrong Organization, Project, or Monitor is indistinguishable from
an unknown one and returns `404`.

Manual Run creation executes the latest immutable revision through the same outbound policy,
execution ceiling, Probe Location, Observation bounds, transactional outbox, and shared
concurrency limit as scheduled and confirmation Runs. It is synchronous: `201` means the Run,
Observation, and `run.recorded.v1` entry committed together. An explicit request may exercise
a draft or paused Monitor, but a Monitor without any revision returns `409`. Capacity is not
queued, so the caller can retry a `429` deliberately.

Development alone exposes anonymous `GET /openapi/v1.json`. There is no OpenAPI UI.

## 4. Authentication, Authorization, and Rate Limiting

Authorization is deny by default for every endpoint. Explicit anonymous exceptions
are `/healthz`, `/readyz`, development OpenAPI, setup status, setup admin, login,
antiforgery issuance, and an opaque `/api/v1/status-pages/{publicationToken}` read.
Logout, session, and the Organization list require authentication.
Creating an Organization requires the instance `Administrator` role.

Every endpoint under `/api/v1/organizations/{organizationId}/` resolves the caller's
membership of that Organization and checks a permission against its role:

- a caller with no membership gets `404`, byte-identical to the response for an
  Organization that does not exist, so membership is not disclosed;
- a member whose role lacks the permission gets `403`;
- the instance `Administrator` role grants **no** implicit access to the monitoring data
  of an Organization it is not a member of.

Organization roles and the permissions they carry are `Administrator` (every permission,
including ones added in later releases) and `Viewer` (every read permission). The
permissions in use are `organization.read`, `organization.write`, `monitor.read`,
`monitor.write`, `incident.read`, `incident.write`, `alert.read`, and
`maintenance.read`, `maintenance.write`, `integration.manage`, and `statusPage.write`.
The last two permissions deliberately do not end in `.read` because Webhook destinations
and private status configuration are Administrator-only rather than Viewer evidence.
The permission catalog itself is internal and not published; endpoints document the
permission they require. Provisioning makes the creator an `Administrator` member of the
new Organization in the same transaction, so no Organization exists without a member.

Endpoint authorization runs before the endpoint antiforgery filter. Consequently an
anonymous unsafe request to a protected endpoint returns `401` even when it has no
antiforgery token. On anonymous setup and login, antiforgery is evaluated before the
handler. Origin rejection is evaluated before antiforgery rejection.

Setup admin and login share one fixed-window limiter partitioned by the transport
peer IP string (or `unknown` when absent):

- default 10 permits per one-minute window;
- configurable as `PROBEHIVE_CREDENTIAL_ATTEMPTS_PER_MINUTE`;
- no queue, so the next attempt receives `429` immediately;
- the limit is shared by setup and login for one address;
- it applies to attempts reaching those endpoints, not only failed credentials.

Anonymous status reads use an independent fixed-window limiter with the same bounded
partition behavior and a default allowance of 120 reads per transport address per minute.
Invalid, missing, and revoked publication capabilities all use the same hidden `404`.

Proxy-derived client IP is not trusted until a separately reviewed forwarded-client
deployment profile exists.

Login normalizes the supplied email as described in section 6. Invalid email, empty
password, unknown email, and incorrect password all return the identical response:

- status `401`;
- title `Invalid credentials`;
- detail `The email and password combination did not match a local account.`

An unknown email must still perform one password-hash verification against a dummy
hash to reduce account-enumeration timing differences. A successful verification may
rehash and atomically replace an outdated password hash.

## 5. Cookies, Sessions, Antiforgery, and Origin

### Session cookie and server-side record

The externally visible session cookie is host-only `probehive.session`:

- `HttpOnly`;
- `SameSite=Lax`;
- `Path=/`;
- `Secure` unconditionally outside Development; secure-when-HTTPS in Development;
- fixed 12-hour server-side lifetime;
- no sliding renewal and no redirects.

Do not set a `Domain` attribute. Login and successful first-administrator setup
issue a cryptographically random token of at least 256 bits; only a cryptographic hash
is stored in PostgreSQL. A session row is
bound to one `users.id`, records a fixed `expires_at`, and is rejected after expiry.
Reading a session never extends it. Logout deletes the matching server-side session
and emits an expired `probehive.session` cookie with the same path/security profile.
Never return, log, or persist the raw token.

The session migration enforces a unique token hash, a foreign key to `users(id)`
with a deliberate delete action, and an expiry lookup and cleanup index.

### Antiforgery flow

`GET /api/v1/auth/antiforgery` returns:

```json
{
  "headerName": "X-ProbeHive-Antiforgery",
  "requestToken": "opaque value"
}
```

It also sets host-only `probehive.antiforgery`; the current cookie is `HttpOnly`,
`SameSite=Strict`, `Path=/`, and uses the same environment-dependent Secure policy.
Every unsafe `/api/v1` request must send both the cookie and
the opaque request token in the response-named header, including anonymous login and
setup. Safe methods are `GET`, `HEAD`, `OPTIONS`, and `TRACE`; other methods are unsafe.

The token is a synchronizer token bound to the current authenticated session identity.
The anonymous pre-login/setup flow must also work: fetch a token as an anonymous
browser, submit setup or login, then fetch a new token after the identity changes.
The React client also refreshes after logout. A missing, malformed, wrong-cookie, or
wrong-identity token returns:

- status `400`;
- title `Antiforgery token missing or invalid`;
- detail `Unsafe requests require the antiforgery request token in the custom header; obtain it from GET /api/v1/auth/antiforgery.`

Authenticated antiforgery records and anonymous pre-authentication MAC key material use
server-side state. Session-bound records store only hashes and are unique by session.
Anonymous tokens use a timestamped random selector and an HMAC-SHA-256 request token
under one PostgreSQL-backed key, so issuance consumes constant database space. The
design works across restarts and replicas without storing raw tokens. A previously
observed anonymous token is never considered when an authenticated principal exists.

### Origin and Referer

For an unsafe request, the expected browser origin is `PROBEHIVE_PUBLIC_ORIGIN` when
that setting is configured; otherwise it is
`{request scheme}://{request Host including port}`.

1. If a non-empty `Origin` is present, compare its complete value case-insensitively
   with the expected browser origin. It must have no path or trailing slash. `Origin:
   null`, multiple origins, and every mismatch fail. Origin takes precedence over
   Referer when both exist.
2. Otherwise, if a non-empty `Referer` is present, it must be an absolute URI and its
   scheme plus authority must match the expected browser origin case-insensitively.
   Its path, query, and fragment do not participate in the comparison.
3. If neither header exists, treat the caller as a non-browser client and allow this
   check; antiforgery is still mandatory.

A failure returns status `403`, title `Browser origin rejected`, and detail
`The Origin or Referer of this request does not match the expected browser origin.`

No CORS middleware or cross-origin credential profile is configured. Browser
deployment is same-origin (the development Vite server uses a proxy). Do not add
cross-origin response headers or treat `OPTIONS` as an authorization bypass; a future
cross-origin profile requires a separate security decision.

## 6. Setup, Users, and Organizations

`GET /api/v1/setup/status` returns `setupComplete: false` exactly while there are zero users,
and `true` once any user exists.

First-administrator validation is:

- `email`: trim, invariant-lowercase, length 1-254, exactly one `@` with non-empty
  sides, and no Unicode whitespace or control character. Failure key `email`, message
  `An email address contains one '@' with non-empty sides, no whitespace, and at most 254 characters.`
- `displayName`: trim Unicode surrounding whitespace, then 1-100 UTF-16 code units.
  Failure key `displayName`, message
  `A display name is 1 to 100 characters after trimming.`
- `password`: 12-128 UTF-16 code units with no trimming or normalization. Failure key
  `password`, message `A password is 12 to 128 characters.`

A successful user has role `Administrator`, the normalized email, trimmed display
name, and one UTC creation instant. Once a user exists, setup returns `409` with title
`Setup already completed` and detail
`The instance already has at least one user; sign in instead.`

Setup then provisions the installation Organization through the same idempotent use case
as `POST /api/v1/organizations`, with slug exactly `default` and display name exactly
`Default`. Setup accepts no Organization fields. The two writes are not one transaction
and run in this order: create the administrator, provision the Organization, issue the
session cookie. If provisioning fails the response is `500`, the administrator exists,
and no session was issued, so the operator signs in and creates an Organization manually
as before. `SetupResponse` carries both the created user and that Organization.

Organization provisioning rules are:

- `slug`: 3-63 lowercase ASCII letters/digits and single interior hyphens, beginning
  and ending with a letter or digit. It is not trimmed or case-normalized. Failure key
  `slug`, message `A slug is 3 to 63 characters of lowercase ASCII letters and digits with single interior hyphens, starting and ending with a letter or digit.`
- `displayName`: trim Unicode surrounding whitespace, then 1-100 UTF-16 code units.
  Failure key `displayName`, message
  `A display name is 1 to 100 characters after trimming.`
- first creation atomically creates an Organization and its default Project using the
  same UUIDv7 timestamp. The Project name is exactly `Default`, `isDefault` is true,
  and its `organizationId` is the new Organization id.
- slug is the idempotency key. An existing slug plus identical trimmed display name
  returns the existing rows with `200` and performs no write. No idempotency header is
  involved.
- an existing slug plus a different trimmed display name returns `409`, title
  `Organization slug already in use`, detail
  `An Organization with slug '{slug}' already exists with a different display name.`
- a uniqueness race re-reads the database winner and applies the same replay/conflict
  rule. Organization and default Project are always inserted in one transaction.

Renaming changes only the display name; the slug is immutable because it is the
idempotency key. `PUT /api/v1/organizations/{organizationId}/name` applies the same
`displayName` rule as provisioning and is last-write-wins with no version token.

Rename moves the replay boundary, which callers must expect: after a rename, provisioning
the same slug with the pre-rename display name returns `409`, and the current display name
is what replays with `200`.

### Organization Operational Overview

`GET /api/v1/organizations/{organizationId}/overview` requires `organization.read`.
The response is produced from one PostgreSQL repeatable-read, read-only transaction; it
does not aggregate the unbounded Monitor list or issue per-Monitor requests.

Monitor lifecycle counts cover every Monitor in the Organization. Health counts cover
Active Monitors only: an Active Monitor without a health row contributes to
`notEvaluated`, while a stored `unknown` health state remains `unknown`. Active Incident
counts cover the Organization, and `activePreview` contains at most five active rows
ordered by `(updatedAt, id)` descending. `activePreviewTruncated` reports omitted rows.

The monitor/health and Incident summaries are JSON `null` when the caller lacks the
corresponding read permission. Integration counts and private status-page state are
JSON `null` unless the caller has `integration.manage` or `statusPage.write`.
`capabilities` contains presentation hints only and never replaces authoritative
authorization. All returned instants are UTC.

### Signed Webhook Integrations

A Webhook Integration is Organization-scoped Administrator configuration. Its normalized
name is trimmed Unicode text containing 1-100 UTF-16 code units. Names are unique by exact
stored string within one Organization; comparison is case-sensitive. The destination is an
absolute HTTPS URL of at most 2,048 bytes with an authority and without user information,
a query string, or a fragment. Creation validates URL syntax only: this slice performs no
DNS lookup, connection, redirect, or delivery.

Creation is transactional and always produces a disabled Integration at version 1 with
active secret version 1. It generates 32 random bytes, exposes the complete signing key as
`phwh_` followed by unpadded base64url, and returns that value only in the successful `201`
response, which carries `Cache-Control: no-store`. Ordinary list responses expose the active
secret version plus nullable pending and retiring version numbers so clients can recover
rotation state after a reload. They never contain a signing secret, ciphertext, nonce, or
wrapping-key identifier. PostgreSQL stores only the AES-256-GCM envelope; associated data
binds the Organization id, Integration id, and secret version. A name conflict creates
neither the Integration nor its secret.

`GET` returns Integrations for exactly the authorized Organization in `(createdAt, id)`
order. Both `GET` and `POST` require `integration.manage`, so a Viewer receives `403` and a
non-member receives the ordinary hidden-scope `404`. The destination URL is deliberately
visible to Administrators through this configuration endpoint.

State changes use `PUT .../{integrationId}/state` with required `enabled` and current
positive `version` fields. A stale version returns `409 webhook.version.conflict`.
Repeating the current state with the current version is idempotent and does not increment
the version. An Organization may have at most five enabled Integrations; enabling a sixth
returns `409 webhook.enabledLimit.exceeded`. State changes are serialized per Organization
so concurrent enables cannot exceed the limit.

Projecting a new `incident.opened` or `incident.resolved` Alert creates one immutable
`webhook_deliveries` route for every Integration enabled at that instant, in the same
transaction as the Alert and processed-outbox marker. The route has a stable UUIDv7 delivery
id and snapshots the Integration version and active secret version. Enabling does not
backfill older Alerts, and disabling does not remove existing routes. A route is not a
Delivery Attempt and makes no sent or delivered claim.

Routing evaluates maintenance at the Alert's immutable `occurredAt`, not at delayed
projection or retry time. A retained window matches only when it was created no later than
that instant, its half-open bounds contain the instant, and it was either not cancelled or
cancelled later. Each matching route is inserted as terminal with `suppressionReason`
`maintenance`, the `maintenanceWindowId`, and zero Delivery Attempts, so the dispatcher
cannot claim it. Cancellation after `occurredAt`, restarts, and replay of the processed
Incident event do not change the stored result.

The operator keyring is required for creation and rotation preparation. Without it, an
otherwise valid request returns `503` with `webhook.keyring.unavailable`. At process
startup every non-retired secret must authenticate using a retained wrapping key; old-key
envelopes are rewrapped under the first configured key. Missing keys, malformed or
unauthenticated ciphertext, and storage failures stop startup rather than exposing a
partially usable Webhook surface. Running without a keyring is allowed only while the
database has no retained Webhook secret.

Every rotation action carries the current positive Integration `version`. A stale value
returns `409 webhook.version.conflict`; each successful action increments the version and
updates `updatedAt` in the same transaction as its secret-state change. Preparation is
allowed only when the Integration has neither a pending nor a retiring secret. It creates
the next secret version in `pending` state, leaves the current secret active, and returns
the new encoded secret exactly once in a `Cache-Control: no-store` response. Another
prepare before the rotation finishes returns `409 webhook.rotation.inProgress`. The
nullable `pendingSecretVersion` and `retiringSecretVersion` metadata report the durable
phase after a retry or reload without redisclosing secret material.

Activation requires the single pending secret. It moves the former active secret to
`retiring`, makes the pending secret `active`, records its activation time, and advances
`activeSecretVersion`; without a pending secret it returns
`409 webhook.rotation.pendingMissing`. Retirement requires the single retiring secret. It
sets that row to `retired`, clears its wrapping-key id, nonce, and ciphertext, records its
retirement time, and retains the bounded Organization, Integration, secret-version, and
creation/activation audit metadata. An unfinished route that snapshots the retiring secret
version blocks retirement with `409 webhook.rotation.retiringInUse`; the ciphertext remains
available until those deliveries finish. Without a retiring secret it returns
`409 webhook.rotation.retiringMissing`.

The embedded delivery dispatcher uses the same policy-enforcing outbound Dialer as HTTP
checks. It revalidates the normalized destination before use, accepts only HTTPS, binds every
connection to a policy-approved address, uses host roots, verifies TLS 1.2 or newer, follows
no redirects, allows at most four calls per process, limits response headers to 16 KiB, and
applies a ten-second total timeout. It never reads or retains a response body.

Before every external call, the dispatcher commits a durable `inProgress` Delivery Attempt
and a 30-second lease. A lost or expired lease finalizes that attempt as `failed` with
`webhook.delivery.outcome.uncertain`; a new worker may then use the same stable delivery id
with the next sequence. Any `2xx` succeeds. Network errors, timeouts, `408`, `425`, `429`, and
`5xx` retry with the outbox bounded exponential delay and jitter. Other `3xx` and `4xx`
responses fail terminally. A delivery makes at most five real calls. Shutdown cancellation
is recorded as `cancelled` and remains eligible for retry unless it was the fifth call.

Every attempt constructs this deterministic version-1 JSON object in the shown property
order; the body is bounded to 16 KiB:

```json
{
  "schemaVersion": "v1",
  "deliveryId": "<stable UUID>",
  "alertId": "<Alert UUID>",
  "alertKind": "incident.opened",
  "organizationId": "<Organization UUID>",
  "projectId": "<Project UUID>",
  "monitorId": "<Monitor UUID>",
  "incidentId": "<Incident UUID>",
  "incidentVersion": 1,
  "occurredAt": "<UTC timestamp>",
  "attempt": 1
}
```

The HMAC-SHA256 input is the exact UTF-8 sequence below, including newlines and the raw JSON
body. The key is the complete `phwh_...` signing-secret string; the signature is unpadded
base64url:

```text
v1\n<delivery-id>\n<unix-timestamp>\n<attempt>\n<secret-version>\n<raw-json-body>
```

The request uses `Content-Type: application/json`, `User-Agent: ProbeHive-Webhook/1`, and
these versioned signing headers:

```text
ProbeHive-Webhook-Version: v1
ProbeHive-Delivery-Id: <stable delivery UUID>
ProbeHive-Timestamp: <Unix seconds>
ProbeHive-Attempt: <positive sequence>
ProbeHive-Secret-Version: <positive version>
ProbeHive-Signature: <unpadded base64url HMAC-SHA256>
```

`GET .../alerts/{alertId}/deliveries` carries Organization, Project, Monitor, and Alert
scope through the authorization and storage query. Viewers with `alert.read` may use it;
wrong-tenant or wrong-parent identities return the ordinary hidden `404`. The response shows
stable route identity, versions, timestamps, outcomes, HTTP status when present, and stable
failure codes. It never contains the destination URL, signing material, ciphertext, response
body, arbitrary headers, or provider text. The React Alert history fetches this evidence only
when a row is expanded.

## 7. Monitor and Revision Semantics

Monitor creation validates all fields before checking Project existence:

- `name`: trimmed Unicode text, 1-100 UTF-16 code units; failure key `name`, message
  `A Monitor name is 1 to 100 characters after trimming.`
- `checkType`: 1-50 characters, starts with a lowercase ASCII letter, then lowercase
  ASCII letters/digits or single interior hyphens, and ends in a letter/digit. Format
  failure key `checkType`, message
  `A check type is 1 to 50 characters of lowercase ASCII letters and digits with single interior hyphens, starting with a letter.`
- a well-formed but unsupported value fails under `checkType` with
  `The check type '{checkType}' is not supported by this build.`

A Monitor starts with `state: "draft"`, `latestRevisionNumber: 0`, and identical
`createdAt`/`updatedAt`. Check type and owner never change. Names are not unique.

The state endpoint accepts exactly lowercase `active`, `paused`, or `archived`.
Anything else, including `draft`, null, or different casing, is `400` under key `state`
with `The target state must be one of: active, paused, archived.` Valid transitions:

- draft -> active only after at least one revision;
- paused -> active;
- active -> paused;
- draft, active, or paused -> archived;
- archived is terminal and read-only.

A valid target rejected by the state machine is `409`, title
`Monitor state transition rejected`. Exact details include:

- activation without revision: `A Monitor cannot be activated before it has a revision.`
- any archived mutation: `An archived Monitor is read-only.`
- other invalid transition: `A Monitor cannot move from '{CurrentDisplayName}' to '{TargetDisplayName}'.`

Rename is allowed in every non-archived state and does not change state. Rename conflict
title is `Monitor rename rejected`. A successful rename or state change advances
`updatedAt`.

Revisions are append-only; no update or delete endpoint exists. They start at 1 and
increase by exactly one per Monitor. A revision copies Organization id and the Monitor's
fixed check type, stores the declared schema version and validated configuration, and
advances the Monitor's `latestRevisionNumber` and `updatedAt` in the same transaction.
Adding a revision never changes lifecycle state and is allowed while draft, active, or
paused. Revision conflict title is `Monitor revision rejected`.

Every lost optimistic-concurrency race for rename, state change, or revision creation
returns `409` with detail
`The Monitor was modified concurrently; retry against its current state.` The client
must GET fresh state and retry its operation. There is no ETag or client-supplied row
version. The server must never silently reorder or renumber revisions.

### Maintenance windows

Maintenance windows are one-time, Monitor-scoped half-open intervals `[startsAt, endsAt)`.
Creation requires explicit zero-offset UTC instants, rejects a start before the server
clock, requires the end after the start, and caps duration at 30 days. Invalid bounds use
the `startsAt` or `endsAt` validation paths and the stable maintenance codes in section 13.

Two non-cancelled windows for one Monitor cannot overlap. Adjacency is valid because the
end instant is excluded. A conflicting create returns `409` `maintenance.overlap`.
Cancellation records `cancelledAt` without deleting the interval and is idempotent. It is
allowed while the window has not ended; an ended window returns `409`
`maintenance.window.ended`. A lost cancellation update returns `409`
`maintenance.concurrentUpdate` unless the competing update already cancelled the window,
in which case the operation succeeds with the retained cancellation.

Response status is evaluated against the server clock: `upcoming` before `startsAt`,
`active` from `startsAt` through but excluding `endsAt`, `ended` at or after `endsAt`, and
`cancelled` whenever cancellation is recorded. The collection retains cancelled windows
until their original end and omits every ended interval. Cancelling a window releases its
time range for a replacement. Item lookup can still return an ended interval by id.

Every create, list, get, and cancel operation carries Organization, Project, and Monitor
scope. Wrong-tenant and wrong-parent identities return the ordinary hidden `404`. Viewers
can inspect windows through `maintenance.read`; only Administrators can schedule or cancel
through `maintenance.write`.

Maintenance does not pause checks or rewrite, suppress, or hide Runs, Observations,
evaluated health, Incidents, or Alerts. Webhook routing overlays that evidence at the Alert
occurrence instant: an applicable route retains the explicit window attribution as terminal
suppression evidence and creates no Delivery Attempt.

### Status-page drafts and publication

An Organization has at most one private status-page draft. Creating it uses request
version `0`; updates use the current positive version and return `409
statusPage.concurrentUpdate` when stale. A draft contains a 1-to-100-UTF-16-unit title
and one through fifty explicitly selected components. Array order is presentation order.

Each component has its own stable UUID, one Organization-scoped Monitor reference, and an
operator-chosen 1-to-100-UTF-16-unit label. Keeping component and Monitor identities
distinct allows public naming and order without renaming monitoring configuration. A
Monitor appears at most once. An archived, cross-Organization, missing, or concurrently
archived Monitor fails the whole replacement as `400
statusPage.component.monitorUnavailable`; the response does not distinguish these cases.
Unchanged Monitor selections keep their component identity across replacement.

The draft response and configuration rows contain no Monitor target, revision, Run,
Observation, health detail, Incident, Alert, Integration, member, or secret. Both `GET`
and `PUT` require `statusPage.write`, so Viewers receive `403` and non-members receive the
normal non-disclosing `404`.

Publishing requires the same permission, origin validation, and antiforgery. It creates a
32-byte random base64url path capability, persists only its SHA-256 digest and publication
instant, and returns the complete anonymous URL once. Authenticated draft reads expose
only `publishedAt`; an operator rotates a lost URL by revoking and publishing again.
Revocation clears the digest idempotently, so the old capability stops resolving
immediately without deleting the draft.

Anonymous reads compare the supplied token digest and return only the page title plus
each selected component's operator label, current evaluated state, state update instant,
and whether a non-cancelled maintenance window is active at the read instant. A Monitor
that is not active presents `unknown` and no maintenance. Component order is the private
draft order. Responses are `no-store`, independently rate limited, and contain no tenant,
Monitor, component, target, evidence, causal-link, configuration, membership, Integration,
secret, or token identity. They describe only current state and make no uptime, history,
delivery, Incident-detail, or SLO claim.

### Run and Observation queries

Run history is a bounded, newest-first keyset query ordered by descending `scheduledFor`,
with descending Run `id` as its tie-breaker. `notBefore` is required exactly once and is
an inclusive RFC 3339 lower bound;
requiring it lets PostgreSQL prune retained monthly partitions. `pageSize` is optional,
defaults to 50, and is an integer from 1 through 500. `cursor`, `outcome`, `kind`, and
`location` are optional and each may appear at most once with a non-empty value.

`outcome` is exactly one of `passed`, `failed`, `errored`, `timedout`, `cancelled`, or
`skipped`. `kind` is exactly one of `scheduled`, `confirmation`, or `manual`. `location`
is 1-63 UTF-8 bytes. A page always returns `items` as an array; an empty page is `[]`.
`nextCursor` is `null` on the final page and otherwise is an opaque exclusive boundary
that the client sends back unchanged with the same lower bound and filters.

A Run id is the external identifier; callers do not supply the internal partition key.
`outcome` is null and only `leaseExpiresAt` is present while the Run is in flight.
`startedAt` and `finishedAt` are present only after a completed execution. A skipped Run
has no execution or lease instants. The secret lease-holder token is never part of a
response.

An Observation exists only for a completed, non-skipped Run. Querying one for an
A confirmation Run carries `confirmation` with the candidate id, triggering Run id and
slot, causation outbox event id, and policy version. Every scheduled or manual Run carries
`confirmation: null`. Confirmation remains a normal visible Run with `kind: "confirmation"`;
it never replaces or mutates its triggering Run.

in-flight or skipped Run returns `404`. Completion persists the Run and Observation in
one transaction, so a completed execution missing its Observation is an internal
integrity error, not an absent resource. Durations and phase timings are non-negative
integer microseconds; a phase that did not occur is zero. `failureCode` is empty for a
passed Run and `failureClass` is empty unless an outbound denial names an address class.
`http` is null when no HTTP response arrived, and `tls` is null when the first hop did not
complete a TLS handshake. The response never contains headers, a body, target-supplied
messages, or credentials.

## 8. HTTP Check Configuration Schema Version 1

`checkConfiguration` must be a JSON object and its raw UTF-8 representation alone
must be at most 16,384 bytes before compact serialization. Schema version other than
1 fails under `checkSchemaVersion` with
`Check type 'http' supports configuration schema version 1 only.` A non-object fails
under `checkConfiguration` with `The configuration must be a JSON object.` An oversized
object fails under `checkConfiguration` with
`The configuration document must not exceed 16384 bytes.`

Top-level property names and header-entry property names are case-sensitive. Unknown
properties are rejected, not ignored. The accepted fields are:

| Field | Exact schema-v1 validation and semantic default |
| --- | --- |
| `url` | Required string, at most 2048 UTF-16 code units; an absolute URI whose normalized scheme is `http` or `https`; no user info and no fragment. Literal IP hosts are allowed. There is no configuration-time DNS, private-range, or metadata screening. |
| `method` | Optional string, default `GET`; exact uppercase one of `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`. |
| `expectedStatusCodes` | Optional array, at most 20 distinct JSON integers fitting int32, each 100-599. Empty/omitted means any 200-299. |
| `timeoutSeconds` | Optional JSON integer fitting int32, 1-60, default 30. |
| `followRedirects` | Optional JSON boolean, default true. |
| `maxRedirects` | Optional JSON integer fitting int32, 0-10, default 5. |
| `headers` | Optional array of at most 20 entries, each exactly an object containing string `name` and string `value`. |

Header names are non-empty RFC 9110 tokens of at most 128 ASCII characters and are
unique case-insensitively. These names are forbidden case-insensitively:
`Authorization`, `Proxy-Authorization`, `Cookie`, `Host`, `Content-Length`, and
`Transfer-Encoding`. Values are at most 1024 UTF-16 code units and contain no Unicode
control character. A request body is not part of schema version 1.

Validation error keys use precise paths such as `checkConfiguration.url`,
`checkConfiguration.expectedStatusCodes[2]`, and
`checkConfiguration.headers[1].name`. Unknown top-level fields use
`checkConfiguration.{field}` with message
`The field is not part of 'http' configuration schema version 1.` Unknown header-entry
fields use `{entryPath}.{field}` with
`The field is not part of a header entry.` All detected top-level failures are returned
together in encounter order and grouped by path in the validation Problem Details.

Defaults are semantic; omitted fields are not materialized into stored JSON. Accepted
configuration is serialized compactly and stored as PostgreSQL `jsonb`. Returning a
revision returns the JSON value, not a quoted JSON string. JSON object whitespace and
key order are not contractual.

## 9. PostgreSQL Schema and Transactions

Embedded SQL migrations define the following initial vertical-slice schema. UUID and timestamp
values are supplied by the application; there are no database-generation defaults.
All listed columns are `NOT NULL` unless marked nullable.

### Core tables

- `organizations`: `id uuid` PK `pk_organizations`; `slug varchar(63)`;
  `display_name varchar(100)`; `created_at timestamptz`. Unique index
  `ux_organizations_slug(slug)`.
- `projects`: `id uuid` PK `pk_projects`; `organization_id uuid` FK
  `fk_projects_organizations` -> `organizations(id)` ON DELETE CASCADE;
  `name varchar(100)`; `is_default boolean`; `created_at timestamptz`. Alternate
  unique constraint `ak_projects_id_organization_id(id, organization_id)`; index
  `ix_projects_organization_id(organization_id)`; partial unique index
  `ux_projects_organization_default(organization_id) WHERE is_default`.
- `users`: `id uuid` PK `pk_users`; `email varchar(254)`; `display_name varchar(100)`;
  `role varchar(50)`; `password_hash text`; `created_at timestamptz`. Unique index
  `ux_users_email(email)`. Email is stored only in normalized form.
- `monitors`: `id uuid` PK `pk_monitors`; `organization_id uuid`; `project_id uuid`;
  `name varchar(100)`; `check_type varchar(50)`; `state varchar(20)`;
  `latest_revision_number integer`; `created_at timestamptz`; `updated_at timestamptz`.
  FK `fk_monitors_organizations` -> `organizations(id)` ON DELETE CASCADE. Composite
  FK `fk_monitors_projects(project_id, organization_id)` ->
  `projects(id, organization_id)` ON DELETE CASCADE. Indexes
  `ix_monitors_organization_project(organization_id, project_id)` and
  `ix_monitors_project_id_organization_id(project_id, organization_id)`.
  PostgreSQL system column `xmin` is the optimistic row version; do not create a
  user-owned `xmin` column.
- `monitor_revisions`: `id uuid` PK `pk_monitor_revisions`; `monitor_id uuid`;
  `organization_id uuid`; `revision_number integer`; `check_type varchar(50)`;
  `check_schema_version integer`; `check_configuration jsonb`; `created_at timestamptz`.
  Unique index `ux_monitor_revisions_monitor_number(monitor_id, revision_number)`;
  index `ix_monitor_revisions_organization_id(organization_id)`; Organization and
  Monitor references cascade as described below.
- `organization_members`: `organization_id uuid`; `user_id uuid`; `role varchar(50)`;
  `created_at timestamptz`. Composite primary key `pk_organization_members(organization_id,
  user_id)` makes a duplicate membership unrepresentable; both foreign keys cascade;
  index `ix_organization_members_user_id(user_id)` serves the caller's Organization list.
  Role strings are exactly `Administrator` and `Viewer`. The "at least one Administrator
  per Organization" rule is a use-case invariant, not a database constraint.
- `maintenance_windows`: `id uuid` PK `pk_maintenance_windows`; `organization_id uuid`;
  `monitor_id uuid`; `starts_at`, `ends_at`, `created_at` as `timestamptz`;
  `cancelled_at` as nullable `timestamptz`. Composite FK
  `fk_maintenance_windows_monitor(monitor_id, organization_id)` ->
  `monitors(id, organization_id)` ON DELETE CASCADE. Checks enforce positive duration no
  longer than 30 days, creation no later than start, and cancellation between creation and
  the excluded end. Monitor/start and non-cancelled-overlap indexes serve bounded queries.
- `sessions`: a 32-byte `token_hash` primary key, `user_id`,
  `authenticated_at`, and `expires_at`, with a cascading user foreign key and
  indexes for user lookup and expiry cleanup.
- `antiforgery_tokens`: 32-byte selector and request-token hashes, one required
- `status_pages`: `id uuid` PK `pk_status_pages`; `organization_id uuid` unique;
  `title varchar(100)`; positive `version bigint`; `created_at`, `updated_at` as
  `timestamptz`; nullable `publication_token_hash bytea` and `published_at timestamptz`
  that are both present or both absent. The publication digest is exactly 32 bytes and
  uniquely indexed when present. The Organization foreign key cascades, and checks enforce
  trimmed title length and timestamp order.
- `status_page_components`: distinct component `id uuid` PK; `organization_id uuid`;
  `status_page_id uuid`; `monitor_id uuid`; `label varchar(100)`; zero-based `position`
  from 0 through 49. Composite page and Monitor foreign keys carry Organization identity
  and cascade. `(status_page_id, position)` and `(status_page_id, monitor_id)` are unique;
  the feature validates one through fifty components before the transactional replacement.
  Whole-draft replacement locks the page, updates its optimistic version, deletes prior
  component rows, verifies every Monitor is in the Organization and not archived, inserts
  all ordered components, and commits atomically.

  session token hash, fixed expiry, and a unique record per authenticated session.
- `anonymous_antiforgery_keys`: one row identified by the checked singleton id
  `1`, containing exactly 32 bytes of HMAC key material and its creation time.

- `runs` and `observations`: Organization-scoped, monthly range-partitioned execution
  records. Run slot uniqueness includes its partition key. Confirmation Runs add a
  candidate id, triggering Run and slot, causation event, and policy version; their
  unique candidate index also includes `scheduled_for` as PostgreSQL requires for a
  partitioned unique index.
- `outbox_entries`: one owning consumer per row, with topic, JSON payload, attempts,
  availability, optional lease, stable failure code, and version-gap first-seen time.
  `processed_outbox_events` retains successful consumer ids; `dead_letter_outbox_entries`
  retains exhausted or permanent failures.
- `health_candidates`: one unique failure or recovery candidate per source revision,
  direction, triggering Run, and triggering slot. It records confirmation causation and
  terminal candidate state.
- `monitor_health` and `health_transitions`: current Monitor-scoped policy state plus
  immutable versioned transitions with complete quorum counts and bounded causal Run pointers.
- `incidents`, `incident_timeline_entries`, and `incident_projection_cursors`: one
  unresolved automatic Incident per Monitor, immutable versioned timeline entries, and
  aggregate-version ordering for the projector. Health timeline entries either carry all
  eight quorum counts or none.
- `alerts`: immutable Incident-derived notification intents with Organization, Project,
  Monitor, source Incident and source Incident version identity, kind, source occurrence,
  and projection creation timestamps. The Organization, Incident, and Incident-version
  tuple is unique for idempotent projection.
- `webhook_deliveries`: immutable Organization-scoped Alert routes that snapshot the
  Integration and signing-secret versions. Nullable `suppression_reason` and
  `maintenance_window_id` retain event-time suppression attribution; the latter has an
  Organization-scoped foreign key to `maintenance_windows`. A check constraint allows only
  the `maintenance` reason paired with a window and requires a suppressed route to be
  completed with zero attempts and no lease. `webhook_delivery_attempts` remain separate
  append-only evidence of actual external calls.

### Tenant foreign-key invariants


`monitors(id, organization_id)` is unique.
`monitor_revisions(monitor_id, organization_id)` is a composite foreign key to
that key with `ON DELETE CASCADE`. The direct Organization foreign key is retained
as an additional integrity constraint. No Monitor-name uniqueness or lifecycle database
check constraint exists; those invariants remain in feature code.

### Migration and credential state

- Embedded sequential `.sql` migrations run transactionally and record applied
  versions in `schema_migrations`. A migration version is applied at most once.
- Password hashes use the versioned Argon2id encoding described below.
- Sessions and antiforgery state use the bounded PostgreSQL structures defined above.

Organization, default Project, and the creator's `Administrator` membership are
inserted in one transaction. Migration `0002` adds `organization_members` and backfills
every instance Administrator as an Organization Administrator of every pre-existing
Organization, so an installation that predates membership keeps access to its own data.

Organization plus default Project insertion is one transaction. First administrator
creation is one transaction that obtains
`pg_advisory_xact_lock(7355608013)`, re-checks whether any user exists after obtaining
the lock, inserts exactly one Administrator, and commits. Concurrent losers return the
same setup-completed `409` as later calls.

Monitor mutation uses the loaded PostgreSQL `xmin` in the update predicate. Revision
creation inserts the immutable revision and advances the Monitor counter in one
transaction. Zero updated Monitor rows is a concurrency conflict; the unique revision
number index is the second backstop and maps to the same `409`.

Maintenance creation locks the scoped Monitor row before checking overlap and inserting,
so concurrent creators cannot commit overlapping windows. Cancellation compares the loaded
PostgreSQL `xmin`; the row remains durable after cancellation and through backup/restore.

## 10. Health Contract

- `GET /healthz` is anonymous liveness. It does not execute the database check and
  returns `200`, body `Healthy`, while the HTTP process can serve.
- `GET /readyz` is anonymous readiness. It checks PostgreSQL connectivity. Healthy is
  `200` with body `Healthy`; an unhealthy database is `503` with the health writer's
  plain-text status (normally `Unhealthy`).

Neither route is under `/api/v1`, requires a cookie, or requires antiforgery.

### Evaluated Monitor health and Incidents

`GET .../monitors/{monitorId}/health` returns the durable evaluator snapshot defined by
this contract. Health states are `unknown`, `healthy`, `degraded`, and `down`; stable state is
`unknown`, `healthy`, or `down`. Phase 1 policy is exactly `phase1.v1`, one configured
embedded location, and one-of-one failure and recovery quorum. A failure candidate and a
recovery candidate each require one matching explicit confirmation Run. Mixed,
indeterminate, location-fault, pending-confirmation, and missing evidence never silently
become Down. Every response carries the configured, eligible, responding, passing,
failing, location-fault, indeterminate, and missing counts.

For an Active Monitor, determinate evidence becomes stale when its configured
evidence age expires and transitions health to Unknown. Paused and archived
Monitors preserve their last evaluated
state. Evidence from a superseded revision or an older scheduled cohort remains queryable
but cannot rewrite current health or Incident history.

An automatic Incident opens only when health becomes Down, may be acknowledged without
changing health, and resolves only when health becomes Healthy. Degraded and Unknown do
neither. Resolved Incidents never reopen; a later Down creates a new Incident. One
unresolved Incident per Monitor is enforced. Creation, acknowledgement, and resolution
append immutable timeline entries. Health entries retain bounded quorum counts and causal
Run identity after raw Run partitions expire.

Incident lists are newest-first by descending `(createdAt, id)`. `pageSize` defaults to
50 and is 1 through 100; `cursor` is optional, opaque, exclusive, and accepted exactly
once. Both malformed values use field-level `400` validation. `nextCursor` is null on the
final page. Every list, detail, and acknowledgement lookup carries Organization, Project,
and Monitor scope. A resolved acknowledgement returns `409` with title
`Incident acknowledgement rejected`.

### Alert intents

An Incident opening creates one `incident.opened` Alert and confirmed recovery creates one
`incident.resolved` Alert. Acknowledgement creates none. Alerts are immutable,
render-neutral intents: they contain no localized prose, Monitor-name snapshot, target,
recipient, route, secret, or provider payload. Alert existence does not mean a notification
was attempted, sent, suppressed, or delivered. The separately queried delivery audit
distinguishes no route, a pending route, terminal maintenance suppression, and actual
Delivery Attempt outcomes.

Alert lists are newest-first by descending `(occurredAt, id)` and use the same default 50,
range 1 through 100, opaque exclusive cursor, and final null `nextCursor` rules as Incident
lists. Every lookup carries Organization, Project, and Monitor scope.

### Internal outbox event contract

The internal PostgreSQL outbox topics are `run.recorded.v1`,
`run.confirmation.requested.v1`, `health.transitioned.v1`, and
`incident.transitioned.v1`. They are internal durable
facts, not public webhooks. Every camelCase payload has `eventId` equal to its owning row,
`organizationId`, `occurredAt`, `aggregateType`, `aggregateId`, `aggregateVersion`, and
the recorded causation where applicable. Consumers verify row/payload Organization,
ignore additional object members, reject invalid required enums, serialize each aggregate,
and mark the event id processed transactionally.

Opening and resolving an Incident writes `incident.transitioned.v1` in the same transaction
as its timeline change. The Alert projector verifies the immutable source timeline fact and
creates the matching Alert in the same transaction that marks the event processed.

Dispatch is at least once with no global or tenant FIFO promise: 60-second leases, batches
up to 32, concurrency up to 4, 12 attempts, exponential retry from one second to five
minutes with deterministic non-negative jitter up to 20 percent, and a 15-minute future
aggregate-version gap deadline. Permanent payload/topic/Organization failures dead-letter
immediately. Processed ids and dead letters retain for 30 days; live rows are never removed
by cleanup. Stable dispatcher codes are part of this contract.

## 11. Current Frontend Fetch Contract

The current React client makes exactly these calls:

| Client operation | Fetch behavior and parsed result |
| --- | --- |
| refresh antiforgery | `GET /api/v1/auth/antiforgery`; parse `headerName` and `requestToken` and cache them in memory |
| setup status | `GET /api/v1/setup/status`; parse `SetupStatusResponse` |
| current session | `GET /api/v1/auth/session`; return `null` only for `401`, otherwise parse `SessionResponse` or throw `ApiError` |
| setup admin | antiforgery-authenticated JSON `POST /api/v1/setup/admin`; parse `SetupResponse`, then refresh antiforgery, then navigate to the returned Organization |
| list Organizations | `GET /api/v1/organizations`; parse `OrganizationResponse[]` |
| login | antiforgery-authenticated JSON `POST /api/v1/auth/login`; parse `SessionResponse`, then refresh antiforgery |
| logout | antiforgery-authenticated `POST /api/v1/auth/logout` with no body; then refresh antiforgery |
| create Organization | antiforgery-authenticated JSON `POST /api/v1/organizations`; parse `OrganizationResponse`; `created` is true only when status is `201`, false for the `200` replay |
| rename Organization | antiforgery-authenticated JSON `PUT /api/v1/organizations/{id}/name`; parse `OrganizationResponse` |
| get Organization | `GET /api/v1/organizations/{encodeURIComponent(id)}`; parse `OrganizationResponse` |
| get Organization overview | `GET /api/v1/organizations/{encodeURIComponent(id)}/overview`; parse permission-aware `OrganizationOverviewResponse` |
| list Monitors | `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors`; parse `MonitorResponse[]` |
| create HTTP Monitor | antiforgery-authenticated JSON `POST` to the Monitor collection; parse the Draft `MonitorResponse` |
| create HTTP revision | antiforgery-authenticated JSON `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions`; send schema version 1 and `{ url }`, then parse `MonitorRevisionResponse` |
| activate Monitor | antiforgery-authenticated JSON `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state`; send `active` and parse `MonitorResponse` |

`GET` calls set no custom headers. Unsafe calls set `Content-Type: application/json`
and the exact dynamic antiforgery header. The fetch calls do not spell out
`credentials`; browser Fetch therefore uses its default `same-origin`, which sends and
accepts same-origin cookies. Do not require `credentials: include`, bearer tokens, an
`Accept` header, local storage, or a frontend-visible session token. Non-success bodies
are parsed as the Problem Details shape in section 1.

The default-Project form creates a Draft, adds its first HTTP revision, and activates it.
The client retains the successfully created Draft between failed steps and invalidates the
scoped Monitor list after every attempt. A validation retry therefore continues the same
Monitor instead of creating a duplicate.

The Organization overview is independently retryable. Same-page Monitor setup and status
draft save, publication, or revocation invalidate its scoped query. The UI distinguishes a
fresh Organization, no Active Monitors, Active Monitors awaiting first evaluation, no
active Incidents, nullable permission-unavailable summaries, and transport failure.

## 12. Playwright and E2E Launch Contract

Playwright runs from `web/` with:

- test directory `web/e2e`;
- Chromium Desktop Chrome project;
- one worker, `fullyParallel: false`, zero retries;
- browser base URL `http://127.0.0.1:5173` by default;
- when no external base URL is supplied,
  API readiness URL `http://127.0.0.1:5080/readyz`, 180-second timeout, never reuse;
- Vite at `127.0.0.1:5173` with `--strictPort`, 120-second timeout, never reuse;
- pinned `locale: 'en-US'` and `timezoneId: 'UTC'`. These are load-bearing, not
  cosmetic: instants render through `Intl` in the viewer's locale and zone,
  so unpinning either makes assertions depend on the machine running them.

Vite proxies `/api` to `http://localhost:5080` without `changeOrigin`, preserving the
browser Host so same-origin validation succeeds.
`PROBEHIVE_E2E_BASE_URL` selects an already running, freshly initialized external
stack and disables both development web-server launchers. The journey derives
unsafe-request `Origin` from that URL and accepts its self-signed HTTPS
certificate only in Playwright. Passing, failing, recovery, and Webhook fixture
targets may be selected with `PROBEHIVE_E2E_PASSING_TARGET`,
`PROBEHIVE_E2E_FAILING_TARGET`, `PROBEHIVE_E2E_RECOVERY_TARGET`, and
`PROBEHIVE_E2E_WEBHOOK_TARGET`; application TLS verification remains enabled.
Before activation, a manual Run must prove that the selected failing target
produces failed evidence. This precondition does not advance evaluated health,
so the subsequent scheduled and confirmation Runs remain the only cause of the
Incident exercised by the journey.

Before launching the API, `web/e2e/start-api.sh` must preserve this reset contract:

1. Read PostgreSQL connection overrides
   `PROBEHIVE_E2E_PGHOST` (default `127.0.0.1`),
   `PROBEHIVE_E2E_PGPORT` (default `5432`),
   `PROBEHIVE_E2E_PGUSER`/`PROBEHIVE_E2E_PGPASSWORD` (both default `probehive`), and
   `PROBEHIVE_E2E_PGDATABASE` (maintenance database, default `probehive`).
2. Using `psql` with `ON_ERROR_STOP=1`, execute
   `DROP DATABASE IF EXISTS probehive_e2e WITH (FORCE)` and then
   `CREATE DATABASE probehive_e2e`.
3. Point the API at `probehive_e2e`, apply all embedded migrations, and listen in
   Development mode on `http://127.0.0.1:5080`, with a fixed test-only Webhook wrapping
   key and a loopback-only outbound allowlist.
4. Build and launch `cmd/probehive` as a fresh process after migration. Keep the
   override names, reset, database name, ports, and readiness gate stable.

The browser journey assumes an empty database, routes `/` to `/setup`, creates and signs
in the first Administrator, and lands directly on the Organization that setup provisioned,
rendering its `Default` heading, default Project, and fresh operational overview. It
renames that Organization and asserts the slug did not move. It creates and operates a
passing HTTP Monitor, triggers a manual Run, follows the returned evidence, then schedules
and cancels a one-time maintenance window. It then publishes the configured status page,
creates an enabled loopback Webhook Integration, a failing HTTP Monitor, and an active
maintenance window. After the Incident and Alert intent appear, the journey asserts that
delivery evidence shows the maintenance reason and exact window id without a Webhook call.
It acknowledges the Incident, returns through the overview's direct Incident link, and
asserts the populated monitoring, active-Incident, enabled-Integration, and published-status
summaries before revocation. It replaces the target through revision 2 and waits for
confirmed recovery and its Alert intent. Finally, it
signs out, signs back in, lands on the Organization list containing the renamed
Organization, creates slug `acme` with display name `Acme Monitoring`, follows the
returned Organization, and renders its default Project. A second journey switches the
interface to `zh-CN`, asserts the translated heading and the `lang` attribute of the
document element, and reloads to confirm the preference
persists rather than renegotiating from the browser.

The journeys and `web/src` are contract consumers: either may change, but a change that
alters observable behavior described here must update this document in the same commit.

## 13. Error Code Catalog

Every code the current build can emit. A client catalog covers this list; an unknown
code falls back to the response's English `message`. Codes are contract;
the sentences elsewhere in this document are not.

Transport and authorization:

```text
auth.unauthorized              auth.forbidden               resource.notFound
request.methodNotAllowed       request.rateLimited          request.malformed
request.antiforgery.invalid    request.origin.rejected      server.internalError
```

Users, sessions, and setup:

```text
user.email.invalid       user.displayName.invalid     user.password.length
user.credentials.invalid user.setup.alreadyCompleted
```

Organizations:

```text
organization.slug.invalid  organization.displayName.invalid  organization.slug.conflict
```

Monitors:

```text
monitor.name.invalid       monitor.checkType.invalid      monitor.checkType.unsupported
monitor.state.invalidTarget monitor.concurrentUpdate      monitor.archived.readOnly
monitor.state.activationWithoutRevision                    monitor.state.transitionNotAllowed
```

Maintenance windows:

```text
maintenance.startsAt.invalid  maintenance.endsAt.invalid
maintenance.duration.invalid  maintenance.overlap
maintenance.window.ended      maintenance.concurrentUpdate
```

Check configuration:

```text
Status-page drafts:

```text
statusPage.title.invalid              statusPage.components.invalid
statusPage.component.label.invalid    statusPage.component.monitor.invalid
statusPage.component.monitor.duplicate statusPage.component.monitorUnavailable
statusPage.concurrentUpdate
```
check.checkType.unsupported          check.schemaVersion.unsupported
check.configuration.notObject        check.configuration.tooLarge
check.http.field.unknown
check.http.url.required              check.http.url.notString
check.http.url.tooLong               check.http.url.notAbsolute
check.http.url.scheme                check.http.url.userInfo
check.http.url.fragment
check.http.method.notString          check.http.method.unsupported
check.http.expectedStatusCodes.notArray      check.http.expectedStatusCodes.tooMany
check.http.expectedStatusCodes.notInteger    check.http.expectedStatusCodes.outOfRange
check.http.expectedStatusCodes.duplicate
check.http.timeoutSeconds.notInteger check.http.timeoutSeconds.outOfRange
check.http.maxRedirects.notInteger   check.http.maxRedirects.outOfRange
check.http.followRedirects.notBoolean
check.http.headers.notArray          check.http.headers.tooMany
check.http.headers.entry.notObject   check.http.headers.entry.unknownField
check.http.headers.name.notString    check.http.headers.name.invalid
check.http.headers.name.forbidden    check.http.headers.name.duplicate
check.http.headers.value.notString   check.http.headers.value.tooLong
check.http.headers.value.controlCharacter
```

Run queries:

```text
run.query.notBefore.invalid  run.query.pageSize.invalid  run.query.cursor.invalid
run.query.outcome.invalid    run.query.kind.invalid      run.query.location.invalid
```


Incident queries:

```text
incident.query.pageSize.invalid  incident.query.cursor.invalid
```

Alert queries:

```text
alert.query.pageSize.invalid  alert.query.cursor.invalid
```

Webhook Integrations:

```text
webhook.name.invalid  webhook.destinationUrl.invalid  webhook.name.conflict
webhook.keyring.unavailable
webhook.enabled.invalid  webhook.enabledLimit.exceeded
webhook.version.invalid  webhook.version.conflict  webhook.rotation.inProgress
webhook.rotation.pendingMissing  webhook.rotation.retiringMissing
webhook.rotation.retiringInUse
```

Webhook Delivery Attempt failure codes:

```text
webhook.delivery.cancelled  webhook.delivery.destination.invalid
webhook.delivery.http.rejected  webhook.delivery.http.retryable
webhook.delivery.network  webhook.delivery.payload.invalid
webhook.delivery.secret.unavailable  webhook.delivery.timeout
webhook.delivery.outcome.uncertain
```

Allowlisted shared outbound-policy failure codes retained by Webhook attempts:

~~~text
outbound.policy.unconfigured
outbound.url.tooLong  outbound.url.invalid  outbound.url.notAbsolute
outbound.url.scheme  outbound.url.userInfo
outbound.host.missing  outbound.host.invalid
outbound.port.invalid  outbound.port.denied
outbound.network.unsupported
outbound.resolution.failed  outbound.resolution.empty
outbound.address.denied  outbound.address.mismatch  outbound.connect.failed
~~~
`monitor.checkType.unsupported` and `check.checkType.unsupported` describe the same
condition at two layers; the Monitor use case screens first, and a catalog may map both
to one sentence.
