# PostgreSQL + NATS JetStream Backup/Restore Runbook

Scope: the shared `postgres` instance (8 per-service logical databases) and
the `nats` JetStream store. Scripts live in `scripts/backup/`. Redis is
intentionally out of scope -- see "Redis" below.

All scripts are POSIX `sh`, follow the `scripts/ci/*.sh` conventions of this
repo (`set -eu`, a `repo=$(...)` header, parameterized Compose project/files,
no hardcoded secrets), and read credentials only from the same environment
variables the Compose files already use.

## Prerequisites

- `docker` and `docker compose` (v2) on the host running the backup.
- Network egress to pull `natsio/nats-box` (the official NATS CLI client
  image) the first time it's used -- pull/pin it into your ops image ahead
  of time if the backup host is air-gapped from Docker Hub.
- The target stack (dev or production Compose project) already running, with
  `postgres` and `nats` healthy.
- The same secrets the stack itself uses: `POSTGRES_PASSWORD` (Postgres
  superuser) and `NATS_USER`/`NATS_PASSWORD` (NATS client auth -- required in
  every environment, see `infrastructure/nats/nats-server.conf`). These
  scripts never hardcode or print secret values.

## Parameterizing target (dev vs. production)

Both backup and restore scripts accept:

- `BACKUP_COMPOSE_PROJECT` -- the Compose project name of the stack to act
  on (default `barber-appointment`). Use the actual project name in use,
  e.g. whatever `-p`/`COMPOSE_PROJECT_NAME` production deploys with.
- `BACKUP_COMPOSE_FILES` (Postgres scripts only) -- space-separated `-f`
  file list (default `<repo>/docker-compose.yml`). Add the production
  overlay explicitly when targeting production:
  `BACKUP_COMPOSE_FILES="<repo>/docker-compose.yml <repo>/infrastructure/caddy/docker-compose.production.yml"`.
- The NATS scripts don't need `-f` files -- they reach `nats` directly over
  the Compose project's `private` Docker network (found by Compose project
  label, not by guessing the network name), so they work unmodified against
  any project.

## Postgres: backup

```sh
export BACKUP_COMPOSE_PROJECT=barber-appointment   # or your prod project name
export POSTGRES_PASSWORD='...'                     # same value the stack uses
scripts/backup/backup-postgres.sh /path/to/backup/root
```

Produces `/path/to/backup/root/<UTC timestamp>/`:
- `globals.sql` -- `pg_dumpall --globals-only` (roles + password hashes,
  not plaintext).
- `<db>.dump` -- one `pg_dump -Fc` (custom format, compressed) per logical
  database: `tenant_db auth_db barber_db catalog_db scheduling_db
  appointment_db notification_db customer_db`.
- `MANIFEST` -- the database names included, used as the default restore set.

**Why per-database `-Fc` dumps instead of one `pg_dumpall` data dump:** this
instance hosts 8 independently-owned service databases. Per-database dumps
let you restore (or inspect) one service without touching the other seven,
support parallel/selective restore via `pg_restore`, and are compressed by
default. `pg_dumpall --globals-only` is used just once, only for the
instance-wide role definitions that live outside any single database.

**Live-safety:** `pg_dump`/`pg_dumpall` take an internal MVCC snapshot per
database and do not block concurrent reads or writes. It is safe to run
against a live production instance. It does take brief `ACCESS SHARE`
catalog locks, which can queue behind (and briefly block) a concurrent
`ALTER TABLE`/migration -- avoid running a backup during a deploy that runs
migrations, or expect a short stall on either side if you do.

## Postgres: restore

```sh
export BACKUP_COMPOSE_PROJECT=barber-appointment
export POSTGRES_PASSWORD='...'
scripts/backup/restore-postgres.sh /path/to/backup/root/<timestamp>
# or restore only specific databases:
scripts/backup/restore-postgres.sh /path/to/backup/root/<timestamp> tenant_db auth_db
```

`pg_restore --clean --if-exists` drops and recreates objects **inside**
each already-existing target database -- it does not drop/recreate the
database itself. This means:

- The target databases, and their `*_db_owner` / `*_db_migrator` /
  `*_db_app` roles, must already exist. In the normal case this is true
  because the Postgres instance ran
  `infrastructure/postgres/init/00-databases.sh` on first boot (dev and
  production both mount this).
- Restoring onto a genuinely blank data directory that never ran the init
  scripts: pass `--restore-globals` first to recreate the roles from
  `globals.sql`, then create each target database
  (`CREATE DATABASE <db> OWNER <db>_owner;`) before restoring data into it.
  `restore-postgres.sh --restore-globals <dir> ...` does the globals step
  for you; database creation is deliberately left to you (or a fresh init
  run), since this repo's role/ownership shape is defined once in
  `00-databases.sh` and should not be duplicated in a second place.
