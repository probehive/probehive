# 0023: Outbound Address Classification, Overrides, and Denial Reasons

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0007](0007-outbound-access-and-ssrf-security.md), [ADR 0020](0020-check-execution-and-outbound-enforcement.md)

## Context

ADR 0007 decided the outbound-access policy and ADR 0020 decided where it lives and what its
interface is. Writing `internal/outbound` left four questions that neither record answers and
that a reader of the code should not have to reconstruct from the code:

1. Which address ranges each class covers, and where a cloud metadata endpoint that is not
   link-local fits.
2. Whether an operator allow list may reinstate *any* denied range, given that ADR 0020 says
   the private profile "adds operator-selected CIDR ranges, still denying metadata endpoints"
   and names nothing else as absolute.
3. What a caller receives when a destination is denied, since an Observation has to record a
   reason and a UI has to show one.
4. What the default port ceiling is when operator configuration names no ports.

These are security decisions with quiet failure modes. An allow list that can reinstate the
IPv4-in-IPv6 transition ranges reopens IPv4 classification through a side door; a denial that
carries the request URL puts a secret-bearing URL in a log.

## Decision

### Classification follows the IANA registries, plus a named metadata list

Addresses are classified against the IANA IPv4 and IPv6 Special-Purpose Address Registries.
An IPv4-mapped IPv6 address is unmapped before classification, so it is judged as the IPv4
address it carries. Classification takes the longest matching prefix, so a specific entry
always beats the wider range containing it, whatever order the table is written in.

Cloud instance-metadata endpoints are listed as individual addresses and classified as
`metadata` ahead of any range. Most of them sit inside a link-local or unique-local range and
would be denied anyway; they are named because an operator allow list may legitimately open
those ranges on a private location. Azure's host agent at `168.63.129.16` is listed even
though it looks globally routable, because it is reachable only from inside a virtual network
and is a metadata endpoint in everything but address range.

### An allow list reinstates only the classes a real network is built from

An operator allow list on the private or operator profile may reinstate `loopback`,
`private`, `linkLocal`, `uniqueLocal`, `sharedAddressSpace`, `protocolAssignment`,
`documentation`, and `benchmark`.

It may never reinstate `metadata`, `transition`, `multicast`, `reserved`, `discard`,
`unspecified`, or a zoned address — not even an allow list of `0.0.0.0/0` and `::/0`. This is
stricter than ADR 0007 and ADR 0020 require, which permit a stricter rule. The transition
ranges (`2002::/16`, `2001::/32`, `64:ff9b::/96`, `64:ff9b:1::/48`, `192.88.99.0/24`) are
absolute for the same reason metadata is: each embeds an IPv4 destination inside an IPv6
address, so admitting them would let a denied IPv4 destination be reached through an address
that classifies as IPv6. The rest are not destinations a check can meaningfully reach.

An installation that genuinely needs to monitor through an operator-run NAT64 network needs
its own decision and its own threat model; it does not get there through the allow list.

### Denials carry a stable reason and no URL detail

Every denial is an `outbound.*` reason with the same stability guarantee as the ADR 0019
error codes: the identifier is contract, the English text beside it is documentation. An
address denial also carries the class that denied it, which is what makes "we blocked your
monitor" explainable without guesswork.

A denial carries the destination host, port, and address, and nothing else. It never carries
a URL path, query, fragment, or user information, so a denial is safe to record in an
Observation, return through the API, and write to a log without a redaction step that a later
change could forget.

A connection that lands on an address other than the one that was validated is its own
reason, not a connect failure, and is never retried against the next candidate.

### Bounds and defaults

The default port ceiling, when operator configuration names none, is 80 and 443 — the ports
the only current check type uses. Widening it is operator configuration, not a code change.

One resolver answer contributes at most 16 candidate addresses, so a hostile or broken name
server cannot turn one connection attempt into an unbounded series of dials.

A host name whose final label is entirely numeric is rejected. Such a name cannot be a valid
DNS name, and rejecting it stops a numeric form that Go's address parser refuses — a
zero-padded, decimal, or hexadecimal IPv4 spelling — from being handed to a resolver that
parses it more liberally than Go does.

A host name must be ASCII, so an internationalized domain name is supplied in its punycode
form. Converting one requires a dependency the standard-library-only rule of ADR 0020 does
not allow in this package, and the same restriction rejects the characters a resolver might
fold into a label separator.

## Consequences

- The reviewable part of the security core stays a table and a decision function, and the
  reasoning behind each entry is recorded next to the decision rather than in commit history.
- An operator cannot reach a metadata endpoint or a transition range by mistake, and cannot
  reach one deliberately without a new decision.
- Adding a cloud provider's metadata endpoint is a one-line change to a named list, which is
  the kind of change that should be easy.
- The default port ceiling will be too narrow for some self-hosted installations, which is
  the intended direction of the error: an operator widens it explicitly.
- A Monitor whose URL carries an internationalized host in Unicode form is rejected at
  execution. If that becomes a real requirement, converting it belongs where the URL is
  accepted, not inside the policy engine.
