#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
file=${1:-}
case "$file" in services/*-service/migrations/*.sql) ;; *) echo "usage: $0 services/<service>/migrations/<new>.sql" >&2; exit 2;; esac
cd "$repo"
[ -f "$file" ] || { echo "migration does not exist: $file" >&2; exit 1; }
if grep -F "  $file" scripts/ci/migration-checksums.sha256 >/dev/null; then
  echo "migration already has an immutable checksum: $file" >&2
  exit 1
fi
sha256sum "$file" >> scripts/ci/migration-checksums.sha256
LC_ALL=C sort -o scripts/ci/migration-checksums.sha256 scripts/ci/migration-checksums.sha256
