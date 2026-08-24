-- Adds a tenant-scoped claim primitive alongside the existing global one.
-- This is purely additive: claim_email_notifications(integer) — the function
-- the production worker's single background Once() loop calls — keeps its
-- exact signature and behavior (a global batch scan across every tenant, the
-- correct design for one shared delivery worker). It is now implemented as a
-- thin wrapper over the new claim_email_notifications_for_tenant(integer,
-- uuid) so the FOR UPDATE SKIP LOCKED batching, lease-expiry reclaim, and
-- claim-token fencing logic has one source of truth; passing NULL for the
-- tenant filter reproduces the prior global-scan behavior exactly.
--
-- The tenant-scoped form lets a caller (currently: integration tests) claim
-- only rows belonging to one tenant, so concurrent test suites sharing one
-- live database no longer interfere with each other's claims - this is a
-- test/tooling primitive, not a change to how the real worker processes
-- notifications in production.
CREATE OR REPLACE FUNCTION public.claim_email_notifications_for_tenant(p_limit integer, p_tenant_id uuid)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_email text,template_name text,template_language text,attempt_count integer,claim_token uuid,recovered boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN RETURN QUERY WITH c AS (
  SELECT n.id, (n.status='processing') AS was_processing FROM public.email_notifications n
  WHERE (p_tenant_id IS NULL OR n.tenant_id=p_tenant_id)
    AND ((n.status='pending' AND n.scheduled_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp())
      OR (n.status='processing' AND n.lease_until<clock_timestamp()))
  ORDER BY n.scheduled_at FOR UPDATE SKIP LOCKED LIMIT LEAST(GREATEST(p_limit,1),100)
), u AS (
  UPDATE public.email_notifications n SET status='processing',attempt_count=n.attempt_count+1,claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '2 minutes',claim_token=gen_random_uuid(),updated_at=clock_timestamp()
  FROM c WHERE n.id=c.id
  RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_email,n.template_name,n.template_language,n.attempt_count,n.claim_token
) SELECT u.id,u.tenant_id,u.appointment_id,u.notification_type,u.recipient_email,u.template_name,u.template_language,u.attempt_count,u.claim_token,c.was_processing FROM u JOIN c ON c.id=u.id; END $$;

CREATE OR REPLACE FUNCTION public.claim_email_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_email text,template_name text,template_language text,attempt_count integer,claim_token uuid,recovered boolean)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  SELECT * FROM public.claim_email_notifications_for_tenant(p_limit, NULL);
$$;

REVOKE ALL ON FUNCTION public.claim_email_notifications_for_tenant(integer, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_email_notifications_for_tenant(integer, uuid) TO notification_db_app;
