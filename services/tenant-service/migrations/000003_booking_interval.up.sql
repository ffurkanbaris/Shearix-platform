ALTER TABLE public.tenant_settings ADD COLUMN booking_interval_minutes integer NOT NULL DEFAULT 15 CHECK (booking_interval_minutes BETWEEN 1 AND 120);
