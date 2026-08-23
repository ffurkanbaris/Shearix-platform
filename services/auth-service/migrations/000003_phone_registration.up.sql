-- Phone identities are global. Tenant-owned membership and session rows remain
-- protected by RLS; no password or credential plaintext is stored here.
ALTER TABLE public.identities
  ALTER COLUMN email DROP NOT NULL,
  ADD COLUMN name text,
  ADD COLUMN phone text,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE public.identities
  ADD CONSTRAINT identities_name_length_check CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 200),
  ADD CONSTRAINT identities_phone_e164_check CHECK (phone IS NULL OR phone ~ '^\+[0-9]{8,15}$');
CREATE UNIQUE INDEX identities_phone_unique ON public.identities(phone) WHERE phone IS NOT NULL;

ALTER TABLE public.credentials
  ADD COLUMN password_changed_at timestamptz NOT NULL DEFAULT now();

-- This is an auth-service outbox-equivalent. It contains delivery metadata
-- only; credential plaintext remains exclusively in Redis.
CREATE TABLE public.credential_delivery_outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  identity_id uuid NOT NULL REFERENCES public.identities(id) ON DELETE CASCADE,
  event_id uuid NOT NULL UNIQUE,
  delivery_id uuid NOT NULL UNIQUE,
  purpose text NOT NULL CHECK (purpose IN ('user_initial_password', 'user_password_reset')),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'published')),
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  claimed_at timestamptz,
  lease_until timestamptz,
  claim_token uuid,
  published_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credential_delivery_outbox_due_idx
  ON public.credential_delivery_outbox(next_attempt_at)
  WHERE status='pending';

ALTER TABLE public.credential_delivery_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.credential_delivery_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY credential_delivery_outbox_tenant ON public.credential_delivery_outbox
USING (current_user='auth_db_owner' OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
WITH CHECK (current_user='auth_db_owner' OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

GRANT SELECT, INSERT, UPDATE ON public.identities, public.credentials, public.tenant_memberships, public.credential_delivery_outbox TO auth_db_app;

-- The authenticated delivery relay intentionally crosses tenants. It returns
-- metadata only and uses claim tokens to protect a recovered lease.
CREATE OR REPLACE FUNCTION public.claim_credential_delivery_outbox(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,identity_id uuid,event_id uuid,delivery_id uuid,purpose text,attempt_count integer,claim_token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  RETURN QUERY WITH candidates AS (
    SELECT o.id
    FROM public.credential_delivery_outbox AS o
    WHERE (o.status='pending' AND o.next_attempt_at<=clock_timestamp())
       OR (o.status='processing' AND o.lease_until<clock_timestamp())
    ORDER BY o.next_attempt_at
    FOR UPDATE SKIP LOCKED
    LIMIT LEAST(GREATEST(p_limit,1),100)
  ), claimed AS (
    UPDATE public.credential_delivery_outbox AS o
    SET status='processing',attempt_count=o.attempt_count+1,
        claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '2 minutes',
        claim_token=gen_random_uuid(),updated_at=clock_timestamp()
    FROM candidates AS c
    WHERE o.id=c.id
    RETURNING o.id,o.tenant_id,o.identity_id,o.event_id,o.delivery_id,o.purpose,o.attempt_count,o.claim_token
  ) SELECT * FROM claimed;
END $$;

CREATE OR REPLACE FUNCTION public.finish_credential_delivery_outbox(
  p_id uuid,p_claim_token uuid,p_published boolean,p_error text,p_retry_at timestamptz
) RETURNS void
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  UPDATE public.credential_delivery_outbox
  SET status=CASE WHEN p_published THEN 'published' ELSE 'pending' END,
      published_at=CASE WHEN p_published THEN clock_timestamp() ELSE published_at END,
      last_error=p_error,
      next_attempt_at=COALESCE(p_retry_at,next_attempt_at),
      claimed_at=NULL,lease_until=NULL,claim_token=NULL,updated_at=clock_timestamp()
  WHERE id=p_id AND status='processing' AND claim_token=p_claim_token
$$;

-- Password resets revoke sessions across all tenants for one global identity.
-- Only auth_db_app may invoke this carefully scoped privileged operation.
CREATE OR REPLACE FUNCTION public.revoke_identity_sessions(p_identity_id uuid,p_keep_session_id uuid)
RETURNS void
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  UPDATE public.sessions
  SET revoked_at=clock_timestamp()
  WHERE identity_id=p_identity_id
    AND revoked_at IS NULL
    AND (p_keep_session_id IS NULL OR id<>p_keep_session_id)
$$;

REVOKE ALL ON FUNCTION public.claim_credential_delivery_outbox(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.finish_credential_delivery_outbox(uuid,uuid,boolean,text,timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.revoke_identity_sessions(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_credential_delivery_outbox(integer) TO auth_db_app;
GRANT EXECUTE ON FUNCTION public.finish_credential_delivery_outbox(uuid,uuid,boolean,text,timestamptz) TO auth_db_app;
GRANT EXECUTE ON FUNCTION public.revoke_identity_sessions(uuid,uuid) TO auth_db_app;
