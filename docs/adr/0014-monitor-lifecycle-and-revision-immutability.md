# 0014: Monitor Lifecycle, Revision Immutability, and Check Configuration Versioning

- Status: Accepted
- Date: 2026-07-24
- Clarifies: [ADR 0003](0003-organization-project-monitor-ownership.md)
- Amended by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)

## Context

ADR 0003 fixes the ownership path `Organization -> Project -> Monitor -> Monitor Revision -> Run -> Observation`, and ADR 0010 requires every check configuration schema to carry an explicit integer version. Before the first Monitor implementation ships, the exact lifecycle states, the immutability contract of a revision, the versioning rules of check configuration, and the validation boundary between configuration time and execution time (ADR 0007) must be recorded so that scheduling, execution, and monitoring-as-code can later build on stable semantics.

## Decision

### Monitor identity and lifecycle

A Monitor is the long-lived identity and lifecycle owned by exactly one Project. It carries its Organization identity explicitly in addition to its Project (ADR 0009), a trimmed Unicode display name of 1 to 100 characters, and one Check Type fixed at creation. Changing the Check Type of an existing Monitor is not an update; it is a new Monitor. Monitor names are not unique; identity comes from the Monitor id.

The Monitor lifecycle uses exactly these states and transitions:

```text
Draft ----activate----> Active <---activate--- Paused
  |                       |  \----pause-----------^
  |                       |
  +-------archive---------+---------archive-------+--- from Draft, Active, or Paused
                                                  v
                                              Archived (terminal)
```

- A new Monitor starts in `Draft` with no revisions.
- `activate` (from `Draft` or `Paused`) requires at least one revision; a Monitor without configuration can never become `Active`.
- `pause` is valid only from `Active`.
- `archive` is valid from every state except `Archived` and is terminal. Unarchiving and hard deletion are deliberately deferred until Run retention and deletion semantics exist; `Archived` is the current soft-delete.
- An `Archived` Monitor is read-only: no rename, no state change, no new revisions.
- Renaming is valid in every non-archived state and never changes lifecycle state.

Whether a Monitor may move between Projects or Organizations remains an open owner decision (ADR 0003); no move operation exists.

### Monitor Revision immutability

A Monitor Revision is an immutable snapshot of check configuration. Revisions are append-only: no update or delete operation exists at any layer, and future Runs reference the exact revision they executed.

- Revision numbers are integers starting at 1 and strictly monotonic per Monitor; the database enforces uniqueness of `(monitor_id, revision_number)`.
- The Monitor row tracks `latest_revision_number` (0 when none). It is the allocation source for the next revision number and the definition selected when execution later starts; Runs will bind to a specific revision, so replacing configuration mid-flight never mutates history.
- A revision may be added in every non-archived state, including `Paused`; adding a revision never changes lifecycle state.
- Each revision carries the Monitor's Check Type, an integer check configuration schema version, and the configuration document persisted as canonicalized JSON (`jsonb`), plus explicit Organization identity.
- Concurrent mutation of one Monitor (revision creation, rename, state change) is serialized by optimistic concurrency on the Monitor row (PostgreSQL `xmin`), with the revision-number unique index as a backstop. A losing request receives `409 Conflict` and retries against fresh state; the API never silently reorders or renumbers revisions.

### Check configuration schema versioning

Every Check Type has an explicit integer configuration schema version (ADR 0010). Version 1 of a Check Type is its first accepted shape. Any change to accepted shape or meaning — including adding an accepted optional field — increments the integer version; the validator for a given version accepts exactly the fields that version defines and rejects unknown fields so that misspelled or unsupported configuration fails loudly instead of being silently ignored. A revision records the schema version its configuration was validated against. The API accepts only schema versions the running build supports.

Check configuration validation logic lives in `ProbeHive.Checks`. The Application layer defines a `ICheckConfigurationValidator` port and never depends on `ProbeHive.Checks`; the composition root (API host) adapts the Checks validator to that port. `ProbeHive.Checks` stays within its recorded dependency boundary (Contracts only; no ASP.NET Core, EF Core, or host dependencies).

### HTTP check, configuration schema version 1

The first Check Type is `http`. Schema version 1 validates configuration only; execution, scheduling, and the outbound policy engine are separate future work. Fields (wire names, camelCase):

| Field | Rules |
| --- | --- |
| `url` | Required. Absolute `http` or `https` URI, no user information, no fragment, at most 2048 characters. Literal IP hosts are accepted at configuration time. |
| `method` | Optional, default `GET`. One of `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`. `TRACE` and `CONNECT` are rejected. |
| `expectedStatusCodes` | Optional array of at most 20 distinct integers, each 100–599. Empty or omitted means "any 200–299". |
| `timeoutSeconds` | Optional integer 1–60, default 30. The fixed 60-second ceiling is the platform ceiling for schema version 1; configurable operator ceilings (ADR 0007) will layer on top and may only lower it. |
| `followRedirects` | Optional boolean, default `true`. |
| `maxRedirects` | Optional integer 0–10, default 5. |
| `headers` | Optional array of at most 20 `{name, value}` pairs. Names are RFC 9110 tokens of at most 128 characters, unique case-insensitively. Values are at most 1024 characters with no control characters. `Authorization`, `Proxy-Authorization`, `Cookie`, `Host`, `Content-Length`, and `Transfer-Encoding` are rejected: secret material belongs in future secret references (never inline configuration), and connection-integrity headers are owned by the executor. |

A request body is not part of schema version 1. The serialized configuration document is limited to 16 KiB. Address-policy screening (private ranges, metadata endpoints, redirect re-validation) is deliberately not a configuration-time check: it depends on resolution at execution time and on the active policy profile, and it is enforced by the shared outbound-access policy of ADR 0007 for every connection attempt. Configuration-time validation enforces shape, bounds, and the header deny list only.

### API surface

Monitor endpoints nest under their owners so every route carries explicit tenant scope:

```text
POST   /api/v1/organizations/{organizationId}/projects/{projectId}/monitors
GET    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors
GET    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}
PUT    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name
PUT    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state
POST   /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions
GET    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions
GET    /api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}
```

Lifecycle states serialize on the wire as lowercase strings (`draft`, `active`, `paused`, `archived`). The state endpoint accepts target states `active`, `paused`, and `archived`; `draft` is never a target. An invalid target shape is `400`; a valid target that the state machine rejects (including activation without a revision) is `409` with Problem Details. All endpoints require the instance Administrator role until Organization membership exists (ADR 0013) and sit behind the browser-session antiforgery model (ADR 0010).

### Tenant integrity in persistence

`monitors` carries `organization_id` and `project_id`; a composite foreign key `(project_id, organization_id)` against a unique key on `projects (id, organization_id)` makes a cross-tenant Project reference unrepresentable. `monitor_revisions` carries `organization_id` explicitly and references its Monitor with cascade delete. All uniqueness rules and queries include Organization identity (ADR 0009).

## Consequences

- Runs and scheduling can later reference immutable revisions without copy-on-execute machinery.
- Strict versioned validation makes configuration typos and unsupported fields visible at creation time instead of at execution time.
- New accepted fields require a new schema version, which keeps older stored revisions unambiguous forever.
- Archived remains the only removal mechanism until retention semantics exist, so no data is destroyed ahead of a recorded deletion design.
- The Checks project starts as a validation library; execution lands behind the same Check Type and schema version identity later.
