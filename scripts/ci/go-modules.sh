#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
manifest="$repo/scripts/ci/go-modules.txt"
action=${1:-}

case "$action" in
  tidy-check|vet|build|test|race|coverage) ;;
  *) echo "usage: $0 {tidy-check|vet|build|test|race|coverage}" >&2; exit 2 ;;
esac

discovered=$(find "$repo" -name go.mod -not -path '*/vendor/*' -printf '%P\n' | sed 's#/go.mod$##' | sort)
declared=$(sort "$manifest")
if [ "$discovered" != "$declared" ]; then
  echo "Go module manifest is stale. Discovered modules:" >&2
  printf '%s\n' "$discovered" >&2
  exit 1
fi

if [ "$action" = coverage ]; then
  mkdir -p "$repo/coverage"
fi

if [ "$action" = tidy-check ]; then
  tidy_snapshot=$(mktemp)
  trap 'rm -f "$tidy_snapshot"' EXIT
  while IFS= read -r module; do
    sha256sum "$repo/$module/go.mod" "$repo/$module/go.sum" >>"$tidy_snapshot"
  done < "$manifest"
fi

while IFS= read -r module; do
  [ -n "$module" ] || continue
  echo "==> $action: $module"
  case "$action" in
    tidy-check)
      (cd "$repo/$module" && GOWORK=off go mod tidy)
      ;;
    vet) (cd "$repo/$module" && GOWORK=off go vet ./...) ;;
    build) (cd "$repo/$module" && GOWORK=off go build ./...) ;;
    test) (cd "$repo/$module" && GOWORK=off go test ./...) ;;
    race) (cd "$repo/$module" && GOWORK=off go test -race ./...) ;;
    coverage)
      output=$(printf '%s' "$module" | tr '/' '_')
      (cd "$repo/$module" && GOWORK=off go test -covermode=atomic -coverprofile="$repo/coverage/$output.out" ./...)
      ;;
  esac
done < "$manifest"

if [ "$action" = tidy-check ]; then
  if ! sha256sum --quiet -c "$tidy_snapshot"; then
    echo "go mod tidy changed dependency files; run it and commit the result" >&2
    exit 1
  fi
fi
