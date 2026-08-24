#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
project=${COMPOSE_PROJECT_NAME:-barber_ci_integration_$$}
cleanup() { docker compose -p "$project" -f "$repo/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
cleanup

for service in tenant auth barber catalog scheduling appointment customer notification; do
  echo "==> integration: $service-service"
  docker compose -p "$project" -f "$repo/docker-compose.yml" --profile test run --build --rm "$service-integration-test"
done

echo "==> integration: platform (Redis/NATS auth)"
docker compose -p "$project" -f "$repo/docker-compose.yml" --profile test run --build --rm platform-auth-integration-test
