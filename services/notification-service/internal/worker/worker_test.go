package worker

import (
	"context"
	"github.com/barber-appointment/notification-service/internal/domain"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeRepo struct {
	item     domain.Notification
	finished int
}

func (r *fakeRepo) Claim(context.Context, int) ([]domain.Notification, error) {
	if r.finished > 0 {
		return nil, nil
	}
	return []domain.Notification{r.item}, nil
}
func (r *fakeRepo) Finish(context.Context, uuid.UUID, uuid.UUID, string, string, *time.Time) error {
	r.finished++
	return nil
}

type fakeSender struct {
	calls   int
	message platformemail.Message
}

func (s *fakeSender) Send(_ context.Context, m platformemail.Message) (platformemail.Result, error) {
	s.calls++
	s.message = m
	return platformemail.Result{ProviderMessageID: "email-id"}, nil
}
func TestWorkerSendsEmailOnce(t *testing.T) {
	repo := &fakeRepo{item: domain.Notification{ID: uuid.New(), ClaimToken: uuid.New(), Email: "user@example.com", Type: "appointment_created", Template: "appointment_created"}}
	sender := &fakeSender{}
	New(repo, sender, slog.New(slog.NewTextHandler(io.Discard, nil))).Once(context.Background())
	if sender.calls != 1 || sender.message.To != "user@example.com" || repo.finished != 1 {
		t.Fatalf("calls=%d message=%+v finished=%d", sender.calls, sender.message, repo.finished)
	}
}
