# 0015: Frontend Dependency and Tooling Baseline

- Status: Accepted
- Date: 2026-07-27
- Clarifies: [ADR 0002](0002-modular-monolith-and-project-topology.md)

## Context

ADR 0002 fixed the frontend baseline as React with strict TypeScript, Vite, React Router, Vitest with React Testing Library, and Playwright, and required a new decision before introducing any further framework. The `web/` application has since taken two dependencies that decision does not name: `@tanstack/react-query` as a runtime dependency and `oxlint` as the `npm run lint` command.

Neither is recorded in this repository. The rationale for TanStack Query exists only in a private planning document that public contributors cannot read, and the rationale for oxlint was never written down anywhere. A public repository must be able to explain its own dependency graph, so both belong in an ADR here regardless of where the choice was originally argued.

## Decision

### Server state uses TanStack Query

`@tanstack/react-query` is the server-state boundary for every first-party web application in this repository. It is adopted for concrete requirements that already exist in `web/`, not as a precaution:

- The authenticated session is read by more than one component in a single render tree, and request deduplication is what keeps that from becoming several `GET /api/v1/auth/session` calls.
- Login, logout, and first-administrator setup each change the authenticated identity, so cached session state must be invalidated or replaced at an exact point rather than left to component lifetimes.
- Monitor status polling is a near-term requirement once check execution lands, and it needs a cache with an explicit staleness policy.

TanStack Query is a server-state cache, not a client-state store. Redux, Zustand, MobX, Recoil, and comparable client-state libraries remain excluded, as do component and styling frameworks. Business rules, authorization, and validation stay in the API; query and mutation code in `web/` only moves already-authorized data.

### Linting uses oxlint

The Go toolchain supplies `gofmt` and `go vet`, so ADR 0008 and the repository validation commands need no third-party Go linter. TypeScript and React have no equivalent toolchain-supplied checks, so the frontend needs exactly one linter.

That linter is oxlint. It ships as a single self-contained binary with no plugin graph, shared-config chain, or parser packages to review separately, which keeps the reviewed dependency surface proportionate to the value. Its configuration lives in `web/.oxlintrc.json`.

`npm run lint` runs with `--deny-warnings`, so a warning fails the command and therefore fails CI. This matches how Go formatting is already enforced through a non-zero `gofmt -l` result: the repository has no category of accepted, permanently reported diagnostic.

ESLint remains the documented fallback. If a rule the repository actually needs has no oxlint equivalent, adopting ESLint is a new decision recorded in a new ADR, not a silent addition.

### Dependency ranges

Frontend dependencies use ordinary caret ranges in `package.json` with an exact committed `package-lock.json` and `npm ci` validation, as required by ADR 0008. `@playwright/test` is the deliberate exception and is pinned exactly, because the package version and the downloaded browser binaries are one unit and a floating package version would silently change the browser under test.

## Consequences

- The public repository explains every dependency it ships without reference to a private document.
- Server-state caching has one owner, so components do not grow ad-hoc fetching and refresh logic.
- Frontend lint diagnostics cannot accumulate as tolerated warnings.
- Replacing either tool is a visible reviewed decision rather than a package change.
- A future frontend capability that needs a genuinely new category of dependency still requires its own ADR.
