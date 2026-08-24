package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrInvalidTransition   = errors.New("invalid appointment transition")
)

type Repository struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) Repository { return Repository{p} }

const appointmentColumns = `id,branch_id,barber_id,service_id,customer_name,customer_contact,start_at,end_at,occupied_start_at,occupied_end_at,status`

func scan(row pgx.Row, a *domain.Appointment) error {
	return row.Scan(&a.ID, &a.BranchID, &a.BarberID, &a.ServiceID, &a.CustomerName, &a.CustomerContact, &a.StartAt, &a.EndAt, &a.OccupiedStartAt, &a.OccupiedEndAt, &a.Status)
}

// Replay returns a previously committed result before callers repeat external
// validation. Concurrent first attempts still serialize inside Create.
func (r Repository) Replay(ctx context.Context, tenantID uuid.UUID, principalScope, operation, key, fingerprint string) (domain.Appointment, bool, error) {
	var appointment domain.Appointment
	found := false
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var existingID uuid.UUID
		var existingFingerprint string
		err := tx.QueryRow(ctx, `SELECT appointment_id,fingerprint FROM public.idempotency_keys WHERE tenant_id=$1 AND principal_scope=$2 AND operation=$3 AND key=$4`, tenantID, principalScope, operation, key).Scan(&existingID, &existingFingerprint)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if existingFingerprint != fingerprint {
			return ErrIdempotencyConflict
		}
		if err = scan(tx.QueryRow(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 AND id=$2`, tenantID, existingID), &appointment); err != nil {
			return err
		}
		found = true
		return nil
	})
	return appointment, found, err
}

func (r Repository) Create(ctx context.Context, tenantID uuid.UUID, in domain.CreateInput, endAt, occupiedStartAt, occupiedEndAt time.Time, principalScope, operation, key, fingerprint string) (domain.Appointment, error) {
	var appointment domain.Appointment
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		if key != "" {
			// Serialize only identical key attempts. This makes concurrent browser
			// retries return the same appointment rather than a unique-key race.
			scope := tenantID.String() + ":" + principalScope + ":" + operation + ":" + key
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, scope); err != nil {
				return err
			}
			var existingID uuid.UUID
			var existingFingerprint string
			err := tx.QueryRow(ctx, `SELECT appointment_id,fingerprint FROM public.idempotency_keys WHERE tenant_id=$1 AND principal_scope=$2 AND operation=$3 AND key=$4`, tenantID, principalScope, operation, key).Scan(&existingID, &existingFingerprint)
			if err == nil {
				if existingFingerprint != fingerprint {
					return ErrIdempotencyConflict
				}
				return scan(tx.QueryRow(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 AND id=$2`, tenantID, existingID), &appointment)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		err := scan(tx.QueryRow(ctx, `INSERT INTO public.appointments(tenant_id,branch_id,barber_id,service_id,customer_id,customer_name,customer_contact,start_at,end_at,occupied_start_at,occupied_end_at,status)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'pending') RETURNING `+appointmentColumns,
			tenantID, in.BranchID, in.BarberID, in.ServiceID, in.CustomerID, in.CustomerName, in.CustomerContact, in.StartAt, endAt, occupiedStartAt, occupiedEndAt), &appointment)
		if err != nil {
			return err
		}
		if key != "" {
			if _, err = tx.Exec(ctx, `INSERT INTO public.idempotency_keys(tenant_id,principal_scope,operation,key,fingerprint,appointment_id) VALUES($1,$2,$3,$4,$5,$6)`, tenantID, principalScope, operation, key, fingerprint, appointment.ID); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, tenantID, appointment, "AppointmentCreated", "appointment.created")
	})
	return appointment, err
}

