#!/bin/sh
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
base="$repo/docker-compose.yml"
production="$repo/infrastructure/caddy/docker-compose.production.yml"
observability="$repo/docker-compose.production-observability.yml"

docker compose -f "$base" config -q

if env -i PATH="$PATH" docker compose -f "$base" -f "$production" -f "$observability" config -q >/dev/null 2>&1; then
  echo "production Compose unexpectedly accepted missing mandatory secrets" >&2
  exit 1
fi

resolved=$(mktemp)
trap 'rm -f "$resolved"' EXIT

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
  SAAS_DOMAIN=shearx.app PLATFORM_ADMIN_HOST=platform.shearx.app PLATFORM_ADMIN_EMAIL=platform@example.invalid \
  PLATFORM_ADMIN_PASSWORD_SCRYPT=00112233445566778899aabbccddeeff:506bc477a50e27e87ccf36b3e0185e962c9ced01bf7f58e58dc8049702234576 \
  PLATFORM_SESSION_SECRET=ci-platform-session-secret-longer-than-32-chars \
  GRAFANA_ADMIN_USER=ci-operator GRAFANA_ADMIN_PASSWORD=ci-grafana-password \
  PRODUCTION_DATA_ROOT=/var/lib/barber \
  ALERTMANAGER_CONFIG_FILE="$repo/infrastructure/observability/production/alertmanager.example.yml" \
  docker compose -f "$base" -f "$production" -f "$observability" config -o "$resolved"

service_block() {
  service=$1
  sed -n "/^  ${service}:/,/^  [a-zA-Z0-9_-]*:/p" "$resolved"
}

grep -q 'platform.shearx.app' "$resolved" || {
  echo "production Caddy lost PLATFORM_ADMIN_HOST" >&2
  exit 1
}
grep -q 'SAAS_DOMAIN: shearx.app' "$resolved" || {
  echo "production Caddy lost SAAS_DOMAIN" >&2
  exit 1
}
platform_block=$(service_block platform-admin-web)
printf '%s\n' "$platform_block" | grep -q 'private' || { echo "platform-admin-web lost its private production network" >&2; exit 1; }
printf '%s\n' "$platform_block" | grep -q 'REDIS_URL:' || { echo "platform-admin-web lost its Redis login limiter" >&2; exit 1; }
if printf '%s\n' "$platform_block" | grep -q 'published:'; then
  echo "platform-admin-web unexpectedly publishes a production host port" >&2
  exit 1
fi

alertmanager_block=$(service_block alertmanager)
printf '%s\n' "$alertmanager_block" | grep -q '/alertmanager/config/alertmanager.yml' || {
  echo "Alertmanager does not read its root-only config through the private data volume" >&2
  exit 1
}
if printf '%s\n' "$alertmanager_block" | grep -q '/etc/alertmanager/alertmanager.yml'; then
  echo "Alertmanager directly mounts a root-only host secret" >&2
  exit 1
fi
init_block=$(service_block observability-volume-init)
printf '%s\n' "$init_block" | grep -q '/alertmanager-source/alertmanager.yml' || {
  echo "observability init does not stage the Alertmanager secret" >&2
  exit 1
}
printf '%s\n' "$init_block" | grep -q 'chmod 0400' || {
  echo "staged Alertmanager secret is not read-only" >&2
  exit 1
}

# The local edge must route the operator hostname directly, while its catch-all
# keeps tenant hostnames on gateway-service.
grep -q '^http://platform\.localhost' "$repo/infrastructure/caddy/Caddyfile.local"
sed -n '/^http:\/\/platform\.localhost/,/^}/p' "$repo/infrastructure/caddy/Caddyfile.local" | grep -q 'platform-admin-web:3000'
sed -n '/^http:\/\/ {/,/^}/p' "$repo/infrastructure/caddy/Caddyfile.local" | grep -q 'gateway-service:8080'

# Production apex/platform hosts are edge-owned. Tenant custom domains must
# remain on the hostless gateway catch-all.
grep -q '^{$PLATFORM_ADMIN_HOST}' "$repo/infrastructure/caddy/Caddyfile"
grep -q '^{$SAAS_DOMAIN}' "$repo/infrastructure/caddy/Caddyfile"
sed -n '/^{$PLATFORM_ADMIN_HOST}/,/^}/p' "$repo/infrastructure/caddy/Caddyfile" | grep -q 'platform-admin-web:3000'
sed -n '/^{$SAAS_DOMAIN}/,/^}/p' "$repo/infrastructure/caddy/Caddyfile" | grep -q 'respond.*Shearx'
sed -n '/^https:\/\/ {/,/^}/p' "$repo/infrastructure/caddy/Caddyfile" | grep -q 'gateway-service:8080'

if grep -q 'customer-development.sql' "$resolved"; then
  echo "production Compose still references the customer development seed" >&2
  exit 1
fi
if ! service_block customer-seed | grep -q 'entrypoint:' || \
   ! service_block customer-seed | grep -q '/bin/true'; then
  echo "production customer-seed is not a side-effect-free no-op" >&2
  exit 1
fi

for service in auth-service customer-service notification-service; do
  block=$(service_block "$service")
  printf '%s\n' "$block" | grep -q 'private' || {
    echo "$service lost its private network" >&2
    exit 1
  }
  printf '%s\n' "$block" | grep -q 'egress' || {
    echo "$service has no SMTP egress network" >&2
    exit 1
  }
  if printf '%s\n' "$block" | grep -q 'published:'; then
    echo "$service unexpectedly publishes a host port" >&2
    exit 1
  fi
done

if ! sed -n '/^  private:/,/^  [a-zA-Z0-9_-]*:/p' "$resolved" | grep -q 'internal: true'; then
  echo "production private network is not internal" >&2
  exit 1
fi

for service in postgres redis nats gateway-service auth-service tenant-service barber-service catalog-service scheduling-service appointment-service customer-service notification-service admin-web booking-web platform-admin-web caddy prometheus alertmanager tempo otel-collector loki alloy grafana; do
  block=$(service_block "$service")
  printf '%s\n' "$block" | grep -q 'max-size: 10m' || {
    echo "$service has no production log rotation" >&2
    exit 1
  }
done

for service in prometheus alertmanager tempo otel-collector loki alloy; do
  if service_block "$service" | grep -q 'published:'; then
    echo "$service unexpectedly publishes a host port" >&2
    exit 1
  fi
done
alloy_block=$(service_block alloy)
printf '%s\n' "$alloy_block" | grep -q '/var/lib/barber/docker/containers' || {
  echo "production Alloy does not read logs from the configured Docker data root" >&2
  exit 1
}
service_block grafana | grep -q 'host_ip: 127.0.0.1' || {
  echo "production Grafana is not bound to loopback" >&2
  exit 1
}
grafana_block=$(service_block grafana)
printf '%s\n' "$grafana_block" | grep -q 'private' || {
  echo "production Grafana lost its private network" >&2
  exit 1
}
printf '%s\n' "$grafana_block" | grep -q 'egress' || {
  echo "production Grafana cannot establish its loopback binding without a gateway network" >&2
  exit 1
}
for service in gateway-service tenant-service auth-service barber-service catalog-service scheduling-service appointment-service customer-service notification-service; do
  service_block "$service" | grep -q 'OTEL_EXPORTER_OTLP_ENDPOINT: otel-collector:4317' || {
    echo "$service is not wired to the production OTEL collector" >&2
    exit 1
  }
done
