-- Credential notifications carry only delivery metadata. The credential and
-- recipient are sourced under a Redis lease at send time and are never stored
-- in notification_db.
ALTER TABLE public.whatsapp_notifications
  ALTER COLUMN appointment_id DROP NOT NULL,
  ALTER COLUMN recipient_phone DROP NOT NULL,
  ADD COLUMN delivery_id uuid,
  ADD COLUMN purpose text,
  ADD COLUMN credential_completed_at timestamptz;

ALTER TABLE public.whatsapp_notifications
  DROP CONSTRAINT whatsapp_notifications_notification_type_check;
ALTER TABLE public.whatsapp_notifications
  ADD CONSTRAINT whatsapp_notifications_notification_type_check
  CHECK (notification_type IN (
    'appointment_created',
    'appointment_rescheduled',
    'appointment_cancelled',
    'appointment_confirmed',
    'appointment_reminder',
    'user_initial_password'
  ));

ALTER TABLE public.whatsapp_notifications
  ADD CONSTRAINT whatsapp_notifications_credential_shape_check
  CHECK (
    (notification_type = 'user_initial_password' AND delivery_id IS NOT NULL AND purpose IS NOT NULL AND appointment_id IS NULL)
    OR notification_type <> 'user_initial_password'
  );

CREATE UNIQUE INDEX whatsapp_notifications_credential_delivery_uq
  ON public.whatsapp_notifications(tenant_id, delivery_id, notification_type)
  WHERE delivery_id IS NOT NULL;
CREATE INDEX whatsapp_notifications_credential_cleanup_idx
  ON public.whatsapp_notifications(created_at)
  WHERE notification_type='user_initial_password' AND status='sent' AND credential_completed_at IS NULL;

-- The worker processes a shared queue across tenants, so this controlled
-- function is the only cross-tenant access path. It exposes metadata only.
CREATE OR REPLACE FUNCTION public.sent_credential_notification_deliveries()
RETURNS TABLE(id uuid, tenant_id uuid, delivery_id uuid, purpose text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  SELECT n.id,n.tenant_id,n.delivery_id,n.purpose
  FROM public.whatsapp_notifications AS n
  WHERE n.notification_type='user_initial_password'
    AND n.status='sent'
    AND n.credential_completed_at IS NULL
    AND n.delivery_id IS NOT NULL
$$;

REVOKE ALL ON FUNCTION public.sent_credential_notification_deliveries() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.sent_credential_notification_deliveries() TO notification_db_app;

-- PostgreSQL cannot change a function's OUT columns in place.
DROP FUNCTION public.claim_whatsapp_notifications(integer);

CREATE OR REPLACE FUNCTION public.claim_whatsapp_notifications(p_limit integer)
RETURNS TABLE(
  id uuid,
  tenant_id uuid,
  appointment_id uuid,
  delivery_id uuid,
  purpose text,
  notification_type text,
  recipient_phone text,
  template_name text,
  template_language text,
  attempt_count integer,
  claim_token uuid
)
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
    RETURNING n.id,n.tenant_id,n.appointment_id,n.delivery_id,n.purpose,n.notification_type,
              n.recipient_phone,n.template_name,n.template_language,n.attempt_count,n.claim_token
  ) SELECT * FROM claimed;
END $$;

REVOKE ALL ON FUNCTION public.claim_whatsapp_notifications(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_whatsapp_notifications(integer) TO notification_db_app;
