# Backend Contract

Status: working implementation specification for the unreleased initial vertical slice.

This document defines the observable backend behavior of the initial vertical slice. It is maintained with
the v1 API, check validation, PostgreSQL adapters and migrations, API tests, React
client, Playwright journey, and ADRs 0012-0019. The web application and browser
journey are contract consumers and remain synchronized with this specification.

ADRs remain normative. Where the current implementation and an ADR differ, this
document calls out the gap rather than turning it into a compatibility promise.

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
- Arrays are returned as bare JSON arrays, not wrapper objects. An existing empty
  Project or Monitor has an empty array, not `null` and not `404`.
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

Non-validation problems carry a stable `code` beside `title` (ADR 0019):

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

All response fields below are required and non-null.

| Type | Exact JSON fields |
| --- | --- |
| `SetupStatusResponse` | `setupComplete: boolean` |
| `SetupResponse` | `user: UserResponse`, `organization: OrganizationResponse` |
| `AntiforgeryTokenResponse` | `headerName: string`, `requestToken: string` |
| `SessionResponse` | `userId: UUID string`, `email: string`, `displayName: string`, `role: string` |
| `UserResponse` | `id: UUID string`, `email: string`, `displayName: string`, `role: string`, `createdAt: UTC timestamp string` |
| `ProjectResponse` | `id: UUID string`, `organizationId: UUID string`, `name: string`, `isDefault: boolean`, `createdAt: UTC timestamp string` |
| `OrganizationResponse` | `id: UUID string`, `slug: string`, `displayName: string`, `createdAt: UTC timestamp string`, `defaultProject: ProjectResponse` |
| `MonitorResponse` | `id`, `organizationId`, `projectId` as UUID strings; `name`, `checkType`, `state` as strings; `latestRevisionNumber: integer`; `createdAt`, `updatedAt` as UTC timestamp strings |
| `MonitorRevisionResponse` | `id`, `monitorId` as UUID strings; `revisionNumber: integer`; `checkType: string`; `checkSchemaVersion: integer`; `checkConfiguration: JSON value`; `createdAt: UTC timestamp string` |

Request shapes are:

| Type | Exact JSON fields |
| --- | --- |
| `CreateFirstAdministratorRequest` | `email`, `displayName`, `password` (nullable strings at decoding boundary; required by validation) |
| `LoginRequest` | `email`, `password` (nullable strings at decoding boundary) |
| `CreateOrganizationRequest` | `slug`, `displayName` (nullable strings at decoding boundary; required by validation) |
| `CreateMonitorRequest` | `name`, `checkType` strings |
| `RenameMonitorRequest` | `name` string |
| `ChangeMonitorStateRequest` | `state` string |
| `CreateMonitorRevisionRequest` | `checkSchemaVersion: integer`, `checkConfiguration: JSON value` |

The only current role string is exactly `Administrator`. Monitor state strings are
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
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | `monitor.write`, unsafe | `201 MonitorResponse` and canonical monitor `Location` | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | `monitor.read` | `200 MonitorResponse[]` in creation order, UUID as tie-breaker | `404` if the Project is not in the Organization; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}` | `monitor.read` | `200 MonitorResponse` | `404`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name` | `monitor.write`, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state` | `monitor.write`, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | `monitor.write`, unsafe | `201 MonitorRevisionResponse` and canonical revision `Location` | `400`, `404`, `409`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | `monitor.read` | `200 MonitorRevisionResponse[]` in ascending revision number | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}` | `monitor.read` | `200 MonitorRevisionResponse` | `404`, `401`, `403` |

The canonical revision `Location` ends in `/revisions/{revisionNumber}`. All monitor
lookups include Organization, Project, and Monitor scope. A real identifier presented
through the wrong Organization or Project is indistinguishable from an unknown one
and returns `404`.

Development alone exposes anonymous `GET /openapi/v1.json`. There is no OpenAPI UI.

## 4. Authentication, Authorization, and Rate Limiting

