# 0015: Adopt Go for the Backend Implementation

- Status: Accepted
- Date: 2026-07-24
- Supersedes: [ADR 0002](0002-modular-monolith-and-project-topology.md)
- Supersedes: [ADR 0008](0008-sdk-and-dependency-reproducibility.md)
- Amends: [ADR 0004](0004-browser-authentication-and-public-compatibility.md)
- Amends: [ADR 0005](0005-postgresql-leases-and-outbox.md)
- Amends: [ADR 0006](0006-cloud-runtime-integration-boundary.md)
- Amends: [ADR 0010](0010-browser-authentication-trust-and-compatibility.md)
- Amends: [ADR 0013](0013-first-administrator-and-local-authentication.md)
- Amends: [ADR 0014](0014-monitor-lifecycle-and-revision-immutability.md)

## Context

ProbeHive is pre-launch and has no external users, contributors, issues, stars, releases, or published compatibility contracts. The existing .NET implementation covers the M1 through M5 behavior: Organization bootstrap with a transactional default Project, first-administrator and local cookie-session authentication, Monitors with immutable revisions and HTTP check configuration validation, the React application, and one Playwright browser journey. That implementation is the behavioral reference for a rewrite, but it is not a released platform that must remain as source.

On 2026-07-24, the owner selected Go as the backend language baseline. The deciding forces are straightforward cross-compilation and single static binaries for Agents and other hosts, a smaller self-hosting footprint, and alignment with the SRE and DevOps contributor ecosystem.

The current stable release was checked on the official Go downloads page on the decision date. The featured stable release was `go1.26.5`; release candidates are not eligible for this baseline.

This is an implementation-baseline decision. PostgreSQL, React with strict TypeScript and Vite, Compose-first deployment, the domain model, tenant isolation, outbound-access controls, browser security semantics, and deliberately versioned public contracts remain unchanged.

## Decision

### Scope and compatibility

Use Go for the ProbeHive backend. The initial rewrite is contract-compatible with the current React frontend and Playwright journey:

- The frontend source and its fetch behavior remain unchanged.
- The Playwright journey remains unchanged except for how it launches the API process.
- Existing JSON names, casing, optionality, routes, methods, status codes, Problem Details shapes, cookie attributes, antiforgery flow, Origin validation, rate-limit responses, authorization policies, and health behavior remain the observable contract.
- The .NET implementation and tests are the behavioral reference until equivalent Go coverage exists.

This ADR supersedes ADR 0002's canonical .NET project topology and ADR 0008's .NET SDK, analyzer, Central Package Management, NuGet, and lock-file rules. It carries forward ADR 0002's modular-monolith, inward-dependency, host-independence, no-speculative-scaffolding, React, and frontend-testing decisions as Go import and package rules. It also carries forward ADR 0008's frontend lockfile and container reproducibility rules.

No released password hashes, sessions, schemas, or API artifacts exist. The rewrite therefore requires no compatibility migration for .NET-specific persisted artifacts, but it must preserve all currently observed application behavior.

### Toolchain and module reproducibility

Use one Go module with module path `github.com/probehive/probehive`.

The supported release line is Go 1.26, and the exact initial toolchain is `go1.26.5`. Record the language baseline in the `go` directive, pin `toolchain go1.26.5` in `go.mod`, pin `go = "1.26.5"` in `.mise.toml`, and make CI install and report that exact version. Do not select a prerelease toolchain.

A toolchain update is a deliberate reviewed change. Discover the then-current stable release from official Go tooling or sources rather than copying a version from memory. Review release notes, compatibility, advisories, and the resulting module graph before changing the pin.

Commit `go.mod` and `go.sum`. They are the module manifest and verified dependency-sum record, not files to regenerate implicitly in locked validation. CI and release validation use `-mod=readonly` and run `go mod verify` so an unreviewed module-graph change fails.

### Feature-oriented package layout

Do not translate the .NET Domain, Contracts, Application, Checks, Infrastructure, and API projects into layer-named Go packages. Organize the modular monolith by feature:

```text
cmd/probehive/
internal/organization/
internal/user/
internal/monitor/
internal/check/
internal/postgres/
internal/httpapi/
internal/httpapi/v1/
web/
```

The package rules are:

- `cmd/probehive` is the API server binary and composition root. Worker, Agent, CLI, and other future hosts become sibling commands only when they gain real behavior; do not scaffold empty commands.
- `internal/organization`, `internal/user`, and `internal/monitor` own their domain types, invariants, state machines, use cases, and the narrow store or service interfaces they need. They remain standard-library-only and import no SQL package, HTTP package, database driver, composition package, or sibling feature implementation.
- `internal/check` owns the check catalog and HTTP check configuration schema version 1 validation. It remains standard-library-only and has no SQL, HTTP, driver, or host dependency.
- `internal/postgres` implements feature-owned store interfaces with pgx and owns embedded migrations. It may import feature packages; feature packages never import it.
- `internal/httpapi` owns routing, HTTP composition, sessions, antiforgery, Origin validation, rate limiting, authentication, authorization, health endpoints, and HTTP adapters. Deliberately versioned wire types live in `internal/httpapi/v1` beside their handlers.
- A separate exported contracts package or module is created only when a real source consumer exists. Cross-repository consumption remains through OpenAPI, published wire contracts, generated clients, authenticated events, and OCI artifacts rather than Go source imports.

