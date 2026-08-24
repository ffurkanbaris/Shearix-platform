#!/bin/sh
# backup-postgres.sh - Dump every per-service PostgreSQL database from a
# running `postgres` Compose service to timestamped, compressed dump files.
#
# Approach: this repo runs ONE Postgres instance holding multiple per-service
# logical databases (tenant_db, auth_db, barber_db, catalog_db,
# scheduling_db, appointment_db, notification_db, customer_db), each owned
# by its own owner/migrator/app roles (see
# infrastructure/postgres/init/00-databases.sh). We back up:
#   1. `pg_dumpall --globals-only` once, to capture role definitions
#      (owner/migrator/app roles and their password hashes) that live
#      outside any single database.
#   2. `pg_dump -Fc` (custom format) once per logical database.
#
# Per-database custom-format dumps were chosen over one `pg_dumpall` data
# dump because:
#   - they let you restore (or inspect) one service's database without
#     touching the other seven sharing the instance;
#   - -Fc is compressed by default and supports parallel/selective restore
#     via pg_restore, unlike pg_dumpall's single plain-SQL stream;
#   - a bad/partial dump of one database does not invalidate the others.
#
# pg_dump/pg_dumpall take an internal MVCC snapshot per database and do not
# block concurrent readers or writers, so this is safe to run against a live
# production instance without stopping any service. It does briefly need a
# few short catalog locks (ACCESS SHARE), which can be blocked by, and
# briefly block, concurrent DDL (e.g. a migration) -- see RUNBOOK.md.
#
# Usage:
#   scripts/backup/backup-postgres.sh [output_dir]
#
# Output: <output_dir>/<UTC timestamp>/globals.sql, one <db>.dump per
# database, and a MANIFEST listing the databases included.
#
# Env:
#   BACKUP_COMPOSE_PROJECT  Compose project name running the stack
#                           (default: barber-appointment)
#   BACKUP_COMPOSE_FILES    Space-separated list of -f compose files
#                           (default: "<repo>/docker-compose.yml"; pass the
#                           production overlay explicitly to target prod,
#                           e.g. "<repo>/docker-compose.yml
#                           <repo>/infrastructure/caddy/docker-compose.production.yml")
#   POSTGRES_PASSWORD       Superuser password for the `postgres` role --
#                           the same variable the compose files already
#                           read. Required. Never printed.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)

project=${BACKUP_COMPOSE_PROJECT:-barber-appointment}
files=${BACKUP_COMPOSE_FILES:-"$repo/docker-compose.yml"}
out_root=${1:-"$PWD/pg-backups"}
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set (same value the postgres service uses)}"

compose_args=""
for f in $files; do compose_args="$compose_args -f $f"; done

compose() {
  # shellcheck disable=SC2086
  docker compose -p "$project" $compose_args "$@"
}

databases="tenant_db auth_db barber_db catalog_db scheduling_db appointment_db notification_db customer_db"

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dest="$out_root/$stamp"
mkdir -p "$dest"

echo "==> project=$project dest=$dest"
echo "==> backing up globals (roles) -> $dest/globals.sql"
compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
  pg_dumpall -U postgres --globals-only > "$dest/globals.sql"

for db in $databases; do
  echo "==> dumping $db -> $dest/$db.dump"
  compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
    pg_dump -U postgres -d "$db" -Fc > "$dest/$db.dump"
done

for db in $databases; do printf '%s\n' "$db"; done > "$dest/MANIFEST"

echo "==> backup complete: $dest"
