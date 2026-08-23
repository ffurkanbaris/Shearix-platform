BEGIN;
SET ROLE tenant_db_owner;
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000001', true);
INSERT INTO public.tenants (id, name, status)
VALUES ('00000000-0000-0000-0000-000000000001', 'Demo Barber', 'active')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status;
INSERT INTO public.tenant_domains (
  tenant_id, hostname, domain_type, verified, active, verification_state,
  ssl_status, verified_at, activated_at
)
VALUES
  ('00000000-0000-0000-0000-000000000001', 'booking.localhost', 'booking', true, true, 'verified', 'development', now(), now()),
  ('00000000-0000-0000-0000-000000000001', 'admin.localhost', 'admin', true, true, 'verified', 'development', now(), now())
ON CONFLICT (hostname) DO UPDATE
SET domain_type = EXCLUDED.domain_type,
    verified = EXCLUDED.verified,
    active = EXCLUDED.active,
    verification_state = EXCLUDED.verification_state,
    ssl_status = EXCLUDED.ssl_status,
    verified_at = EXCLUDED.verified_at,
    activated_at = EXCLUDED.activated_at
WHERE public.tenant_domains.tenant_id = EXCLUDED.tenant_id;
INSERT INTO public.tenant_settings (
  tenant_id, business_timezone, booking_interval_minutes, reminder_offsets_minutes,
  cancellation_policy, cancellation_notice_minutes, booking_horizon_days,
  minimum_booking_notice_minutes
)
VALUES ('00000000-0000-0000-0000-000000000001', 'Europe/Istanbul', 15, ARRAY[1440, 120], 'allow_until_notice', 120, 60, 60)
ON CONFLICT (tenant_id) DO UPDATE
SET business_timezone = EXCLUDED.business_timezone,
    booking_interval_minutes = EXCLUDED.booking_interval_minutes,
    reminder_offsets_minutes = EXCLUDED.reminder_offsets_minutes,
    cancellation_policy = EXCLUDED.cancellation_policy,
    cancellation_notice_minutes = EXCLUDED.cancellation_notice_minutes,
    booking_horizon_days = EXCLUDED.booking_horizon_days,
    minimum_booking_notice_minutes = EXCLUDED.minimum_booking_notice_minutes,
    updated_at = now();
COMMIT;
