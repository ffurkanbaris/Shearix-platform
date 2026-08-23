ALTER TABLE public.credentials DROP CONSTRAINT credentials_initial_delivery_status_check;
ALTER TABLE public.credentials ADD CONSTRAINT credentials_initial_delivery_status_check
  CHECK (initial_delivery_status IN ('pending','sending','sent','failed'));
ALTER TABLE public.credentials
  ADD COLUMN initial_delivery_claim_token uuid,
  ADD COLUMN initial_delivery_claim_until timestamptz;

