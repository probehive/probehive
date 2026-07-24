# 0001: License and Open-Core Boundary

- Status: Accepted
- Date: 2026-07-22
- Clarified by: [ADR 0011](0011-third-party-licenses-and-notices.md)

## Context

ProbeHive must be genuinely self-hostable and easy for enterprises to evaluate, modify, integrate, and redistribute. The official hosted service also needs a clear private boundary for commercial operations.

## Decision

License the complete public `probehive` repository under Apache-2.0, including the server, workers, checks, Agent, CLI, React application, public contracts, schemas, generated clients, documentation, and deployment assets unless a third-party artifact requires a compatible notice.

Keep hosted signup, subscriptions, billing, metering, managed-fleet operations, abuse controls, support tooling, compliance operations, and production infrastructure implementation in separate proprietary repositories. Use Developer Certificate of Origin sign-off and a separate trademark policy. Do not require a contributor license agreement without a later concrete relicensing need and legal review.

## Consequences

- The public product remains complete and usable without a hosted account.
- Enterprises receive an explicit patent grant and a permissive integration path.
- Competitors may privately modify or host the public core.
- Commercial differentiation must come from the managed network, operational quality, convenience, retention, support, compliance, and execution rather than a deliberately crippled public edition.
- Legal review remains required before the first public release, external contributions, or paid service launch.
