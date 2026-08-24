#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
base="$repo/docker-compose.yml"
production="$repo/infrastructure/caddy/docker-compose.production.yml"

docker compose -f "$base" config -q

if env -i PATH="$PATH" docker compose -f "$base" -f "$production" config -q >/dev/null 2>&1; then
  echo "production Compose unexpectedly accepted missing mandatory secrets" >&2
  exit 1
fi

env \
  POSTGRES_PASSWORD=ci-postgres TENANT_DB_OWNER_PASSWORD=ci-tenant-owner TENANT_DB_APP_PASSWORD=ci-tenant-app \
  AUTH_DB_OWNER_PASSWORD=ci-auth-owner AUTH_DB_APP_PASSWORD=ci-auth-app \
  BARBER_DB_OWNER_PASSWORD=ci-barber-owner BARBER_DB_APP_PASSWORD=ci-barber-app \
  CATALOG_DB_OWNER_PASSWORD=ci-catalog-owner CATALOG_DB_APP_PASSWORD=ci-catalog-app \
  SCHEDULING_DB_OWNER_PASSWORD=ci-scheduling-owner SCHEDULING_DB_APP_PASSWORD=ci-scheduling-app \
  APPOINTMENT_DB_OWNER_PASSWORD=ci-appointment-owner APPOINTMENT_DB_APP_PASSWORD=ci-appointment-app \
  CUSTOMER_DB_OWNER_PASSWORD=ci-customer-owner CUSTOMER_DB_APP_PASSWORD=ci-customer-app \
  NOTIFICATION_DB_OWNER_PASSWORD=ci-notification-owner NOTIFICATION_DB_APP_PASSWORD=ci-notification-app \
  INTERNAL_AUTH_TOKEN=ci-internal-auth-token-long-enough PLATFORM_ADMIN_TOKEN=ci-platform-token-long-enough \
  SERVICE_INTERNAL_TOKEN=ci-service-internal-token-long-enough \
  REDIS_PASSWORD=ci-redis-password NATS_USER=ci-nats-user NATS_PASSWORD=ci-nats-password \
  EMAIL_PROVIDER=smtp EMAIL_FROM=ci@example.invalid SMTP_ADDRESS=smtp.example.invalid:587 \
  SMTP_USERNAME=ci-user SMTP_PASSWORD=ci-smtp-password CADDY_ACME_EMAIL=ci@example.invalid \
  docker compose -f "$base" -f "$production" config -q
