#!/bin/sh
# Restore the newest encrypted snapshot to disposable local storage and verify
# every PostgreSQL dump is readable. A quarterly isolated full service restore
# remains required; see the runbook.
set -eu
umask 077
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is required}"
: "${RESTIC_PASSWORD:?RESTIC_PASSWORD is required}"
: "${BACKUP_VERIFY_ROOT:?BACKUP_VERIFY_ROOT is required}"
: "${BACKUP_VERIFY_HEARTBEAT_URL:?BACKUP_VERIFY_HEARTBEAT_URL is required}"
restic_image=${RESTIC_IMAGE:-restic/restic:0.18.0@sha256:4cf4a61ef9786f4de53e9de8c8f5c040f33830eb0a10bf3d614410ee2fcb6120}
postgres_image=${BACKUP_POSTGRES_IMAGE:-postgres:16.15-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685}
mkdir -p "$BACKUP_VERIFY_ROOT"
chmod 700 "$BACKUP_VERIFY_ROOT"
dest=$(mktemp -d "$BACKUP_VERIFY_ROOT/verify.XXXXXX")
cleanup() { case "$dest" in "$BACKUP_VERIFY_ROOT"/verify.*) rm -rf -- "$dest" ;; esac; }
failed() { curl -fsS --max-time 15 "$BACKUP_VERIFY_HEARTBEAT_URL/fail" >/dev/null 2>&1 || true; cleanup; }
on_exit() {
  exit_status=$?
  trap - EXIT HUP INT TERM
  [ "$exit_status" -eq 0 ] || failed
  exit "$exit_status"
}
trap 'exit 1' HUP INT TERM
trap on_exit EXIT

docker run --rm -e RESTIC_REPOSITORY -e RESTIC_PASSWORD \
  -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -e GOOGLE_PROJECT_ID \
  -v "$dest:/restore" \
  "$restic_image" restore latest --tag production --target /restore

backup_root=$(find "$dest" -type d -path '*/backup' -print | head -n1)
[ -n "$backup_root" ] || { echo "restored snapshot has no backup directory" >&2; exit 1; }
for db in tenant auth barber catalog scheduling appointment customer notification; do
  dump=$(find "$backup_root/postgres" -type f -name "${db}_db.dump" -print | head -n1)
  [ -s "$dump" ] || { echo "missing ${db}_db.dump" >&2; exit 1; }
  docker run --rm -v "$dump:/backup.dump:ro" "$postgres_image" pg_restore --list /backup.dump >/dev/null
done
find "$backup_root/postgres" -type f -name globals.sql -size +0c | grep -q .
find "$backup_root/nats" -type f -name MANIFEST | grep -q .
curl -fsS --max-time 15 "$BACKUP_VERIFY_HEARTBEAT_URL" >/dev/null
trap - EXIT HUP INT TERM
cleanup
echo "latest encrypted snapshot restored and backup formats verified"
