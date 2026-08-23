-- Customer sessions are tenant-owned. The same opaque token hash may exist in
-- distinct tenant authentication domains, but never twice in one tenant.
ALTER TABLE public.customer_sessions DROP CONSTRAINT customer_sessions_token_hash_key;
ALTER TABLE public.customer_sessions
  ADD CONSTRAINT customer_sessions_tenant_token_hash_key UNIQUE (tenant_id, token_hash);
DROP INDEX public.customer_sessions_lookup_idx;
CREATE INDEX customer_sessions_lookup_idx
  ON public.customer_sessions (tenant_id, token_hash, expires_at)
  WHERE revoked_at IS NULL;

-- Use the same explicit fail-closed tenant expression as other services.
DROP POLICY customers_tenant ON public.customers;
DROP POLICY customer_sessions_tenant ON public.customer_sessions;
CREATE POLICY customers_tenant ON public.customers
  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY customer_sessions_tenant ON public.customer_sessions
  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- A sending delivery must have a complete live claim; no other state may
-- retain claim material. Existing repository transitions already obey this.
ALTER TABLE public.customers
  ADD CONSTRAINT customers_initial_delivery_claim_state_check CHECK (
    (initial_delivery_status = 'sending'
      AND initial_delivery_claim_token IS NOT NULL
      AND initial_delivery_claim_until IS NOT NULL)
    OR
    (initial_delivery_status <> 'sending'
      AND initial_delivery_claim_token IS NULL
      AND initial_delivery_claim_until IS NULL)
  );
