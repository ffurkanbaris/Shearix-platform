-- A claim token binds completion to the worker that currently owns the lease.
-- It prevents a late worker from finalizing work that was safely reclaimed.
ALTER TABLE public.whatsapp_notifications ADD COLUMN claim_token uuid;

-- PostgreSQL does not permit CREATE OR REPLACE to change OUT columns.
DROP FUNCTION public.claim_whatsapp_notifications(integer);

CREATE OR REPLACE FUNCTION public.claim_whatsapp_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_phone text,template_name text,template_language text,attempt_count integer,claim_token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  RETURN QUERY WITH candidates AS (
    SELECT n.id
    FROM public.whatsapp_notifications AS n
    WHERE (n.status = 'pending' AND n.scheduled_at <= clock_timestamp() AND n.next_attempt_at <= clock_timestamp())
       OR (n.status = 'processing' AND n.lease_until < clock_timestamp())
    ORDER BY n.scheduled_at
    FOR UPDATE SKIP LOCKED
    LIMIT LEAST(GREATEST(p_limit, 1), 100)
  ), claimed AS (
    UPDATE public.whatsapp_notifications AS n
    SET status = 'processing',
        attempt_count = n.attempt_count + 1,
        claimed_at = clock_timestamp(),
        lease_until = clock_timestamp() + interval '2 minutes',
        claim_token = gen_random_uuid(),
        updated_at = clock_timestamp()
    FROM candidates AS c
    WHERE n.id = c.id
    RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_phone,
              n.template_name,n.template_language,n.attempt_count,n.claim_token
  ) SELECT * FROM claimed;
END $$;

CREATE OR REPLACE FUNCTION public.finish_whatsapp_notification(p_id uuid,p_claim_token uuid,p_message_id text,p_error text,p_retry_at timestamptz)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  UPDATE public.whatsapp_notifications
  SET status = CASE WHEN p_message_id IS NOT NULL THEN 'sent' WHEN p_retry_at IS NOT NULL THEN 'pending' ELSE 'failed' END,
      provider_message_id = COALESCE(p_message_id, provider_message_id),
      sent_at = CASE WHEN p_message_id IS NOT NULL THEN clock_timestamp() ELSE sent_at END,
      last_error = p_error,
      next_attempt_at = COALESCE(p_retry_at, next_attempt_at),
      claimed_at = NULL,
      lease_until = NULL,
      claim_token = NULL,
      updated_at = clock_timestamp()
  WHERE id = p_id AND status = 'processing' AND claim_token = p_claim_token;
$$;

REVOKE ALL ON FUNCTION public.claim_whatsapp_notifications(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.finish_whatsapp_notification(uuid,uuid,text,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_whatsapp_notifications(integer) TO notification_db_app;
GRANT EXECUTE ON FUNCTION public.finish_whatsapp_notification(uuid,uuid,text,text,timestamptz) TO notification_db_app;
