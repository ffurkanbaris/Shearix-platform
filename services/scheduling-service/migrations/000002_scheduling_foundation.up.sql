CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE public.barber_working_hours (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    start_time time NOT NULL,
    end_time time NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (start_time < end_time),
    UNIQUE (tenant_id, id),
    EXCLUDE USING gist (tenant_id WITH =, barber_id WITH =, weekday WITH =, tsrange(timestamp '2000-01-01' + start_time, timestamp '2000-01-01' + end_time, '[)') WITH &&)
);

CREATE TABLE public.schedule_overrides (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    override_date date NOT NULL,
    kind text NOT NULL CHECK (kind IN ('unavailable', 'vacation', 'custom_hours')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, barber_id, override_date),
    UNIQUE (tenant_id, id, barber_id, override_date)
);

CREATE TABLE public.schedule_override_intervals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    override_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    override_date date NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    CHECK (start_time < end_time),
    FOREIGN KEY (tenant_id, override_id, barber_id, override_date) REFERENCES public.schedule_overrides (tenant_id, id, barber_id, override_date) ON DELETE CASCADE,
    EXCLUDE USING gist (tenant_id WITH =, barber_id WITH =, override_date WITH =, tsrange(timestamp '2000-01-01' + start_time, timestamp '2000-01-01' + end_time, '[)') WITH &&)
);

CREATE TABLE public.blocked_periods (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    barber_id uuid NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (start_at < end_at),
    UNIQUE (tenant_id, id)
);
CREATE INDEX blocked_periods_tenant_barber_range_idx ON public.blocked_periods (tenant_id, barber_id, start_at, end_at);
CREATE INDEX schedule_overrides_tenant_barber_date_idx ON public.schedule_overrides (tenant_id, barber_id, override_date);

ALTER TABLE public.barber_working_hours ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.barber_working_hours FORCE ROW LEVEL SECURITY;
ALTER TABLE public.schedule_overrides ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.schedule_overrides FORCE ROW LEVEL SECURITY;
ALTER TABLE public.schedule_override_intervals ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.schedule_override_intervals FORCE ROW LEVEL SECURITY;
ALTER TABLE public.blocked_periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.blocked_periods FORCE ROW LEVEL SECURITY;

CREATE POLICY working_hours_tenant ON public.barber_working_hours USING (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY overrides_tenant ON public.schedule_overrides USING (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY override_intervals_tenant ON public.schedule_override_intervals USING (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY blocked_periods_tenant ON public.blocked_periods USING (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (current_user = 'scheduling_db_owner' OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON SCHEMA public TO scheduling_db_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO scheduling_db_app;
