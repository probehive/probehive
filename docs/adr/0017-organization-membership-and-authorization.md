# 0017: Organization Membership and Authorization

- Status: Proposed
- Date: 2026-07-27
- Would amend: [ADR 0013](0013-first-administrator-and-local-authentication.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0010](0010-browser-authentication-trust-and-compatibility.md), [ADR 0012](0012-organization-provisioning-idempotency.md)

## Context

ADR 0013 created instance-scoped `User` records with a single instance role, `Administrator`, and made every Organization and Monitor endpoint require it. It named Organization membership and per-Organization roles as a future ADR and stated plainly that they are required before non-administrator users can be authorized meaningfully.

That gap now blocks three separate things. The self-hosted product is single-administrator, so a team cannot use it. Cloud ADR 0004 requires the public API to authorize every monitoring operation from a validated Organization context, and there is currently no membership record for a hosted subject to map onto. And the instance `Administrator` role today doubles as unrestricted access to every Organization's data, which is a tenancy posture the product has never actually chosen.

This ADR proposes the model. It is deliberately `Proposed` rather than `Accepted`: it changes what authorization means on every existing tenant-scoped endpoint, and that is an owner decision. No implementation depends on it, and nothing in the repository should until it is accepted.

## Proposed Decision

### Membership is the unit of Organization authorization

An Organization membership joins one instance `User` to one `Organization` and carries exactly one Organization role. A user may belong to several Organizations with a different role in each, which is the shape cloud ADR 0004 already assumes.

```text
organization_members
  organization_id uuid   -- FK -> organizations (id) ON DELETE CASCADE
  user_id         uuid   -- FK -> users (id) ON DELETE CASCADE
  role            varchar(50)
  created_at      timestamptz
  PRIMARY KEY (organization_id, user_id)
```

The primary key makes a duplicate membership unrepresentable. "At least one Owner per Organization" cannot be expressed as a table constraint and is enforced in the use case, which rejects removing or demoting the last Owner.

### Organization roles

Four roles, chosen to cover the situations that exist today rather than to anticipate a policy engine:

| Role | May do |
| --- | --- |
| `Owner` | Everything an Administrator may do, plus transferring ownership and any future Organization-level destructive operation. At least one per Organization at all times. |
| `Administrator` | Manage members below Owner, Projects, Monitors, Agents, Probe Locations, and Organization settings. |
| `Member` | Create and configure Monitors and read everything in the Organization. No member management and no Organization settings. |
| `Viewer` | Read-only. |

`Member` exists because the common case is an engineer who should configure monitoring without administering people. Collapsing it into `Administrator` would force every practitioner to hold member-management rights.

Custom roles, per-Project roles, and fine-grained permission policy stay out. The blueprint places richer RBAC in Phase 2 and custom roles in Phase 5, and adding a role to this table later is additive.

### The instance role stops being implicit tenant access

The instance `Administrator` role keeps a narrow, enumerated meaning: completing first-administrator bootstrap, creating Organizations, and future instance-wide operation and configuration surfaces. It does **not** confer access to the monitoring data of an Organization the user is not a member of.

This is the part that changes existing behavior, and it is deliberate. ADR 0009 requires every tenant-scoped authorization decision to carry Organization identity explicitly; a role that silently satisfies all of them contradicts that. An operator who must inspect a tenant joins that Organization as a member, which produces a record. Standing cross-Organization support access, if it is ever wanted, is a separate audited capability in its own ADR — and in the hosted product it belongs to the private support tooling of cloud ADR 0001, not to a public super-user role.

### Authorization behavior

- Deny by default is unchanged. Every `/api/v1/organizations/{organizationId}/...` route resolves membership once per request and derives the effective Organization role from the persisted record.
- **Non-membership returns `404`, not `403`.** A real Organization the caller does not belong to must be indistinguishable from one that does not exist, matching the rule the backend contract already applies to identifiers presented through the wrong scope. `403` is reserved for a caller who *is* a member but whose role is insufficient — there, the resource's existence is already known.
- Membership never comes from a request header, a browser-selected context, or a hosted claim taken at face value. Under ADR 0010 and cloud ADR 0004 the API validates a signed assertion and then resolves membership from its own records.
- The current endpoints map as: Monitor and revision reads require `Viewer`; Monitor creation, rename, state change, and revision creation require `Member`; Organization settings and member management require `Administrator`.

### Provisioning creates the first membership

The idempotent provisioning use case of ADR 0012 gains one responsibility: the authenticated caller becomes the new Organization's `Owner` in the same transaction that creates the Organization and its default Project. There is no second path that creates an Organization without an Owner, which is the same single-use-case rule ADR 0009 applies to default Project creation.

Idempotent replay stays idempotent: replaying provisioning for an existing slug with an identical display name returns the existing Organization and performs no membership write, including when the replaying caller differs from the original Owner.

### Deliberately deferred

Each of these needs its own decision and none blocks the above: invitation and email-verification flows; OIDC group and SAML attribute mapping to Organization roles; service accounts and scoped API tokens; recovery when an Organization's last Owner account is lost; cross-Organization support access; and the upgrade path that grants existing single-administrator installations membership in the Organizations they already created.

## Consequences If Accepted

- ProbeHive becomes usable by a team, and the hosted identity trust contract gains the membership records it needs.
- Tenant isolation stops depending on there being exactly one human, which makes the isolation tests meaningful rather than vacuous.
- Existing installations need a migration that grants current instance administrators `Owner` membership in every Organization, or they lose access to their own data. This must ship in the same change as enforcement.
- Every existing tenant-scoped test that authenticates as an instance administrator has to establish membership instead, and the backend contract's endpoint matrix and status codes change accordingly.
- Operator inspection of tenant data becomes a recorded act rather than an ambient capability, which is a real workflow cost accepted in exchange for an honest tenancy boundary.
