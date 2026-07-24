# 0004: Browser Authentication and Public Compatibility

- Status: Superseded by [ADR 0010](0010-browser-authentication-trust-and-compatibility.md)
- Date: 2026-07-22
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

The React application is a separately deployable static client. Authentication must support local accounts and OIDC without exposing long-lived credentials to browser script, while public APIs and Agent contracts need deliberate compatibility from the first release.

## Decision

Use a same-origin production topology by default. The static React application calls `/api/v1` on the same public origin through a gateway or reverse proxy. The ASP.NET Core API owns local authentication and completes the OIDC authorization-code flow.

Browser sessions use `Secure`, `HttpOnly`, `SameSite=Lax` cookies. Unsafe requests require an antiforgery token in a custom header and validated Origin or Referer metadata. Do not store access or refresh tokens in browser local storage or session storage. Disable CORS by default and allow only explicit trusted origins for deployments that cannot use the same-origin route.

CLI and service clients use scoped revocable bearer tokens. Agent enrollment uses a short-lived one-time token followed by renewable mutually authenticated identity.

Start the HTTP API at `/api/v1` with one reviewed OpenAPI document per supported major. Compatible additive changes remain within a major; breaking changes require a new major route and migration plan. Version Agent negotiation independently with major and minor values. Give check configurations, results, and external events explicit schema versions. Version packages and generated clients with SemVer 2.0 and release OCI images with immutable version tags and digests.

## Consequences

- The production browser does not need to persist bearer or refresh tokens.
- Static frontend and API deployments remain operationally separate while sharing one public origin.
- Cross-origin deployments require explicit additional security configuration.
- Public compatibility becomes a release concern from the first endpoint and Agent message.
