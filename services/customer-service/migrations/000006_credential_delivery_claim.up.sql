ALTER TABLE public.customers DROP CONSTRAINT customers_initial_delivery_status_check;
ALTER TABLE public.customers ADD CONSTRAINT customers_initial_delivery_status_check
  CHECK (initial_delivery_status IN ('pending','sending','sent','failed'));
ALTER TABLE public.customers
  ADD COLUMN initial_delivery_claim_token uuid,
  ADD COLUMN initial_delivery_claim_until timestamptz;

