package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/barber-appointment/notification-service/internal/worker"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scriptedSender struct {
	errors  []error
	calls   int
	message string
}

func (s *scriptedSender) Send(context.Context, platformemail.Message) (platformemail.Result, error) {
	s.calls++
	if s.calls <= len(s.errors) && s.errors[s.calls-1] != nil {
		return platformemail.Result{}, s.errors[s.calls-1]
	}
	return platformemail.Result{ProviderMessageID: s.message}, nil
}

type timeoutSender struct{ calls int }

func (s *timeoutSender) Send(ctx context.Context, _ platformemail.Message) (platformemail.Result, error) {
	s.calls++
	<-ctx.Done()
	return platformemail.Result{}, ctx.Err()
}

func TestNotificationProviderReliabilityIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL and NOTIFICATION_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE notification_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant) }()
	repo := repository.New(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	plan := func() uuid.UUID {
		eventID := uuid.New()
		created, planErr := repo.Plan(ctx, tenant, domain.Envelope{EventID: eventID, AggregateID: uuid.New()}, "appointment_created", "recipient@example.test", "appointment_created", "en", time.Now().Add(-time.Second))
		if planErr != nil || !created {
			t.Fatalf("plan created=%v err=%v", created, planErr)
		}
		var id uuid.UUID
		if scanErr := owner.QueryRow(ctx, `SELECT id FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID).Scan(&id); scanErr != nil {
			t.Fatal(scanErr)
		}
		return id
	}
	state := func(id uuid.UUID) (string, int, string, *string) {
		var status, problem string
		var attempts int
		var messageID *string
		if scanErr := owner.QueryRow(ctx, `SELECT status,attempt_count,COALESCE(last_error,''),provider_message_id FROM public.email_notifications WHERE id=$1`, id).Scan(&status, &attempts, &problem, &messageID); scanErr != nil {
			t.Fatal(scanErr)
		}
		return status, attempts, problem, messageID
	}
	makeDue := func(id uuid.UUID) {
		if _, updateErr := owner.Exec(ctx, `UPDATE public.email_notifications SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=$1`, id); updateErr != nil {
			t.Fatal(updateErr)
		}
	}

	t.Run("temporary failures then success", func(t *testing.T) {
		id := plan()
		sender := &scriptedSender{errors: []error{
			platformemail.TemporaryError{Err: errors.New("provider detail one")},
			platformemail.TemporaryError{Err: errors.New("provider detail two")},
		}, message: "final-provider-message"}
		w := worker.New(repo, sender, logger)
		w.Once(ctx)
		makeDue(id)
		w.Once(ctx)
		makeDue(id)
		w.Once(ctx)
		status, attempts, problem, messageID := state(id)
		if sender.calls != 3 || status != "sent" || attempts != 3 || problem != "" || messageID == nil || *messageID != "final-provider-message" {
			t.Fatalf("calls=%d status=%s attempts=%d problem=%q message=%v", sender.calls, status, attempts, problem, messageID)
		}
	})

	t.Run("permanent failure is bounded and sanitized", func(t *testing.T) {
		id := plan()
		sender := &scriptedSender{errors: []error{errors.New("secret provider diagnostic")}}
		worker.New(repo, sender, logger).Once(ctx)
		status, attempts, problem, messageID := state(id)
		if sender.calls != 1 || status != "failed" || attempts != 1 || problem != "email delivery failed" || messageID != nil {
			t.Fatalf("calls=%d status=%s attempts=%d problem=%q message=%v", sender.calls, status, attempts, problem, messageID)
		}
	})

	t.Run("provider timeout releases claim for retry", func(t *testing.T) {
		id := plan()
		sender := &timeoutSender{}
		started := time.Now()
		worker.NewWithTimeout(repo, sender, logger, 25*time.Millisecond).Once(ctx)
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("provider timeout was not bounded: %s", elapsed)
		}
		status, attempts, problem, messageID := state(id)
		if sender.calls != 1 || status != "pending" || attempts != 1 || problem != "email delivery temporarily unavailable" || messageID != nil {
			t.Fatalf("calls=%d status=%s attempts=%d problem=%q message=%v", sender.calls, status, attempts, problem, messageID)
		}
		var claim *uuid.UUID
		if err = owner.QueryRow(ctx, `SELECT claim_token FROM public.email_notifications WHERE id=$1`, id).Scan(&claim); err != nil || claim != nil {
			t.Fatalf("timeout left claim=%v err=%v", claim, err)
		}
	})
}
