-- Extend claim_email_notifications to report whether each claimed row was a
-- fresh 'pending' row or a reclaim of a 'processing' row whose lease had
-- already expired (i.e. a previous worker crashed or stalled mid-send). This
-- is purely for observability (notification_claim_recovered_total); it does
-- not change which rows are eligible or how they are locked/claimed - the
-- WHERE/FOR UPDATE SKIP LOCKED fencing logic is unchanged.
DROP FUNCTION IF EXISTS public.claim_email_notifications(integer);
CREATE FUNCTION public.claim_email_notifications(p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,appointment_id uuid,notification_type text,recipient_email text,template_name text,template_language text,attempt_count integer,claim_token uuid,recovered boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN RETURN QUERY WITH c AS (
  SELECT n.id, (n.status='processing') AS was_processing FROM public.email_notifications n
  WHERE (n.status='pending' AND n.scheduled_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp())
     OR (n.status='processing' AND n.lease_until<clock_timestamp())
  ORDER BY n.scheduled_at FOR UPDATE SKIP LOCKED LIMIT LEAST(GREATEST(p_limit,1),100)
), u AS (
  UPDATE public.email_notifications n SET status='processing',attempt_count=n.attempt_count+1,claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '2 minutes',claim_token=gen_random_uuid(),updated_at=clock_timestamp()
  FROM c WHERE n.id=c.id
  RETURNING n.id,n.tenant_id,n.appointment_id,n.notification_type,n.recipient_email,n.template_name,n.template_language,n.attempt_count,n.claim_token
) SELECT u.id,u.tenant_id,u.appointment_id,u.notification_type,u.recipient_email,u.template_name,u.template_language,u.attempt_count,u.claim_token,c.was_processing FROM u JOIN c ON c.id=u.id; END $$;

REVOKE ALL ON FUNCTION public.claim_email_notifications(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.claim_email_notifications(integer) TO notification_db_app;
