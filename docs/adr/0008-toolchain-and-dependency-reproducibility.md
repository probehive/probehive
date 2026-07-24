# 0008: Toolchain and Dependency Reproducibility

- Status: Accepted
- Date: 2026-07-23

## Context

Local development, CI, and released artifacts must resolve the same reviewed Go toolchain, module graph, frontend dependency graph, and container bases. Toolchain or dependency updates must not happen implicitly.

## Decision

Use one Go module with module path `github.com/probehive/probehive`. The supported release line is Go 1.26, and the exact initial toolchain is `go1.26.5`. Record the language baseline in the `go` directive, pin `toolchain go1.26.5` in `go.mod`, pin `go = "1.26.5"` in `.mise.toml`, and make CI install and report that exact version. Prerelease toolchains are not eligible.

A toolchain update is a deliberate reviewed change. Discover the current stable release from official Go tooling or sources, then review release notes, compatibility, advisories, and the resulting module graph before changing the pin.

Commit `go.mod` and `go.sum`. CI and release validation use `-mod=readonly` and run `go mod verify` so an unreviewed module-graph change fails. Discover exact dependency versions with current repository tooling, review ownership, support, advisories, transitive dependencies, native code, and exact-version licenses, and install only the reviewed version.

Prefer the Go standard library, then official `golang.org/x` modules, before third-party dependencies. Do not use floating versions, unexplained pseudo-versions, prereleases, or unpinned archives.

Record a bounded supported Node.js LTS major for the frontend, commit `package-lock.json`, and use `npm ci` with lifecycle scripts disabled by default. Registry, integrity, and lockfile changes require review.

Pin release container base images by immutable version and digest, generate an SBOM, record provenance, and update base digests deliberately. Convenience tags are not release contracts.

## Consequences

- Local development, CI, and release outputs use a known Go toolchain and reviewed module graph.
- Package and base-image updates create visible reviewable diffs.
- Module manifests, checksum records, and lock files are repository artifacts and change only through deliberate dependency updates.