Use short, lowercase, singular package names without stutter. Document exported identifiers. Keep pure state transitions separate from transport and persistence.

An architecture test inspects the import graph with `go list -deps` or `golang.org/x/tools/go/packages`. It must prove that the feature packages and `internal/check` import neither `internal/postgres` nor `internal/httpapi` and that their non-test dependency graph is standard-library-only.

### Dependency policy and mandatory tooling

Prefer the standard library, then official `golang.org/x` modules, before a third-party dependency. The initial dependency baseline is:

- `net/http` with `ServeMux` method and wildcard patterns; do not add chi, Echo, Gin, or another router.
- `github.com/jackc/pgx/v5` and `pgxpool` for PostgreSQL.
- `golang.org/x/crypto/argon2` for Argon2id password hashing.
- `golang.org/x/time/rate` for the login and setup rate limiter.
- `encoding/json`, using `DisallowUnknownFields` wherever the wire or configuration contract requires strict unknown-field rejection.

Do not add an ORM, validation framework, dependency-injection framework, router framework, result-wrapper library, generic repository, generic unit-of-work abstraction, message bus, cache, or job framework without a concrete requirement and a later decision.

Discover exact module versions with current `go get` tooling. Review ownership, support, advisories, transitive dependencies, native code, and exact-version licenses before accepting the resulting `go.mod` and `go.sum` changes.

`gofmt` and `go vet` are mandatory and CI-enforced. Do not add a third-party linter in this rewrite.

### Mapping earlier .NET-specific decisions

The accepted decision bodies remain historical records. Their .NET-specific mechanism wording maps as follows:

- ADR 0004 is already superseded by ADR 0010. Its historical statement that the ASP.NET Core API owns authentication maps to the Go server composed from `cmd/probehive` and `internal/httpapi`. ADR 0010 remains the active security and compatibility decision.
- ADR 0005 retains PostgreSQL as the one durable production dependency, including leases, outbox semantics, retry tolerance, idempotency, backup, restore, partitioning, and retention. EF Core and Npgsql are replaced by explicit SQL through pgx and pgxpool; no ORM replaces EF Core.
- ADR 0006's prohibition on cross-repository project references, source links, implementation assemblies, and direct database access becomes a prohibition on cross-repository Go module, package, source, or `replace` dependencies. The public/private runtime boundary is unchanged.
- ADR 0010's ASP.NET Core Data Protection mechanism is replaced by PostgreSQL-backed server-side sessions and synchronizer antiforgery tokens described below. Its same-origin topology, cookie security, OIDC protections, hosted-identity validation, authorization separation, CORS posture, API versioning, schema versioning, Agent negotiation, and published compatibility rules remain in force.
- ADR 0013's ASP.NET Identity PBKDF2 adapter is replaced by Argon2id from `golang.org/x/crypto`. Its first-administrator, password-policy, timing-resistance, generic-error, cookie, antiforgery, Origin, rate-limit, status-code, and deny-by-default authorization semantics remain in force.
- ADR 0013's ASP.NET cookie ticket and Data Protection key persistence are replaced by server-side sessions. The Data Protection keys table and its special Infrastructure-to-ASP.NET package exception disappear.
- ADR 0014's `ProbeHive.Checks` validator becomes `internal/check`. A narrow validator interface is owned by the consuming feature package and is satisfied by the check validator at composition time, so the feature package does not import a sibling implementation.
- ADR 0014's EF Core concurrency plumbing is replaced by direct pgx use of PostgreSQL `xmin`. Its revision immutability, strict configuration schema, lifecycle, tenant integrity, unique-index backstop, `409 Conflict`, and client-retry semantics remain in force.

References in earlier ADRs to an Application use case mean the use case owned by the appropriate feature package. That terminology mapping does not change the Organization provisioning or idempotency decisions.

### PostgreSQL access and migrations

Use pgx and pgxpool with explicit SQL and transactions. PostgreSQL behavior is authoritative and survives the language change:

