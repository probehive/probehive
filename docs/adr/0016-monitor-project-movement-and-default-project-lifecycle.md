# 0016: Monitor Project Movement and Default Project Lifecycle

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0003](0003-organization-project-monitor-ownership.md), [ADR 0014](0014-monitor-lifecycle-and-revision-immutability.md)

## Context

ADR 0003 fixed the ownership path `Organization -> Project -> Monitor` and left open whether a Monitor may move between Projects or Organizations. ADR 0014 repeated that the question was unresolved and shipped Monitor persistence without answering it. The lifecycle of the default Project — whether it can be archived, deleted, or demoted — was never recorded at all.

Both questions had to be settled before the persistence model froze. It has now frozen: migration `0001_initial.sql` is applied, `monitors` carries a composite foreign key to `projects (id, organization_id)`, and `monitor_revisions` carries `organization_id` explicitly. Run, Observation, Incident, and Alert tables will all carry Organization identity on the same pattern, so every additional table makes an unrecorded answer more expensive to change.

## Decision

### Movement between Organizations is permanently excluded

A Monitor never moves between Organizations. The Organization is the tenant, authorization, and data-isolation boundary, and Monitor Revisions, Runs, Observations, Incidents, and Alerts each carry Organization identity explicitly under ADR 0009. Re-parenting a Monitor would rewrite tenant-scoped history, invalidate every authorization decision already recorded against it, and make retention and deletion ambiguous.

Moving monitoring configuration between tenants is an export and import problem, not an update. No move-between-Organizations operation exists at any layer, and adding one requires a new ADR that also answers what happens to historical Runs and Observations.

### Movement between Projects inside one Organization is allowed

A Monitor may move to another Project of the same Organization through an explicit dedicated operation. It is never a side effect of rename, state change, or a general update, so an ordinary edit can never silently re-parent a Monitor.

The reserved surface is:

```text
PUT /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/project
```

Its semantics are fixed here even though it is not implemented yet:

- The target Project must belong to the same Organization. The composite foreign key `fk_monitors_projects (project_id, organization_id)` already makes a cross-tenant target unrepresentable in storage; the use case rejects it before reaching persistence, and an unknown or out-of-Organization target is `404` like every other misaddressed identifier.
- It is valid in every non-archived state. An archived Monitor is read-only under ADR 0014, so the operation is `409` with the existing archived detail.
- It does not change lifecycle state, revision numbering, or revision history. Revisions stay bound to their Monitor and keep their Organization identity; only `monitors.project_id` changes.
- It advances `updatedAt` and uses the same optimistic-concurrency contract as rename and state change: the loaded `xmin` is the update predicate, and a lost race is `409` with the existing concurrent-modification detail.

No migration is required. `monitors.project_id` is already a mutable column guarded by the composite foreign key, which is exactly the shape this decision needs.

### The default Project is permanent

Every Organization has exactly one default Project, created transactionally with the Organization by the idempotent provisioning use case (ADR 0012), and the partial unique index `ux_projects_organization_default` enforces the "at most one" half of that rule.

The default Project cannot be deleted, archived, or demoted, and `is_default` is never cleared for the lifetime of its Organization. Reassigning the default to a different Project is deliberately excluded: the default Project exists so that provisioning is idempotent and a newly created Organization can accept a Monitor immediately, and a movable default would reintroduce the ordering problem it was created to remove.

### Archival and deletion of other Projects remain deferred

Creating additional Projects, archiving them, and deleting them are deferred on the same grounds ADR 0014 used to defer Monitor hard deletion: they depend on Run and Observation retention and deletion semantics that do not exist yet. Deciding how to remove a Project before deciding what happens to the observations underneath it would produce either orphaned history or an undeclared destructive cascade.

The `ON DELETE CASCADE` clauses in migration `0001_initial.sql` are referential-integrity constraints that keep a partially deleted tenant unrepresentable. They are not a published deletion behavior, and no API operation deletes an Organization, a Project, or a Monitor. Organization deletion, tenant export, and the retention model that governs both are future decisions.

## Consequences

- The ownership path is fully specified, so Run, Observation, Incident, and Alert schemas can be designed against a settled answer.
- Tenant history can never be re-parented, which keeps every recorded authorization decision meaningful.
- Re-parenting inside an Organization has a reserved shape, so implementing it later adds an endpoint rather than reopening the domain model.
- The default Project stays a dependable anchor for onboarding, hosted provisioning, and idempotent replay.
- Nothing in the product can destroy tenant data until a retention and deletion design exists.
