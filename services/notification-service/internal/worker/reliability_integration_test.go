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
	"github.com/prometheus/client_golang/prometheus"
)

// counterValue reads the current total value of a counter (summed across all
// its label combinations) off reg via a scrape (Gather).
func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		var total float64
		for _, m := range family.GetMetric() {
			total += m.GetCounter().GetValue()
		}
		return total
	}
	return 0
}

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

// tenantScopedRepo adapts repository.Repository to worker's
// notificationRepository interface via ClaimForTenant instead of the global,
// cross-tenant Claim the production worker actually calls. This keeps these
// scripted-sender assertions (which depend on exact call counts/order)
// deterministic when other integration test packages are concurrently
// planning/claiming their own, unrelated rows against this same shared
// database - without serializing the test binary. worker.go and its
// production Claim path are unchanged; only this test's own repository
// access is scoped.
type tenantScopedRepo struct {
	repo   repository.Repository
	tenant uuid.UUID
}

func (r tenantScopedRepo) Claim(ctx context.Context, limit int) ([]domain.Notification, error) {
	return r.repo.ClaimForTenant(ctx, r.tenant, limit)
}
func (r tenantScopedRepo) Finish(ctx context.Context, id, claim uuid.UUID, messageID, problem string, retryAt *time.Time) error {
	return r.repo.Finish(ctx, id, claim, messageID, problem, retryAt)
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
		registry := prometheus.NewRegistry()
		w := worker.New(tenantScopedRepo{repo, tenant}, sender, logger).WithObservability(registry)
		w.Once(ctx)
		makeDue(id)
		w.Once(ctx)
		makeDue(id)
		w.Once(ctx)
		status, attempts, problem, messageID := state(id)
		if sender.calls != 3 || status != "sent" || attempts != 3 || problem != "" || messageID == nil || *messageID != "final-provider-message" {
			t.Fatalf("calls=%d status=%s attempts=%d problem=%q message=%v", sender.calls, status, attempts, problem, messageID)
		}
		// Two temporary failures scheduled a retry each; the final attempt sent.
		if got := counterValue(t, registry, "notification_retried_total"); got != 2 {
			t.Fatalf("notification_retried_total = %v, want 2", got)
		}
		if got := counterValue(t, registry, "notification_sent_total"); got != 1 {
			t.Fatalf("notification_sent_total = %v, want 1", got)
		}
		if got := counterValue(t, registry, "notification_failed_total"); got != 0 {
			t.Fatalf("notification_failed_total = %v, want 0", got)
		}
	})

	t.Run("permanent failure is bounded and sanitized", func(t *testing.T) {
		id := plan()
		sender := &scriptedSender{errors: []error{errors.New("secret provider diagnostic")}}
		registry := prometheus.NewRegistry()
		worker.New(tenantScopedRepo{repo, tenant}, sender, logger).WithObservability(registry).Once(ctx)
		status, attempts, problem, messageID := state(id)
		if sender.calls != 1 || status != "failed" || attempts != 1 || problem != "email delivery failed" || messageID != nil {
			t.Fatalf("calls=%d status=%s attempts=%d problem=%q message=%v", sender.calls, status, attempts, problem, messageID)
		}
		if got := counterValue(t, registry, "notification_failed_total"); got != 1 {
			t.Fatalf("notification_failed_total = %v, want 1", got)
		}
		if got := counterValue(t, registry, "notification_sent_total"); got != 0 {
			t.Fatalf("notification_sent_total = %v, want 0", got)
		}
	})

	t.Run("provider timeout releases claim for retry", func(t *testing.T) {
		id := plan()
		sender := &timeoutSender{}
		started := time.Now()
		worker.NewWithTimeout(tenantScopedRepo{repo, tenant}, sender, logger, 25*time.Millisecond).Once(ctx)
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
