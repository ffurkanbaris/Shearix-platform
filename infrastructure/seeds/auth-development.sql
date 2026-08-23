BEGIN;
SET ROLE auth_db_owner;
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000001', true);
INSERT INTO public.identities (id, email, name, phone, status)
VALUES ('00000000-0000-0000-0000-000000000101', 'demo-owner@booking.local', 'Demo Owner', '+905550000001', 'active')
ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, phone = EXCLUDED.phone, status = EXCLUDED.status, updated_at = now();
INSERT INTO public.credentials (identity_id, password_hash)
VALUES ('00000000-0000-0000-0000-000000000101', '$2a$10$PNJcLaVdJtlGPn2x2APuSOpQ5CK9DEqQRD6Hf0HfaGaf/US7KoKKS')
ON CONFLICT (identity_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, must_change_password = false, updated_at = now();
INSERT INTO public.tenant_memberships (tenant_id, identity_id, role, status)
VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000101', 'OWNER', 'active')
ON CONFLICT (tenant_id, identity_id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status;
COMMIT;
