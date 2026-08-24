# Production Runbook

Scope: operating the production Compose deployment described by
`docker-compose.yml` + `infrastructure/caddy/docker-compose.production.yml`.
This file is the operator-facing entry point; it links out to
`scripts/backup/RUNBOOK.md` for backup/restore detail rather than
duplicating it.

All commands below assume a shell in the repo root, with a production
secrets file (never committed) passed via `--env-file`, e.g.:

```sh
docker compose --env-file /etc/barber-appointment/production.env \
  -f docker-compose.yml -f infrastructure/caddy/docker-compose.production.yml \
  <command>
```

The rest of this document abbreviates that prefix as `dc`.

## 1. Required secrets

Every value below is mandatory in production - the Compose files use
`${VAR:?VAR is required}` for all of them, so `dc up` fails fast (not
silently falls back to a dev default) if any is missing. `.env.example`
lists every name with a placeholder value; it is never a source of real
secrets.

| Secret | Used by |
| --- | --- |
| `POSTGRES_PASSWORD` | Postgres superuser |
| `*_DB_OWNER_PASSWORD`, `*_DB_APP_PASSWORD` (8 services) | per-service Postgres roles |
| `REDIS_PASSWORD` | Redis `requirepass`; consumed via `auth-service`/`tenant-service`/`customer-service`'s `REDIS_URL` |
| `NATS_USER`, `NATS_PASSWORD` | NATS client auth (`infrastructure/nats/nats-server.conf`); consumed via `appointment-service`/`notification-service`'s `NATS_URL` |
| `INTERNAL_AUTH_TOKEN` | gateway -> backend internal-auth tier |
| `SERVICE_INTERNAL_TOKEN` | backend -> backend internal-auth tier (see §3) |
| `PLATFORM_ADMIN_TOKEN` | platform-admin-only gateway routes |
| `EMAIL_PROVIDER`, `EMAIL_FROM`, `SMTP_ADDRESS`, `SMTP_USERNAME`, `SMTP_PASSWORD` | auth/customer/notification email delivery. `EMAIL_PROVIDER=log` is rejected at startup in production (`APP_ENV=production`) - it must be `smtp`. |
| `CADDY_ACME_EMAIL` | Caddy's ACME account contact |

`platform/config.ServiceConfig.Validate()` additionally rejects, in
production, any of the above (plus `DATABASE_URL`/`REDIS_URL`/`NATS_URL`)
that contain `development`, `dev-password`, `dev-user`, `changeme`,
`replace-with`, or `default` - catching a forgotten dev default before it
reaches a backend service. `REDIS_URL`/`NATS_URL` are additionally required
to contain an `@` (i.e. embedded credentials) in production.

**Never** print these values to a shared terminal, ticket, or log. None of
this repo's own code logs them (`platform/logging` only emits an
allowlisted field set - see Phase 7A); this is an operator discipline note,
not a code guarantee against `set -x`/shell history/copy-paste into chat.

## 2. NATS and Redis authentication

Both `nats` and `redis` require credentials in every environment, including
local dev - there is no anonymous-access fallback to accidentally carry into
production.

