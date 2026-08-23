#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
project=${COMPOSE_PROJECT_NAME:-barber_ci_appointment_upgrade_$$}
compose="docker compose -p $project -f $repo/docker-compose.yml"
cleanup() { $compose down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
cleanup

$compose up -d --wait postgres
apply() {
  $compose exec -T postgres psql -v ON_ERROR_STOP=1 -U postgres -d appointment_db < "$1"
}

apply "$repo/services/appointment-service/migrations/000001_baseline.up.sql"
apply "$repo/services/appointment-service/migrations/000002_appointment_foundation.up.sql"
apply "$repo/services/appointment-service/migrations/000003_outbox_publisher.up.sql"

# This is data accepted by the pre-000004 schema: the old contract did not
# constrain either field. It represents the raw semantic request material and
# an unbounded client key that exposed the upgrade failure on existing data.
$compose exec -T postgres psql -v ON_ERROR_STOP=1 -U postgres -d appointment_db <<'SQL'
WITH inserted AS (
  INSERT INTO public.appointments(
    tenant_id, branch_id, barber_id, service_id, customer_name,
    customer_contact, start_at, end_at, occupied_start_at, occupied_end_at, status
  ) VALUES (
    '10000000-0000-0000-0000-000000000001',
    '10000000-0000-0000-0000-000000000002',
    '10000000-0000-0000-0000-000000000003',
    '10000000-0000-0000-0000-000000000004',
    'Legacy Customer', 'legacy@example.test',
    '2034-01-01T10:00:00Z', '2034-01-01T10:30:00Z',
    '2034-01-01T10:00:00Z', '2034-01-01T10:30:00Z', 'pending'
  ) RETURNING tenant_id, id
)
INSERT INTO public.idempotency_keys(tenant_id, key, fingerprint, appointment_id)
SELECT tenant_id,
       repeat('legacy key with spaces ', 8),
       '{"branch_id":"10000000-0000-0000-0000-000000000002","start_at":"2034-01-01T10:00:00Z"}',
       id
FROM inserted;
SQL

apply "$repo/services/appointment-service/migrations/000003a_normalize_legacy_idempotency.up.sql"
apply "$repo/services/appointment-service/migrations/000004_idempotency_retention_privileges.up.sql"

$compose exec -T postgres psql -v ON_ERROR_STOP=1 -U postgres -d appointment_db <<'SQL'
DO $$
DECLARE legacy_count integer;
BEGIN
  SELECT count(*) INTO legacy_count
  FROM public.idempotency_keys
  WHERE principal_scope = 'legacy'
    AND operation = 'create_appointment'
    AND key ~ '^[A-Za-z0-9._:-]+$'
    AND length(key) BETWEEN 1 AND 128
    AND fingerprint ~ '^[0-9a-f]{64}$';
  IF legacy_count <> 1 THEN
    RAISE EXCEPTION 'legacy idempotency row was not safely normalized';
  END IF;
END $$;
SQL