Authorization is deny by default for every endpoint. Explicit anonymous exceptions
are `/healthz`, `/readyz`, development OpenAPI, setup status, setup admin, login, and
antiforgery issuance. Logout, session, and the Organization list require authentication.
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
permission catalog itself is internal and not published; endpoints document the
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
- `sessions`: a 32-byte `token_hash` primary key, `user_id`,
  `authenticated_at`, and `expires_at`, with a cascading user foreign key and
  indexes for user lookup and expiry cleanup.
- `antiforgery_tokens`: 32-byte selector and request-token hashes, one required
  session token hash, fixed expiry, and a unique record per authenticated session.
- `anonymous_antiforgery_keys`: one row identified by the checked singleton id
  `1`, containing exactly 32 bytes of HMAC key material and its creation time.

### Tenant foreign-key invariants

`monitors(id, organization_id)` is unique.
`monitor_revisions(monitor_id, organization_id)` is a composite foreign key to
that key with `ON DELETE CASCADE`. The direct Organization foreign key is retained
as an additional integrity constraint. No Monitor-name uniqueness or lifecycle database
check constraint exists; those invariants remain in feature code.

### Migration and credential state

- Embedded sequential `.sql` migrations run transactionally and record applied
  versions in `schema_migrations`. A migration version is applied at most once.
- Password hashes use the versioned Argon2id encoding defined by ADR 0013.
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

## 10. Health Contract

- `GET /healthz` is anonymous liveness. It does not execute the database check and
  returns `200`, body `Healthy`, while the HTTP process can serve.
- `GET /readyz` is anonymous readiness. It checks PostgreSQL connectivity. Healthy is
  `200` with body `Healthy`; an unhealthy database is `503` with the health writer's
  plain-text status (normally `Unhealthy`).

Neither route is under `/api/v1`, requires a cookie, or requires antiforgery.

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
| get Organization | `GET /api/v1/organizations/{encodeURIComponent(id)}`; parse `OrganizationResponse` |

`GET` calls set no custom headers. Unsafe calls set `Content-Type: application/json`
and the exact dynamic antiforgery header. The fetch calls do not spell out
`credentials`; browser Fetch therefore uses its default `same-origin`, which sends and
accepts same-origin cookies. Do not require `credentials: include`, bearer tokens, an
`Accept` header, local storage, or a frontend-visible session token. Non-success bodies
are parsed as the Problem Details shape in section 1.

The current frontend makes no Monitor API calls; Monitor compatibility is exercised by
the API test suite and ADR 0014.

## 12. Playwright and E2E Launch Contract

Playwright runs from `web/` with:

- test directory `web/e2e`;
- Chromium Desktop Chrome project;
- one worker, `fullyParallel: false`, zero retries;
- browser base URL `http://127.0.0.1:5173`;
- API readiness URL `http://127.0.0.1:5080/readyz`, 180-second timeout, never reuse;
- Vite at `127.0.0.1:5173` with `--strictPort`, 120-second timeout, never reuse.

Vite proxies `/api` to `http://localhost:5080` without `changeOrigin`, preserving the
browser Host so same-origin validation succeeds.

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
   Development mode on `http://127.0.0.1:5080`.
4. Build and launch `cmd/probehive` as a fresh process after migration. Keep the
   override names, reset, database name, ports, and readiness gate stable.

The browser journey assumes an empty database, routes `/` to `/setup`, creates and signs
in the first Administrator, and lands directly on the Organization that setup provisioned,
rendering its `Default` heading and default Project. It then signs out, signs back in,
lands on the Organization list containing `Default`, creates slug `acme` with display name
`Acme Monitoring`, follows the returned Organization, and renders its default Project.
The journey and `web/src` are contract consumers: either may change, but a change that
alters observable behavior described here must update this document in the same commit.

## 13. Error Code Catalog

Every code the current build can emit. A client catalog covers this list; an unknown
code falls back to the response's English `message` (ADR 0019). Codes are contract;
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

Check configuration:

```text
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

`monitor.checkType.unsupported` and `check.checkType.unsupported` describe the same
condition at two layers; the Monitor use case screens first, and a catalog may map both
to one sentence.
