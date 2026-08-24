#!/bin/sh
# restore-postgres.sh - restore per-service PostgreSQL databases from a
# backup produced by backup-postgres.sh.
#
# Restore strategy: `pg_restore --clean --if-exists` drops and recreates
# objects *inside* each already-existing target database, rather than
# dropping/recreating the database itself. That means the target databases
# (and their owner/migrator/app roles) must already exist -- normally true
# because a fresh Postgres instance runs
# infrastructure/postgres/init/00-databases.sh on first boot. If you are
# restoring onto a completely blank data directory that never ran the init
# scripts, pass --restore-globals first to recreate the roles from the
# backup's globals.sql, then create empty databases owned by the matching
# *_db_owner role before restoring data into them (00-databases.sh shows the
# exact role/ownership shape expected).
#
# Usage:
#   scripts/backup/restore-postgres.sh [--restore-globals] <backup_dir> [db ...]
#
# With no database names given, all databases listed in <backup_dir>/MANIFEST
# are restored.
#
# Env:
#   BACKUP_COMPOSE_PROJECT  Compose project name (default: barber-appointment)
#   BACKUP_COMPOSE_FILES    Space-separated -f compose file list
#                           (default: "<repo>/docker-compose.yml")
#   POSTGRES_PASSWORD       Superuser password for the `postgres` role.
#                           Required. Never printed.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)

restore_globals=0
if [ "${1:-}" = "--restore-globals" ]; then
  restore_globals=1
  shift
fi

backup_dir=${1:?usage: restore-postgres.sh [--restore-globals] <backup_dir> [db ...]}
shift

project=${BACKUP_COMPOSE_PROJECT:-barber-appointment}
files=${BACKUP_COMPOSE_FILES:-"$repo/docker-compose.yml"}
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set (same value the postgres service uses)}"

[ -d "$backup_dir" ] || { echo "backup dir not found: $backup_dir" >&2; exit 1; }

compose_args=""
for f in $files; do compose_args="$compose_args -f $f"; done

compose() {
  # shellcheck disable=SC2086
  docker compose -p "$project" $compose_args "$@"
}

if [ "$#" -gt 0 ]; then
  databases="$*"
elif [ -f "$backup_dir/MANIFEST" ]; then
  databases=$(cat "$backup_dir/MANIFEST")
else
  echo "no database names given and no MANIFEST found in $backup_dir" >&2
  exit 1
fi

if [ "$restore_globals" -eq 1 ]; then
  [ -f "$backup_dir/globals.sql" ] || { echo "globals.sql not found in $backup_dir" >&2; exit 1; }
  echo "==> restoring globals (roles) from $backup_dir/globals.sql"
  # ON_ERROR_STOP=0: globals.sql unconditionally (re)creates roles: on a
  # target that already has some of them, "role already exists" errors are
  # expected and harmless -- we want the run to continue past those rather
  # than abort partway through the role list.
  compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
    psql -U postgres -v ON_ERROR_STOP=0 < "$backup_dir/globals.sql"
fi

for db in $databases; do
  dump="$backup_dir/$db.dump"
  [ -f "$dump" ] || { echo "dump not found: $dump" >&2; exit 1; }
  echo "==> restoring $db from $dump"
  compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
    pg_restore -U postgres -d "$db" --clean --if-exists < "$dump"
done

echo "==> restore complete"
