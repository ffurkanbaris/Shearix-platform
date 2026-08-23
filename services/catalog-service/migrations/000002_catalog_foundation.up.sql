CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public.services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    duration_minutes integer NOT NULL CHECK (duration_minutes >= 0),
    buffer_before_minutes integer NOT NULL DEFAULT 0 CHECK (buffer_before_minutes >= 0),
    buffer_after_minutes integer NOT NULL DEFAULT 0 CHECK (buffer_after_minutes >= 0),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);
CREATE INDEX services_tenant_active_idx ON public.services (tenant_id, active);

CREATE TABLE public.pricing (
    service_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    amount numeric(12,2) NOT NULL CHECK (amount >= 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, service_id),
    FOREIGN KEY (tenant_id, service_id) REFERENCES public.services (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE public.barber_services (
    tenant_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    service_id uuid NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, barber_id, service_id),
    FOREIGN KEY (tenant_id, service_id) REFERENCES public.services (tenant_id, id)
);
CREATE INDEX barber_services_tenant_barber_active_idx ON public.barber_services (tenant_id, barber_id, active);

ALTER TABLE public.services ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.services FORCE ROW LEVEL SECURITY;
ALTER TABLE public.pricing ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.pricing FORCE ROW LEVEL SECURITY;
ALTER TABLE public.barber_services ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.barber_services FORCE ROW LEVEL SECURITY;

CREATE POLICY services_tenant ON public.services
    USING (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY pricing_tenant ON public.pricing
    USING (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY barber_services_tenant ON public.barber_services
    USING (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'catalog_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON SCHEMA public TO catalog_db_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.services, public.pricing, public.barber_services TO catalog_db_app;
