# 0008: SDK and Dependency Reproducibility

- Status: Accepted
- Date: 2026-07-23
- Superseded by: [ADR 0015](0015-adopt-go-for-the-backend-implementation.md)
- Amended: 2026-07-23 — Recorded that warnings-as-errors is enforced only in CI builds.

## Context

Developers should be able to use a current stable .NET 10 feature band, while CI and released artifacts must not change because a newer SDK, analyzer rule set, transitive package, or container base image appeared implicitly.

## Decision

Keep `global.json` as the supported .NET 10 minimum baseline with `latestFeature` and prerelease resolution disabled for local development. CI and release environments pin one reviewed exact .NET 10 SDK version or immutable SDK image and report the resolved version.

Pin the built-in analyzer rule set to `10-recommended`. Raising the analyzer level is a deliberate repository change with a reviewed diff; it does not happen automatically when a newer feature band is installed. Warnings are treated as errors only in CI builds, so a local feature-band SDK update cannot hard-break local builds while CI remains strict.

Use Central Package Management with exact direct package versions, no project-level version overrides, and no central transitive pinning by default. Enable NuGet lock files for every project, commit them, and use locked restore in CI and release validation. Dependency updates use the current CLI and an unlocked restore to regenerate reviewed lock files before locked validation.

When the frontend exists, record a bounded supported Node.js LTS major, commit `package-lock.json`, and use `npm ci`. Registry, integrity, and lockfile changes require review.

When container assets exist, pin release base images by immutable version and digest, generate an SBOM, record provenance, and update base digests deliberately. Convenience tags are not release contracts.

## Consequences

- Local development can move within the supported .NET 10 line without silently changing analyzer policy.
- CI and release outputs use a known SDK and complete locked dependency graph.
- Package and base-image updates create visible reviewable diffs.
- Lock files are repository artifacts and must be updated whenever dependency resolution intentionally changes.
