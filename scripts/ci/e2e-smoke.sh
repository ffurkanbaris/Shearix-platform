#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
project=${COMPOSE_PROJECT_NAME:-barber_ci_e2e_$$}
compose="docker compose -p $project -f $repo/docker-compose.yml -f $repo/scripts/ci/e2e-compose.yml"
cookie=$(mktemp)
cleanup() {
  rm -f "$cookie"
  $compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
cleanup

$compose up -d --build --wait
e2e_host=${E2E_HOST:-127.0.0.1}
gateway=http://$e2e_host:18080
booking_frontend=http://$e2e_host:13001
admin_frontend=http://$e2e_host:13000
json_id() { python3 -c 'import json,sys; value=json.load(sys.stdin)["id"]; assert value; print(value)' ; }
first_slot() { python3 -c 'import json,sys; value=json.load(sys.stdin); assert value; print(value[0]["start_at"])' ; }
curl --fail --silent --show-error -H 'Host: booking.localhost' "$gateway/api/v1/public/config" >/dev/null

# Exercise the real browser-facing Next proxy, not only the gateway. The TCP
# destination is the frontend container while Host remains tenant routing data.
curl --fail --silent --show-error -H 'Host: booking.localhost' "$booking_frontend/api/v1/public/config" | grep -q '"app_type":"booking"' || {
  echo "booking frontend proxy lost tenant Host routing" >&2; exit 1;
}
curl --fail --silent --show-error -H 'Host: admin.localhost' "$admin_frontend/api/v1/public/config" | grep -q '"app_type":"admin"' || {
  echo "admin frontend proxy lost tenant Host routing" >&2; exit 1;
}
[ "$(curl --fail --silent --show-error -H 'Host: booking.localhost' "$booking_frontend/api/v1/public/services")" = '[]' ] || {
  echo "empty public service collection must serialize as [] through the frontend proxy" >&2; exit 1;
}

# The deployed frontend routes must expose the email-only authentication
# contract. Keep this check scoped to auth pages so legitimate business contact
# phone fields elsewhere remain allowed.
booking_register=$(curl --fail --silent --show-error -H 'Host: booking.localhost' "$booking_frontend/register")
curl --fail --silent --show-error -H 'Host: admin.localhost' "$admin_frontend/login" >/dev/null
printf '%s' "$booking_register" | grep -q 'type="email"' || { echo "booking registration is missing its email identity field" >&2; exit 1; }
printf '%s' "$booking_register" | grep -Eiq 'whatsapp|type="tel"|name="phone"' && { echo "authentication frontend contains a phone/WhatsApp identity control" >&2; exit 1; }

curl --fail --silent --show-error -c "$cookie" -X POST -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data '{"email":"demo-owner@booking.local","password":"password"}' "$gateway/api/v1/admin/auth/login" >/dev/null

branch=$(curl --fail --silent --show-error -b "$cookie" -X POST -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data '{"name":"CI Branch","address":"CI","active":true}' "$gateway/api/v1/admin/branches" | json_id)
barber=$(curl --fail --silent --show-error -b "$cookie" -X POST -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data "{\"display_name\":\"CI Barber\",\"bio\":\"CI\",\"active\":true,\"branch_ids\":[\"$branch\"]}" "$gateway/api/v1/admin/barbers" | json_id)
service=$(curl --fail --silent --show-error -b "$cookie" -X POST -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data '{"name":"CI Cut","duration_minutes":30,"buffer_before_minutes":0,"buffer_after_minutes":0,"price":"100.00","currency":"TRY","active":true}' "$gateway/api/v1/admin/services" | json_id)
curl --fail --silent --show-error -b "$cookie" -X POST -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data "{\"service_id\":\"$service\"}" "$gateway/api/v1/admin/barbers/$barber/services" >/dev/null
curl --fail --silent --show-error -b "$cookie" -X PUT -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  --data '[{"weekday":0,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":1,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":2,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":3,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":4,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":5,"intervals":[{"start":"00:00","end":"23:59"}]},{"weekday":6,"intervals":[{"start":"00:00","end":"23:59"}]}]' \
  "$gateway/api/v1/admin/barbers/$barber/working-hours" >/dev/null

date=$(date -u -d '+2 days' +%F)
slot=$(curl --fail --silent --show-error -G -H 'Host: booking.localhost' \
  --data-urlencode "barber_id=$barber" --data-urlencode "service_id=$service" --data-urlencode "date=$date" \
  "$gateway/api/v1/public/availability" | first_slot)
appointment_payload="{\"branch_id\":\"$branch\",\"barber_id\":\"$barber\",\"service_id\":\"$service\",\"customer_name\":\"CI Customer\",\"customer_contact\":\"ci-customer@example.invalid\",\"start_at\":\"$slot\"}"
idempotency_key=00000000-0000-4000-8000-000000000201
appointment=$(curl --fail --silent --show-error -X POST -H 'Host: booking.localhost' -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $idempotency_key" --data "$appointment_payload" "$gateway/api/v1/public/appointments" | json_id)

# A client that timed out after submission retries the identical semantic
# request with the same key. The gateway/API path must replay, not create.
replayed=$(curl --fail --silent --show-error -X POST -H 'Host: booking.localhost' -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $idempotency_key" --data "$appointment_payload" "$gateway/api/v1/public/appointments" | json_id)
[ "$replayed" = "$appointment" ] || { echo "idempotency replay returned a different appointment" >&2; exit 1; }

# The same already-observed slot is now stale. PostgreSQL remains the final
# double-booking boundary even after a successful availability response.
stale_status=$(curl --silent --show-error -o /dev/null -w '%{http_code}' -X POST -H 'Host: booking.localhost' -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 00000000-0000-4000-8000-000000000202' --data "$appointment_payload" "$gateway/api/v1/public/appointments")
[ "$stale_status" = 409 ] || { echo "stale availability booking status=$stale_status, want 409" >&2; exit 1; }

i=0
while [ "$i" -lt 30 ]; do
  published=$($compose exec -T postgres sh -c \
    "PGPASSWORD=appointment-owner-dev-password psql -qtAX -h 127.0.0.1 -U appointment_db_migrator -d appointment_db -c \"SET ROLE appointment_db_owner; SET app.tenant_id='00000000-0000-0000-0000-000000000001'; SELECT count(*) FROM public.outbox_events WHERE aggregate_id='$appointment' AND published_at IS NOT NULL\"" | tail -1)
  notified=$($compose exec -T postgres sh -c \
    "PGPASSWORD=notification-owner-dev-password psql -qtAX -h 127.0.0.1 -U notification_db_migrator -d notification_db -c \"SET ROLE notification_db_owner; SET app.tenant_id='00000000-0000-0000-0000-000000000001'; SELECT count(*) FROM public.email_notifications WHERE appointment_id='$appointment' AND notification_type='appointment_created'\"" | tail -1)
  [ "$published" = 1 ] && [ "$notified" = 1 ] && exit 0
  i=$((i + 1))
  sleep 1
done
echo "critical flow did not converge (published_outbox=$published notification_records=$notified)" >&2
exit 1
