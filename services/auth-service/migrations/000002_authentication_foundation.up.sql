CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public.identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE public.credentials (
    identity_id uuid PRIMARY KEY REFERENCES public.identities(id) ON DELETE CASCADE,
    password_hash text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE public.tenant_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    identity_id uuid NOT NULL REFERENCES public.identities(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('OWNER', 'MANAGER', 'BARBER', 'RECEPTIONIST')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, identity_id)
);
CREATE INDEX idx_tenant_memberships_tenant_identity_active ON public.tenant_memberships (tenant_id, identity_id) WHERE status = 'active';
CREATE TABLE public.sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    identity_id uuid NOT NULL REFERENCES public.identities(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, token_hash)
);
CREATE INDEX idx_sessions_tenant_token_active ON public.sessions (tenant_id, token_hash) WHERE revoked_at IS NULL;

ALTER TABLE public.tenant_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.tenant_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE public.sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_memberships_isolation ON public.tenant_memberships
USING (current_user = 'auth_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
WITH CHECK (current_user = 'auth_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY sessions_isolation ON public.sessions
USING (current_user = 'auth_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
WITH CHECK (current_user = 'auth_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON SCHEMA public TO auth_db_app;
GRANT SELECT ON public.identities, public.credentials TO auth_db_app;
GRANT SELECT ON public.tenant_memberships TO auth_db_app;
GRANT SELECT, INSERT, UPDATE ON public.sessions TO auth_db_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO auth_db_app;
