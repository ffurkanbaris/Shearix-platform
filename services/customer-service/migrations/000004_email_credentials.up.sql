ALTER TABLE public.customers
  ADD COLUMN normalized_email text,
  ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

ALTER TABLE public.customers ALTER COLUMN normalized_phone DROP NOT NULL;

CREATE UNIQUE INDEX customers_tenant_email_unique
  ON public.customers (tenant_id, normalized_email)
  WHERE normalized_email IS NOT NULL;

ALTER TABLE public.customers
  ADD CONSTRAINT customers_email_normalized_check
  CHECK (normalized_email IS NULL OR normalized_email = lower(btrim(normalized_email)));
