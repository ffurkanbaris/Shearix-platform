-- Ordinary staff APIs must never remove the final active tenant owner. The
-- trigger is defense in depth behind application-level OWNER-only checks.
CREATE OR REPLACE FUNCTION public.prevent_last_active_owner_change()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
BEGIN
  IF OLD.role = 'OWNER' AND OLD.status = 'active'
     AND (NEW.role <> 'OWNER' OR NEW.status <> 'active')
     AND NOT EXISTS (
       SELECT 1
       FROM public.tenant_memberships AS m
       WHERE m.tenant_id = OLD.tenant_id
         AND m.identity_id <> OLD.identity_id
         AND m.role = 'OWNER'
         AND m.status = 'active'
     ) THEN
    RAISE EXCEPTION 'cannot remove or demote the last active tenant owner'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS tenant_memberships_last_owner_guard ON public.tenant_memberships;
CREATE TRIGGER tenant_memberships_last_owner_guard
BEFORE UPDATE OF role, status ON public.tenant_memberships
FOR EACH ROW EXECUTE FUNCTION public.prevent_last_active_owner_change();

REVOKE ALL ON FUNCTION public.prevent_last_active_owner_change() FROM PUBLIC;
