CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public.branches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    address text NOT NULL DEFAULT '',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);
CREATE INDEX branches_tenant_active_idx ON public.branches (tenant_id, active);

CREATE TABLE public.barbers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    display_name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
CREATE INDEX barbers_tenant_active_idx ON public.barbers (tenant_id, active);

CREATE TABLE public.barber_profiles (
    barber_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    bio text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, barber_id),
    FOREIGN KEY (tenant_id, barber_id) REFERENCES public.barbers (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE public.barber_branch_assignments (
    tenant_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    branch_id uuid NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, barber_id, branch_id),
    FOREIGN KEY (tenant_id, barber_id) REFERENCES public.barbers (tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES public.branches (tenant_id, id)
);
CREATE INDEX barber_branch_assignments_tenant_branch_active_idx ON public.barber_branch_assignments (tenant_id, branch_id, active);

ALTER TABLE public.branches ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.branches FORCE ROW LEVEL SECURITY;
ALTER TABLE public.barbers ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.barbers FORCE ROW LEVEL SECURITY;
ALTER TABLE public.barber_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.barber_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE public.barber_branch_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.barber_branch_assignments FORCE ROW LEVEL SECURITY;

CREATE POLICY branches_tenant ON public.branches
    USING (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY barbers_tenant ON public.barbers
    USING (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY barber_profiles_tenant ON public.barber_profiles
    USING (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY barber_branch_assignments_tenant ON public.barber_branch_assignments
    USING (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user = 'barber_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON SCHEMA public TO barber_db_app;
GRANT SELECT, INSERT, UPDATE ON public.branches, public.barbers, public.barber_profiles, public.barber_branch_assignments TO barber_db_app;
