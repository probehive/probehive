# 0017: Organization Membership and Authorization

- Status: Accepted
- Date: 2026-07-27
- Amends: [ADR 0013](0013-first-administrator-and-local-authentication.md)
- Related: [ADR 0009](0009-tenant-scope-default-project-and-telemetry.md), [ADR 0010](0010-browser-authentication-trust-and-compatibility.md), [ADR 0012](0012-organization-provisioning-idempotency.md)

## Context

ADR 0013 created instance-scoped `User` records with a single instance role, `Administrator`, made every Organization and Monitor endpoint require it, and named Organization membership as a future ADR that is required before non-administrator users can be authorized meaningfully.

Three things depend on closing that gap. The self-hosted product is single-administrator, so a team cannot use it. Cloud ADR 0004 requires the public API to authorize every monitoring operation from a validated Organization context, and there is no membership record for a hosted subject to map onto. And the instance `Administrator` role currently doubles as unrestricted access to every Organization's data, which is a tenancy posture the product never deliberately chose.

Two competing pressures shape the answer. An individual developer self-hosting ProbeHive for their own sites and servers must never have to configure roles to get started. An organization with thirty engineers eventually needs access control that a fixed list of roles cannot express. A design that serves only one of them is wrong.

## Decision

### Authorization is permission-based; roles are named permission sets

Authorization checks resolve a permission, never a role name. No handler, query, or middleware compares against the string `Administrator`.

Permissions use a `resource.action` shape. The set that exists today is:

```text
organization.read   monitor.read   monitor.write   member.read   member.write
```

This catalog is **internal**. It is not published in OpenAPI, not part of the wire contract, and may change freely while it stays internal. It becomes public API only when custom roles ship, and from that point it is versioned like any other published contract under ADR 0010.

Making the check permission-based costs nothing today and is what makes custom roles an additive change later rather than a re-authorization of every endpoint.

### Two built-in roles

| Role | Permissions |
| --- | --- |
| `Administrator` | All permissions, including every permission added in future releases. |
| `Viewer` | Read-only: every `*.read` permission, including future ones. |

`Viewer` is built in rather than left to custom roles because read-only access is a day-one need at every scale, including for a single developer who wants a second account that cannot change anything. Requiring a permission matrix to obtain it would make the simplest case harder, which is the opposite of the intent.

Roles deliberately **not** created now:

- `Owner` is a commercial concept — who pays for the tenant and who may transfer or close it. Cloud ADR 0001 puts account lifecycle and subscriptions in the private repository, and ADR 0016 defers Organization deletion entirely, so no operation today requires it. In a self-hosted installation the person who runs the server is the owner, and recording that in a table adds nothing.
- A middle role that configures Monitors without managing members is a real need at roughly thirty people and no need at three. Adding a role is backward compatible; removing a published one is breaking. When uncertain, ship fewer.

The invariant that replaces "at least one Owner" is: **an Organization always has at least one member holding `Administrator`.** The use case rejects removing or demoting the last one.

The instance role and the Organization role both use the name `Administrator`. They are different scopes carried in different fields — the instance role in `SessionResponse.role`, the Organization role in membership responses — and using one word for one idea keeps the product easier to explain than inventing a synonym.

### Custom roles are a reserved, public extension

Custom roles are not built now, but the model must not have to change to accept them:

- Membership references a role by a stable identifier, so a custom role occupies the same field a built-in role does.
- Built-in role names are reserved and cannot be taken by a custom role.
- When a release introduces a new permission, built-in roles acquire it by definition. **Custom roles never acquire a permission automatically**; they fail closed, and the release notes must list the new permission so operators can grant it deliberately.

Custom roles ship in the **public** repository when they are built. Access control is foundational self-hosted capability under ADR 0001, and gating it commercially would produce exactly the deliberately crippled community edition that decision forbids. What a hosted plan may gate is *entitlement* — see below — never the mechanism.

### Membership persistence

```text
organization_members
  organization_id uuid   -- FK -> organizations (id) ON DELETE CASCADE
  user_id         uuid   -- FK -> users (id) ON DELETE CASCADE
  role            varchar(50)
  created_at      timestamptz
  PRIMARY KEY (organization_id, user_id)
```

The primary key makes a duplicate membership unrepresentable. The "at least one Administrator" invariant cannot be expressed as a table constraint and lives in the use case.

### Authorization behavior

