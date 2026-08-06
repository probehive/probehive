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
have no host port mappings. The gateway and API share a project-scoped application
bridge; the API and PostgreSQL share a separate internal data bridge. The application
bridge provides the API's required monitoring egress, while the shared outbound
policy still validates every tenant-influenced destination. The gateway's runtime
nginx configuration can reach only the fixed API service and static assets. Every
service declares a numeric non-root user, drops Linux capabilities, and disables
privilege escalation.
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
without every referenced wrapping key. The logical backup contains password
hashes, session state, Monitor configuration, Observation details, Incident and
Alert evidence, Webhook destinations, and encrypted signing secrets. Protect it
as secret-bearing operator data.

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

## Raw Evidence Retention

`PROBEHIVE_RETENTION_DAYS` controls raw Run and Observation evidence. It accepts
1 through 730 whole days and defaults to 30. The embedded worker enforces the
window at startup and every six hours by dropping only monthly `runs` and
`observations` partitions whose entire range is older than the cutoff. Dropping
whole partitions avoids row-delete bloat, but it also means the configured
window is a floor: raw evidence can remain for up to one additional month.

At least one API process with `PROBEHIVE_WORKER_ENABLED=true` must remain active
to create future partitions and enforce expiry. The packaged API enables the
worker by default. An API-only replica does not perform partition maintenance.

Raw expiry does not remove Organizations, Projects, Monitors, Monitor Revisions,
health transitions, Incidents and their timelines, Alerts, Webhook
Integrations, or Webhook delivery evidence. Durable health and Incident evidence
keeps bounded counts plus the causal Run id and scheduled instant; after the raw
partition expires, a link to the full Run or Observation can legitimately return
not found. Processed outbox markers and dead letters have a separate fixed
30-day cleanup; live outbox rows are not removed by that cleanup. Session expiry
is also independent.

Backups contain only the evidence that exists when the dump begins. Choose the
retention window before scheduling backup coverage that depends on raw
Observations. Changing the setting never rewrites or partially deletes a current
monthly partition; a newly expired whole partition is removed by the next
maintenance pass.

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

## Diagnose Startup and Readiness

Select the Compose implementation once, then inspect service state, bounded log
tails, the published gateway port, and PostgreSQL readiness without printing
secret contents:

~~~bash
compose=(podman compose)
# Or: compose=(docker compose)

"${compose[@]}" -f deploy/compose/compose.yaml ps
"${compose[@]}" -f deploy/compose/compose.yaml logs \
  --no-color --tail=200 postgres api web
"${compose[@]}" -f deploy/compose/compose.yaml port web 8443
"${compose[@]}" -f deploy/compose/compose.yaml exec -T postgres \
  pg_isready -U probehive -d probehive -h 127.0.0.1
~~~

Do not `cat`, echo, or paste the database URL, PostgreSQL password, Webhook
keyring, TLS private key, or logical dump into diagnostics. The Compose
health-check and application logs do not require their values.

Use the first failing service to narrow the cause:

| Signal | Meaning and safe next check |
| --- | --- |
| postgres is unhealthy | Check its logs, named-volume availability, and secret-file readability. The password embedded in the database URL must match the PostgreSQL password. |
| api exits before becoming healthy | Read the final API error. Invalid `PROBEHIVE_*` values name the rejected setting; migration errors name the failing database step; `initialize Webhook secrets` means the complete matching keyring is unavailable or invalid. |
| `/healthz` succeeds but `/readyz` returns 503 `Unhealthy` | The process is live but its PostgreSQL ping failed. Check postgres state and the data network before restarting or changing configuration. |
| web is unhealthy or no port is printed | Check web logs, the loopback port mapping, and whether the certificate and key files are readable by the numeric container user. |
| the page loads but browser writes are rejected | Confirm `PROBEHIVE_PUBLIC_ORIGIN` exactly matches the browser's external HTTPS scheme, host, and port. |
| Runs fail with an outbound policy code | Inspect the stable failure code in the Observation. Review allowed CIDRs, ports, resolver reachability, and target DNS; do not disable TLS or the outbound policy. |

Partition maintenance emits `created Run partitions` or `expired Run
partitions` only when it changes the catalog. `cannot create Run partitions` and
`cannot expire Run partitions` are actionable database errors. The API can
remain live while maintenance reports an error, so investigate it before the
current partitions are exhausted. Inspect partition names and bounds without
reading evidence rows:

