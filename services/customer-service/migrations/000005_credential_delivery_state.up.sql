ALTER TABLE public.customers
  ADD COLUMN initial_delivery_status text NOT NULL DEFAULT 'sent'
    CHECK (initial_delivery_status IN ('pending','sent','failed')),
  ADD COLUMN initial_delivery_attempts integer NOT NULL DEFAULT 0 CHECK(initial_delivery_attempts>=0),
  ADD COLUMN initial_delivery_last_error text,
  ADD COLUMN initial_delivery_updated_at timestamptz;