- No service needs to be stopped for the restore of a database it owns to
  succeed, but the service **will observe an inconsistent database** for
  the (usually sub-second-to-seconds) duration of the restore, since
  `--clean` drops tables before recreating them. For a true point-in-time
  restore (not just testing/validation), stop the owning service(s) first.

## Postgres: verifying a restore

Query the restored data back through the owning role, the same way the
verification test below does:

```sh
docker compose -p "$BACKUP_COMPOSE_PROJECT" -f docker-compose.yml exec -T \
  -e PGPASSWORD="$TENANT_DB_OWNER_PASSWORD" postgres \
  psql -U tenant_db_migrator -d tenant_db -c "SET ROLE tenant_db_owner; SELECT count(*) FROM <a real table>;"
```

Compare row counts / known rows against what you expect from the backup
point. For a stronger check, compare `pg_dump --schema-only` of the restored
database against the backup source, and/or run the service's own
integration test suite (`scripts/ci/integration.sh`) against the restored
database.

## NATS JetStream: backup

```sh
export BACKUP_COMPOSE_PROJECT=barber-appointment
export NATS_USER='...'
export NATS_PASSWORD='...'
scripts/backup/backup-nats.sh /path/to/backup/root
```

Discovers all streams (`nats stream ls -n`) and runs `nats stream backup
<stream> <dir>` (JetStream's network-API snapshot, via the official
`natsio/nats-box` client) for each, including consumer definitions. Produces
`/path/to/backup/root/<UTC timestamp>/<stream>/...` plus a `MANIFEST` of
stream names.

**Why the JetStream network API instead of a raw `nats_data` volume
snapshot:** this repo runs one, non-clustered, file-store JetStream server.
The `nats stream backup` approach:
- runs against the live server with no downtime -- `nats`, its publishers
  (e.g. the appointment-service outbox publisher), and its consumers all
  keep running;
- produces a self-consistent per-stream snapshot via JetStream's own
  mechanism, instead of raw files that could be mid-write if copied from a
  live volume with e.g. `docker run --volumes-from`;
- restores through the same API regardless of the server's on-disk storage
  version, whereas a raw volume copy only restores cleanly onto a
  byte-compatible NATS server version.

Tradeoff: it streams data over the network rather than doing a raw file
copy, so it is slower for very large stores. That's an acceptable cost here
-- this repo's streams hold short-lived domain-event data (e.g. the
`APPOINTMENTS` stream used by the outbox publisher); **Postgres, not
JetStream, is the durable system of record**, so JetStream backups are a
convenience/RTO optimization, not the last line of defense.

## NATS JetStream: restore

```sh
export BACKUP_COMPOSE_PROJECT=barber-appointment
export NATS_USER='...'
export NATS_PASSWORD='...'
scripts/backup/restore-nats.sh /path/to/backup/root/<timestamp>
# or restore only specific streams:
scripts/backup/restore-nats.sh /path/to/backup/root/<timestamp> APPOINTMENTS
```

`nats stream restore` recreates the stream from its captured config and
replays all messages (and consumers) into it. **It refuses to restore over
a stream name that already exists.** If restoring in place (not into a
freshly emptied `nats`), remove or purge the existing stream first:

```sh
docker run --rm --network <project>_private -e NATS_USER -e NATS_PASSWORD \
  natsio/nats-box:latest nats -s nats://nats:4222 stream rm <STREAM> -f
```

## NATS JetStream: verifying a restore

Check stream state and actually consume:

```sh
docker run --rm --network <project>_private -e NATS_USER -e NATS_PASSWORD \
  natsio/nats-box:latest nats -s nats://nats:4222 stream info <STREAM>
# messages/first_seq/last_seq should match the backup's stream info

docker run --rm --network <project>_private -e NATS_USER -e NATS_PASSWORD \
  natsio/nats-box:latest nats -s nats://nats:4222 consumer next <STREAM> <CONSUMER> --count N
# confirms messages are not just present but consumable via the restored consumer
```

## RPO / RTO caveats

- **RPO = time since the last backup run.** Neither Postgres nor NATS backup
  here is continuous/streaming (no WAL archiving, no JetStream mirroring to
  a standby). Run these scripts on a schedule (e.g. cron/systemd timer) and
  size the schedule to the RPO your ops requirements demand -- e.g. hourly
  backups mean up to ~1h of data loss in the worst case.
- **RTO** is dominated by: (a) how long `pg_restore`/`nats stream restore`
  takes for your actual data volume (both scale roughly linearly with
  data size; per-database/per-stream parallelism is possible but not wired
  up in these scripts), and (b) how long it takes an operator to identify
  the correct backup timestamp and start the restore. Neither script
  supports point-in-time recovery within a backup interval -- restores land
  exactly on a backup's timestamp, nothing finer-grained.
