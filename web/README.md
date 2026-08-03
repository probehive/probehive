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

Production deployments serve the built static assets behind a same-origin gateway
together with the API; there is no Node.js production runtime.

Monitor detail pages expose immutable Alert intents as read-only Incident-derived audit history. The view links each intent to its source Incident and deliberately presents no sent or delivered state because external delivery is not implemented.

## Localization

English is the source language and `src/i18n/en.ts` is the source catalog. Other locales are typed against it, so a key missing from a locale is a compile error. Adding a locale means adding a catalog and registering it in `src/i18n/locale.ts`; no component changes are needed.

User-visible text never appears inline in a component — it comes from a catalog key. Instants, numbers, and plurals are formatted with `Intl` rather than translated, so times render in the viewer's own time zone while the wire stays UTC.

API responses are always English. The client localizes from the stable error code
each Problem Details response carries and falls back to the server's message for a
code the catalog does not know.
