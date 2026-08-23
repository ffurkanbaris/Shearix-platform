ALTER TABLE public.barbers ADD COLUMN identity_id uuid;
CREATE UNIQUE INDEX barbers_tenant_identity_id_unique ON public.barbers (tenant_id, identity_id) WHERE identity_id IS NOT NULL;
