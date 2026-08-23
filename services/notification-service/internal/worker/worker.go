package worker

import (
	"context"
	"errors"
	"fmt"
	"github.com/barber-appointment/notification-service/internal/domain"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

type notificationRepository interface {
	Claim(context.Context, int) ([]domain.Notification, error)
	Finish(context.Context, uuid.UUID, uuid.UUID, string, string, *time.Time) error
}
type Worker struct {
	r           notificationRepository
	p           platformemail.Sender
	log         *slog.Logger
	sendTimeout time.Duration
}

func New(r notificationRepository, p platformemail.Sender, l *slog.Logger) Worker {
	return NewWithTimeout(r, p, l, 10*time.Second)
}
func NewWithTimeout(r notificationRepository, p platformemail.Sender, l *slog.Logger, timeout time.Duration) Worker {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return Worker{r: r, p: p, log: l, sendTimeout: timeout}
}
func (w Worker) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		w.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (w Worker) Once(ctx context.Context) {
	items, err := w.r.Claim(ctx, 50)
	if err != nil {
		w.log.Warn("notification claim failed", "error", err)
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
		result, sendErr := w.p.Send(sendCtx, platformemail.Message{To: item.Email, Subject: fmt.Sprintf("Appointment update: %s", item.Type), Text: fmt.Sprintf("There is an update to your appointment: %s.", item.Type), Template: item.Template, IdempotencyKey: item.ID.String()})
		cancel()
		if sendErr == nil {
			if err = w.r.Finish(ctx, item.ID, item.ClaimToken, result.ProviderMessageID, "", nil); err != nil {
				w.log.Warn("notification completion persistence failed", "notification_id", item.ID, "error", err)
			}
			continue
		}
		temporary := platformemail.IsTemporary(sendErr) || errors.Is(sendErr, context.DeadlineExceeded)
		problem := "email delivery failed"
		if temporary {
			problem = "email delivery temporarily unavailable"
		}
		if temporary && item.Attempts < 5 {
			next := time.Now().UTC().Add(time.Duration(1<<min(item.Attempts, 5)) * time.Minute)
			_ = w.r.Finish(ctx, item.ID, item.ClaimToken, "", problem, &next)
		} else {
			_ = w.r.Finish(ctx, item.ID, item.ClaimToken, "", problem, nil)
		}
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
