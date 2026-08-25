#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
mode=${1:-check}
case "$mode" in generate|check) ;; *) echo "usage: $0 {generate|check}" >&2; exit 2;; esac

sqlc_image=sqlc/sqlc:1.29.0@sha256:0de3f476fb9c11192b4dd5413c531512e9fc5b3ec3cce075b7d7b4709a0c3e58

if [ "$mode" = check ]; then
  snapshot=$(mktemp -d)
  trap 'rm -rf "$snapshot"' EXIT
  for module in tenant-service auth-service barber-service catalog-service scheduling-service appointment-service; do
    mkdir -p "$snapshot/$module"
    cp -R "$repo/services/$module/generated" "$snapshot/$module/generated"
  done
fi

for module in tenant-service auth-service barber-service catalog-service scheduling-service appointment-service; do
  docker run --rm -v "$repo:/repo" -w "/repo/services/$module" "$sqlc_image" generate
done

if [ "$mode" = check ]; then
  for module in tenant-service auth-service barber-service catalog-service scheduling-service appointment-service; do
    if ! diff -ru "$snapshot/$module/generated" "$repo/services/$module/generated"; then
      echo "sqlc output is stale for $module; regenerate and commit it" >&2
      exit 1
    fi
  done
fi
