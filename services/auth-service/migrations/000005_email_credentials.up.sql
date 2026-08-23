ALTER TABLE public.credentials
  ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS identities_normalized_email_unique
  ON public.identities (lower(email)) WHERE email IS NOT NULL;

-- The old metadata-only credential outbox is no longer used. Plaintext is
-- delivered directly from process memory and never staged in Redis/JetStream.
DROP FUNCTION IF EXISTS public.claim_credential_delivery_outbox(integer);
DROP FUNCTION IF EXISTS public.finish_credential_delivery_outbox(uuid,uuid,boolean,text,timestamptz);
DROP TABLE IF EXISTS public.credential_delivery_outbox;