func eventPayload(a domain.Appointment) []byte {
	// Customer contact is intentionally excluded from the durable event body.
	p, _ := json.Marshal(struct {
		AppointmentID uuid.UUID `json:"appointment_id"`
		BranchID      uuid.UUID `json:"branch_id"`
		BarberID      uuid.UUID `json:"barber_id"`
		ServiceID     uuid.UUID `json:"service_id"`
		StartAt       time.Time `json:"start_at"`
		EndAt         time.Time `json:"end_at"`
		Status        string    `json:"status"`
	}{a.ID, a.BranchID, a.BarberID, a.ServiceID, a.StartAt, a.EndAt, a.Status})
	return p
}

func appendEvent(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a domain.Appointment, eventName, outboxType string) error {
	payload := eventPayload(a)
	if _, err := tx.Exec(ctx, `INSERT INTO public.appointment_events(tenant_id,appointment_id,type,payload) VALUES($1,$2,$3,$4)`, tenantID, a.ID, eventName, payload); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO public.outbox_events(tenant_id,aggregate_id,type,payload) VALUES($1,$2,$3,$4)`, tenantID, a.ID, outboxType, payload)
	return err
}

func (r Repository) Get(ctx context.Context, tenantID, id uuid.UUID) (domain.Appointment, error) {
	var appointment domain.Appointment
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		err := scan(tx.QueryRow(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 AND id=$2`, tenantID, id), &appointment)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return appointment, err
}

func (r Repository) List(ctx context.Context, tenantID uuid.UUID) ([]domain.Appointment, error) {
	out := []domain.Appointment{}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 ORDER BY start_at DESC`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a domain.Appointment
			if err = rows.Scan(&a.ID, &a.BranchID, &a.BarberID, &a.ServiceID, &a.CustomerName, &a.CustomerContact, &a.StartAt, &a.EndAt, &a.OccupiedStartAt, &a.OccupiedEndAt, &a.Status); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (r Repository) ListForCustomer(ctx context.Context, tenantID, customerID uuid.UUID, upcoming bool) ([]domain.Appointment, error) {
	out := []domain.Appointment{}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		q := `SELECT ` + appointmentColumns + ` FROM public.appointments WHERE tenant_id=$1 AND customer_id=$2`
		if upcoming {
			q += ` AND start_at >= now() AND status IN ('pending','confirmed')`
		} else {
			q += ` AND (start_at < now() OR status IN ('cancelled','completed','no_show'))`
		}
		q += ` ORDER BY start_at DESC`
		rows, err := tx.Query(ctx, q, tenantID, customerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a domain.Appointment
			if err = rows.Scan(&a.ID, &a.BranchID, &a.BarberID, &a.ServiceID, &a.CustomerName, &a.CustomerContact, &a.StartAt, &a.EndAt, &a.OccupiedStartAt, &a.OccupiedEndAt, &a.Status); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (r Repository) GetForCustomer(ctx context.Context, tenantID, customerID, id uuid.UUID) (domain.Appointment, error) {
	var a domain.Appointment
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		err := scan(tx.QueryRow(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 AND customer_id=$2 AND id=$3`, tenantID, customerID, id), &a)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return a, err
}

var transitions = map[string]map[string]bool{
	"pending":   {"confirmed": true, "cancelled": true},
	"confirmed": {"cancelled": true, "completed": true, "no_show": true},
}

