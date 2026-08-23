#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
app=${1:-}
case "$app" in admin-web|booking-web) ;; *) echo "usage: $0 {admin-web|booking-web}" >&2; exit 2;; esac
cd "$repo/apps/$app"
npm ci
npm run lint
npm run typecheck
npm test
npm run build
