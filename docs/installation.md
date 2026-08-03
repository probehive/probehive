# Self-Hosted Compose Installation

This guide is the supported production-like installation path for the current
pre-alpha source tree. It builds local images; ProbeHive does not yet publish
images, releases, or upgrade guarantees.

## Prerequisites

- Rootless Podman with Podman Compose, or Docker with Docker Compose.
- OpenSSL for initial local secret and certificate generation.
- Enough local resources for one Go application, one PostgreSQL database, and
  the static React gateway.

The package was exercised with Podman 5.8.4 and podman-compose 1.6.0. Its
Compose model uses portable service, build, volume, network, health-check, and
file-secret features; Docker Compose remains a supported target but was not
available for local verification in this change.

## Start a Local Evaluation Installation

From the repository root, create one set of ignored local secrets:

~~~bash
./deploy/compose/generate-secrets.sh
~~~

The generator refuses to replace an existing secrets/ path. It creates:

- an independent PostgreSQL password and an API connection URL containing it;
- a 32-byte Webhook wrapping key; and
- a 30-day self-signed TLS certificate for localhost and the internal web
  service name.

Start the package with one of:

~~~bash
podman compose -f deploy/compose/compose.yaml up --detach --build
docker compose -f deploy/compose/compose.yaml up --detach --build
~~~

Wait for all three services to become healthy, then open
https://localhost:8443. The generated certificate is intentionally self-signed,
so a local evaluation browser requires an explicit trust exception. ProbeHive
remains in production mode: browser cookies are Secure, the OpenAPI development
endpoint is disabled, and unsafe requests still require same-origin antiforgery
validation.

Use the setup page to create the first administrator. Setup also creates the
installation Organization and its default Project.

## Runtime Shape

The package preserves the architecture baseline:

- web contains only the static React build and nginx. It terminates TLS, serves
  the SPA, and forwards /api, /healthz, and /readyz to the same-origin API.
- api is the existing Go application and embedded worker. Node.js exists only
  in the web build stage and is absent from runtime images.
- postgres is the single durable database. Its data is stored in the
  probehive-postgres named volume, with the Compose project prefix applied by
  the container engine.

Only the TLS gateway is published, on loopback by default. The API and database
remain on an internal Compose network. Every service declares a numeric
non-root user, drops Linux capabilities, and disables privilege escalation.
The API and web root filesystems are read-only apart from bounded /tmp mounts.

Image tags and multi-platform manifest digests are pinned in the Containerfiles
and Compose file. Their provenance and licenses are recorded in
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## Secrets and TLS

Compose mounts secrets as read-only files under /run/secrets. It does not place
the database URL, database password, Webhook keyring, certificate, or private
key in the Compose model or container environment.

The default secret paths are under the ignored repository-local secrets/
directory. The generator keeps that directory accessible only to its owner and
makes the files themselves read-only so numeric non-root container users can
read individual file mounts. When an operator secret store materializes files
elsewhere, protect each parent directory with mode 0700 and expose each mounted
file with mode 0444; a non-root Compose bind mount otherwise preserves a mode
that its container user cannot read. Override locations with absolute paths:

| Variable | File contents |
| --- | --- |
| PROBEHIVE_POSTGRES_PASSWORD_FILE | PostgreSQL role password |
| PROBEHIVE_DATABASE_URL_FILE | complete pgx connection URL |
| PROBEHIVE_WEBHOOK_KEYRING_FILE | ordered Webhook wrapping keyring |
| PROBEHIVE_TLS_CERT_FILE | PEM certificate chain |
| PROBEHIVE_TLS_KEY_FILE | matching PEM private key |

The password embedded in the database URL must match the PostgreSQL password.
Back up the database and the complete Webhook keyring with matching recovery
coverage. A database containing retained Webhook secrets cannot be restored
without every referenced wrapping key.

The generated TLS material is for loopback evaluation only. Before exposing
the gateway beyond the local machine:

1. Supply a certificate and key for the real hostname.
2. Set PROBEHIVE_BIND_ADDRESS to the intended interface.
3. Set PROBEHIVE_PUBLIC_ORIGIN to the exact external HTTPS origin, including a
   non-default port.
4. Enforce host firewall and ingress policy outside Compose.

There is no setting that disables TLS verification.

## Operator Configuration

The gateway publishes PROBEHIVE_HTTPS_PORT=8443 by default. The Compose file
also passes through the documented worker, retention, resolver, and outbound
policy variables while preserving application defaults. See
[development.md](development.md) for their exact meanings and security
ceilings.

A private target remains denied unless its prefix is explicitly included in
PROBEHIVE_OUTBOUND_ALLOWED_CIDRS. Metadata endpoints remain denied. Do not use
the smoke overlay for an operator installation; it deliberately permits private
Compose-network ranges only so a local Monitor can reach its fixture.

## Health and Lifecycle

The gateway exposes:

- GET /healthz for process liveness; and
- GET /readyz for API and PostgreSQL readiness.

Each service has a Compose health check. The API receives SIGTERM, stops
accepting requests, cancels embedded workers, waits for them, and has 30 seconds
before the container engine forces termination. Stop without deleting data:

~~~bash
podman compose -f deploy/compose/compose.yaml stop
docker compose -f deploy/compose/compose.yaml stop
~~~

A normal down also retains the named PostgreSQL volume. Do not add --volumes
unless the installation data is intentionally disposable; that option deletes
the database volume.

## Deterministic Smoke Check

Run the package smoke check from the repository root:

~~~bash
./deploy/compose/smoke.sh
~~~

It creates disposable secrets and an isolated Compose project, builds both
images, waits on readiness, verifies the static application, creates the first
administrator, creates an HTTP Monitor against the local TLS gateway, requires
its manual Run and Observation to pass with HTTP 200, and verifies graceful API
shutdown. It then removes only its own containers, network, volume, and
temporary files. The behavior under test uses no public DNS or public target.

Set PROBEHIVE_SMOKE_PORT when port 18443 is already occupied.

## Current Limitations

- Images and releases are not published; every installation builds from a
  reviewed source revision.
- The generated certificate is not suitable for remote or unattended clients.
- PostgreSQL backup and restore, schema upgrade exercises, and rollback
  boundaries are the next operability work and are not yet a supported
  procedure.
- The package is single-node and provides no high-availability orchestration.
- The default loopback bind is deliberate. Remote exposure requires the
  operator-controlled TLS, origin, firewall, and ingress configuration above.

For startup failures, inspect Compose service state and logs without printing
secret files. PostgreSQL readiness failures usually indicate a mismatched
password and database URL. A gateway that is healthy while browser writes fail
usually indicates that PROBEHIVE_PUBLIC_ORIGIN does not exactly match the
browser origin.