- Recreate the current tables, constraints, unique indexes, partial indexes, and composite foreign keys that carry `organization_id`, including the tenant-integrity rules of ADR 0009 and ADR 0014.
- Preserve PostgreSQL-backed lease and outbox semantics from ADR 0005.
- Preserve `pg_advisory_xact_lock` for serializing first-administrator creation.
- Preserve `xmin` as the optimistic concurrency token for Monitor mutation and retain the unique index on revision numbering as the race backstop.
- Keep explicit transaction boundaries for Organization plus default Project creation, administrator bootstrap, and every other accepted atomic behavior.
- Keep the existing `probehive_e2e` database reset workflow usable by the browser journey.

EF Core migrations disappear. Use a minimal internal migration runner with sequentially versioned `.sql` files embedded through `embed.FS`. Apply each unapplied migration transactionally in version order and record success in a `schema_migrations` table only in the same transaction. A failed migration leaves neither its schema changes nor its version record committed.

The initial Go migrations faithfully recreate the current application schema while omitting `__EFMigrationsHistory` and the Data Protection keys table. Add only the persistence required by the Go mechanisms, including server-side sessions and antiforgery state. Do not silently weaken or omit accepted database constraints.

### Passwords, sessions, antiforgery, and request security

Hash new passwords with Argon2id from `golang.org/x/crypto/argon2`. Store a versioned, self-describing encoding of the parameters, salt, and derived key, and support rehash-on-success when the configured policy changes. Keep the existing 12-to-128-character, no-trimming, no-normalization password policy. Verify a synthetic hash when the email is unknown and return the same generic invalid-credentials problem. There is no released PBKDF2 data to migrate.

Replace encrypted client-side authentication tickets with server-side sessions in PostgreSQL:

- Generate each cookie token with `crypto/rand` and at least 256 bits of entropy.
- Send the opaque token to the browser and store only a cryptographic hash of it server-side.
- Bind the session to the user, authentication instant, fixed expiry, and antiforgery state needed to validate requests.
- Issue a new session on successful setup or login. Logout requires authentication and antiforgery, deletes the server-side session, and expires the cookie.
- Creating a session transactionally removes sessions already expired at its authentication instant; the session foreign key cascades cleanup to bound antiforgery state.
- Preserve the host-only `probehive.session` cookie, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, fixed 12-hour lifetime, and no sliding renewal. `Secure` is unconditional outside Development and secure-when-HTTPS in Development, as ADR 0013 specifies.
- Preserve API `401` and `403` Problem Details responses; never redirect API clients.

Use a synchronizer antiforgery token bound to the server-side session. Preserve `GET /api/v1/auth/antiforgery`, the returned header name `X-ProbeHive-Antiforgery`, and the current frontend flow. Keep at most one hashed antiforgery record per authenticated session. Anonymous setup and login use a timestamped 256-bit selector with a 192-bit random nonce and an HMAC-SHA-256 request token under one PostgreSQL-backed 256-bit key. Only the singleton key persists, so callers that omit cookies do not create per-token rows; all replicas load the same key, MAC validation precedes timestamp checks, and issuance allows at most one minute of future clock skew while retaining the fixed 12-hour expiry. Successful authentication rotates into a new authenticated session, and anonymous MAC tokens are never considered when an authenticated principal exists.

Every unsafe browser-cookie request retains antiforgery and exact Origin or Referer validation. A present origin must match the request authority; `Origin: null` and mismatches return `403`; a missing origin remains allowed for non-browser clients only when the antiforgery token is valid. Bearer-token, Agent, webhook, and health surfaces retain their separate authentication models.

Use `golang.org/x/time/rate` behind a per-client-address adapter that preserves ADR 0013's fixed-window behavior and default of 10 setup or login attempts per minute. Preserve the `429` contract. Hard-cap the in-process partition table at 4096 addresses, prune stale entries at most once per window, and place previously unseen addresses beyond the cap into one fail-closed shared overflow window; this bounds memory and cleanup work without granting extra attempts under high-cardinality abuse. Reverse-proxy trust remains an explicit deployment concern rather than an implicit trust of forwarded headers.

The Go HTTP stack retains the deny-by-default authorization policy and the exact anonymous exceptions from ADR 0013. Organization endpoints remain Administrator-only until Organization membership is designed. ADR 0010's OIDC and hosted-identity requirements remain normative; choose a Go OIDC dependency only when that feature is implemented and review it under this ADR's dependency rules.

### Concurrency, time, and cancellation

Use PostgreSQL behavior directly rather than emulating it in memory:

- Acquire the transaction-scoped advisory lock and re-check for an existing user inside the first-administrator transaction.
- Read `xmin` with Monitor state and include the expected value in mutation predicates. A zero-row update after a valid request is an optimistic-concurrency loss and returns `409 Conflict`.
- Allocate revision numbers from the Monitor row under the same optimistic-concurrency rule. The unique index remains the final defense, and a loser re-reads fresh state rather than silently renumbering.
- Preserve all unique-violation replay-or-conflict rules, including Organization provisioning.

