#!/bin/sh
set -eu

# Verify the real production containers can open a TCP connection to the
# configured SMTP endpoint. Run after the production Compose graph is healthy.
# This sends no credentials and no email.

repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
base="$repo/docker-compose.yml"
production="$repo/infrastructure/caddy/docker-compose.production.yml"

: "${SMTP_ADDRESS:?SMTP_ADDRESS must be host:port}"

smtp_host=${SMTP_ADDRESS%:*}
smtp_port=${SMTP_ADDRESS##*:}
if [ -z "$smtp_host" ] || [ -z "$smtp_port" ] || [ "$smtp_host" = "$SMTP_ADDRESS" ]; then
  echo "SMTP_ADDRESS must be host:port" >&2
  exit 2
fi

set -- docker compose -f "$base" -f "$production"
if [ -n "${PRODUCTION_ENV_FILE:-}" ]; then
  set -- "$@" --env-file "$PRODUCTION_ENV_FILE"
fi

for service in auth-service customer-service notification-service; do
  echo "==> SMTP TCP path: $service -> $smtp_host:$smtp_port"
  "$@" exec -T "$service" sh -c 'nc -z -w 10 "$1" "$2"' sh "$smtp_host" "$smtp_port"
done
