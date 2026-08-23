CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE public.whatsapp_notifications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, event_id uuid NOT NULL, appointment_id uuid NOT NULL,
 notification_type text NOT NULL CHECK(notification_type IN ('appointment_created','appointment_rescheduled','appointment_cancelled','appointment_confirmed','appointment_reminder')),
 recipient_phone text NOT NULL, template_name text NOT NULL, template_language text NOT NULL DEFAULT 'en', status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','sent','failed','cancelled')),
 attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count >= 0), provider_message_id text, scheduled_at timestamptz NOT NULL DEFAULT now(), next_attempt_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz, last_error text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,event_id,notification_type)
);
CREATE INDEX whatsapp_notifications_due_idx ON public.whatsapp_notifications(scheduled_at,next_attempt_at) WHERE status='pending';
CREATE INDEX whatsapp_notifications_appointment_idx ON public.whatsapp_notifications(tenant_id,appointment_id);
ALTER TABLE public.whatsapp_notifications ENABLE ROW LEVEL SECURITY; ALTER TABLE public.whatsapp_notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY whatsapp_notifications_tenant ON public.whatsapp_notifications USING(current_user='notification_db_owner' OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(current_user='notification_db_owner' OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
GRANT USAGE ON SCHEMA public TO notification_db_app; GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA public TO notification_db_app;

CREATE OR REPLACE FUNCTION public.claim_whatsapp_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_phone text,template_name text,template_language text,attempt_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN RETURN QUERY WITH c AS (SELECT n.id FROM public.whatsapp_notifications n WHERE n.status='pending' AND n.scheduled_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp() ORDER BY n.scheduled_at FOR UPDATE SKIP LOCKED LIMIT LEAST(GREATEST(p_limit,1),100)), u AS (UPDATE public.whatsapp_notifications n SET status='processing',attempt_count=n.attempt_count+1,updated_at=clock_timestamp() FROM c WHERE n.id=c.id RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_phone,n.template_name,n.template_language,n.attempt_count) SELECT * FROM u; END $$;
CREATE OR REPLACE FUNCTION public.finish_whatsapp_notification(p_id uuid,p_message_id text,p_error text,p_retry_at timestamptz)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$ UPDATE public.whatsapp_notifications SET status=CASE WHEN p_message_id IS NOT NULL THEN 'sent' WHEN p_retry_at IS NOT NULL THEN 'pending' ELSE 'failed' END, provider_message_id=COALESCE(p_message_id,provider_message_id), sent_at=CASE WHEN p_message_id IS NOT NULL THEN clock_timestamp() ELSE sent_at END, last_error=p_error,next_attempt_at=COALESCE(p_retry_at,next_attempt_at),updated_at=clock_timestamp() WHERE id=p_id AND status='processing'; $$;
REVOKE ALL ON FUNCTION public.claim_whatsapp_notifications(integer) FROM PUBLIC; REVOKE ALL ON FUNCTION public.finish_whatsapp_notification(uuid,text,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_whatsapp_notifications(integer) TO notification_db_app; GRANT EXECUTE ON FUNCTION public.finish_whatsapp_notification(uuid,text,text,timestamptz) TO notification_db_app;
