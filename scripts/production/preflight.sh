#!/bin/sh
# Read-only production readiness checks. This script never starts, restarts,
# or deploys containers.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
env_file=${PRODUCTION_ENV_FILE:-/etc/barber-appointment/production.env}
[ -r "$env_file" ] || { echo "FAIL: production env file is not readable: $env_file" >&2; exit 1; }
set -a
# The root-owned environment file is trusted operator input.
# shellcheck disable=SC1090
. "$env_file"
set +a

failures=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
require_command() { command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"; }
require_value() {
  name=$1
  eval "value=\${$name-}"
  [ -n "$value" ] || { fail "$name is unset"; return; }
  case "$value" in *replace-with*|*example.com*|*192.0.2.*|*dev-password*|*development*|*changeme*) fail "$name still contains a placeholder" ;; esac
}

for command in docker getent findmnt df awk sed grep nc curl stat; do require_command "$command"; done
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is unavailable"

required_secrets="POSTGRES_PASSWORD TENANT_DB_OWNER_PASSWORD TENANT_DB_APP_PASSWORD AUTH_DB_OWNER_PASSWORD AUTH_DB_APP_PASSWORD BARBER_DB_OWNER_PASSWORD BARBER_DB_APP_PASSWORD CATALOG_DB_OWNER_PASSWORD CATALOG_DB_APP_PASSWORD SCHEDULING_DB_OWNER_PASSWORD SCHEDULING_DB_APP_PASSWORD APPOINTMENT_DB_OWNER_PASSWORD APPOINTMENT_DB_APP_PASSWORD CUSTOMER_DB_OWNER_PASSWORD CUSTOMER_DB_APP_PASSWORD NOTIFICATION_DB_OWNER_PASSWORD NOTIFICATION_DB_APP_PASSWORD REDIS_PASSWORD NATS_USER NATS_PASSWORD INTERNAL_AUTH_TOKEN SERVICE_INTERNAL_TOKEN PLATFORM_ADMIN_TOKEN PLATFORM_ADMIN_PASSWORD_SCRYPT PLATFORM_SESSION_SECRET SMTP_USERNAME SMTP_PASSWORD GRAFANA_ADMIN_PASSWORD RESTIC_PASSWORD"
for name in $required_secrets; do require_value "$name"; done
case "${RESTIC_REPOSITORY-}" in
  gs:*) require_value GOOGLE_PROJECT_ID ;;
  *) require_value AWS_ACCESS_KEY_ID; require_value AWS_SECRET_ACCESS_KEY ;;
esac
for name in IMAGE_REGISTRY RELEASE_TAG PRODUCTION_IMAGE_LOCK_FILE EMAIL_PROVIDER EMAIL_FROM SMTP_ADDRESS CADDY_ACME_EMAIL SAAS_DOMAIN PLATFORM_ADMIN_HOST PLATFORM_ADMIN_EMAIL GRAFANA_ADMIN_USER ALERTMANAGER_CONFIG_FILE PRODUCTION_DOMAINS EXPECTED_PUBLIC_IP PRODUCTION_DATA_ROOT RESTIC_REPOSITORY BACKUP_HEARTBEAT_URL BACKUP_VERIFY_HEARTBEAT_URL BACKUP_STAGING_ROOT BACKUP_COMPOSE_PROJECT BACKUP_COMPOSE_FILES; do require_value "$name"; done
printf '%s' "$PLATFORM_ADMIN_PASSWORD_SCRYPT" | grep -Eq '^[0-9a-f]{32,}:[0-9a-f]{64}$' || fail "PLATFORM_ADMIN_PASSWORD_SCRYPT must be a valid salt:key pair"
[ "${#PLATFORM_SESSION_SECRET}" -ge 32 ] || fail "PLATFORM_SESSION_SECRET must contain at least 32 characters"
[ "${EMAIL_PROVIDER-}" = smtp ] || fail "EMAIL_PROVIDER must be smtp"
if [ "${#RELEASE_TAG}" -ne 40 ] || ! printf '%s' "$RELEASE_TAG" | grep -Eq '^[0-9a-f]{40}$'; then
  fail "RELEASE_TAG must be a full lowercase Git SHA"