Propagate `context.Context` through request, use-case, persistence, and external-I/O boundaries, and honor cancellation in pgx and HTTP operations. Pure value operations remain context-free.

Inject a small clock interface into domain-relevant use cases instead of calling `time.Now` directly. Persist UTC timestamps. Use monotonic clock readings carried by `time.Time` values for elapsed durations rather than subtracting wall-clock timestamps from separate sources.

### Testing and validation

Use the standard `testing` package and `net/http/httptest` for domain, use-case, and HTTP composition tests. Keep the security-relevant suite local and deterministic.

The Go test baseline includes:

- Exact wire serialization, validation, Problem Details, route, method, and status behavior.
- The complete anonymous and authenticated cookie plus antiforgery flow.
- `401`, `403`, `409`, and `429` behavior.
- Origin mismatch, null origin, missing-origin non-browser behavior, logout, and session expiry.
- Cross-Organization denial and composite-foreign-key enforcement.
- HTTP check configuration schema version 1, including bounds, forbidden headers, canonical JSON, and unknown-field rejection.
- Import-graph architecture assertions for feature isolation and standard-library-only feature packages.

Run real-PostgreSQL integration tests for migrations, Organization provisioning, session persistence, first-administrator contention, Monitor revision contention, unique and partial indexes, composite tenant foreign keys, and `xmin` conflict behavior. Use a disposable PostgreSQL container; `public.ecr.aws/docker/library/postgres` is the approved image source when Docker Hub is unreachable.
Starting the disposable database may require separately approved `sudo` access to the container engine in a restricted sandbox. The test harness must never elevate privileges implicitly.

CI runs `gofmt` verification, `go vet`, `go mod verify`, and `go test` with the module graph read-only. It also runs the unchanged frontend sequence: `npm ci` with lifecycle scripts disabled, lint, typecheck, Vitest, and build, followed by the Playwright journey against the real Go API and reset `probehive_e2e` database.

The acceptance criterion is that the existing frontend and browser journey pass unchanged, apart from the API launcher. Any other observed contract deviation is a rewrite defect rather than an allowed cleanup.

### One-time pre-launch history reset

The owner authorizes one local history reset for the public `probehive` repository so the eventual public Go-era history does not publish the discarded .NET implementation history.

Before rewriting history, create `archive/dotnet` at the original `main` tip so the complete .NET history, including the three unpushed M5 commits, remains locally reachable. Do not delete the GitHub repository or push the archive.

After the final tree and full validation are complete, create an orphan branch and record a small sequence of clean, reviewable, English Conventional Commits with DCO sign-off and no cryptographic signing. The curated sequence separates, at minimum, governance plus documentation and ADRs, the Go backend, `web/`, and deployment plus CI. Point local `main` at that history and report the new root hashes.

Do not push, force-push, tag, publish, or update a superproject submodule pointer. The owner will review, cryptographically sign, and force-push the curated `main`; decide whether to keep or remove `archive/dotnet`; and update the private workspace submodule pointer.

This authorization is specific to the 2026-07-24 pre-launch rewrite. It does not relax the normal prohibition on amend, rebase, reset, force-push, or history rewriting for later work.

### Explicitly unchanged and out of scope

This decision does not change:

- The ownership path `Organization -> Project -> Monitor -> Monitor Revision -> Run -> Observation`, default Project invariant, Organization provisioning idempotency, tenant scope, or telemetry rules.
- Monitor lifecycle, revision immutability, check schema versioning, HTTP check schema version 1, or the shared outbound-access and SSRF policy.
- Same-origin browser deployment, cookie attributes, antiforgery and Origin requirements, deny-by-default authorization, OIDC trust, hosted-identity validation, or bearer and Agent authentication separation.
- `/api/v1` versioning, published compatibility rules, Agent negotiation, Problem Details, OpenAPI, event, package, generated-client, schema, or OCI versioning decisions.
- PostgreSQL as the only initial durable dependency, PostgreSQL leases and outbox, React with strict TypeScript and Vite, the static frontend deployment model, or Compose-first self-hosting.
- The public/private repository boundary, Apache-2.0 licensing, third-party notice policy, or product scope.

## Consequences

- Backend and Agent artifacts can be distributed as cross-compiled single binaries with a smaller runtime footprint.
- Feature packages expose architecture through ordinary imports instead of a translation of .NET project layers.
- Explicit SQL makes PostgreSQL constraints and concurrency visible, but the project owns more query and migration code directly.
- Server-side sessions make immediate revocation technically possible and remove Data Protection key management, while adding session rows and database lookups to authenticated requests.
- The rewrite carries substantial regression risk, so the unchanged frontend, browser journey, extracted wire contract, and real-PostgreSQL race tests are release gates.
- The one-time curated history removes .NET implementation commits from the future public `main` while retaining them locally on `archive/dotnet` for owner review.
