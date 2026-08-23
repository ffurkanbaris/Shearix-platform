-- name: ListBranches :many
SELECT id, name, address, active FROM public.branches WHERE tenant_id=$1 ORDER BY name;
-- name: ListPublicBranches :many
SELECT id, name, address FROM public.branches WHERE tenant_id=$1 AND active=true ORDER BY name;
