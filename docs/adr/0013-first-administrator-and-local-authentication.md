# 0013: First Administrator Bootstrap and Local Authentication

- Status: Accepted
- Date: 2026-07-24
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

The API now exposes `/api/v1` Organization endpoints without authentication. ADR 0010 defines the browser-session security profile (same-origin cookies, antiforgery, persisted Data Protection keys) but not the identity model, the first-administrator bootstrap, or the exact enforcement semantics. Those must be recorded before the implementation exists.

## Decision

### Instance users and roles

Local accounts are instance-scoped `User` records: identifier, normalized email address, display name, opaque password hash, instance role, and UTC creation instant. Users are a documented exception to the Organization-scoped data rule in ADR 0009 because they exist before and across Organizations; Organization membership and per-Organization roles are a future ADR and are required before non-administrator users can be authorized meaningfully.

The only instance role today is `Administrator`. Every `/api/v1` endpoint requires an authenticated session by default via a deny-by-default fallback authorization policy; the explicit anonymous exceptions are the health endpoints, the development-only OpenAPI document, setup status, first-administrator creation, login, and the antiforgery token endpoint. Organization endpoints additionally require the `Administrator` role until Organization membership exists.

### First-administrator bootstrap

`GET /api/v1/setup/status` anonymously reports whether setup is complete so the browser application can route to setup or login. `POST /api/v1/setup/admin` creates the first administrator only while the instance has zero users. The store serializes creation with a PostgreSQL transaction-scoped advisory lock and re-checks emptiness inside the transaction, so concurrent bootstrap attempts produce exactly one administrator and every loser receives the same "setup already completed" conflict as any later attempt. Successful bootstrap signs the new administrator in immediately; the response cookie is a freshly issued ticket, satisfying the rotation-on-authentication rule of ADR 0010.

### Passwords

Password policy is length-based only: 12 to 128 characters after no transformation (passwords are never trimmed or normalized). Validation happens in the Application layer; the Domain only ever sees the opaque hash. Hashing uses the ASP.NET Core Identity `PasswordHasher` (PBKDF2, format V3) from `Microsoft.Extensions.Identity.Core` behind an Application port, with rehash-on-successful-login when the stored format is outdated. Login verifies a hash even when the email is unknown so response timing does not reveal account existence, and failed logins return one generic invalid-credentials problem. Full ASP.NET Core Identity (user manager, security stamps, lockout, TOTP) is deliberately deferred until account management, lockout, or TOTP requirements land; adopting it then supersedes this paragraph's hashing adapter, not the password policy.

### Sessions

The session cookie is host-only `probehive.session` with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, a fixed 12-hour lifetime, and no sliding renewal. The `Secure` policy is unconditional outside the Development environment; Development uses secure-when-https so the plain-http Vite development proxy works, and a production deployment whose TLS terminates at a same-origin gateway must forward the original scheme and host (`ASPNETCORE_FORWARDEDHEADERS_ENABLED=true` with a trusted proxy) or antiforgery refuses to issue tokens. Logging in always issues a new ticket. Logout requires an authenticated session plus antiforgery and deletes the cookie. The API never redirects: unauthenticated requests receive `401` and forbidden requests `403`, both as Problem Details. Server-side session revocation (deleting a user, forced sign-out) requires security stamps and arrives with the deferred Identity adoption; until user management exists there is no way to remove a user, so the 12-hour ticket bound is the revocation bound.

### Antiforgery and browser-origin validation

`GET /api/v1/auth/antiforgery` issues the antiforgery cookie and returns the request token plus the required header name `X-ProbeHive-Antiforgery`. Every unsafe `/api/v1` request must present a valid antiforgery header token — including the anonymous login and setup endpoints, which is deliberately stricter than the ADR 0010 minimum so login CSRF is covered. Unsafe requests also validate browser origin metadata: when an `Origin` or `Referer` header is present it must match the request authority exactly; a missing header is treated as a non-browser client and allowed (the antiforgery token is still required); `Origin: null` or any mismatch is rejected with `403`. Future bearer-token, Agent, and webhook surfaces use their own authentication models per ADR 0010 and will be explicitly excluded from cookie antiforgery enforcement when they are introduced.

Login and setup endpoints are rate limited with a fixed window per client address (configurable limit, default 10 attempts per minute) to bound credential guessing; the client address is the transport peer, which is a documented approximation until reverse-proxy forwarding is a configured deployment concern. Account lockout is deferred with Identity adoption.

### Data Protection keys

Data Protection keys persist in the existing PostgreSQL database through the official `Microsoft.AspNetCore.DataProtection.EntityFrameworkCore` provider with the fixed application name `ProbeHive`, so sessions and antiforgery tokens survive restarts and are shared across replicas. Keys are currently stored unencrypted at rest in the operator's own database; protecting them with a certificate is an operator hardening option to be wired when certificate configuration exists. The architecture rule "Infrastructure must not reference ASP.NET Core" gains one deliberate exception: `Microsoft.AspNetCore.DataProtection.*` packages are persistence adapters, not web-host dependencies, and only they are allowed.

## Consequences

- The API is deny-by-default; adding an endpoint without thinking about authorization leaves it authenticated-only rather than anonymous.
- A fresh installation is unusable until the operator completes first-administrator setup, and the setup surface disappears atomically once one user exists.
- Browser clients must fetch and echo the antiforgery token for every unsafe request, including login.
- Deleting users, forced sign-out, lockout, TOTP, and Organization membership are explicitly deferred and tracked; the current model is single-administrator.
- The React application must gain setup, login, and session handling to keep consuming `/api/v1`.
