# Backend Contract for the Go Rewrite

Status: working implementation specification for the unreleased M1-M5 behavior.

This document freezes the behavior that the Go backend must preserve while the
existing .NET implementation is replaced. It is extracted from the v1 contracts,
API, checks, PostgreSQL adapters and migrations, API tests, current React client,
Playwright journey, and ADRs 0012-0014. The existing `web/` source and browser
journey must work unchanged. The only permitted e2e change is how the API process
and its migrations are launched.

ADRs remain normative. Where the current implementation and an ADR differ, this
document calls out the gap rather than turning it into a compatibility promise.
The Go rewrite intentionally changes only implementation mechanisms recorded by
ADR 0015: encrypted cookie tickets become opaque PostgreSQL-backed sessions,
Data Protection and EF migration artifacts disappear, and pgx replaces EF Core.
Those substitutions must not change the HTTP behavior below.

## 1. Common HTTP and JSON Rules

- The versioned API root is `/api/v1`. There is no unversioned API contract.
- Requests and successful structured responses use JSON. Response property names
  are camelCase exactly as listed below.
- Identifiers are JSON strings containing UUIDs. New Organization, Project, User,
  Monitor, and Monitor Revision identifiers are time-ordered UUID version 7 values.
- Persisted and returned timestamps are UTC. They are JSON ISO 8601 strings; clients
  must not depend on a particular fractional-second precision.
- Request object properties use the ASP.NET web JSON behavior today: property-name
  matching is case-insensitive and unknown envelope properties are ignored. Do not
  apply global strict unknown-field rejection in Go. Strict rejection applies only
  inside `checkConfiguration`, as specified in section 8.
- A missing or JSON `null` string field reaches use-case validation and produces the
  same field-level `400` response as an invalid string. A JSON value of the wrong
  primitive type or malformed JSON is a framework-level `400`.
- Arrays are returned as bare JSON arrays, not wrapper objects. An existing empty
  Project or Monitor has an empty array, not `null` and not `404`.
- Canonical route parameters are UUID strings. A route value that does not satisfy a
  `guid` constraint, or a revision number that does not satisfy an `int` constraint,
  does not match and returns `404`.
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

Validation errors use this extension:

```json
{
  "type": "a standards problem type URI",
  "title": "One or more validation errors occurred.",
  "status": 400,
  "errors": {
    "field.path": ["One or more messages in validation order."]
  }
}
```

The legacy Problem Details writer can add a request-specific `traceId` extension.
The frontend deliberately treats `type`, `title`, `status`, `detail`, and `errors`
as optional and ignores unknown properties. Go must preserve the media type,
numeric status, titles/details explicitly listed in this document, and validation
error map. It may use its own standards URI and request-id extension.

Bare authorization and routing statuses are also Problem Details: unauthenticated is
`401` with title `Unauthorized`, authenticated but unauthorized is `403` with title
`Forbidden`, missing resources are `404`, and exhausted rate limits are `429`.
Authentication never redirects to an HTML login or access-denied page.

## 2. Wire Types

All response fields below are required and non-null.

| Type | Exact JSON fields |
| --- | --- |
| `SetupStatusResponse` | `setupComplete: boolean` |
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

`Anonymous` means no session is required. `Administrator` means an authenticated
session carrying the instance role `Administrator`; a non-administrator session gets
`403`. `Unsafe` means the antiforgery and origin rules in section 5 also apply.

