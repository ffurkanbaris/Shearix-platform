-- name: ListWorkingHours :many
SELECT weekday, start_time, end_time FROM public.barber_working_hours WHERE tenant_id=$1 AND barber_id=$2 ORDER BY weekday, start_time;
