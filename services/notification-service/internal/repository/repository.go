package repository

import (
	"context"
	"errors"
	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) Repository { return Repository{pool: pool} }
func (r Repository) Plan(ctx context.Context, tenant uuid.UUID, event domain.Envelope, kind, recipient, template, language string, scheduled time.Time) (bool, error) {
	if recipient == "" {
		return false, errors.New("invalid recipient email")
	}
	created := false
	err := db.WithTenantTx(ctx, r.pool, tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO public.email_notifications(tenant_id,event_id,appointment_id,notification_type,recipient_email,template_name,template_language,scheduled_at,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) ON CONFLICT(tenant_id,event_id,notification_type) DO NOTHING`, tenant, event.EventID, event.AggregateID, kind, recipient, template, language, scheduled)
		created = tag.RowsAffected() == 1
		return err
	})
	return created, err
}

// terminalRecipientPlaceholder marks a Fail() audit row: it is never a real
// delivery destination, just a satisfaction of email_notifications' NOT NULL
// recipient_email column for a message that could not be delivered at all
// (e.g. because its actual recipient could never be resolved).
const terminalRecipientPlaceholder = "undeliverable@notification-service.invalid"

// Fail persists an auditable terminal record for an event that JetStream has
// stopped redelivering (MaxDeliver exhausted, message Term()'d). It reuses
// the same (tenant_id,event_id,notification_type) identity as Plan so a
// terminal failure for an event that was never successfully planned is still
// visible for operator investigation, without disturbing a row that already
// reached a final state such as 'sent' or 'cancelled'.
func (r Repository) Fail(ctx context.Context, tenant uuid.UUID, event domain.Envelope, kind, template, language, reason string) error {
	return db.WithTenantTx(ctx, r.pool, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO public.email_notifications(tenant_id,event_id,appointment_id,notification_type,recipient_email,template_name,template_language,status,last_error,scheduled_at,next_attempt_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'failed',$8,clock_timestamp(),clock_timestamp())
			ON CONFLICT(tenant_id,event_id,notification_type) DO UPDATE SET status='failed',last_error=EXCLUDED.last_error,updated_at=clock_timestamp()
			WHERE public.email_notifications.status NOT IN('sent','cancelled')`,
			tenant, event.EventID, event.AggregateID, kind, terminalRecipientPlaceholder, template, language, reason)
		return err
	})
}
func (r Repository) CancelReminders(ctx context.Context, tenant, appointment uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE public.email_notifications SET status='cancelled',updated_at=clock_timestamp() WHERE tenant_id=$1 AND appointment_id=$2 AND notification_type='appointment_reminder' AND status='pending'`, tenant, appointment)
		return err
	})
}
func (r Repository) Claim(ctx context.Context, limit int) ([]domain.Notification, error) {
	return r.claim(ctx, `SELECT id,tenant_id,appointment_id,notification_type,recipient_email,template_name,template_language,attempt_count,claim_token,recovered FROM public.claim_email_notifications($1)`, limit)
}

// ClaimForTenant claims only rows belonging to tenant, using the same
// FOR UPDATE SKIP LOCKED batching, lease-expiry reclaim, and claim-token
// fencing as Claim (both are backed by the same underlying SQL logic - see
// migration 000009). The production worker never calls this: its single
// background loop always processes the full cross-tenant backlog via Claim,
// which is the correct design for one shared delivery worker. This exists so
// a caller that must not observe or interfere with other tenants' rows -
// currently, integration tests sharing one live database with other test
// packages - can do so safely.
func (r Repository) ClaimForTenant(ctx context.Context, tenant uuid.UUID, limit int) ([]domain.Notification, error) {
	return r.claim(ctx, `SELECT id,tenant_id,appointment_id,notification_type,recipient_email,template_name,template_language,attempt_count,claim_token,recovered FROM public.claim_email_notifications_for_tenant($1,$2)`, limit, tenant)
}

func (r Repository) claim(ctx context.Context, query string, args ...any) ([]domain.Notification, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Notification{}
	for rows.Next() {
		var item domain.Notification
		if err = rows.Scan(&item.ID, &item.TenantID, &item.AppointmentID, &item.Type, &item.Email, &item.Template, &item.Language, &item.Attempts, &item.ClaimToken, &item.Recovered); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r Repository) Finish(ctx context.Context, id, claim uuid.UUID, messageID, problem string, retryAt *time.Time) error {
	_, err := r.pool.Exec(ctx, `SELECT public.finish_email_notification($1,$2,$3,$4,$5)`, id, claim, messageID, problem, retryAt)
	return err
}
