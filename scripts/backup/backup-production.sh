#!/bin/sh
# Create logical Postgres and JetStream backups, then encrypt and upload them
# with restic. Plaintext exists only in a mode-0700 staging directory.
set -eu
umask 077
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is required}"
: "${RESTIC_PASSWORD:?RESTIC_PASSWORD is required}"
: "${BACKUP_STAGING_ROOT:?BACKUP_STAGING_ROOT is required}"
: "${BACKUP_HEARTBEAT_URL:?BACKUP_HEARTBEAT_URL is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${NATS_USER:?NATS_USER is required}"
: "${NATS_PASSWORD:?NATS_PASSWORD is required}"

restic_image=${RESTIC_IMAGE:-restic/restic:0.18.0@sha256:4cf4a61ef9786f4de53e9de8c8f5c040f33830eb0a10bf3d614410ee2fcb6120}
mkdir -p "$BACKUP_STAGING_ROOT"
chmod 700 "$BACKUP_STAGING_ROOT"
stage=$(mktemp -d "$BACKUP_STAGING_ROOT/run.XXXXXX")

cleanup() {
  case "$stage" in "$BACKUP_STAGING_ROOT"/run.*) rm -rf -- "$stage" ;; esac
}
failed() {
  curl -fsS --max-time 15 "$BACKUP_HEARTBEAT_URL/fail" >/dev/null 2>&1 || true
  cleanup
}
on_exit() {
  exit_status=$?
  trap - EXIT HUP INT TERM
  [ "$exit_status" -eq 0 ] || failed
  exit "$exit_status"
}
trap 'exit 1' HUP INT TERM
trap on_exit EXIT

restic() {
  docker run --rm \
    -e RESTIC_REPOSITORY -e RESTIC_PASSWORD \
    -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY \
    -v "$stage:/backup:ro" "$restic_image" "$@"
}

mkdir -p "$stage/postgres" "$stage/nats"
"$repo/scripts/backup/backup-postgres.sh" "$stage/postgres"
"$repo/scripts/backup/backup-nats.sh" "$stage/nats"

if ! restic cat config >/dev/null 2>&1; then restic init; fi
restic backup /backup --tag production --tag automated
restic forget --prune \
  --keep-hourly "${RESTIC_KEEP_HOURLY:-24}" \
  --keep-daily "${RESTIC_KEEP_DAILY:-14}" \
  --keep-weekly "${RESTIC_KEEP_WEEKLY:-8}" \
  --keep-monthly "${RESTIC_KEEP_MONTHLY:-12}"

curl -fsS --max-time 15 "$BACKUP_HEARTBEAT_URL" >/dev/null
trap - EXIT HUP INT TERM
cleanup
echo "encrypted off-host production backup completed"