func (r Repository) Transition(ctx context.Context, tenantID, id uuid.UUID, to string) (domain.Appointment, error) {
	var appointment domain.Appointment
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `SELECT status FROM public.appointments WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !transitions[from][to] {
			return ErrInvalidTransition
		}
		err = scan(tx.QueryRow(ctx, `UPDATE public.appointments SET status=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING `+appointmentColumns, tenantID, id, to), &appointment)
		if err != nil {
			return err
		}
		titles := map[string]string{"confirmed": "AppointmentConfirmed", "cancelled": "AppointmentCancelled", "completed": "AppointmentCompleted", "no_show": "AppointmentNoShow"}
		return appendEvent(ctx, tx, tenantID, appointment, titles[to], "appointment."+to)
	})
	return appointment, err
}

func (r Repository) Reschedule(ctx context.Context, tenantID, id uuid.UUID, startAt, endAt, occupiedStartAt, occupiedEndAt time.Time) (domain.Appointment, error) {
	var appointment domain.Appointment
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM public.appointments WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "pending" && status != "confirmed" {
			return ErrInvalidTransition
		}
		err := scan(tx.QueryRow(ctx, `UPDATE public.appointments SET start_at=$3,end_at=$4,occupied_start_at=$5,occupied_end_at=$6,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING `+appointmentColumns, tenantID, id, startAt, endAt, occupiedStartAt, occupiedEndAt), &appointment)
		if err != nil {
			return err
		}
		return appendEvent(ctx, tx, tenantID, appointment, "AppointmentRescheduled", "appointment.rescheduled")
	})
	return appointment, err
}

func (r Repository) Occupancy(ctx context.Context, tenantID, barberID uuid.UUID, from, to time.Time) ([]domain.Appointment, error) {
	out := []domain.Appointment{}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+appointmentColumns+` FROM public.appointments WHERE tenant_id=$1 AND barber_id=$2 AND status IN ('pending','confirmed') AND occupied_start_at<$4 AND occupied_end_at>$3`, tenantID, barberID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a domain.Appointment
			if err = rows.Scan(&a.ID, &a.BranchID, &a.BarberID, &a.ServiceID, &a.CustomerName, &a.CustomerContact, &a.StartAt, &a.EndAt, &a.OccupiedStartAt, &a.OccupiedEndAt, &a.Status); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (r Repository) ClaimOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,aggregate_id,type,payload,created_at FROM public.claim_outbox_events($1)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		if err = rows.Scan(&e.ID, &e.TenantID, &e.AggregateID, &e.Type, &e.Payload, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r Repository) MarkOutboxPublished(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `SELECT public.mark_outbox_event_published($1)`, id)
	return err
}

// ReleaseOutbox reclaims a failed publish attempt back onto the outbox queue
// (or, once the bounded-retry threshold in release_outbox_event is crossed,
// marks the row terminal via failed_at). It reports the resulting attempt
// count and whether this call is the one that pushed the row terminal, so
// callers can distinguish "still retrying" from "just went terminal" without
// a second query.
func (r Repository) ReleaseOutbox(ctx context.Context, id uuid.UUID) (attempts int, terminal bool, err error) {
	err = r.pool.QueryRow(ctx, `SELECT publish_attempts, failed_at IS NOT NULL FROM public.release_outbox_event($1)`, id).Scan(&attempts, &terminal)
	return attempts, terminal, err
}

// OutboxBacklogStats reports the count and age of the oldest pending
// (unpublished, non-terminal) outbox row, for the outbox_backlog and
// outbox_oldest_pending_age_seconds gauges.
func (r Repository) OutboxBacklogStats(ctx context.Context) (count int, oldestPendingAge time.Duration, err error) {
	var ageSeconds float64
	err = r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(EXTRACT(EPOCH FROM (now() - min(created_at))), 0) FROM public.outbox_events WHERE published_at IS NULL AND failed_at IS NULL`).Scan(&count, &ageSeconds)
	if err != nil {
		return 0, 0, err
	}
	return count, time.Duration(ageSeconds * float64(time.Second)), nil
}

func (r Repository) CleanupRetention(ctx context.Context, limit int) (int, int, error) {
	var idempotencyDeleted, outboxDeleted int
	if err := r.pool.QueryRow(ctx, `SELECT public.cleanup_expired_idempotency_keys($1)`, limit).Scan(&idempotencyDeleted); err != nil {
		return 0, 0, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT public.cleanup_published_outbox_events($1)`, limit).Scan(&outboxDeleted); err != nil {
		return idempotencyDeleted, 0, err
	}
	return idempotencyDeleted, outboxDeleted, nil
}

func IsConflict(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && (pgErr.SQLState() == "23P01" || pgErr.SQLState() == "23505")
}
