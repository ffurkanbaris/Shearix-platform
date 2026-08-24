#!/bin/sh
# backup-nats.sh - back up JetStream stream data (messages + consumer state)
# from the `nats` Compose service.
#
# Approach: this repo's NATS runs as a single (non-clustered) file-store
# JetStream instance (`-js -sd /data`, volume `nats_data`). We back up over
# the JetStream network API (`nats stream backup`, via the official
# `natsio/nats-box` client image) rather than taking a filesystem-level
# snapshot of the nats_data volume, because:
#   - it works against a live, running server -- no need to stop `nats` or
#     any publisher/consumer, so there is no downtime;
#   - JetStream's own snapshot mechanism produces a self-consistent
#     per-stream backup, instead of raw files that could be mid-write if
#     copied from a live volume;
#   - the backup restores through the same API regardless of the server's
#     on-disk storage layout/version, whereas a raw volume copy only
#     restores cleanly onto a compatible NATS server version.
# Tradeoff: this streams data over the network rather than doing a raw file
# copy, so it is slower for very large stores; that's an acceptable cost at
# this repo's JetStream data volumes (small, short-lived domain-event
# streams -- Postgres, not JetStream, is the durable system of record).
#
# Usage:
#   scripts/backup/backup-nats.sh [output_dir]
#
# Output: <output_dir>/<UTC timestamp>/<stream>/... (one directory per
# stream, JetStream's native backup format) plus a MANIFEST of stream names.
#
# Env:
#   BACKUP_COMPOSE_PROJECT  Compose project name running the stack
#                           (default: barber-appointment)
#   BACKUP_NATS_IMAGE       NATS CLI client image
#                           (default: natsio/nats-box:latest)
#   NATS_USER, NATS_PASSWORD
#                           Credentials for the `nats` service -- the same
#                           variables the compose files already read
#                           (infrastructure/nats/nats-server.conf requires
#                           auth in every environment). Required. Never
#                           printed.
set -eu

project=${BACKUP_COMPOSE_PROJECT:-barber-appointment}
nats_image=${BACKUP_NATS_IMAGE:-natsio/nats-box:latest}
out_root=${1:-"$PWD/nats-backups"}
: "${NATS_USER:?NATS_USER must be set (same value the nats service uses)}"
: "${NATS_PASSWORD:?NATS_PASSWORD must be set (same value the nats service uses)}"

network=$(docker network ls \
  --filter "label=com.docker.compose.project=$project" \
  --filter "label=com.docker.compose.network=private" \
  --format '{{.Name}}' | head -n1)
[ -n "$network" ] || {
  echo "could not find the 'private' Compose network for project '$project' (is the stack up?)" >&2
  exit 1
}

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dest="$out_root/$stamp"
mkdir -p "$dest"

echo "==> project=$project network=$network dest=$dest"
echo "==> discovering JetStream streams"
streams=$(docker run --rm --network "$network" -e NATS_USER -e NATS_PASSWORD "$nats_image" \
  nats stream ls -n -s nats://nats:4222 2>/dev/null | tr -d '\r' || true)

if [ -z "$streams" ]; then
  echo "==> no JetStream streams found, nothing to back up"
  : > "$dest/MANIFEST"
  exit 0
fi

for stream in $streams; do
  echo "==> backing up stream $stream -> $dest/$stream"
  docker run --rm --network "$network" -e NATS_USER -e NATS_PASSWORD -v "$dest:/backup" "$nats_image" \
    nats stream backup "$stream" "/backup/$stream" -s nats://nats:4222 --no-progress
done

printf '%s\n' "$streams" > "$dest/MANIFEST"
echo "==> backup complete: $dest"