- Deny by default is unchanged. Every `/api/v1/organizations/{organizationId}/...` route resolves membership once per request and derives the effective permission set.
- **Non-membership returns `404`, not `403`.** A real Organization the caller does not belong to must be indistinguishable from one that does not exist, matching the rule the backend contract already applies to identifiers presented through the wrong scope. `403` is reserved for a caller who is a member but lacks the permission — there the resource's existence is already known.
- Membership never comes from a request header, browser-selected context, or a hosted claim taken at face value. Under ADR 0010 and cloud ADR 0004 the API validates a signed assertion and then resolves membership from its own records.
- Current endpoints map as: Monitor and revision reads require `monitor.read`; Monitor creation, rename, state change, and revision creation require `monitor.write`; membership changes require `member.write`.

### The instance Administrator manages membership but holds no implicit data access

The instance `Administrator` role keeps a narrow, enumerated meaning: first-administrator bootstrap, creating Organizations, managing membership of any Organization, and future instance-wide configuration.

It confers **no** implicit read or write access to the monitoring data of an Organization it is not a member of. ADR 0009 requires every tenant-scoped authorization decision to carry Organization identity explicitly, and a role that silently satisfies all of them contradicts that.

Retaining membership management is what keeps the rule safe for self-hosting: the operator can always grant themselves `Administrator` membership and can never be locked out of their own server. That grant is an ordinary recorded write, so inspecting a tenant leaves evidence instead of being ambient.

### The individual-developer path

For a single developer nothing above is visible:

1. Complete first-administrator setup.
2. Create an Organization. Provisioning writes the caller's `Administrator` membership in the same transaction that creates the Organization and its default Project — one use case, no second path that can produce an Organization without a member, matching how ADR 0009 treats default Project creation.
3. Create Monitors.

No role is chosen, no member screen is opened, no permission is granted. Any future capability that would require configuring roles or membership before first use is a regression against this path.

Idempotent replay stays idempotent: replaying provisioning for an existing slug with an identical display name returns the existing Organization and writes no membership, including when the replaying caller differs from the original creator.

## Self-Hosted and Cloud Differences

The mechanism is public; only commercial gating and hosted identity are private.

| Concern | Owner |
| --- | --- |
| Membership records, roles, permissions, enforcement, custom roles when built | Public core. It is the only writer of membership rows (cloud ADR 0004). |
| How many members an Organization may have, and whether custom roles are available on a plan | Private cloud entitlements, applied through public limits — the same pattern cloud ADR 0001 uses for scheduling quotas. Never a public feature flag. |
| Signup, email invitations, SSO and SCIM provisioning into public membership records | Private cloud. The public core accepts adding an existing instance user to an Organization; richer provisioning layers on top through public APIs. |
| Support staff access to tenant data | Private cloud support tooling, audited (cloud ADR 0001). Never the public instance role. |

The instance `Administrator` role means different things in the two deployments and this difference is security-relevant. Self-hosted, it is the operator who runs the server. In Cloud the "instance" is ProbeHive's shared multi-tenant deployment, so an instance administrator would be ProbeHive staff with membership-management power over every tenant. **Cloud deployments must not expose the instance-administrator surface on any tenant-reachable route**, and hosted onboarding must not create instance administrators. Enforcing that at the edge belongs to the hosted trust-contract specification cloud ADR 0004 already requires.

### Deliberately deferred

Each needs its own decision and none blocks the above: custom roles and their published permission catalog; invitation and email-verification flows; OIDC group and SAML attribute mapping; service accounts and scoped API tokens; per-Project scoping of roles; recovery when an Organization's last Administrator account is lost; and cross-Organization support access.

## Consequences

- ProbeHive becomes usable by a team without an individual developer paying any setup cost for it.
- Custom roles become an additive change, because nothing authorizes on a role name.
- Tenant isolation stops depending on there being exactly one human, which makes the isolation tests meaningful rather than vacuous.
- Existing installations need a migration granting current instance administrators `Administrator` membership in every Organization they created, shipped in the same change as enforcement, or they lose access to their own data.
- Every existing tenant-scoped test that authenticates as an instance administrator must establish membership instead, and the backend contract's endpoint matrix and status codes change accordingly.
- Operator inspection of tenant data becomes a recorded act rather than an ambient capability — a real workflow cost, accepted for an honest tenancy boundary.
- Access control cannot later become a paid feature of the hosted product without contradicting this decision and ADR 0001.
