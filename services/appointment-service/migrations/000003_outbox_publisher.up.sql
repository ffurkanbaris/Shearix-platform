-- Outbox claims are intentionally implemented as tightly-scoped privileged
-- functions: the application role remains subject to tenant RLS for domain
-- tables, while the publisher may safely process every tenant's queued event.
ALTER TABLE public.outbox_events ADD COLUMN publishing_at timestamptz;
CREATE INDEX outbox_events_unpublished_idx ON public.outbox_events(created_at) WHERE published_at IS NULL;

CREATE OR REPLACE FUNCTION public.claim_outbox_events(p_limit integer)
RETURNS TABLE(id uuid, tenant_id uuid, aggregate_id uuid, type text, payload jsonb, created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
BEGIN
  RETURN QUERY
  WITH candidates AS (
    SELECT e.id
    FROM public.outbox_events AS e
    WHERE e.published_at IS NULL
      AND (e.publishing_at IS NULL OR e.publishing_at < clock_timestamp() - interval '1 minute')
    ORDER BY e.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT LEAST(GREATEST(p_limit, 1), 100)
  ), claimed AS (
    UPDATE public.outbox_events AS e
    SET publishing_at = clock_timestamp()
    FROM candidates AS c
    WHERE e.id = c.id
    RETURNING e.id, e.tenant_id, e.aggregate_id, e.type, e.payload, e.created_at
  )
  SELECT c.id, c.tenant_id, c.aggregate_id, c.type, c.payload, c.created_at FROM claimed AS c;
END;
$$;

CREATE OR REPLACE FUNCTION public.mark_outbox_event_published(p_id uuid)
RETURNS void
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  UPDATE public.outbox_events
  SET published_at = clock_timestamp(), publishing_at = NULL
  WHERE id = p_id AND published_at IS NULL;
$$;

CREATE OR REPLACE FUNCTION public.release_outbox_event(p_id uuid)
RETURNS void
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  UPDATE public.outbox_events SET publishing_at = NULL WHERE id = p_id AND published_at IS NULL;
$$;

REVOKE ALL ON FUNCTION public.claim_outbox_events(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.mark_outbox_event_published(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.release_outbox_event(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_outbox_events(integer) TO appointment_db_app;
GRANT EXECUTE ON FUNCTION public.mark_outbox_event_published(uuid) TO appointment_db_app;
GRANT EXECUTE ON FUNCTION public.release_outbox_event(uuid) TO appointment_db_app;