fi
[ "${INTERNAL_AUTH_TOKEN-}" != "${SERVICE_INTERNAL_TOKEN-}" ] || fail "gateway and service internal tokens must differ"
[ "${INTERNAL_AUTH_TOKEN-}" != "${PLATFORM_ADMIN_TOKEN-}" ] || fail "internal and platform admin tokens must differ"
[ "${PLATFORM_ADMIN_HOST-}" = "platform.${SAAS_DOMAIN-}" ] || fail "PLATFORM_ADMIN_HOST must be platform.SAAS_DOMAIN"
[ -r "${ALERTMANAGER_CONFIG_FILE-}" ] || fail "Alertmanager secret config is not readable"
if [ -r "${ALERTMANAGER_CONFIG_FILE-}" ] && grep -Eq 'replace-with|\.invalid' "$ALERTMANAGER_CONFIG_FILE"; then fail "Alertmanager config still has a placeholder receiver"; fi
env_mode=$(stat -c '%a' "$env_file" 2>/dev/null || true)
[ "$env_mode" = 600 ] || fail "production env file mode must be 600"
if [ "$(id -u)" -eq 0 ]; then [ "$(stat -c '%u' "$env_file")" -eq 0 ] || fail "production env file must be owned by root"; fi
pass "required configuration inventory checked"

old_ifs=$IFS; IFS=,
platform_domain_present=false
saas_domain_present=false
for domain in ${PRODUCTION_DOMAINS-}; do
  IFS=$old_ifs
  domain=$(printf '%s' "$domain" | tr -d ' ')
  resolved=$(getent ahosts "$domain" 2>/dev/null | awk '{print $1}' | sort -u || true)
  printf '%s\n' "$resolved" | grep -Fxq "${EXPECTED_PUBLIC_IP-}" || fail "$domain does not resolve to EXPECTED_PUBLIC_IP"
  [ "$domain" = "${PLATFORM_ADMIN_HOST-}" ] && platform_domain_present=true
  [ "$domain" = "${SAAS_DOMAIN-}" ] && saas_domain_present=true
  IFS=,
done
IFS=$old_ifs
[ "$platform_domain_present" = true ] || fail "PRODUCTION_DOMAINS must include PLATFORM_ADMIN_HOST"
[ "$saas_domain_present" = true ] || fail "PRODUCTION_DOMAINS must include SAAS_DOMAIN"
pass "DNS records checked"