| Method and path | Access | Success | Other application results |
| --- | --- | --- | --- |
| `GET /api/v1/setup/status` | Anonymous | `200 SetupStatusResponse` | none |
| `POST /api/v1/setup/admin` | Anonymous, unsafe, credential rate limit | `201 UserResponse`, signs in and sets a new session cookie; no `Location` | `400` validation; `409` completed; `429` |
| `GET /api/v1/auth/antiforgery` | Anonymous | `200 AntiforgeryTokenResponse`, stores antiforgery cookie | none |
| `POST /api/v1/auth/login` | Anonymous, unsafe, credential rate limit | `200 SessionResponse`, sets a fresh session cookie | `401` generic invalid credentials; `429` |
| `POST /api/v1/auth/logout` | Authenticated, unsafe | `204` empty body, invalidates session and expires cookie | `401` |
| `GET /api/v1/auth/session` | Authenticated | `200 SessionResponse` | `401` |
| `POST /api/v1/organizations` | Administrator, unsafe | first create: `201 OrganizationResponse` and `Location: /api/v1/organizations/{id}`; identical replay: `200 OrganizationResponse` without creating state | `400`, `409`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}` | Administrator | `200 OrganizationResponse` | `404`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | Administrator, unsafe | `201 MonitorResponse` and canonical monitor `Location` | `400`, `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors` | Administrator | `200 MonitorResponse[]` in creation order, UUID as tie-breaker | `404` if the Project is not in the Organization; `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}` | Administrator | `200 MonitorResponse` | `404`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name` | Administrator, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state` | Administrator, unsafe | `200 MonitorResponse` | `400`, `404`, `409`, `401`, `403` |
| `POST /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | Administrator, unsafe | `201 MonitorRevisionResponse` and canonical revision `Location` | `400`, `404`, `409`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions` | Administrator | `200 MonitorRevisionResponse[]` in ascending revision number | `404`, `401`, `403` |
| `GET /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}` | Administrator | `200 MonitorRevisionResponse` | `404`, `401`, `403` |

The canonical revision `Location` ends in `/revisions/{revisionNumber}`. All monitor
lookups include Organization, Project, and Monitor scope. A real identifier presented
through the wrong Organization or Project is indistinguishable from an unknown one
and returns `404`.

Development alone exposes anonymous `GET /openapi/v1.json`. There is no OpenAPI UI.

## 4. Authentication, Authorization, and Rate Limiting

Authorization is deny by default for every endpoint. Explicit anonymous exceptions
are `/healthz`, `/readyz`, development OpenAPI, setup status, setup admin, login, and
antiforgery issuance. Logout and session require authentication. Organization and
Monitor endpoints additionally require `Administrator`.

Endpoint authorization runs before the endpoint antiforgery filter. Consequently an
anonymous unsafe request to a protected endpoint returns `401` even when it has no
antiforgery token. On anonymous setup and login, antiforgery is evaluated before the
handler. Origin rejection is evaluated before antiforgery rejection.

Setup admin and login share one fixed-window limiter partitioned by the transport
peer IP string (or `unknown` when absent):

- default 10 permits per one-minute window;
- configurable as `Authentication:CredentialAttemptsPerMinute` in the legacy host;
- no queue (`QueueLimit = 0`), so the next attempt receives `429` immediately;
- the limit is shared by setup and login for one address;
- it applies to attempts reaching those endpoints, not only failed credentials.

The Go host may expose a Go-style configuration name, but `web/e2e/start-api.sh` and
tests must be able to override the same semantic limit. Proxy-derived client IP is
not trusted until the separately reviewed forwarded-header deployment profile exists.

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

Do not set a `Domain` attribute. Login and successful first-administrator setup issue
a newly generated cookie value. The Go value is a cryptographically random token of
at least 256 bits; only a cryptographic hash is stored in PostgreSQL. A session row is
bound to one `users.id`, records a fixed `expires_at`, and is rejected after expiry.
Reading a session never extends it. Logout deletes the matching server-side session
and emits an expired `probehive.session` cookie with the same path/security profile.
Never return, log, or persist the raw token.

The exact name of the Go session table is implementation-internal, but its migration
must enforce a unique token hash, a foreign key to `users(id)` with a deliberate delete
action, and an expiry lookup/cleanup index. This is the required replacement for the
legacy encrypted ticket and is not permission to change the cookie contract.

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
design works across restarts and replicas without restoring ASP.NET Data Protection or
storing raw tokens. A previously observed anonymous token is never considered when an
authenticated principal exists.

### Origin and Referer

For an unsafe request:

1. If a non-empty `Origin` is present, compare its complete value case-insensitively
   with `{request scheme}://{request Host including port}`. It must have no path or
   trailing slash. `Origin: null`, multiple origins, and every mismatch fail. Origin
   takes precedence over Referer when both exist.
2. Otherwise, if a non-empty `Referer` is present, it must be an absolute URI and its
   scheme plus authority must match the request origin case-insensitively. Its path,
   query, and fragment do not participate in the comparison.
