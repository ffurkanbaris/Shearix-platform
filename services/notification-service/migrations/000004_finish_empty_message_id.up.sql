-- Go represents an absent provider message ID as an empty string. Treat it as
-- SQL NULL so temporary and permanent provider failures never become sent.
CREATE OR REPLACE FUNCTION public.finish_whatsapp_notification(p_id uuid,p_claim_token uuid,p_message_id text,p_error text,p_retry_at timestamptz)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  UPDATE public.whatsapp_notifications
  SET status = CASE WHEN NULLIF(p_message_id, '') IS NOT NULL THEN 'sent' WHEN p_retry_at IS NOT NULL THEN 'pending' ELSE 'failed' END,
      provider_message_id = COALESCE(NULLIF(p_message_id, ''), provider_message_id),
      sent_at = CASE WHEN NULLIF(p_message_id, '') IS NOT NULL THEN clock_timestamp() ELSE sent_at END,
      last_error = p_error,
      next_attempt_at = COALESCE(p_retry_at, next_attempt_at),
      claimed_at = NULL,
      lease_until = NULL,
      claim_token = NULL,
      updated_at = clock_timestamp()
  WHERE id = p_id AND status = 'processing' AND claim_token = p_claim_token;
$$;
REVOKE ALL ON FUNCTION public.finish_whatsapp_notification(uuid,uuid,text,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.finish_whatsapp_notification(uuid,uuid,text,text,timestamptz) TO notification_db_app;