if [ -d "${PRODUCTION_DATA_ROOT-}" ]; then
  mount_target=$(findmnt -n -o TARGET -T "$PRODUCTION_DATA_ROOT" 2>/dev/null || true)
  [ -n "$mount_target" ] || fail "production data root is not on a mounted filesystem"
  [ "$mount_target" != / ] || fail "production data root must use a dedicated persistent disk, not the VM root filesystem"
  free_kb=$(df -Pk "$PRODUCTION_DATA_ROOT" | awk 'NR==2 {print $4}')
  min_kb=$(( ${MIN_FREE_DISK_GB:-50} * 1024 * 1024 ))
  [ "${free_kb:-0}" -ge "$min_kb" ] || fail "production disk has less than ${MIN_FREE_DISK_GB:-50} GiB free"
  docker_root=$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)
  case "$docker_root" in "$PRODUCTION_DATA_ROOT"|"$PRODUCTION_DATA_ROOT"/*) : ;; *) fail "Docker data root is not on PRODUCTION_DATA_ROOT" ;; esac
else
  fail "production data root does not exist"
fi
pass "persistent disk checked"

[ -r "${PRODUCTION_IMAGE_LOCK_FILE-}" ] || fail "immutable image lock file is not readable"
compose="docker compose --env-file $env_file -p ${BACKUP_COMPOSE_PROJECT-} -f $repo/docker-compose.yml -f $repo/infrastructure/caddy/docker-compose.production.yml -f $repo/docker-compose.production-observability.yml -f ${PRODUCTION_IMAGE_LOCK_FILE-}"
resolved=$(mktemp)
trap 'rm -f -- "$resolved"' EXIT HUP INT TERM
# shellcheck disable=SC2086
if $compose config >"$resolved" 2>/dev/null; then
  images=$($compose config --images)
  mutable=$(printf '%s\n' "$images" | grep -v '@sha256:[0-9a-f]\{64\}$' || true)
  [ -z "$mutable" ] || fail "resolved Compose graph contains mutable image references"
  if [ -z "$mutable" ]; then
    for image in $images; do
      docker manifest inspect "$image" >/dev/null 2>&1 || fail "image digest is not readable from the configured registry: $image"
    done
  fi
  grep -q -- '--requirepass' "$resolved" || fail "Redis requirepass is absent from resolved Compose"
  grep -q 'NATS_PASSWORD:' "$resolved" || fail "NATS credentials are absent from resolved Compose"
  grep -q 'OTEL_EXPORTER_OTLP_ENDPOINT: otel-collector:4317' "$resolved" || fail "OTEL exporter is not wired to the collector"
  grep -q '127.0.0.1:' "$resolved" || fail "Grafana is not loopback-bound"
  volume_names=$($compose config --volumes)
  for volume in postgres_data redis_data nats_data caddy_data caddy_config prometheus_data alertmanager_data tempo_data loki_data alloy_data grafana_data; do
    printf '%s\n' "$volume_names" | grep -Fxq "$volume" || fail "required persistent volume is missing: $volume"
    docker_volume="${BACKUP_COMPOSE_PROJECT-}_$volume"
    if docker volume inspect "$docker_volume" >/dev/null 2>&1; then
      mountpoint=$(docker volume inspect "$docker_volume" --format '{{.Mountpoint}}')
      case "$mountpoint" in "$docker_root"/*) : ;; *) fail "$docker_volume is outside Docker's persistent data root" ;; esac
    fi
  done
else
  fail "production Compose graph does not validate"
fi
pass "Compose, auth, observability, and immutable image references checked"

caddy_image=caddy:2.10.2-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d
docker run --rm --entrypoint caddy -e CADDY_ACME_EMAIL -e INTERNAL_AUTH_TOKEN -e SAAS_DOMAIN -e PLATFORM_ADMIN_HOST \
  -v "$repo/infrastructure/caddy/Caddyfile:/etc/caddy/Caddyfile:ro" \
  "$caddy_image" validate --config /etc/caddy/Caddyfile >/dev/null 2>&1 || fail "Caddy configuration is invalid"
pass "Caddy configuration checked"

smtp_host=${SMTP_ADDRESS%:*}; smtp_port=${SMTP_ADDRESS##*:}
[ -n "$smtp_host" ] && [ -n "$smtp_port" ] && [ "$smtp_host" != "$smtp_port" ] || fail "SMTP_ADDRESS must be host:port"
nc -z -w 10 "$smtp_host" "$smtp_port" >/dev/null 2>&1 || fail "SMTP endpoint is unreachable from the production VM"
pass "SMTP path checked"

restic_image=${RESTIC_IMAGE:-restic/restic:0.18.0@sha256:4cf4a61ef9786f4de53e9de8c8f5c040f33830eb0a10bf3d614410ee2fcb6120}
if docker run --rm -e RESTIC_REPOSITORY -e RESTIC_PASSWORD -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -e GOOGLE_PROJECT_ID "$restic_image" cat config >/dev/null 2>&1; then
  pass "encrypted off-host backup repository is initialized and reachable"
else
  fail "restic backup repository is unreachable or not initialized"
fi

prom_image=prom/prometheus:v2.54.1@sha256:f6639335d34a77d9d9db382b92eeb7fc00934be8eae81dbc03b31cfe90411a94
docker run --rm --entrypoint /bin/promtool \
  -v "$repo/infrastructure/observability/production/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
  -v "$repo/infrastructure/observability/production/alerts.yml:/etc/prometheus/alerts.yml:ro" \
  "$prom_image" check config /etc/prometheus/prometheus.yml >/dev/null 2>&1 || fail "Prometheus configuration is invalid"
docker run --rm --entrypoint /bin/promtool \
  -v "$repo/infrastructure/observability/production/alerts.yml:/etc/prometheus/alerts.yml:ro" \
  "$prom_image" check rules /etc/prometheus/alerts.yml >/dev/null 2>&1 || fail "Prometheus alert rules are invalid"
pass "observability configuration checked"

if [ "$failures" -ne 0 ]; then echo "preflight failed with $failures issue(s)" >&2; exit 1; fi
echo "production preflight passed; no containers were started"
