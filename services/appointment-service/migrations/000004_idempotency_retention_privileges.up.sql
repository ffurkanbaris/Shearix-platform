-- Scope idempotency to the trusted principal and operation, and tie every
-- retained result to an appointment in the same tenant.
ALTER TABLE public.idempotency_keys
  ADD COLUMN principal_scope text NOT NULL DEFAULT 'legacy',
  ADD COLUMN operation text NOT NULL DEFAULT 'create_appointment';

ALTER TABLE public.idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE public.idempotency_keys
  ADD CONSTRAINT idempotency_keys_pkey
  PRIMARY KEY (tenant_id, principal_scope, operation, key),
  ADD CONSTRAINT idempotency_keys_key_check
  CHECK (length(key) BETWEEN 1 AND 128 AND key ~ '^[A-Za-z0-9._:-]+$'),
  ADD CONSTRAINT idempotency_keys_fingerprint_check
  CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  ADD CONSTRAINT idempotency_keys_appointment_tenant_fk
  FOREIGN KEY (tenant_id, appointment_id)
  REFERENCES public.appointments (tenant_id, id) ON DELETE CASCADE;

CREATE INDEX idempotency_keys_retention_idx
  ON public.idempotency_keys (created_at);
CREATE INDEX outbox_events_published_retention_idx
  ON public.outbox_events (published_at, id) WHERE published_at IS NOT NULL;

-- Seven days covers delayed/mobile client retries without retaining keys
-- forever. Deletion is bounded and concurrent cleanup workers skip locks.
CREATE OR REPLACE FUNCTION public.cleanup_expired_idempotency_keys(p_limit integer DEFAULT 500)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE deleted_count integer;
BEGIN
  WITH candidates AS (
    SELECT i.tenant_id, i.principal_scope, i.operation, i.key
    FROM public.idempotency_keys AS i
    WHERE i.created_at < clock_timestamp() - interval '7 days'
    ORDER BY i.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT LEAST(GREATEST(p_limit, 1), 1000)
  )
  DELETE FROM public.idempotency_keys AS i
  USING candidates AS c
  WHERE (i.tenant_id, i.principal_scope, i.operation, i.key) =
        (c.tenant_id, c.principal_scope, c.operation, c.key);
  GET DIAGNOSTICS deleted_count = ROW_COUNT;
  RETURN deleted_count;
END;
$$;

-- Only acknowledged rows older than 30 days are eligible. Unpublished rows
-- survive indefinitely so an infrastructure outage cannot cause event loss.
CREATE OR REPLACE FUNCTION public.cleanup_published_outbox_events(p_limit integer DEFAULT 500)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE deleted_count integer;
BEGIN
  WITH candidates AS (
    SELECT e.id
    FROM public.outbox_events AS e
    WHERE e.published_at < clock_timestamp() - interval '30 days'
    ORDER BY e.published_at, e.id
    FOR UPDATE SKIP LOCKED
    LIMIT LEAST(GREATEST(p_limit, 1), 1000)
  )
  DELETE FROM public.outbox_events AS e
  USING candidates AS c
  WHERE e.id = c.id;
  GET DIAGNOSTICS deleted_count = ROW_COUNT;
  RETURN deleted_count;
END;
$$;

REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM appointment_db_app;
GRANT SELECT, INSERT, UPDATE ON public.appointments TO appointment_db_app;
GRANT INSERT ON public.appointment_events TO appointment_db_app;
GRANT SELECT, INSERT ON public.idempotency_keys TO appointment_db_app;
GRANT INSERT ON public.outbox_events TO appointment_db_app;

REVOKE ALL ON FUNCTION public.cleanup_expired_idempotency_keys(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.cleanup_published_outbox_events(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.cleanup_expired_idempotency_keys(integer) TO appointment_db_app;
GRANT EXECUTE ON FUNCTION public.cleanup_published_outbox_events(integer) TO appointment_db_app;
