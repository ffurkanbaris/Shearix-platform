-- release_outbox_event previously returned void, forcing publisher.go to
-- guess whether a release call pushed a row terminal (failed_at newly set)
-- or merely counted another retry. Returning the resulting attempt count and
-- terminal flag lets the outbox publisher emit accurate
-- outbox_publish_retries_total / outbox_terminal_total metrics without a
-- second round-trip query. Postgres cannot change a function's return type
-- via CREATE OR REPLACE, so the old void-returning function is dropped first.
DROP FUNCTION IF EXISTS public.release_outbox_event(uuid);

CREATE FUNCTION public.release_outbox_event(p_id uuid)
RETURNS TABLE(publish_attempts integer, failed_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
  UPDATE public.outbox_events
  SET publishing_at = NULL,
      publish_attempts = publish_attempts + 1,
      failed_at = CASE WHEN publish_attempts + 1 >= 20 THEN clock_timestamp() ELSE failed_at END
  WHERE id = p_id AND published_at IS NULL
  RETURNING publish_attempts, failed_at;
$$;

REVOKE ALL ON FUNCTION public.release_outbox_event(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.release_outbox_event(uuid) TO appointment_db_app;
