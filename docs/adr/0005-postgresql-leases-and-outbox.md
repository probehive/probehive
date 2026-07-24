# 0005: PostgreSQL, Leases, and Outbox Baseline

- Status: Accepted
- Date: 2026-07-22
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

The initial product needs durable tenant configuration, high-volume observations, reliable scheduling, and external effects without making self-hosting depend on multiple distributed systems.

## Decision

Use PostgreSQL as the initial production database with EF Core and Npgsql. Store relational configuration, identity, incidents, alerts, audit data, Runs, and bounded Observations in PostgreSQL. Define retention and partitioning before collecting high-volume data.

Use PostgreSQL-backed leases for scheduling and an outbox for reliable effects that follow committed state changes. Scheduling and ingestion must tolerate retries, duplicate delivery, restarts, clock differences, and partial failure through stable identifiers and idempotency.

Do not add Redis, RabbitMQ, Kafka, Quartz, Hangfire, a second database provider, or a specialized time-series store until measured requirements justify the operational cost.

## Consequences

- The initial self-hosted deployment has one durable data dependency.
- Lease and outbox correctness becomes a first-class database design concern.
- PostgreSQL scaling, partitioning, retention, backup, and restore must be validated before production use.
- A future specialized store or broker requires workload evidence and a new ADR.
