# 0020: Check Execution Placement and Outbound Access Enforcement

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0002](0002-modular-monolith-and-project-topology.md), [ADR 0007](0007-outbound-access-and-ssrf-security.md)
- Clarified by: [ADR 0023](0023-outbound-policy-classification-and-denial-reasons.md), [ADR 0024](0024-http-check-execution-and-observation-content.md)

## Context

Nothing in the product performs a check yet. `internal/check` validates configuration and stops there, and that is the whole of the execution story.

Two recorded decisions collide the moment execution starts. ADR 0002 says feature packages and `internal/check` import no HTTP package, so the executor cannot live beside the configuration it executes — and the architecture guard now enforces that, rather than leaving it to discipline. ADR 0007 specifies the outbound-access policy in full — canonicalize, resolve through an operator-controlled resolver, classify every candidate address, bind the connection to a validated address while preserving Host and SNI, revalidate every redirect, fail closed — but says nothing about where that logic lives or what its interface is.

Outbound execution is the highest-risk surface in the product. Deciding its placement while writing the first probe would mean deciding it under pressure, in the same change that has to get the security right.

## Decision

### Two new packages, with the security core kept small

```text
internal/outbound/   standard-library-only policy engine and validating dialer
internal/probe/      protocol execution; may use HTTP and other protocol clients
```

`internal/outbound` owns everything ADR 0007 specifies about *where a connection may go*: policy profiles, address classification against special-purpose registries and the metadata deny list, resolution through the operator-configured trusted resolver, and a dialer that connects only to an address the active profile allows.

It is standard-library-only and imports no HTTP or SQL package. `net`, `net/netip`, and `crypto/tls` are the tools it needs; it never learns what protocol is being spoken over the connection it returns. That constraint is deliberate: it keeps the part that must be right small enough to review, and it lets the whole policy be tested with local listeners and a stub resolver, as ADR 0007 requires, without an HTTP server in the loop.

`internal/probe` owns protocol execution. It converts a validated configuration from `internal/check` plus an outbound dialer into an Observation. Its HTTP implementation builds an `http.Transport` whose `DialContext` is the outbound dialer, which is what makes the policy unavoidable rather than advisory: there is no code path from a probe to a socket that does not pass through it.

### Dependency direction

- `internal/probe` may import `internal/check` and `internal/outbound`. Neither imports `internal/probe`.
- `internal/probe` and `internal/outbound` import no persistence, HTTP-API, or composition package. They receive their configuration; they do not read it.
- `internal/outbound` joins the standard-library-only set the architecture guard checks. `internal/probe` does not, because HTTP execution is its purpose, but it stays barred from persistence and composition packages.
- `internal/check` is unchanged and stays validation-only. A Check Type is defined in one place and executed in another, joined by the Check Type identifier and integer schema version of ADR 0014.

### The dialer is the enforcement point

Every outbound connection attempt, including each redirect hop and each address-family fallback, goes through one validating dial:

1. Canonicalize scheme, authority, port, and user information; reject oversized input.
2. Resolve through the operator-configured resolver. This resolver is never tenant-selectable. A tenant-selected resolver is only ever the *destination* of a DNS check, validated like any other target.
3. Classify every candidate address, including IPv4-mapped and transition forms, against the active profile and the metadata deny list.
4. Dial only allowed addresses, binding the connection to the address that was validated, so nothing re-resolves between the check and the connect.
5. Keep the intended host name for the HTTP Host header and TLS SNI.
6. Fail closed when no candidate remains; never fall back to an unvalidated address.

Connection reuse stays inside the validated authority and policy scope. The HTTP client sets `CheckRedirect` so each hop re-enters this sequence rather than trusting the transport's own redirect handling.

Profiles are `managed` (denies loopback, link-local, private, reserved, multicast, benchmark, documentation, and transition ranges with no tenant exceptions), `private` (adds operator-selected CIDR ranges, still denying metadata endpoints), and `operator` (for operator-configured infrastructure integrations, never reachable from tenant configuration). The default for a self-hosted installation is `private` with an empty allow list, which behaves like `managed` until an operator opts in.

### Operator ceilings are configuration, not code

Ceilings for total execution time, per-phase timeouts, redirect count, allowed ports, response and body sizes, and concurrency are read from operator configuration and passed into the executor. User configuration may only be stricter, and the API already rejects configuration above the schema-v1 bounds; the executor enforces the effective value again at execution time so a stale or hand-edited stored revision cannot exceed the ceiling.

### Execution runs in-process first

Execution starts inside `cmd/probehive` as an embedded worker with a bounded pool, matching the blueprint's intent that the simplest self-hosted installation is one process. It can be switched off so an operator can run an API-only replica.

A sibling `cmd/probehive-worker` and the remote Agent protocol are deliberately not created now. ADR 0002 already allows sibling commands once they gain real behavior, and both are additive: `internal/probe` takes a configuration and returns an Observation, which is the same shape whether the caller is in-process or a remote Agent.

### Not decided here

Run and Observation persistence, scheduling, leases, retention, and confirmation policy belong to their own decision. Check Types beyond HTTP, browser execution, and the Agent wire protocol each need their own record; ADR 0007 already requires a threat model before a new outbound protocol category.

## Consequences

- The rule that made the obvious placement illegal now has a legal placement, decided before the first probe rather than during it.
- The security-critical code is one small standard-library-only package that can be reviewed and fuzzed on its own.
- A probe cannot reach a socket without passing the policy, because the policy *is* the dialer rather than a check the protocol code is expected to call.
- Two more packages exist, and the architecture guard has one more entry to maintain.
- In-process execution means a misbehaving probe consumes the API process's resources; bounded concurrency and enforced ceilings are what keep that acceptable until a separate worker exists.
