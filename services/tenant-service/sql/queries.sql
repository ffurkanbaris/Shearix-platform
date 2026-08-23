-- name: GetTenantSettings :one
SELECT tenant_id,
       business_timezone,
       booking_interval_minutes,
       reminder_offsets_minutes,
       cancellation_policy,
       cancellation_notice_minutes,
       booking_horizon_days,
       minimum_booking_notice_minutes
FROM public.tenant_settings
WHERE tenant_id = $1;

-- name: UpdateTenantSettings :one
UPDATE public.tenant_settings
SET business_timezone = $2,
    booking_interval_minutes = $3,
    reminder_offsets_minutes = $4,
    cancellation_policy = $5,
    cancellation_notice_minutes = $6,
    booking_horizon_days = $7,
    minimum_booking_notice_minutes = $8,
    updated_at = clock_timestamp()
WHERE tenant_id = $1
RETURNING tenant_id,
          business_timezone,
          booking_interval_minutes,
          reminder_offsets_minutes,
          cancellation_policy,
          cancellation_notice_minutes,
          booking_horizon_days,
          minimum_booking_notice_minutes;

-- name: CreateTenant :one
INSERT INTO public.tenants (name, status)
VALUES ($1, 'active')
RETURNING id, name, status, created_at;

-- name: GetTenant :one
SELECT id, name, status, created_at
FROM public.tenants
WHERE id = $1;

-- name: InitializeTenantSettings :one
INSERT INTO public.tenant_settings (
    tenant_id,
    business_timezone,
    booking_interval_minutes,
    reminder_offsets_minutes,
    cancellation_policy,
    cancellation_notice_minutes,
    booking_horizon_days,
    minimum_booking_notice_minutes
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING tenant_id,
          business_timezone,
          booking_interval_minutes,
          reminder_offsets_minutes,
          cancellation_policy,
          cancellation_notice_minutes,
          booking_horizon_days,
          minimum_booking_notice_minutes;

-- name: CreateTenantDomain :one
INSERT INTO public.tenant_domains (
    tenant_id,
    hostname,
    domain_type,
    verification_token_hash,
    verification_requested_at
)
SELECT $1, $2, $3, $4, now()
WHERE EXISTS (
    SELECT 1 FROM public.tenants t WHERE t.id = $1 AND t.status = 'active'
)
RETURNING id,
          tenant_id,
          hostname,
          domain_type,
          verified,
          active,
          verification_state,
          verification_requested_at,
          last_verification_attempt_at,
          verified_at,
          activated_at,
          deactivated_at,
          created_at;

-- name: GetTenantDomain :one
SELECT id,
       tenant_id,
       hostname,
       domain_type,
       verified,
       active,
       verification_state,
       verification_requested_at,
       last_verification_attempt_at,
       verified_at,
       activated_at,
       deactivated_at,
       created_at
FROM public.tenant_domains
WHERE tenant_id = $1 AND id = $2;

-- name: GetTenantDomainForVerification :one
SELECT id,
       tenant_id,
       hostname,
       domain_type,
       verified,
       active,
       verification_state,
       verification_token_hash,
       verification_requested_at,
       last_verification_attempt_at,
       verified_at,
       activated_at,
       deactivated_at,
       created_at
FROM public.tenant_domains
WHERE tenant_id = $1 AND id = $2;

-- name: ListTenantDomains :many
SELECT id,
       tenant_id,
       hostname,
       domain_type,
       verified,
       active,
       verification_state,
       verification_requested_at,
       last_verification_attempt_at,
       verified_at,
       activated_at,
       deactivated_at,
       created_at
FROM public.tenant_domains
WHERE tenant_id = $1
ORDER BY created_at, id;

-- name: MarkDomainVerificationSucceeded :one
UPDATE public.tenant_domains
SET verified = true,
    verification_state = 'verified',
    verification_token_hash = NULL,
    verified_at = COALESCE(verified_at, now()),
    last_verification_attempt_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING id,
          tenant_id,
          hostname,
          domain_type,
          verified,
          active,
          verification_state,
          verification_requested_at,
          last_verification_attempt_at,
          verified_at,
          activated_at,
          deactivated_at,
          created_at;

-- name: MarkDomainVerificationFailed :one
UPDATE public.tenant_domains
SET verification_state = 'failed',
    last_verification_attempt_at = now()
WHERE tenant_id = $1 AND id = $2 AND verified = false
RETURNING id,
          tenant_id,
          hostname,
          domain_type,
          verified,
          active,
          verification_state,
          verification_requested_at,
          last_verification_attempt_at,
          verified_at,
          activated_at,
          deactivated_at,
          created_at;

-- name: ActivateTenantDomain :one
UPDATE public.tenant_domains d
SET active = true,
    activated_at = COALESCE(d.activated_at, now()),
    deactivated_at = NULL
FROM public.tenants t
WHERE d.tenant_id = $1
  AND d.id = $2
  AND d.tenant_id = t.id
  AND t.status = 'active'
  AND d.verified = true
RETURNING d.id,
          d.tenant_id,
          d.hostname,
          d.domain_type,
          d.verified,
          d.active,
          d.verification_state,
          d.verification_requested_at,
          d.last_verification_attempt_at,
          d.verified_at,
          d.activated_at,
          d.deactivated_at,
          d.created_at;

-- name: DeactivateTenantDomain :one
UPDATE public.tenant_domains
SET active = false,
    deactivated_at = now()
WHERE tenant_id = $1 AND id = $2
RETURNING id,
          tenant_id,
          hostname,
          domain_type,
          verified,
          active,
          verification_state,
          verification_requested_at,
          last_verification_attempt_at,
          verified_at,
          activated_at,
          deactivated_at,
          created_at;
