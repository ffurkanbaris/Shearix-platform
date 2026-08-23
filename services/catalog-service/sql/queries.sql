-- name: ListActiveServices :many
SELECT s.id,s.name,s.duration_minutes,s.buffer_before_minutes,s.buffer_after_minutes,p.amount::text AS price,p.currency FROM public.services s JOIN public.pricing p ON p.service_id=s.id AND p.tenant_id=s.tenant_id WHERE s.tenant_id=$1 AND s.active=true ORDER BY s.name;
