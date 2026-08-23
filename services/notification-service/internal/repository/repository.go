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
func (r Repository) CancelReminders(ctx context.Context, tenant, appointment uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE public.email_notifications SET status='cancelled',updated_at=clock_timestamp() WHERE tenant_id=$1 AND appointment_id=$2 AND notification_type='appointment_reminder' AND status='pending'`, tenant, appointment)
		return err
	})
}
func (r Repository) Claim(ctx context.Context, limit int) ([]domain.Notification, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,appointment_id,notification_type,recipient_email,template_name,template_language,attempt_count,claim_token FROM public.claim_email_notifications($1)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Notification{}
	for rows.Next() {
		var item domain.Notification
		if err = rows.Scan(&item.ID, &item.TenantID, &item.AppointmentID, &item.Type, &item.Email, &item.Template, &item.Language, &item.Attempts, &item.ClaimToken); err != nil {
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
