#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
manifest="$repo/scripts/ci/migration-checksums.sha256"
cd "$repo"
sha256sum -c "$manifest"
expected=$(sed 's/^.*  //' "$manifest" | sort)
actual=$(find services -path '*/migrations/*.sql' -type f | sort)
if [ "$expected" != "$actual" ]; then
  echo "migration checksum manifest is incomplete; add new forward migrations with scripts/ci/add-migration-checksum.sh" >&2
  exit 1
fi