3. If neither header exists, treat the caller as a non-browser client and allow this
   check; antiforgery is still mandatory.

A failure returns status `403`, title `Browser origin rejected`, and detail
`The Origin or Referer of this request does not match the request authority.`

No CORS middleware or cross-origin credential profile is configured. Browser
deployment is same-origin (the development Vite server uses a proxy). Do not add
cross-origin response headers or treat `OPTIONS` as an authorization bypass; a future
cross-origin profile requires a separate security decision.

## 6. Setup, Users, and Organizations

`GET /setup/status` returns `setupComplete: false` exactly while there are zero users,
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
- other invalid transition: `A Monitor cannot move from '{CurrentPascalCase}' to '{TargetPascalCase}'.`

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

The Go SQL migrations recreate the following logical M1-M5 schema. UUID and timestamp
values are supplied by the application; there are no database-generation defaults.
All listed columns are `NOT NULL` unless marked nullable.

### Legacy tables to preserve logically

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
  `ix_monitors_organization_project(organization_id, project_id)` and the legacy
  supporting index `IX_monitors_project_id_organization_id(project_id, organization_id)`.
  PostgreSQL system column `xmin` is the optimistic row version; do not create a
  user-owned `xmin` column.
- `monitor_revisions`: `id uuid` PK `pk_monitor_revisions`; `monitor_id uuid`;
  `organization_id uuid`; `revision_number integer`; `check_type varchar(50)`;
  `check_schema_version integer`; `check_configuration jsonb`; `created_at timestamptz`.
  Unique index `ux_monitor_revisions_monitor_number(monitor_id, revision_number)`;
  index `IX_monitor_revisions_organization_id(organization_id)`; Organization and
  Monitor references cascade as described below.

The legacy authentication migration additionally creates `data_protection_keys`:
`id integer` identity-by-default PK `pk_data_protection_keys`, nullable
`friendly_name text`, and nullable `xml text`. EF also creates its framework-owned
`__EFMigrationsHistory` bookkeeping table. Both are retired by ADR 0015 and are listed
only so the extraction of the current schema is complete.

### Required tenant-FK hardening in Go

The legacy migration has a known schema gap: `monitor_revisions.monitor_id` references
only `monitors(id)`, while `organization_id` independently references
`organizations(id)`. It therefore does not make a mismatched Monitor/Organization pair
unrepresentable even though queries carry both ids and ADR 0009 requires tenant scope.
This is not observable HTTP behavior to preserve.

The Go schema must add a unique key on `monitors(id, organization_id)` and make
`monitor_revisions(monitor_id, organization_id)` a composite FK to that key with
ON DELETE CASCADE. Keep the direct Organization FK only if useful; it must not weaken
the composite invariant. This hardening is required by ADR 0009 and the rewrite brief,
not a contract deviation.

The legacy constraint names are `fk_monitor_revisions_monitors` for the single-column
Monitor FK and `fk_monitor_revisions_organizations` for the Organization FK. Replace
the former with the composite invariant in Go. No Monitor-name uniqueness or lifecycle
database check constraint exists; those invariants remain in feature code.

### Go-only persistence substitutions

- Do not recreate `data_protection_keys` or `__EFMigrationsHistory`.
- Add the PostgreSQL-backed session storage required by section 5.
- Embedded sequential `.sql` migrations run transactionally and record applied
  versions in `schema_migrations`. A migration version is applied at most once.
- Password hashes for newly created Go-era users use the ADR 0015 Argon2id encoding.
  There is no released PBKDF2 data to migrate.

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
| setup admin | antiforgery-authenticated JSON `POST /api/v1/setup/admin`; parse `UserResponse`, then refresh antiforgery |
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
3. Point the API at `probehive_e2e`, apply all embedded Go migrations, and listen in
   Development mode on `http://127.0.0.1:5080`.
4. Replace only the legacy `dotnet ef`/`dotnet run` commands with the Go build,
   migration, and process launch. Keep the override names, reset, database name, ports,
   readiness gate, and fresh-process behavior.

The browser journey assumes an empty database, routes `/` to `/setup`, creates and
signs in the first Administrator, signs out, signs back in, creates slug `acme` with
display name `Acme Monitoring`, follows the returned Organization, and renders its
default Project. This journey and `web/src` must remain unchanged.
