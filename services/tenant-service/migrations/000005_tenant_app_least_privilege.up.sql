-- The tenant-service application role creates tenants but has no tenant status
-- mutation API. Do not leave an unused control-plane UPDATE privilege behind.
REVOKE UPDATE ON public.tenants FROM tenant_db_app;
