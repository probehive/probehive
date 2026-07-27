# 0024: HTTP Check Execution and Observation Content

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0020](0020-check-execution-and-outbound-enforcement.md), [ADR 0021](0021-run-observation-retention-and-scheduling.md)

## Context

ADR 0020 decided that `internal/probe` converts a validated configuration plus an outbound
dialer into an Observation, and ADR 0021 decided what an Observation is *for*: the bounded
detail of one Run, holding phase timings, assertion results, protocol details, and
diagnostics, redacted before it is stored rather than after.

Neither says what the first executor actually produces. Writing it raised questions that a
reader should not have to reconstruct from an HTTP client's configuration:

1. What an Observation contains, and what it deliberately does not contain, given ADR 0021's
   rule that redaction happens before persistence.
2. How a Run outcome is chosen, since ADR 0021 requires an assertion failure, a timeout, a
   cancellation, and an internal error to stay distinguishable.
3. What is measured, when the connection is established inside a package that must stay a
   policy engine and not grow timing hooks.
4. Which limits the executor enforces again at execution time, and what happens when a stored
   revision disagrees with the operator ceiling.
5. How redirects, connection reuse, proxies, and TLS verification behave, each of which is a
   way to leave the validated path without noticing.

## Decision

### An Observation carries codes and numbers, never free text

An Observation records the outcome, the wall-clock instants the execution started and
finished, its elapsed duration, phase timings, and — when a response arrived — the status
code, protocol, redirect count, response body length, whether that length hit the ceiling,
and the negotiated TLS version, cipher suite, and leaf-certificate expiry.

A non-passed Observation carries a stable code and, for an outbound denial, the class that
denied it. It carries no message string, no target-supplied text, no response headers, and no
response body. This is the same contract as the ADR 0019 error codes and the ADR 0023 denial
reasons: the identifier is contract, the English text beside it is documentation.

The reason is ADR 0021's ordering requirement. An Observation that contains no free text and
no target-supplied bytes has nothing to redact, so "redaction runs before persistence" is
satisfied by construction rather than by a redaction step a later change could forget. When
an assertion needs response content — a body match, a header assertion — the content it
retains is bounded and redacted at that point, and that is a decision for the check type that
introduces it.

The response body is read to the operator ceiling and discarded. Reading it is what makes the
size and the transfer real rather than a header claim, and bounding the read is what stops a
hostile target from making the probe pay for an unbounded response. Only the byte count and a
truncation flag survive.

Run identity, Organization identity, Probe Location, and persistence belong to the caller
under ADR 0021. The executor returns the detail of one execution and knows nothing about the
Run it will become part of.

### Outcomes

`passed` and `failed` are the target's answer: the request completed and the status assertion
either accepted it or did not. `timedout` is the effective execution deadline expiring.
`cancelled` is the caller's context being cancelled, which is what a graceful shutdown or a
lost lease looks like. Everything else is `errored`: a denied destination, a resolution
failure, a connection failure, a TLS failure, an unusable stored configuration, or a redirect
budget exhausted.

`skipped` is ADR 0021's scheduler outcome and is never produced by an executor.

Execution therefore has no error return. A failure to reach a target is the measurement, not
an error in taking it, and returning it as an Observation removes the possibility that a
caller records an outcome and a returned error that disagree.

### Phase timings describe the first connection and the first response

Recorded phases are the time to establish the connection, the TLS handshake, and the time to
the first response byte, each measured from the first hop, with the total covering the whole
execution including redirects. First-hop values are what "how fast does my endpoint answer"
means; a later hop may reuse a connection and produce a zero that reads as an anomaly rather
than a reuse.

Resolution time is not reported separately. Resolution happens inside `internal/outbound`,
whose value under ADR 0020 is being small enough to review, and a separate DNS timing is not
worth adding timing callbacks to the package that decides where a connection may go. It is
counted inside the connect phase, and splitting it later is additive.

Durations are measured monotonically; the two instants come from the injected clock and are
the wall-clock record, so a clock step changes the recorded instants and never the measured
latency.

### Effective limits are the stricter of user and operator

The effective timeout and redirect budget are the smaller of the stored configuration's value
and the operator ceiling. The API already rejects configuration above the schema bounds, so
clamping is not the normal path — it is what makes a stale or hand-edited revision harmless
rather than authoritative, as ADR 0020 requires.

A zero or absent ceiling is not "unbounded": every ceiling falls back to a built-in default,
because an executor that treats missing configuration as no limit fails open.

`followRedirects: false` means the redirect response *is* the answer and is asserted like any
other status. `maxRedirects: 0` with following enabled means the budget is zero, so the first
redirect exhausts it and the Run is `errored` with a redirect-budget code. The distinction is
deliberate: one says do not follow, the other says following was allowed and ran out.

### Leaving the validated path is closed off explicitly

- The HTTP transport dials only through the outbound dialer, so there is no path to a socket
  that skips the policy.
- Proxy support is disabled explicitly rather than left at the client default, which reads
  proxy environment variables. An environment proxy would send every request to an address
  the policy never validated, which is the whole enforcement point inverted.
- Every redirect hop re-enters `Policy.CheckURL` before it is followed and the dialer again
  when it is dialed.
- Each execution builds its own transport and closes its connections when it finishes, so a
  connection validated for one Run is never reused by another. Reuse inside one execution
  stays within the connection pool's own authority key, which is the scope ADR 0007 allows.
- Certificate verification is always on. An installation with an internal certificate
  authority supplies its roots as operator configuration; there is no setting that skips
  verification, because a monitoring product that can be told to ignore certificates will
  eventually be told to. A verification failure is a distinguishable outcome rather than a
  generic transport error, since an expired certificate is a thing operators monitor for.
- No cookie jar exists, so nothing a target sets survives a hop or a Run.

### Not decided here

Scheduling, leases, Run persistence, and retention remain ADR 0021's. Health evaluation,
confirmation runs, and incident lifecycle remain undecided. Check types beyond HTTP, body and
header assertions, certificate expiry thresholds, browser execution, and the Agent wire
protocol each need their own record; ADR 0007 still requires a threat model before a new
outbound protocol category.

## Consequences

- An Observation is safe to log, store, and return by construction, which is a stronger
  guarantee than a redaction pass and a cheaper one to keep true.
- The failure distinctions ADR 0021 promised exist from the first executor rather than being
  reconstructed later from timings.
- Operators lose a separate DNS timing until it is worth the callbacks, and lose the ability
  to disable certificate verification permanently.
- Building a transport per execution costs a connection setup per Run. At check frequencies
  that is a rounding error, and it buys the guarantee that no Run inherits another's socket.
- The response body is transferred and discarded, so a probe pays the bandwidth of a bounded
  read for a measurement it keeps only the size of. An operator lowers that ceiling; making
  it zero would make the size meaningless, which is why it is a ceiling and not a switch.
