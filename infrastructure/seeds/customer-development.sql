BEGIN;
SET ROLE customer_db_owner;
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000001', true);

-- Development-only booking account. The bcrypt hash is for the local-only
-- password documented in README.md; plaintext credentials are never stored.
INSERT INTO public.customers (
  id,
  tenant_id,
  name,
  normalized_email,
  normalized_phone,
  password_hash,
  status,
  must_change_password,
  initial_delivery_status,
  initial_delivery_attempts,
  initial_delivery_updated_at,
  password_changed_at
)
VALUES (
  '00000000-0000-0000-0000-000000000201',
  '00000000-0000-0000-0000-000000000001',
  'Demo Customer',
  'demo-customer@booking.local',
  NULL,
  '$2a$10$PNJcLaVdJtlGPn2x2APuSOpQ5CK9DEqQRD6Hf0HfaGaf/US7KoKKS',
  'active',
  false,
  'sent',
  1,
  now(),
  now()
)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  normalized_email = EXCLUDED.normalized_email,
  normalized_phone = EXCLUDED.normalized_phone,
  password_hash = EXCLUDED.password_hash,
  status = EXCLUDED.status,
  must_change_password = false,
  initial_delivery_status = 'sent',
  initial_delivery_claim_token = NULL,
  initial_delivery_claim_until = NULL,
  initial_delivery_last_error = NULL,
  initial_delivery_updated_at = now(),
  password_changed_at = now(),
  updated_at = now();

COMMIT;
