-- name: ListAppointments :many
SELECT id,barber_id,service_id,customer_name,customer_contact,start_at,end_at,status FROM public.appointments WHERE tenant_id=$1 ORDER BY start_at DESC;
