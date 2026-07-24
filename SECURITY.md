# Security Policy

ProbeHive executes outbound network checks and processes tenant-scoped monitoring data. Security reports are treated as a high priority, especially when they involve SSRF, DNS rebinding, cloud metadata access, authentication, authorization, tenant isolation, Agent identity, secret exposure, unsafe redirects, or resource exhaustion.

## Supported Versions

ProbeHive has not published a supported release yet. This policy will list supported release lines before the first public release.

## Reporting a Vulnerability

Do not report a vulnerability through a public issue, discussion, pull request, or social channel.

Use GitHub private vulnerability reporting or a private security advisory for this repository when available. If that facility is unavailable, contact the repository owner privately through GitHub and request a secure reporting channel. Do not include exploit details or secrets in the initial public-facing contact.

Include, when possible:

- The affected revision or release.
- The affected component and deployment mode.
- Reproduction steps or a minimal proof of concept.
- Expected and observed behavior.
- Security impact and prerequisites.
- Suggested mitigations, if known.

## Response and Disclosure

Maintainers will acknowledge reports as capacity permits, validate the issue, coordinate a fix, and agree on disclosure timing with the reporter. Do not disclose an unresolved vulnerability publicly before maintainers have had a reasonable opportunity to investigate and release a correction.

No response-time or remediation-time guarantee is offered before a supported release and published security service policy exist.

## Deployment Responsibility

Self-hosting operators remain responsible for secure deployment configuration, supported versions, TLS termination, network policy, backups, secret injection, and timely upgrades. Product documentation must not silently weaken these controls for convenience.
