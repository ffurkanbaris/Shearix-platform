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
  if ! command -v git >/dev/null 2>&1 || ! git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "git worktree unavailable; dependency-file cleanliness check cannot run" >&2
    exit 1
  fi
  git -C "$repo" diff --exit-code -- '**/go.mod' '**/go.sum'
fi
