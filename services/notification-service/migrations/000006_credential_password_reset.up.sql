ALTER TABLE public.whatsapp_notifications
  DROP CONSTRAINT whatsapp_notifications_notification_type_check,
  DROP CONSTRAINT whatsapp_notifications_credential_shape_check;

ALTER TABLE public.whatsapp_notifications
  ADD CONSTRAINT whatsapp_notifications_notification_type_check
  CHECK (notification_type IN (
    'appointment_created',
    'appointment_rescheduled',
    'appointment_cancelled',
    'appointment_confirmed',
    'appointment_reminder',
    'user_initial_password',
    'user_password_reset'
  )),
  ADD CONSTRAINT whatsapp_notifications_credential_shape_check
  CHECK (
    (notification_type IN ('user_initial_password','user_password_reset') AND delivery_id IS NOT NULL AND purpose IS NOT NULL AND appointment_id IS NULL)
    OR notification_type NOT IN ('user_initial_password','user_password_reset')
  );

CREATE OR REPLACE FUNCTION public.sent_credential_notification_deliveries()
RETURNS TABLE(id uuid, tenant_id uuid, delivery_id uuid, purpose text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
  SELECT n.id,n.tenant_id,n.delivery_id,n.purpose
  FROM public.whatsapp_notifications AS n
  WHERE n.notification_type IN ('user_initial_password','user_password_reset')
    AND n.status='sent'
    AND n.credential_completed_at IS NULL
    AND n.delivery_id IS NOT NULL
$$;
REVOKE ALL ON FUNCTION public.sent_credential_notification_deliveries() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.sent_credential_notification_deliveries() TO notification_db_app;
