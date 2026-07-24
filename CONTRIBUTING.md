# Contributing to ProbeHive

Thank you for helping improve ProbeHive. The project welcomes focused bug fixes, security improvements, documentation, tests, protocol fixtures, and well-scoped product changes.

## Before You Start

- Search existing issues and pull requests before opening a duplicate.
- Discuss substantial features, public contract changes, new protocols, new dependencies, and architectural changes before implementation.
- Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
- Follow the community standards in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) in all project spaces.
- Keep contributions focused on the complete self-hosted product. Hosted billing, managed-fleet operations, fraud controls, and other commercial service behavior do not belong in this repository.

## Contribution Requirements

- Write source, documentation, commit messages, logs, examples, and user-facing source strings in English. Intentional localization resources and tests are exceptions.
- Preserve tenant isolation, authorization, TLS validation, secret redaction, SSRF defenses, timeouts, and resource limits.
- Do not add secrets, customer data, private operational data, or generated local state.
- Keep feature packages standard-library-only and keep dependencies pointing from composition and adapters toward feature-owned interfaces.
- Add or update tests in proportion to the risk of the change.
- Discover dependency versions with current repository tooling. Do not copy versions from memory or unrelated projects.
- Use concise Conventional Commit messages.

## Developer Certificate of Origin

ProbeHive uses the Developer Certificate of Origin 1.1 instead of a contributor license agreement. Sign off every commit to certify that you have the right to submit the contribution:

```text
git commit -s
```

The sign-off adds a `Signed-off-by` trailer using your Git name and email. The full Developer Certificate of Origin is available at <https://developercertificate.org/>.

## Local Verification

Run the checks documented by the repository for every affected surface:

```text
go version
go mod verify
test -z "$(gofmt -l .)"
go vet -mod=readonly ./...
go test -mod=readonly -race ./...
go build -mod=readonly ./cmd/probehive
npm --prefix web ci
npm --prefix web run lint
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run e2e
npm --prefix web run build
```

PostgreSQL integration tests require `PROBEHIVE_TEST_DATABASE_URL`. They create and remove isolated schemas and skip explicitly when no database URL is configured. HTTP tests use `net/http/httptest`; other Go tests use the standard `testing` package.

Tests must not depend on the public internet, public DNS, local time zone, execution order, or arbitrary sleeps. Use injected clocks, deterministic random sources, local protocol fixtures, and disposable PostgreSQL state.

## Pull Requests

- Explain the problem and the chosen solution.
- Identify security, compatibility, migration, deployment, and retention consequences.
- Include the checks that were actually run and disclose relevant checks that could not be run.
- Keep generated files, migrations, OpenAPI documents, deployment assets, and documentation synchronized with the implementation.
- Do not mix unrelated changes in one pull request.

All contributions are reviewed under the repository license and project policies.