- Postgres backups here are **logical**, not physical/WAL-based -- no
  continuous archiving is configured (no `archive_command`/replication
  slot). If a tighter RPO than "time between dump runs" is required later,
  that needs WAL archiving/PITR set up separately; these scripts don't
  provide it.
- NATS backups snapshot **stream data as of the moment `nats stream backup`
  runs**; a message published mid-backup may or may not be included
  depending on timing relative to the snapshot, same as Postgres's MVCC
  snapshot semantics.
- Restoring Postgres data does **not** restore anything published to NATS
  in between (and vice versa) -- the two backups are not transactionally
  consistent with each other. If both are restored to recover from an
  incident, expect the appointment-service outbox to attempt to re-publish
  events for any Postgres row it finds unpublished, which is the intended
  at-least-once recovery path already built into the outbox publisher; a
  human should not need to manually reconcile stream contents against
  Postgres rows.

## Operational gotchas encountered

- The Postgres backup/restore scripts authenticate as the `postgres`
  superuser (`POSTGRES_PASSWORD`) rather than per-database owner
  passwords, so a single credential backs up/restores every database in
  one run. This is a deliberate simplification (fewer secrets to plumb
  through one script) -- it does mean whoever runs these scripts needs the
  Postgres superuser password, which is a higher-privilege secret than any
  individual service holds.
  `pg_restore --clean --if-exists` still respects per-database ownership:
  restored objects come back owned by the same `*_db_owner` role captured
  in the dump, so `00-databases.sh`'s `REVOKE ALL ... FROM PUBLIC` posture
  is not weakened by restoring as superuser.
- `nats stream backup`/`restore` need a NATS *client* (the CLI), not just
  network reachability to the server -- these scripts run the official
  `natsio/nats-box` image as a one-off `docker run` attached to the
  target Compose project's `private` network (resolved by Compose project
  label, so it works for any project name without hardcoding a network
  name).
- `nats stream restore` errors out if the stream already exists (see
  "NATS JetStream: restore" above) -- this is the one place where a restore
  onto a still-running, non-empty target needs a manual pre-step.
- Both NATS scripts require `NATS_USER`/`NATS_PASSWORD` to be set --
  `infrastructure/nats/nats-server.conf` requires authentication in every
  environment (dev included), there is no anonymous-access fallback.

## What was actually verified

An end-to-end backup -> wipe -> restore -> verify cycle was run for both
Postgres and NATS JetStream against a throwaway, uniquely-named Compose
project (`down -v` at the end, no containers/networks/volumes left behind):

- **Postgres:** created a table with 3 rows in `tenant_db` (as
  `tenant_db_owner`), ran `backup-postgres.sh`, dropped the table, ran
  `restore-postgres.sh <backup> tenant_db`, queried the table back via
  `tenant_db_migrator`/`SET ROLE tenant_db_owner` and got the same 3 rows.
- **NATS:** created stream `TESTSTREAM` with a durable pull consumer,
  published 3 messages, ran `backup-nats.sh`, deleted the stream entirely
  (`nats stream rm -f`), ran `restore-nats.sh <backup>`, confirmed
  `stream info` showed 3 messages / 1 active consumer, then pulled all 3
  messages through the restored consumer and got back the original payloads
  (`hello-1`, `hello-2`, `hello-3`) in order.

## Redis

Redis in this repo (`platform/ratelimit`) holds **only short-lived
fixed-window rate-limit counters** -- keys with per-window TTLs, recreated
from scratch on the next request after expiry, holding no data that is a
source of truth anywhere. `--appendonly yes` is enabled (AOF persistence),
but losing it entirely just means rate-limit counters reset to zero, which
is a momentary, self-healing availability blip, not a data-loss incident.

**Decision: no backup/restore tooling was built for Redis.** Backing up
transient counters would (a) restore stale rate-limit state that may be
more confusing than an empty reset, and (b) add operational surface for
data that doesn't need durability guarantees. If Redis's role in this repo
ever expands to hold durable business data, this decision should be
revisited.

## Known limitations

- No automated scheduling is provided -- these are one-shot scripts,
  intended to be invoked by cron/systemd timer/CI on whatever cadence meets
  your RPO target.
- No backup encryption or off-host upload is implemented -- dumps/snapshots
  land on local disk (`output_dir`) as-is; wire up encryption-at-rest and
  transport to durable/offsite storage (S3, etc.) as a wrapper around these
  scripts before relying on them for disaster recovery.
- No retention/rotation policy is implemented -- old timestamped backup
  directories accumulate until pruned externally.
- `restore-postgres.sh` does not create missing databases -- see "Postgres:
  restore" above for the blank-instance path.
- These scripts were verified against a single-node dev-shaped Postgres and
  a single-node (non-clustered) NATS. Production-scale data volumes, and
  any future NATS clustering, were not exercised here and may change
  runtime/RTO expectations.
