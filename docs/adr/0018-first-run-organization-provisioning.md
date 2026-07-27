# 0018: First-Run Organization Provisioning

- Status: Accepted
- Date: 2026-07-27
- Amends: [ADR 0013](0013-first-administrator-and-local-authentication.md)
- Clarifies: [ADR 0012](0012-organization-provisioning-idempotency.md)

## Context

A fresh self-hosted installation required three steps before it could hold a Monitor: complete first-administrator setup, create an Organization by inventing a slug and display name, and only then create a Monitor. For an operator monitoring their own sites and servers the middle step is pure ceremony — they will have exactly one Organization and no reason to name it before they have anything in it.

Reaching a working Monitor must not require configuring an organizational hierarchy. Removing the step is the first application of that rule.

ADR 0009 constrains how. Every Organization creation path must go through the one idempotent provisioning use case, so setup cannot insert Organization rows itself, and the repository baseline rules out a generic unit-of-work wrapper that would let one transaction span the user and Organization features.

## Decision

### Setup provisions the installation Organization

`POST /api/v1/setup/admin` creates the first administrator and then provisions the installation Organization by calling the same idempotent use case every other creation path uses (ADR 0012), with the reserved slug `default` and display name `Default`. That use case creates the Organization and its default Project in one transaction, so the invariant of ADR 0009 is unchanged. Setup adds no second Organization creation path.

The slug is free at this point: `POST /api/v1/organizations` requires an authenticated Administrator, and setup only runs while the instance has zero users, so no Organization can exist yet.

### Ordering and the failure mode

The order is: create the administrator, provision the Organization, then issue the session cookie.

The two writes are **not** one transaction, for the reasons above. Provisioning before the session is issued is what makes that acceptable. If provisioning fails, the operator has an administrator and no session; they sign in normally and create an Organization by hand, which is exactly the behavior that existed before this decision rather than a half-signed-in state. Retrying is safe because provisioning is idempotent on the slug.

### Setup accepts no Organization input

The setup request keeps its three fields. Adding an Organization name would reintroduce a decision the operator has no basis to make yet. Renaming an Organization is the separate future operation ADR 0012 already names, and it is the way to replace `Default`.

### Response shape

Setup returns `201` with a `SetupResponse` carrying both the created `user` and the provisioned `organization`, so the client can navigate straight to it. This replaces the bare `UserResponse`. It is a breaking change to an unreleased contract, permitted under ADR 0010, and it happens before any published artifact exposes the endpoint.

### Listing Organizations

`GET /api/v1/organizations` is added, returning each Organization with its default Project in creation order with the Organization id as a tie-breaker, and a bare empty array when none exist. It requires an Administrator session.

Without it, signing back in would land on a create-an-Organization page the operator no longer needs, which would put the removed ceremony straight back into the flow. Until Organization membership exists (ADR 0017) an instance Administrator sees every Organization; that filter is exactly where membership will apply, so the endpoint's contract does not change when membership lands.

`POST /api/v1/organizations` is unchanged. Multiple Organizations remain fully supported; provisioning one is now a choice rather than a prerequisite.

## Self-Hosted and Cloud Boundary

This decision governs self-hosted first run only. ProbeHive Cloud has no first-administrator setup: its instance is the shared multi-tenant deployment, and hosted signup is a private capability whose onboarding calls the same public idempotent provisioning operation over `/api/v1` (ADR 0006). Hosted onboarding therefore inherits the transactional Organization and default Project guarantee without inheriting anything in this record about setup endpoints, the `default` slug, or the setup response. How a hosted account maps to an Organization belongs to the private repository's contract documentation, not here.

## Consequences

- A self-hosted operator goes from a fresh installation to a Monitor without configuring anything organizational.
- Every installation has an Organization named `Default` until Organization rename exists, which makes rename a more visible gap than it was.
- Setup performs two writes that are not atomic; the failure mode is documented above and degrades to the previous behavior rather than to an inconsistent one.
- The Organization list is a published endpoint whose meaning tightens when membership arrives, from "every Organization" to "the caller's Organizations", without a shape change.
- The browser journey now asserts that setup lands on a usable Organization, so a regression that reintroduces a manual creation step fails the build.
