# 0013: First Administrator Bootstrap and Local Authentication

- Status: Accepted
- Date: 2026-07-24
- Amended by: [ADR 0017](0017-organization-membership-and-authorization.md), [ADR 0018](0018-first-run-organization-provisioning.md), [ADR 0019](0019-internationalization-and-error-codes.md)

## Context

The API needs an identity model, first-administrator bootstrap, and exact enforcement semantics before Organization endpoints can be exposed safely. ADR 0010 defines the same-origin cookie and antiforgery security profile; this decision defines the concrete local-account and session behavior.

## Decision

### Instance users and roles

Local accounts are instance-scoped `User` records: identifier, normalized email address, display name, opaque password hash, instance role, and UTC creation instant. Users are a documented exception to the Organization-scoped data rule in ADR 0009 because they exist before and across Organizations; Organization membership and per-Organization roles are decided in [ADR 0017](0017-organization-membership-and-authorization.md) and are required before non-administrator users can be authorized meaningfully.

The only instance role today is `Administrator`. Every `/api/v1` endpoint requires an authenticated session by default via a deny-by-default fallback authorization policy; the explicit anonymous exceptions are the health endpoints, the development-only OpenAPI document, setup status, first-administrator creation, login, and the antiforgery token endpoint. Organization endpoints additionally require the `Administrator` role until Organization membership exists.

### First-administrator bootstrap

`GET /api/v1/setup/status` anonymously reports whether setup is complete so the browser application can route to setup or login. `POST /api/v1/setup/admin` creates the first administrator only while the instance has zero users. The store serializes creation with a PostgreSQL transaction-scoped advisory lock and re-checks emptiness inside the transaction, so concurrent bootstrap attempts produce exactly one administrator and every loser receives the same "setup already completed" conflict as any later attempt. Successful bootstrap signs the new administrator in immediately with a newly issued server-side session, satisfying the rotation-on-authentication rule of ADR 0010.

### Passwords

Password policy is length-based only: 12 to 128 UTF-16 code units with no trimming or normalization, matching the browser-facing string-length contract. The `internal/user` package validates the opaque password before hashing it with Argon2id from `golang.org/x/crypto/argon2`. Store a versioned, self-describing encoding of the parameters, salt, and derived key, and rehash after successful login when the configured policy changes. Login verifies a synthetic hash even when the email is unknown so response timing does not reveal account existence, and failed logins return one generic invalid-credentials problem. Account management, lockout, TOTP, and recovery require later decisions.

### Sessions

The session cookie is host-only `probehive.session` with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, a fixed 12-hour lifetime, and no sliding renewal. The `Secure` policy is unconditional outside Development; Development uses secure-when-HTTPS so the plain-http Vite proxy works. A production gateway that terminates TLS must preserve the public Host and configure `PROBEHIVE_PUBLIC_ORIGIN` to the exact browser origin. Each setup or login creates a token with at least 256 bits of entropy, sends only the raw token to the browser, and stores only its cryptographic hash in PostgreSQL. Logout requires an authenticated session plus antiforgery, deletes the server-side row, and expires the cookie. The API never redirects: unauthenticated requests receive `401` and forbidden requests `403`, both as Problem Details.

### Antiforgery and browser-origin validation

`GET /api/v1/auth/antiforgery` issues the antiforgery cookie and returns the request token plus the required header name `X-ProbeHive-Antiforgery`. Every unsafe `/api/v1` request must present a valid antiforgery header token — including the anonymous login and setup endpoints, which is deliberately stricter than the ADR 0010 minimum so login CSRF is covered. Unsafe requests also validate browser origin metadata: when an `Origin` or `Referer` header is present it must match the expected browser origin exactly. The expected browser origin is `PROBEHIVE_PUBLIC_ORIGIN` when configured; otherwise it is the request scheme and Host. A missing header is treated as a non-browser client and allowed (the antiforgery token is still required); `Origin: null` or any mismatch is rejected with `403`. Future bearer-token, Agent, and webhook surfaces use their own authentication models per ADR 0010 and will be explicitly excluded from cookie antiforgery enforcement when they are introduced.

Login and setup endpoints share a fixed-window limit per client address, configurable through `PROBEHIVE_CREDENTIAL_ATTEMPTS_PER_MINUTE` and defaulting to 10 attempts per minute. The client address is the transport peer; proxy-supplied addresses are not trusted until a separate forwarded-client profile is designed. Account lockout requires a later decision.

### Server-side security state

PostgreSQL stores session token hashes, one hashed antiforgery record per authenticated session, and one bounded singleton key for anonymous HMAC antiforgery tokens. Raw session and antiforgery tokens are never persisted or logged. Session foreign keys cascade to authenticated antiforgery state, expired records are removed opportunistically, and every API replica uses the same database-backed state.

## Consequences

- The API is deny-by-default; adding an endpoint without thinking about authorization leaves it authenticated-only rather than anonymous.
- A fresh installation is unusable until the operator completes first-administrator setup, and the setup surface disappears atomically once one user exists.
- Browser clients must fetch and echo the antiforgery token for every unsafe request, including login.
- User deletion, administrative forced sign-out, lockout, TOTP, recovery, and Organization membership are explicitly deferred and tracked; the current model is single-administrator.
- The React application provides setup, login, and session handling while consuming `/api/v1`.
