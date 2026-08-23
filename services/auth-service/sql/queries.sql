-- name: FindCredentialByEmail :one
SELECT i.id, COALESCE(i.name,''), i.email, COALESCE(i.phone,''), i.status, c.password_hash,
       c.must_change_password, c.initial_delivery_status
FROM public.identities AS i
JOIN public.credentials AS c ON c.identity_id = i.id
WHERE lower(i.email) = lower($1);

-- name: FindActiveMembership :one
SELECT id, role FROM public.tenant_memberships
WHERE tenant_id = $1 AND identity_id = $2 AND status = 'active';
