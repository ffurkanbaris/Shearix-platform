-- Production control-plane lifecycle. Existing verified domains retain their
-- previous routing behaviour when this immutable migration is applied.
ALTER TABLE public.tenants
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE public.tenant_domains
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS verification_state text NOT NULL DEFAULT 'pending'
        CHECK (verification_state IN ('pending', 'verified', 'failed')),
    ADD COLUMN IF NOT EXISTS verification_token_hash bytea,
    ADD COLUMN IF NOT EXISTS verification_requested_at timestamptz,
    ADD COLUMN IF NOT EXISTS last_verification_attempt_at timestamptz,
    ADD COLUMN IF NOT EXISTS verified_at timestamptz,
    ADD COLUMN IF NOT EXISTS activated_at timestamptz,
    ADD COLUMN IF NOT EXISTS deactivated_at timestamptz;

-- This preserves the pre-lifecycle meaning of a verified legacy domain while
-- all newly registered domains remain inactive until explicitly activated.
UPDATE public.tenant_domains
SET active = true,
    verification_state = 'verified',
    verified_at = COALESCE(verified_at, created_at),
    activated_at = COALESCE(activated_at, created_at)
WHERE verified = true;

ALTER TABLE public.tenant_settings
    ADD COLUMN IF NOT EXISTS reminder_offsets_minutes integer[] NOT NULL DEFAULT ARRAY[1440, 120]::integer[],
    ADD COLUMN IF NOT EXISTS cancellation_policy text NOT NULL DEFAULT 'allow_until_notice'
        CHECK (cancellation_policy IN ('allow_until_notice', 'no_cancellation')),
    ADD COLUMN IF NOT EXISTS cancellation_notice_minutes integer NOT NULL DEFAULT 120
        CHECK (cancellation_notice_minutes BETWEEN 0 AND 10080),
    ADD COLUMN IF NOT EXISTS booking_horizon_days integer NOT NULL DEFAULT 60
        CHECK (booking_horizon_days BETWEEN 1 AND 365),
    ADD COLUMN IF NOT EXISTS minimum_booking_notice_minutes integer NOT NULL DEFAULT 60
        CHECK (minimum_booking_notice_minutes BETWEEN 0 AND 10080);

ALTER TABLE public.tenant_settings
    DROP CONSTRAINT IF EXISTS tenant_settings_reminder_offsets_not_empty;
ALTER TABLE public.tenant_settings
    ADD CONSTRAINT tenant_settings_reminder_offsets_not_empty
    CHECK (cardinality(reminder_offsets_minutes) > 0
           AND array_position(reminder_offsets_minutes, NULL) IS NULL
           AND 0 < ALL (reminder_offsets_minutes)
           AND 10080 >= ALL (reminder_offsets_minutes));

CREATE INDEX IF NOT EXISTS idx_tenant_domains_resolution
    ON public.tenant_domains (hostname)
    WHERE verified = true AND active = true;

-- Domain resolution is deliberately the one privileged, constrained lookup.
-- It does not expose tenant tables to callers and always resolves only active
-- verified domains of active tenants.
CREATE OR REPLACE FUNCTION public.resolve_verified_domain(p_hostname text)
RETURNS TABLE (tenant_id uuid, app_type text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT d.tenant_id, d.domain_type
    FROM public.tenant_domains AS d
    JOIN public.tenants AS t ON t.id = d.tenant_id
    WHERE d.hostname = lower(p_hostname)
      AND d.verified = true
      AND d.active = true
      AND t.status = 'active'
    LIMIT 1
$$;

REVOKE ALL ON FUNCTION public.resolve_verified_domain(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_verified_domain(text) TO tenant_db_app;

-- The service application role is still NOINHERIT/NOBYPASSRLS. It can create
-- control-plane tenants, while tenant-owned settings/domains require the
-- transaction-local tenant context enforced by their RLS policies.
GRANT INSERT, UPDATE ON public.tenants TO tenant_db_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.tenant_domains, public.tenant_settings TO tenant_db_app;
