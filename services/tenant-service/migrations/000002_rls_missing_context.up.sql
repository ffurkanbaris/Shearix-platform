-- `set_config(..., true)` resets to an empty string after COMMIT on some
-- connections. NULLIF preserves deny-by-default RLS behavior outside a tenant
-- transaction without attempting to cast that empty value to uuid.
DROP POLICY IF EXISTS tenant_domains_isolation ON public.tenant_domains;
CREATE POLICY tenant_domains_isolation ON public.tenant_domains
USING (current_user = 'tenant_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
WITH CHECK (current_user = 'tenant_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_settings_isolation ON public.tenant_settings;
CREATE POLICY tenant_settings_isolation ON public.tenant_settings
USING (current_user = 'tenant_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
WITH CHECK (current_user = 'tenant_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
