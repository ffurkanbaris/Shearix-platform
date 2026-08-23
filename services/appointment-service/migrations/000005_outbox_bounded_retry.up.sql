-- Bound how long a permanently-failing outbox row is retried. Without this,
-- publishBatch retries a poison row (e.g. malformed payload) forever, since
-- release_outbox_event previously only cleared publishing_at with no attempt
-- counter or terminal state.
ALTER TABLE public.outbox_events
  ADD COLUMN publish_attempts integer NOT NULL DEFAULT 0 CHECK (publish_attempts >= 0),
  ADD COLUMN failed_at timestamptz;

-- Terminal rows stay out of claim_outbox_events but are never deleted by
-- cleanup_published_outbox_events (which only ever considers published_at),
-- so they remain queryable for operator investigation indefinitely.
CREATE INDEX outbox_events_failed_idx ON public.outbox_events (failed_at) WHERE failed_at IS NOT NULL;

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
      AND e.failed_at IS NULL
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

-- 20 failed publish attempts mirrors notification-service's consumer
-- MaxDeliver bound. The outbox relay polls every 500ms and only counts an
-- attempt when a publish was actually issued against a broker ensureReady()
-- considered healthy (a NATS outage skips publishBatch entirely, so downtime
-- never advances publish_attempts) - so 20 attempts is roughly 10s of actual
-- failed publish calls against a connected broker, not 10s of wall-clock
-- outage tolerance. That is intentionally generous relative to a single
-- flaky publish while still terminating a poison row in well under a minute
-- instead of retrying it forever.
CREATE OR REPLACE FUNCTION public.release_outbox_event(p_id uuid)
RETURNS void
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  UPDATE public.outbox_events
  SET publishing_at = NULL,
      publish_attempts = publish_attempts + 1,
      failed_at = CASE WHEN publish_attempts + 1 >= 20 THEN clock_timestamp() ELSE failed_at END
  WHERE id = p_id AND published_at IS NULL;
$$;
