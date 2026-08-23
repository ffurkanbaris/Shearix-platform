#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
mode=${1:-check}
case "$mode" in generate|check) ;; *) echo "usage: $0 {generate|check}" >&2; exit 2;; esac

for module in tenant-service auth-service barber-service catalog-service scheduling-service appointment-service; do
  docker run --rm -v "$repo:/repo" -w "/repo/services/$module" sqlc/sqlc:1.29.0 generate
done

if [ "$mode" = check ]; then
  if ! command -v git >/dev/null 2>&1 || ! git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "git worktree unavailable; sqlc freshness check cannot run" >&2
    exit 1
  fi
  git -C "$repo" diff --exit-code -- 'services/*/generated'
fi