~~~bash
"${compose[@]}" -f deploy/compose/compose.yaml exec -T postgres sh -c \
  'PGPASSWORD="$(cat /run/secrets/postgres_password)" exec psql \
    --username=probehive --dbname=probehive --no-align --tuples-only \
    --command="SELECT parent.relname, child.relname,
      pg_get_expr(child.relpartbound, child.oid)
      FROM pg_inherits
      JOIN pg_class parent ON parent.oid = pg_inherits.inhparent
      JOIN pg_class child ON child.oid = pg_inherits.inhrelid
      WHERE parent.relname IN ('\''runs'\'', '\''observations'\'')
      ORDER BY parent.relname, child.relname;"'
~~~

## PostgreSQL Backup and Restore

Use PostgreSQL's custom logical format from the PostgreSQL image pinned by this
package. The commands below never place the database password on the command
line and do not print the Webhook keyring. Run them from the repository root.
Set the Compose implementation once for the examples:

~~~bash
compose=(podman compose)
# Or: compose=(docker compose)
~~~

Create a private backup directory outside the repository and record the exact
reviewed source revision separately. Stop API and web writes before the dump;
PostgreSQL remains available to `pg_dump`:

~~~bash
install -d -m 0700 /path/to/private/probehive-backup
"${compose[@]}" -f deploy/compose/compose.yaml stop api web
"${compose[@]}" -f deploy/compose/compose.yaml exec -T postgres sh -c \
  'PGPASSWORD="$(cat /run/secrets/postgres_password)" pg_dump \
    --username=probehive --dbname=probehive --format=custom \
    --no-owner --no-privileges' \
  >/path/to/private/probehive-backup/probehive.dump
cp -- "${PROBEHIVE_WEBHOOK_KEYRING_FILE:-secrets/webhook-keyring}" \
  /path/to/private/probehive-backup/webhook-keyring
chmod 0400 /path/to/private/probehive-backup/probehive.dump \
  /path/to/private/probehive-backup/webhook-keyring
"${compose[@]}" -f deploy/compose/compose.yaml start api web
~~~

Keep every wrapping key referenced by retained Webhook secrets, not only the
currently active key. Keep the database dump and keyring under the same access,
encryption, retention, and restore-testing controls. TLS private keys are an
independent installation identity and are not required to decrypt database
content; restore them separately when clients depend on that certificate.

Restore only into a newly generated, disposable Compose project first. This
procedure deliberately does not overwrite the source installation:

~~~bash
./deploy/compose/generate-secrets.sh /path/to/restore-secrets
chmod 0600 /path/to/restore-secrets/webhook-keyring
cp -- /path/to/private/probehive-backup/webhook-keyring \
  /path/to/restore-secrets/webhook-keyring
chmod 0444 /path/to/restore-secrets/webhook-keyring

export PROBEHIVE_POSTGRES_PASSWORD_FILE=/path/to/restore-secrets/postgres-password
export PROBEHIVE_DATABASE_URL_FILE=/path/to/restore-secrets/database-url
export PROBEHIVE_WEBHOOK_KEYRING_FILE=/path/to/restore-secrets/webhook-keyring
export PROBEHIVE_TLS_CERT_FILE=/path/to/restore-secrets/tls.crt
export PROBEHIVE_TLS_KEY_FILE=/path/to/restore-secrets/tls.key
export PROBEHIVE_WORKER_ENABLED=false
export PROBEHIVE_PUBLIC_ORIGIN=https://localhost:18444
export PROBEHIVE_HTTPS_PORT=18444
restore_project=probehive-restore

"${compose[@]}" -f deploy/compose/compose.yaml -p "$restore_project" \
  up --detach postgres
~~~

Wait until `compose ps` reports that this new PostgreSQL service is healthy,
then restore the custom archive:

~~~bash
"${compose[@]}" -f deploy/compose/compose.yaml -p "$restore_project" \
  exec -T postgres sh -c \
  'PGPASSWORD="$(cat /run/secrets/postgres_password)" pg_restore \
    --username=probehive --dbname=probehive --clean --if-exists \
    --no-owner --no-privileges --exit-on-error' \
  </path/to/private/probehive-backup/probehive.dump
"${compose[@]}" -f deploy/compose/compose.yaml -p "$restore_project" \
  up --detach --build
~~~

Wait for `https://localhost:18444/readyz`, sign in with a restored administrator,
and inspect representative Organizations, Monitors, Runs, Observations,
Incidents, Alerts, Webhook Integrations, and Webhook delivery evidence. A ready
API also proves that every retained encrypted Webhook secret could be opened by
the supplied keyring during startup. If startup reports `initialize Webhook
secrets`, stop: the matching complete keyring has not been supplied or its
content is invalid. Do not generate replacement wrapping keys for restored
ciphertext.

