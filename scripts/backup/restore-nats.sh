#!/bin/sh
# restore-nats.sh - restore JetStream streams from a backup produced by
# backup-nats.sh.
#
# `nats stream restore` recreates each stream (using the configuration
# captured in the backup) and replays its messages into it over the
# JetStream network API. It refuses to restore over a stream name that
# already exists -- if you are restoring in place (not into a fresh `nats`
# instance), remove or purge the existing stream first. See RUNBOOK.md.
#
# Usage:
#   scripts/backup/restore-nats.sh <backup_dir> [stream ...]
#
# With no stream names given, all streams listed in <backup_dir>/MANIFEST
# are restored.
#
# Env:
#   BACKUP_COMPOSE_PROJECT  Compose project name (default: barber-appointment)
#   BACKUP_NATS_IMAGE       NATS CLI client image
#                           (default: pinned nats-box 0.19.2 image)
#   NATS_USER, NATS_PASSWORD
#                           Credentials for the `nats` service -- the same
#                           variables the compose files already read.
#                           Required. Never printed.
set -eu

backup_dir=${1:?usage: restore-nats.sh <backup_dir> [stream ...]}
shift

project=${BACKUP_COMPOSE_PROJECT:-barber-appointment}
nats_image=${BACKUP_NATS_IMAGE:-natsio/nats-box:0.19.2@sha256:8031d190c7ee24081f3f27cc939fb647a1eeb29ebb5c60fef9b5b6c7a846d6a2}
: "${NATS_USER:?NATS_USER must be set (same value the nats service uses)}"
: "${NATS_PASSWORD:?NATS_PASSWORD must be set (same value the nats service uses)}"

[ -d "$backup_dir" ] || { echo "backup dir not found: $backup_dir" >&2; exit 1; }

network=$(docker network ls \
  --filter "label=com.docker.compose.project=$project" \
  --filter "label=com.docker.compose.network=private" \
  --format '{{.Name}}' | head -n1)
[ -n "$network" ] || {
  echo "could not find the 'private' Compose network for project '$project' (is the stack up?)" >&2
  exit 1
}

if [ "$#" -gt 0 ]; then
  streams="$*"
elif [ -f "$backup_dir/MANIFEST" ]; then
  streams=$(cat "$backup_dir/MANIFEST")
else
  echo "no stream names given and no MANIFEST found in $backup_dir" >&2
  exit 1
fi

for stream in $streams; do
  [ -d "$backup_dir/$stream" ] || { echo "backup for stream not found: $backup_dir/$stream" >&2; exit 1; }
  echo "==> restoring stream $stream from $backup_dir/$stream"
  docker run --rm --network "$network" -e NATS_USER -e NATS_PASSWORD -v "$backup_dir:/backup:ro" "$nats_image" \
    nats stream restore "/backup/$stream" -s nats://nats:4222 --no-progress
done

echo "==> restore complete"
