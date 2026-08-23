ALTER TABLE public.whatsapp_notifications ADD COLUMN claimed_at timestamptz;
ALTER TABLE public.whatsapp_notifications ADD COLUMN lease_until timestamptz;
ALTER TABLE public.whatsapp_notifications ADD COLUMN provider_delivery_status text;
ALTER TABLE public.whatsapp_notifications ADD COLUMN provider_status_updated_at timestamptz;
CREATE INDEX whatsapp_notifications_provider_message_idx ON public.whatsapp_notifications(provider_message_id) WHERE provider_message_id IS NOT NULL;

CREATE OR REPLACE FUNCTION public.claim_whatsapp_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_phone text,template_name text,template_language text,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN RETURN QUERY WITH c AS (
 SELECT n.id FROM public.whatsapp_notifications n
 WHERE (n.status='pending' AND n.scheduled_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp())
    OR (n.status='processing' AND n.lease_until<clock_timestamp())
 ORDER BY n.scheduled_at FOR UPDATE SKIP LOCKED LIMIT LEAST(GREATEST(p_limit,1),100)
), u AS (
 UPDATE public.whatsapp_notifications n SET status='processing',attempt_count=n.attempt_count+1,claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
 FROM c WHERE n.id=c.id
 RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_phone,n.template_name,n.template_language,n.attempt_count
) SELECT * FROM u; END $$;
CREATE OR REPLACE FUNCTION public.finish_whatsapp_notification(p_id uuid,p_message_id text,p_error text,p_retry_at timestamptz)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$ UPDATE public.whatsapp_notifications SET status=CASE WHEN p_message_id IS NOT NULL THEN 'sent' WHEN p_retry_at IS NOT NULL THEN 'pending' ELSE 'failed' END, provider_message_id=COALESCE(p_message_id,provider_message_id),sent_at=CASE WHEN p_message_id IS NOT NULL THEN clock_timestamp() ELSE sent_at END,last_error=p_error,next_attempt_at=COALESCE(p_retry_at,next_attempt_at),claimed_at=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=p_id AND status='processing'; $$;
CREATE OR REPLACE FUNCTION public.apply_whatsapp_delivery_status(p_message_id text,p_status text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$ BEGIN UPDATE public.whatsapp_notifications SET provider_delivery_status=p_status,provider_status_updated_at=clock_timestamp(),updated_at=clock_timestamp() WHERE provider_message_id=p_message_id AND (provider_delivery_status IS DISTINCT FROM p_status); RETURN FOUND; END $$;
REVOKE ALL ON FUNCTION public.apply_whatsapp_delivery_status(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.apply_whatsapp_delivery_status(text,text) TO notification_db_app;