After verification, remove only the disposable project and its volume:

~~~bash
"${compose[@]}" -f deploy/compose/compose.yaml -p "$restore_project" \
  down --volumes --remove-orphans
~~~

Keep the verified backup according to operator policy. ProbeHive does not yet
define a product retention or recovery-time guarantee.

## Upgrade and Rollback Boundary

Schema migrations are forward-only and run automatically while the API starts.
The API acquires a PostgreSQL advisory lock, verifies every previously recorded
migration name, and applies each pending migration in its own transaction before
readiness can succeed.

Before changing to a newer reviewed source revision:

1. Stop the API and Web containers so the database has no application writers.
2. Create and verify a logical PostgreSQL backup using the procedure above.
3. Preserve the complete Webhook keyring and record the current source revision.
4. Review the newer revision's migration files and build its API and Web images.
5. Start the package, wait for `/readyz`, and verify representative Organization,
   Monitor, Run, and Observation evidence before returning it to service.

Do not infer rollback safety from a failed container start. Migrations commit one
version at a time, so the database may have advanced even when a later migration
or another startup step fails. After the newer API has attempted startup, the
rollback path is to stop it, restore the verified pre-upgrade dump into a clean
PostgreSQL volume with the matching Webhook keyring, and run the previous reviewed
source revision. Do not delete `schema_migrations` rows or hand-reverse schema
changes. An older API rejects migration versions it does not contain and remains
unready rather than running against an unknown schema.

This is the observed boundary of the current source package, not a general
cross-version compatibility or recovery-time guarantee.

## Deterministic Upgrade Check

Run the packaged upgrade exercise from the repository root:

~~~bash
./deploy/compose/upgrade.sh
~~~

It creates a disposable PostgreSQL volume, applies every source migration except
the latest as the baseline, and seeds Organization, Project, membership, Monitor
revision, Run, and Observation evidence. It then builds and starts the current
packaged API so the production migration runner applies the remaining migration.
The check compares the migration manifest and an evidence fingerprint before and
after the upgrade, restarts the API to verify idempotency, and removes only its
own Compose project, volume, secrets, and temporary files.

The exercise covers the one-step penultimate-to-current schema transition in the
checked-out source tree. It does not claim that arbitrary historical revisions
can skip directly to the current schema or that an upgraded volume can be used by
an older binary.

## Deterministic Retention and Diagnosis Check

Run the packaged retention exercise from the repository root:

~~~bash
./deploy/compose/retention.sh
~~~

It creates a disposable PostgreSQL volume, migrates it through the packaged API,
and seeds raw Run and Observation evidence in an expired month and the current
month. Starting the normal embedded worker must drop only the expired raw
partitions while preserving the current raw rows, Monitor definition, health
transition, Incident timeline, and Alert. The check also requires the expiry log
signal, proves `/readyz` reports a stopped PostgreSQL dependency, and verifies
that an out-of-range retention window fails startup with a named setting. It
then removes only its own Compose project, volume, generated secrets, and
temporary files.

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
The smoke check verifies that the Compose engine published the gateway port before
waiting for readiness, so an unsupported or incomplete network setup fails with a
direct diagnostic.

Set PROBEHIVE_SMOKE_PORT when port 18443 is already occupied.

## Deterministic Recovery Check

Run `./deploy/compose/recovery.sh` to exercise the documented logical backup
and clean-restore path. It creates isolated source and restore projects, seeds
real Organization, Monitor, Run, Observation, Incident, Alert, Webhook route,
and delivery-attempt evidence, stops source writers, dumps PostgreSQL, restores
into a distinct clean volume, and starts the restored API with the copied
keyring. It verifies only counts and identifiers and does not print secrets.
Both projects, volumes, the logical dump, cookies, and generated secrets are
removed when the check exits. Set `PROBEHIVE_RECOVERY_SOURCE_PORT` and
`PROBEHIVE_RECOVERY_RESTORE_PORT` if 18443 or 18444 is occupied.

## Current Limitations

- Images and releases are not published; every installation builds from a
  reviewed source revision.
- The generated certificate is not suitable for remote or unattended clients.
- Schema migrations are forward-only. Rollback after an attempted migration
  restores the pre-upgrade backup into a clean volume and runs the prior reviewed
  source revision.
- The package is single-node and provides no high-availability orchestration.
- The default loopback bind is deliberate. Remote exposure requires the
  operator-controlled TLS, origin, firewall, and ingress configuration above.
