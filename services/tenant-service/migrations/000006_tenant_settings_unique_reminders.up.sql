-- Keep tenant setting validation defensible even if a future internal caller
-- bypasses HTTP validation. Existing range/non-empty constraints remain in
-- place; this immutable migration adds unique reminder offsets.
CREATE OR REPLACE FUNCTION public.integer_array_is_unique(items integer[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
STRICT
SET search_path = pg_catalog
AS $$
    SELECT cardinality(items) = cardinality(ARRAY(SELECT DISTINCT item FROM unnest(items) AS item))
$$;

ALTER TABLE public.tenant_settings
    DROP CONSTRAINT IF EXISTS tenant_settings_reminder_offsets_unique;
ALTER TABLE public.tenant_settings
    ADD CONSTRAINT tenant_settings_reminder_offsets_unique
    CHECK (public.integer_array_is_unique(reminder_offsets_minutes));
