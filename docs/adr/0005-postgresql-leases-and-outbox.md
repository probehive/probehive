# 0005: PostgreSQL, Leases, and Outbox Baseline

- Status: Accepted
- Date: 2026-07-22
- Clarified by: [ADR 0021](0021-run-observation-retention-and-scheduling.md)

## Context

The initial product needs durable tenant configuration, high-volume observations, reliable scheduling, and external effects without making self-hosting depend on multiple distributed systems.

## Decision

Use PostgreSQL as the initial production database through explicit SQL with pgx and pgxpool. Store relational configuration, identity, incidents, alerts, audit data, Runs, and bounded Observations in PostgreSQL. Apply embedded, sequential SQL migrations transactionally and define retention and partitioning before collecting high-volume data.

Use PostgreSQL-backed leases for scheduling and an outbox for reliable effects that follow committed state changes. Scheduling and ingestion must tolerate retries, duplicate delivery, restarts, clock differences, and partial failure through stable identifiers and idempotency.

Do not add Redis, RabbitMQ, Kafka, another job or scheduling framework, a second database provider, or a specialized time-series store until measured requirements justify the operational cost.

## Consequences

- The initial self-hosted deployment has one durable data dependency.
- Lease and outbox correctness becomes a first-class database design concern.
- PostgreSQL scaling, partitioning, retention, backup, and restore must be validated before production use.
- A future specialized store or broker requires workload evidence and a new ADR.
