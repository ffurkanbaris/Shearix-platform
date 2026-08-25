CREATE TABLE public.platform_audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor text NOT NULL,
    action text NOT NULL,
    tenant_id uuid REFERENCES public.tenants(id),
    request_id text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX platform_audit_log_created_idx ON public.platform_audit_log (created_at DESC);
CREATE INDEX platform_audit_log_tenant_idx ON public.platform_audit_log (tenant_id, created_at DESC);

GRANT SELECT, INSERT ON public.platform_audit_log TO tenant_db_app;
GRANT UPDATE ON public.tenants TO tenant_db_app;
