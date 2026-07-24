# 0006: Official Cloud Runtime Integration Boundary

- Status: Accepted
- Date: 2026-07-22
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

The official hosted service needs the same monitoring behavior as self-hosted ProbeHive without creating a private fork or making the public repository depend on private source.

## Decision

The public repository publishes the complete monitoring core, versioned HTTP APIs, authenticated events, generated clients, deliberately published wire-contract packages, and OCI artifacts.

The official Cloud deploys released public API, Worker, and Agent artifacts in shared multi-tenant service pools. Private services integrate only through documented runtime boundaries. They must not add cross-repository project references, source links, direct public-core database access, copied source, or in-process references to public Domain, Application, Infrastructure, API, or other implementation assemblies.

The public core owns Organization-scoped Monitors, Runs, Observations, Incidents, Alerts, Status Components, Agent enrollment, tenant isolation, fair scheduling, and generic resource limits. Private services own hosted account lifecycle, subscriptions, billing, metering, hosted quota values, managed-fleet operations, abuse controls, support, and compliance records.

## Consequences

- Public code never requires a private feed, private project, or hosted account.
- Cloud deployments pin and validate released public contracts and artifacts independently.
- Private data stores have separate credentials and ownership and do not mutate public-core tables directly.
- Any future in-process reuse of public implementation assemblies is a new owner decision and ADR.
