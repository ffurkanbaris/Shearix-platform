#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
project=${COMPOSE_PROJECT_NAME:-barber_ci_migrations_$$}
cleanup() { docker compose -p "$project" -f "$repo/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
cleanup

docker compose -p "$project" -f "$repo/docker-compose.yml" up -d --wait postgres
for service in tenant auth barber catalog scheduling appointment customer notification; do
  echo "==> empty-database migration: $service-service"
  docker compose -p "$project" -f "$repo/docker-compose.yml" run --build --rm "$service-migrate"
done
