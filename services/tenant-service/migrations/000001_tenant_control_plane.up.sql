CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS public.tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.tenant_domains (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES public.tenants(id),
    hostname text NOT NULL,
    domain_type text NOT NULL CHECK (domain_type IN ('booking', 'admin')),
    verified boolean NOT NULL DEFAULT false,
    ssl_status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (hostname)
);
CREATE INDEX IF NOT EXISTS idx_tenant_domains_tenant_type ON public.tenant_domains (tenant_id, domain_type);

CREATE TABLE IF NOT EXISTS public.tenant_settings (
    tenant_id uuid PRIMARY KEY REFERENCES public.tenants(id),
    business_timezone text NOT NULL DEFAULT 'Europe/Istanbul',
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE public.tenant_domains ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.tenant_domains FORCE ROW LEVEL SECURITY;
ALTER TABLE public.tenant_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.tenant_settings FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_domains_isolation ON public.tenant_domains;
CREATE POLICY tenant_domains_isolation ON public.tenant_domains USING (current_user = 'tenant_db_owner' OR tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (current_user = 'tenant_db_owner' OR tenant_id = current_setting('app.tenant_id', true)::uuid);
DROP POLICY IF EXISTS tenant_settings_isolation ON public.tenant_settings;
CREATE POLICY tenant_settings_isolation ON public.tenant_settings USING (current_user = 'tenant_db_owner' OR tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (current_user = 'tenant_db_owner' OR tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE OR REPLACE FUNCTION public.resolve_verified_domain(p_hostname text)
RETURNS TABLE (tenant_id uuid, app_type text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT d.tenant_id, d.domain_type
    FROM public.tenant_domains AS d
    JOIN public.tenants AS t ON t.id = d.tenant_id
    WHERE d.hostname = lower(p_hostname)
      AND d.verified = true
      AND t.status = 'active'
    LIMIT 1
$$;

REVOKE ALL ON FUNCTION public.resolve_verified_domain(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_verified_domain(text) TO tenant_db_app;
GRANT USAGE ON SCHEMA public TO tenant_db_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.tenant_domains, public.tenant_settings TO tenant_db_app;
GRANT SELECT ON public.tenants TO tenant_db_app;
