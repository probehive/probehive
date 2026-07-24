# 0010: Browser Authentication Trust and Compatibility

- Status: Accepted
- Date: 2026-07-23
- Supersedes: [ADR 0004](0004-browser-authentication-and-public-compatibility.md)
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

The original browser-authentication decision established same-origin cookies and `/api/v1`. The implementation boundary also needs explicit OIDC protections, key persistence, hosted-identity validation, cross-origin behavior, and a distinction between unreleased source and published compatibility contracts.

## Decision

Use `/api/v1` as the canonical first public HTTP contract and publish one reviewed OpenAPI document for each supported API major version. `/api` may be a gateway routing prefix, but it is not a separate unversioned API contract and must not silently rewrite compatibility semantics.

Use a same-origin production topology by default. The static React application and API may be deployed independently behind a gateway while sharing one public origin. The public API owns local authentication and completes self-hosted OIDC authorization-code flows with PKCE, state, nonce, correlation protection, bounded timeouts, and validated callback destinations.

Browser sessions use host-only `Secure`, `HttpOnly`, `SameSite=Lax` cookies with bounded lifetime and rotation after authentication or privilege changes. Persist and protect ASP.NET Core Data Protection keys so restarts and multiple API replicas do not invalidate or diverge session protection. Browser access and refresh tokens are never stored in local storage or session storage.

Every unsafe cookie-authenticated request requires an antiforgery token in a custom header and validated Origin or Referer metadata. Bearer-token, Agent, webhook-receiver, and health endpoints use their own authentication model and must not accidentally accept the browser cookie as an alternative credential.

CORS is disabled by default. A deployment that cannot use the same-origin route requires an explicit reviewed profile with exact trusted origins, credential and CSRF protection, and a cookie mode appropriate to whether the sites are same-site or truly cross-site. Wildcard origins are never combined with credentials.

When hosted identity is enabled, the public API validates issuer, audience, signature, lifetime, subject, Organization context, and signing-key rotation under a documented trust contract. Edge components strip spoofable identity headers. Selecting an Organization in browser state or sending an unsigned header never grants membership.

Unreleased source and local development contracts may change before the first public artifact is published. Once an API, schema, event, package, generated client, Agent protocol, or OCI artifact is published, its declared version is honored. Compatible additive changes remain within the version; breaking meaning or shape requires a new major route or schema version, migration guidance, and an explicit deprecation plan.

Give every check configuration and result schema an explicit integer schema version. Give every external event an immutable type name and integer schema version. Version released packages and generated clients with SemVer 2.0, and publish released OCI images with immutable version tags and digests rather than treating `latest` as a compatibility contract.

CLI and service clients use scoped revocable bearer tokens. Agent enrollment uses a short-lived one-time token followed by renewable mutually authenticated identity. Agent major and minor negotiation remains independent from HTTP API versioning; peers advertise supported ranges and fail safely on an unsupported major.

## Consequences

- Browser scripts do not persist long-lived bearer credentials.
- Authentication remains valid across API restarts and replicas when the deployment supplies the required key store.
- Hosted identity cannot bypass public Organization authorization.
- Cross-origin deployments are an explicit security profile rather than a casual CORS switch.
- Published preview artifacts may evolve, but never through silent breaking replacement of an existing declared version.
