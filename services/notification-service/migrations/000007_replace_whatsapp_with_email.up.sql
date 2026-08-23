ALTER TABLE public.whatsapp_notifications RENAME TO email_notifications;
ALTER TABLE public.email_notifications RENAME COLUMN recipient_phone TO recipient_email;
ALTER INDEX public.whatsapp_notifications_due_idx RENAME TO email_notifications_due_idx;
ALTER INDEX public.whatsapp_notifications_appointment_idx RENAME TO email_notifications_appointment_idx;

DROP FUNCTION IF EXISTS public.claim_whatsapp_notifications(integer);
DROP FUNCTION IF EXISTS public.finish_whatsapp_notification(uuid,uuid,text,text,timestamptz);
DROP FUNCTION IF EXISTS public.apply_whatsapp_delivery_status(text,text);
DROP FUNCTION IF EXISTS public.sent_credential_notification_deliveries();

CREATE OR REPLACE FUNCTION public.claim_email_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_email text,template_name text,template_language text,attempt_count integer,claim_token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN RETURN QUERY WITH c AS (
  SELECT n.id FROM public.email_notifications n
  WHERE (n.status='pending' AND n.scheduled_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp())
     OR (n.status='processing' AND n.lease_until<clock_timestamp())
  ORDER BY n.scheduled_at FOR UPDATE SKIP LOCKED LIMIT LEAST(GREATEST(p_limit,1),100)
), u AS (
  UPDATE public.email_notifications n SET status='processing',attempt_count=n.attempt_count+1,claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '2 minutes',claim_token=gen_random_uuid(),updated_at=clock_timestamp()
  FROM c WHERE n.id=c.id
  RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_email,n.template_name,n.template_language,n.attempt_count,n.claim_token
) SELECT * FROM u; END $$;

CREATE OR REPLACE FUNCTION public.finish_email_notification(p_id uuid,p_claim_token uuid,p_message_id text,p_error text,p_retry_at timestamptz)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
UPDATE public.email_notifications SET
 status=CASE WHEN p_message_id<>'' THEN 'sent' WHEN p_retry_at IS NOT NULL THEN 'pending' ELSE 'failed' END,
 provider_message_id=CASE WHEN p_message_id<>'' THEN p_message_id ELSE provider_message_id END,
 sent_at=CASE WHEN p_message_id<>'' THEN clock_timestamp() ELSE sent_at END,
 last_error=p_error,next_attempt_at=COALESCE(p_retry_at,next_attempt_at),claimed_at=NULL,lease_until=NULL,claim_token=NULL,updated_at=clock_timestamp()
WHERE id=p_id AND status='processing' AND claim_token=p_claim_token $$;

REVOKE ALL ON FUNCTION public.claim_email_notifications(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.finish_email_notification(uuid,uuid,text,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_email_notifications(integer) TO notification_db_app;
GRANT EXECUTE ON FUNCTION public.finish_email_notification(uuid,uuid,text,text,timestamptz) TO notification_db_app;

-- Credential delivery no longer passes through notification-service.
DELETE FROM public.email_notifications WHERE notification_type IN ('user_initial_password','user_password_reset');
ALTER TABLE public.email_notifications DROP CONSTRAINT IF EXISTS whatsapp_notifications_credential_shape_check;
ALTER TABLE public.email_notifications DROP CONSTRAINT IF EXISTS whatsapp_notifications_notification_type_check;
DROP INDEX IF EXISTS public.whatsapp_notifications_credential_delivery_uq;
DROP INDEX IF EXISTS public.whatsapp_notifications_credential_cleanup_idx;
ALTER TABLE public.email_notifications DROP COLUMN IF EXISTS delivery_id;
ALTER TABLE public.email_notifications DROP COLUMN IF EXISTS purpose;
ALTER TABLE public.email_notifications DROP COLUMN IF EXISTS credential_completed_at;
ALTER TABLE public.email_notifications ALTER COLUMN appointment_id SET NOT NULL;
ALTER TABLE public.email_notifications ALTER COLUMN recipient_email SET NOT NULL;
ALTER TABLE public.email_notifications ADD CONSTRAINT email_notifications_type_check CHECK(notification_type IN('appointment_created','appointment_rescheduled','appointment_cancelled','appointment_confirmed','appointment_reminder'));
UPDATE public.email_notifications SET status='cancelled',last_error='legacy WhatsApp delivery removed' WHERE recipient_email NOT LIKE '%@%' AND status IN('pending','processing');
