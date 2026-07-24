# ProbeHive Web

`@probehive/web` is the static React administration application for the self-hosted ProbeHive control plane. It is built with React, strict TypeScript, Vite, React Router, and TanStack Query, and consumes the versioned HTTP API at `/api/v1`. It owns no authoritative authorization or business rules.

See [docs/development.md](../docs/development.md) for the development loop and validation commands. In short:

```bash
npm ci          # install with lifecycle scripts disabled
npm run dev     # Vite dev server; proxies /api to http://localhost:5080
npm run lint
npm run typecheck
npm test
npm run e2e     # Playwright journeys against the real API and a disposable database
npm run build   # static production assets in dist/
```

Production deployments serve the built static assets behind a same-origin gateway together with the API (ADR 0010); there is no Node.js production runtime.
