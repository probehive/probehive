# 0007: Outbound Access and SSRF Security

- Status: Accepted
- Date: 2026-07-23
- Amended: 2026-07-23 — Distinguished tenant-selected resolvers as DNS check destinations from the operator-controlled trusted resolver used for validation resolution.

## Context

Probe execution is not the only outbound path influenced by untrusted data. HTTP redirects, tenant-configured webhooks, notification integrations, future browser journeys, and future protocol checks can all become SSRF, metadata-access, credential-exfiltration, or egress-abuse paths.

## Decision

Apply one shared outbound-access policy to every destination influenced by a tenant, monitored target, or user-provided configuration. Protocol implementations may add stricter rules, but they must not bypass the shared policy.

Use explicit policy profiles:

- Managed locations deny loopback, link-local, private, reserved, multicast, benchmark, documentation, transition, and other special-purpose address ranges without tenant exceptions.
- User-operated private locations may allow operator-selected private CIDR ranges. Cloud metadata endpoints remain denied by every built-in profile.
- Operator-configured infrastructure integrations, such as a self-hosted private OIDC provider, use a separate operator-trusted profile and are never made tenant-configurable implicitly.

For every new connection attempt and redirect:

1. Canonicalize and validate the scheme, authority, port, user information, and input size.
2. Resolve through the operator-configured trusted resolver. This validation resolver is operator-controlled and never tenant-selectable.
3. Normalize and classify every candidate address, including IPv4-mapped IPv6 and transition forms, against current special-purpose registries and the explicit metadata deny list.
4. Attempt only addresses allowed by the active policy profile.
5. Bind the connection to the allowed address while retaining the intended HTTP Host value and TLS SNI name.
6. Fail closed when no allowed candidate remains; never fall back to an unvalidated address.

Connection reuse must remain within the same validated authority and policy scope. Redirects, new connections, and address-family fallback repeat validation. Tenant-controlled proxies are not allowed in managed locations. A tenant-selected resolver is permitted only as the destination of a DNS check, including resolver comparison, where it is validated against the active policy profile like any other target: managed locations deny private, special-purpose, and metadata addresses, and port and protocol limits apply. A tenant-selected resolver never performs validation resolution for other check types or destinations. Any operator-configured proxy must preserve the effective destination policy or be paired with network-layer enforcement that cannot be bypassed by the application.

Application validation is mandatory even when network-layer egress controls exist. Managed deployments also enforce policy at the network layer as defense in depth.

Operator ceilings define the maximum timeout, redirects, ports, concurrency, request and response sizes, bandwidth, retained artifacts, and total execution budget. User configuration may be stricter but cannot exceed those ceilings. The API rejects invalid configuration rather than silently expanding or truncating it, the control plane records the bounded effective policy, and the executing Worker or Agent enforces it. An Agent may apply stricter local limits.

Outbound logs, observations, artifacts, and delivery records use deterministic redaction. Tests use local protocol fixtures and cover redirects, mixed address families, DNS changes, metadata destinations, proxy behavior, blocked ports, and resource exhaustion without using the public internet.

## Consequences

- Probe and notification features share one security boundary instead of implementing independent allow and deny logic.
- Private-network monitoring remains possible only through an explicitly configured private-location policy.
- Metadata access cannot be enabled casually through ordinary monitor configuration.
- New outbound protocol categories require a threat model and policy integration before implementation.
