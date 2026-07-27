# 0019: Internationalization, Localization, and Stable Error Codes

- Status: Accepted
- Date: 2026-07-27
- Amends: [ADR 0012](0012-organization-provisioning-idempotency.md), [ADR 0013](0013-first-administrator-and-local-authentication.md), [ADR 0014](0014-monitor-lifecycle-and-revision-immutability.md)

## Context

ProbeHive is an international product, and its self-hosted operators are not all English speakers. Nothing in the repository currently says how a second language is supposed to work, so every string added from here on is a guess.

The timing matters because of one specific coupling. Today the exact English text of validation messages, Problem Details titles, and Problem Details details is contract: ADR 0012, ADR 0013, and ADR 0014 each pin literal sentences, and the backend contract reproduces them character for character. That has two consequences. A client cannot present a message in another language, because all it receives is English prose. And the project cannot reword its own copy, because rewording a pinned sentence is a contract change.

Both problems have the same fix, and it is cheapest now, while the API is unreleased.

## Decision

### English is the source language and the API is never localized

Identifiers, source, comments, log messages, API text, and OpenAPI descriptions are English. Localization resources and localization tests are the only exception, exactly as the workspace language rule already requires.

The HTTP API performs **no** locale negotiation. It ignores `Accept-Language`, and Problem Details prose is always English. Localizing the API would make every response body depend on a request header, would force machine clients and log pipelines to handle text in arbitrary languages, and would turn each translation into a compatibility event under ADR 0010. Presentation is the client's job.

### Every Problem Details response carries a stable code

Localization needs something stable to translate from, so responses carry machine-readable codes.

Validation problems change shape. Each entry becomes an object rather than a bare string:

```json
{
  "type": "about:blank",
  "title": "One or more validation errors occurred.",
  "status": 400,
  "errors": {
    "slug": [{ "code": "organization.slug.invalid", "message": "A slug is 3 to 63 characters…" }]
  }
}
```

Non-validation problems carry a top-level `code` beside `title` and `status`:

```json
{ "type": "about:blank", "title": "Invalid credentials", "status": 401, "code": "user.credentials.invalid", "detail": "…" }
```

Codes are lowercase ASCII words joined by dots, shaped `<area>.<subject>.<rule>`, for example `organization.slug.invalid`, `user.password.length`, `monitor.state.activationWithoutRevision`, `check.http.url.scheme`. A code is an identifier, not a sentence, and it never contains user input; variable parts stay in the message and in future structured fields.

This is a breaking change to unreleased contracts, permitted by ADR 0010 and made now rather than after publication.

### English prose stops being contract; codes take its place

Once a code identifies a failure, the accompanying English `message`, `title`, and `detail` are **documentation, not contract**. They may be reworded, clarified, or corrected without a compatibility event, and clients must not match on them.

What is contract is the code: its spelling, its meaning, the field path it appears under, and the HTTP status it accompanies. Once published, a code's meaning never changes. A new rule gets a new code; adding a code to an existing field is additive and compatible; removing or repurposing one is breaking and needs a new API major.

This supersedes the character-exact message requirements in ADR 0012, ADR 0013, and ADR 0014. Those sentences remain the current English text and stay in the backend contract as a reference, but they are no longer the thing clients depend on.

### Clients localize; the fallback chain is explicit

A client resolves a code against its own catalog. The chain is: the active locale, then `en`, then the server's English `message`. An unknown code always renders something, so a client built against an older release degrades to English rather than to a blank field.

### Locale resolution and formatting in the web application

Locale is resolved from an explicit user preference when one is stored, otherwise from `navigator.languages`, otherwise `en`. The preference is a display setting, not a credential, so persisting it in browser storage does not conflict with ADR 0010's rule against storing tokens there. The resolved locale is written to the `lang` attribute of the document element. `dir` is left for a future right-to-left locale.

Values are formatted, not translated:

- Instants render in the viewer's time zone with `Intl.DateTimeFormat`. Timestamps stay UTC on the wire; only presentation is local. A monitoring product that shows every time in UTC is wrong for most of its users.
- Numbers, durations, and percentages use `Intl.NumberFormat`; plurals use `Intl.PluralRules`; locale-aware ordering uses `Intl.Collator`.

### Catalogs, not a framework, for now

Each locale is one module of flat keys, typed so that a key missing from a locale is a compile error rather than a runtime blank. `en` is the source catalog and the fallback. Interpolation is positional named substitution; `Intl` supplies plural and format behavior.

No i18n framework is adopted yet, consistent with the repository's rule against dependencies without a concrete need. Revisit when there is a concrete requirement the catalogs cannot meet: lazily loaded locale bundles, an external translator pipeline with XLIFF or PO interchange, or a catalog large enough that hand-maintained modules stop being reviewable. Adopting one is a new ADR.

The initial locales are `en` and `zh-CN`. Adding a locale must never require changing application code.

### Not localized

API prose, logs, traces, metric labels, OpenAPI descriptions, ADRs, repository documentation, commit messages, and Conventional Commit subjects stay English. Slugs, check types, lifecycle states, and every other identifier stay ASCII by design. Organization, Project, and Monitor names are user data in whatever language the user chose; they are stored and displayed as given and never machine-translated.

### Text handling already in place

Display names are Unicode, trimmed with Unicode whitespace rules, and bounded in UTF-16 code units so the limit a browser enforces and the limit the server enforces agree. That bound is deliberate and stays.

## Self-Hosted and Cloud Boundary

The localization mechanism, the code catalog, and every shipped locale are public. Language is not a commercial feature, and gating it would produce the crippled community edition ADR 0001 forbids.

The hosted product may add locales for its own cloud-only interface and may run a private translation pipeline, using the same public mechanism. It must not restrict which languages a self-hosted installation can use.

Localizing notification and alert content is deliberately deferred: it needs a per-recipient locale, which needs Organization membership (ADR 0017) to exist first. Public status pages, email templates, the CLI, and the documentation site are each deferred to their own decisions.

## Consequences

- The project can reword its own English copy without a compatibility event, which the character-exact contract previously prevented.
- Clients gain a programmatic handle on failures, which serves the CLI, monitoring-as-code, and any generated client, not only translation.
- Every new validation rule now costs a code as well as a message, and the code has to be chosen deliberately because it is permanent.
- Every locale must cover every code, or the fallback quietly serves English; a missing catalog key is a compile error, but an unknown code is only a runtime fallback.
- Tests that asserted exact English prose now assert codes, which makes them stable against copy changes.
- Time rendering becomes locale and time-zone dependent, so browser tests must pin a time zone to stay deterministic.
