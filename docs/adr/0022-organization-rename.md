# 0022: Organization Rename

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0012](0012-organization-provisioning-idempotency.md), [ADR 0017](0017-organization-membership-and-authorization.md)

## Context

ADR 0012 named renaming an Organization as "a distinct future update operation" and left it there. ADR 0018 then made every installation start with an Organization called `Default`, because setup deliberately accepts no Organization input. Without rename, that name is permanent, and it reads particularly poorly in a non-English interface where one English word sits among translated labels.

Rename is small, but it has one consequence that is not obvious and must not be discovered in production.

## Decision

### The slug is immutable; the display name is not

`PUT /api/v1/organizations/{organizationId}/name` changes the display name. It does not change the slug.

The slug is the idempotency key for provisioning (ADR 0012). Changing it would make an earlier successful provisioning call stop being a replay of anything, and would break every stored reference that used it. An Organization that needs a different slug is a new Organization, the same way a Monitor that needs a different Check Type is a new Monitor (ADR 0014).

Validation is the existing display-name rule: trimmed Unicode text of 1 to 100 UTF-16 code units, reported under field `displayName` with code `organization.displayName.invalid`.

### Rename changes what counts as an idempotent replay

This is the consequence worth recording. Provisioning treats an existing slug plus an *identical* trimmed display name as a successful replay and any other display name as a `409` conflict. Renaming therefore moves that boundary: after an Organization is renamed, a provisioning call that previously replayed cleanly now conflicts, and one that previously conflicted may now replay.

That is correct rather than unfortunate. The rule exists so a retry cannot silently rewrite an Organization's name, and it keeps working after a rename: the name a caller must supply to replay is simply the current one. Automation that provisions with a fixed display name and expects a `200` must not rename out from under itself, and the conflict is the signal that it did.

### Authorization

Rename requires a new permission, `organization.write`, checked against the caller's membership like every other Organization-scoped operation (ADR 0017). Because built-in roles are defined by rule rather than by a list, `Administrator` acquires it automatically and `Viewer` does not, which is exactly the intent — this is the first live demonstration that adding a permission is additive.

### Concurrency

Rename is last-write-wins. It carries no optimistic-concurrency token, unlike Monitor rename.

The difference is deliberate. Monitor mutations are serialized because revision creation allocates a number from the Monitor row, so a lost update could renumber history. An Organization display name has no such dependency: two concurrent renames produce one of the two names and no inconsistent state. Adding a version token here would be ceremony that buys nothing, and it can be added later without changing the endpoint's shape if a real conflict case appears.

### Not decided here

Changing a slug, transferring an Organization, and renaming a Project are each separate and remain unimplemented. Deleting an Organization stays out of scope under ADR 0016 until retention and deletion semantics exist.

## Consequences

- An installation is no longer stuck with `Default`, which was the visible cost of ADR 0018's decision to take no Organization input at setup.
- The permission model gains its first additive extension, exercising the rule-based role definition instead of leaving it theoretical.
- Provisioning automation that hard-codes a display name and expects an idempotent `200` becomes sensitive to renames; the failure is a loud `409` rather than a silent overwrite.
- Two concurrent renames resolve arbitrarily, which is acceptable for a display name and is recorded here so it is a choice rather than an oversight.
