CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public.customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    name text NOT NULL CHECK (length(btrim(name)) > 0),
    normalized_phone text NOT NULL,
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    password_changed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, normalized_phone)
);
CREATE TABLE public.customer_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES public.customers(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX customer_sessions_lookup_idx ON public.customer_sessions(token_hash, expires_at) WHERE revoked_at IS NULL;
ALTER TABLE public.customers ENABLE ROW LEVEL SECURITY; ALTER TABLE public.customers FORCE ROW LEVEL SECURITY;
ALTER TABLE public.customer_sessions ENABLE ROW LEVEL SECURITY; ALTER TABLE public.customer_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY customers_tenant ON public.customers USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE POLICY customer_sessions_tenant ON public.customer_sessions USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON public.customers, public.customer_sessions TO customer_db_app;