- **NATS**: `infrastructure/nats/nats-server.conf`'s `authorization` block
  requires `$NATS_USER`/`$NATS_PASSWORD` (resolved from the container's own
  environment at startup). The unauthenticated `http_port: 8222` monitoring
  endpoint (used by this repo's healthcheck) is intentionally left open -
  it exposes only server health/stats, not client data, and is reachable
  only inside the private Docker network.
- **Redis**: `redis-server --requirepass "$REDIS_PASSWORD"`.
- Both are exercised by `platform-auth-integration-test`
  (`scripts/ci/integration.sh`), which proves missing/wrong/valid
  credentials are handled correctly against a real server.

## 3. Internal service authentication (two-tier)

`platform/internalauth.MultiTokenVerifier` lets every backend accept either
of two credentials on the same `X-Internal-Token` header and route surface:

- `INTERNAL_AUTH_TOKEN` - presented only by the gateway when proxying
  browser-facing requests to a backend.
- `SERVICE_INTERNAL_TOKEN` - presented by every backend-to-backend HTTP
  client (`adminauth`, `barberclient`, `schedulingdeps`, `tenantsettings`,
  notification's appointment-recipient client).

This is additive, not a breaking change to the existing single-shared-secret
contract: every one of the 94 internal routes still accepts exactly what it
did before, plus the new second credential. Leaking one tier's value does
not hand out the other tier's.

**Honest limitation:** verification only checks "is this a currently-valid
credential", not caller identity - a compromised backend that holds
`SERVICE_INTERNAL_TOKEN` can still call any other backend's internal routes,
same as before. True per-service credentials (one secret per backend,
checked per caller) would close that gap but requires each of ~8 backends
and ~5 client packages to hold and select the correct one of ~8 distinct
secrets per destination - judged out of scope as a "major redesign" per the
original request. Revisit if/when this matters more than the added secret-
management complexity.

## 4. Image pinning

Every image in both Compose files is pinned to an exact tag **and** digest
(`image:tag@sha256:...`), not `latest`:

| Image | Pinned to |
| --- | --- |
| `postgres` | `16.15-alpine` |
| `redis` | `7.4.11-alpine` |
| `nats` | `2.10-alpine` |
| `caddy` | `2.10.2-alpine` |
| `prom/prometheus` (dev-only observability overlay) | `v2.54.1` |
| `otel/opentelemetry-collector-contrib` (dev-only) | `0.108.0` |
| `grafana/grafana` (dev-only) | `11.2.0` |

To bump a pin: resolve the new tag's digest (`docker buildx imagetools
inspect <image>:<tag>` or the registry API), update both the tag and the
`@sha256:...` together, then re-run `scripts/ci/compose.sh` and a full
`docker compose build` before deploying.

## 5. Startup

```sh
dc up -d --build
```

Dependency ordering (via `depends_on`/healthchecks, already encoded in the
Compose files) is: `postgres`/`redis`/`nats` healthy -> per-service
`*-migrate` completes -> service starts -> `gateway-service` healthy ->
`caddy` starts (needs `gateway-service`, `tenant-service`, `admin-web`,
`booking-web` all healthy). `admin-web`/`booking-web`/`gateway-service` are
`ports: !reset []` in production - only `caddy` publishes host ports
(80/443), everything else is reachable only inside the `private` network.

Verify:
```sh
dc ps                              # everything healthy, none restarting
curl -sf https://<your-domain>/api/v1/public/config   # gateway reachable via Caddy
```

## 6. Shutdown

```sh
dc stop            # graceful stop, containers/volumes kept
# or, to also remove containers (volumes/data untouched unless you add -v):
dc down
```

Never run `dc down -v` against production - it deletes `postgres_data`,
`redis_data`, `nats_data`, `caddy_data`, `caddy_config` named volumes.

## 7. Migrations

Each service's `*-migrate` one-shot container runs automatically on
`dc up`. To re-run migrations only (e.g. after a schema-only release):

```sh
dc up --no-deps <service>-migrate
```

Migrations are forward-only (`scripts/ci/migration-checksums.sh` locks every
already-shipped migration file's checksum in CI - a merged migration can
never be silently edited). There is no down-migration tooling in this repo;
rolling back a bad migration means writing and shipping a new forward
migration that undoes it, or restoring from a Postgres backup (§9).

## 8. Secret rotation

**Database passwords** (`*_DB_OWNER_PASSWORD`/`*_DB_APP_PASSWORD`,
`POSTGRES_PASSWORD`): change the role's password in Postgres first
(`ALTER ROLE ... WITH PASSWORD '...'`), then update the secret and
`dc up -d <affected services>` to pick up the new `DATABASE_URL`. A window
where the old secret is set but the role's password already changed will
show as connection failures in that service's logs/`dependency_up` metric -
keep the window short.

**`REDIS_PASSWORD`**: Redis's `requirepass` takes the value the container
started with; there is no live-rotation without a restart.
1. `dc up -d redis` with the new `REDIS_PASSWORD` (brief unavailability
   while it restarts).
2. `dc up -d auth-service tenant-service customer-service` with the same
   new value so their `REDIS_URL` matches.
   Rate-limit state lost on the Redis restart is self-healing (fixed-window
   counters, not durable data - see `scripts/backup/RUNBOOK.md`'s Redis
   section).

**`NATS_USER`/`NATS_PASSWORD`**: same shape as Redis - `dc up -d nats` with
new credentials, then `dc up -d appointment-service notification-service`.
A brief window of publish/consume errors during the restart is expected;
the outbox publisher and JetStream consumer both retry.

**`INTERNAL_AUTH_TOKEN` / `SERVICE_INTERNAL_TOKEN`**: rotate one tier at a
time to avoid a full-stack outage window:
1. Roll `SERVICE_INTERNAL_TOKEN` first (backend-to-backend tier): update
   the secret, `dc up -d <all 8 backend services>`. Every backend now both
   presents and accepts the new value; `INTERNAL_AUTH_TOKEN` (gateway tier)
   is untouched throughout, so gateway traffic is unaffected.
2. Roll `INTERNAL_AUTH_TOKEN` next: update the secret, `dc up -d
   gateway-service <all 8 backend services>` (backends need the new value
   to *verify* it even though they don't present it).
   `MultiTokenVerifier` accepting either the old or new value during a
   rolling restart is not implemented - a full `dc up -d` batch rotates all
   backends near-simultaneously, so plan for a short window (seconds, one
   Compose apply) where a request could race a not-yet-restarted backend.

**`PLATFORM_ADMIN_TOKEN`**: `dc up -d gateway-service` only.

**SMTP credentials**: `dc up -d auth-service customer-service
notification-service`.

## 9. Backup / restore

See **`scripts/backup/RUNBOOK.md`** for the full procedure, verified
end-to-end (a real backup -> wipe -> restore -> query/consume cycle was run
for both Postgres and NATS JetStream - see that file's "What was actually
verified" section). Summary:

- **Postgres**: `scripts/backup/backup-postgres.sh` / `restore-postgres.sh`
  - per-database `pg_dump -Fc` + one `pg_dumpall --globals-only`. Logical,
    not WAL/PITR - RPO is bounded by backup interval, not continuous.
- **NATS JetStream**: `scripts/backup/backup-nats.sh` / `restore-nats.sh`
  - `nats stream backup`/`restore` via the JetStream network API. Postgres
    is the system of record; JetStream backups are an RTO optimization, not
    the last line of defense (the outbox publisher's at-least-once
    re-publish reconciles any gap).
- **Redis**: intentionally has no backup tooling - it holds only short-lived
  fixed-window rate-limit counters (`platform/ratelimit`), losing them is a
  momentary self-healing blip, not data loss.

Point both scripts at the production project/files via
`BACKUP_COMPOSE_PROJECT`/`BACKUP_COMPOSE_FILES` - see that runbook's
"Parameterizing target" section.

## 10. Failed-deployment rollback

This repo has no blue/green or canary tooling - a deploy is `dc up -d
--build` against the new image(s)/config in place. To roll back:

1. **Application code/image only** (no new migration shipped): re-deploy
   the previous known-good commit -
   `git checkout <previous-tag-or-sha> -- .` (or check out that ref
   entirely) then `dc up -d --build`. Since Postgres schema is unchanged,
   this is safe and immediate.
2. **A new migration shipped and is suspected of causing the failure**:
   migrations are forward-only (§7) - do not attempt to hand-edit
   already-applied migration state. Options, in order of preference:
   - Ship a new forward migration that undoes the problematic change, then
     redeploy.
   - If the migration also corrupted data (not just schema), restore the
     affected database(s) from the most recent pre-deploy backup (§9),
     accepting the RPO gap back to that backup.
3. **A secret/config change is suspected**: revert just that value and
   `dc up -d <affected services>` - no rollback of code needed.
4. In every case, confirm recovery via `dc ps` (all healthy) and the
   observability checks in §12 before considering the incident closed.

## 11. NATS / Redis recovery

Both are `restart: unless-stopped` and use a durable named volume
(`nats_data`, `redis_data`), so a crash or host reboot self-recovers without
operator action in the common case.

**NATS**: if `nats` becomes permanently unhealthy (corrupted store, disk
full):
1. Check `docker logs <nats container>` and disk space on the `nats_data`
   volume's backing filesystem.
2. If the on-disk store is corrupted beyond the server's own recovery, the
   only path back is `scripts/backup/restore-nats.sh` against the most
   recent backup (§9) - there is no replica to fail over to (single-node
   JetStream). Any events published after that backup but not yet consumed
   are lost from the stream directly, but the appointment-service outbox
   (Postgres-backed, `publish_attempts`/`failed_at` bounded retry) will
   re-attempt publishing any outbox row it never got a confirmed publish
   for, once `nats` is back - this is the designed recovery path, not a
   manual reconciliation.
3. Restart dependents after `nats` is healthy again:
   `dc up -d appointment-service notification-service`.

**Redis**: if `redis` becomes unhealthy, auth-service/customer-service's
rate limiting is unavailable but each service's readiness check treats
Redis as a soft dependency (see Phase 6.6/7A `dependency_up` gauge) - it
does not take the whole service down. Restart `redis`
(`dc up -d redis`), then confirm `dependency_up{dependency="redis"}` returns
to `1` on `/metrics` for `auth-service`, `tenant-service`, and
`customer-service`.

## 12. Observability checks

(Full detail: Phase 7A. Dev-only stack: `docker-compose.observability.yml`,
**never** applied to production - see that file's own comment header.)

Production has no bundled Prometheus/Grafana; each backend and the gateway
expose `/metrics` (Prometheus exposition format) on their private port only
- never proxied publicly (verified by
`gateway-service`'s `TestMetricsRouteTableNeverProxiesToABackend`/
`TestMetricsIsServedLocallyNeverProxied` tests). Point your own external
Prometheus (or an ops host inside the `private` network) at each service's
`:8080/metrics`.

Minimum checks after any deploy or incident:
- `dependency_up{service=<svc>,dependency=<postgres|redis|nats>}` is `1`
  for every service that depends on it.
- `outbox_backlog` / `outbox_oldest_pending_age_seconds`
  (appointment-service) are not climbing.
- `outbox_terminal_total` / `nats_messages_terminal_total` /
  `notification_failed_total` are not incrementing unexpectedly (each
  represents an event or notification that exhausted its bounded retries -
  see Phase 6.6/6.6-follow-through).
- `http_requests_total{status=~"5.."}` is not elevated for any service.
- Every `/ready` returns 200 (`dc ps` health status reflects this already).
- OpenTelemetry tracing is optional in production (unset
  `OTEL_EXPORTER_OTLP_ENDPOINT` = no-op tracer, never blocks startup/
  readiness) - if you do wire a collector in production, point
  `OTEL_EXPORTER_OTLP_ENDPOINT` at it via the same overlay pattern as the
  dev stack, on the `private` network only.
