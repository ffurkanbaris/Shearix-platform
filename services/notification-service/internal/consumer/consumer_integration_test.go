package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/notification-service/internal/consumer"
	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type failingRecipientResolver struct{}

func (failingRecipientResolver) Email(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", errors.New("appointment service unavailable")
}

type testRecipientResolver struct{}

func (testRecipientResolver) Email(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "customer@example.com", nil
}

type tenantOffsets struct {
	mu     sync.RWMutex
	values map[uuid.UUID][]time.Duration
}

func (o *tenantOffsets) ReminderOffsets(_ context.Context, tenant uuid.UUID) ([]time.Duration, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return append([]time.Duration(nil), o.values[tenant]...), nil
}

func (o *tenantOffsets) Set(tenant uuid.UUID, offsets []time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.values[tenant] = append([]time.Duration(nil), offsets...)
}

// TestJetStreamPlanningIntegration uses a real JetStream server and PostgreSQL.
// It intentionally uses an isolated stream/durable so it cannot interfere with
// the running appointment/notification consumer.
func TestJetStreamPlanningIntegration(t *testing.T) {
	appDSN, ownerDSN, natsURL := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_NATS_URL")
	if appDSN == "" || ownerDSN == "" || natsURL == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL, NOTIFICATION_TEST_OWNER_DATABASE_URL, and NOTIFICATION_TEST_NATS_URL are required")
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
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Drain()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	stream, durable := "NOTIFY_TEST_"+uuid.NewString()[:8], "notify-test-"+uuid.NewString()
	subject := "notify.test." + uuid.NewString() + ".>"
	if _, err = js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = js.DeleteStream(stream) }()

	tenant, appointment, eventID := uuid.New(), uuid.New(), uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant) }()
	r := repository.New(pool)
	settings := &tenantOffsets{values: map[uuid.UUID][]time.Duration{tenant: []time.Duration{3 * time.Hour}}}

	spanRecorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(spanRecorder))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("consumer-test")
	registry := prometheus.NewRegistry()

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		consumer.NewWithConfigAndReminderOffsets(js, r, slog.New(slog.NewTextHandler(io.Discard, nil)), stream, subject, durable, settings, testRecipientResolver{}).WithObservability(tracer, registry).Run(runCtx)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
			t.Error("consumer did not stop after fetch completed")
		}
	}()

	payload, _ := json.Marshal(map[string]time.Time{"start_at": time.Now().UTC().Add(12 * time.Hour)})
	event := domain.Envelope{EventID: eventID, EventType: "appointment.created", EventVersion: 1, TenantID: tenant, AggregateID: appointment, OccurredAt: time.Now().UTC(), Payload: payload}
	body, _ := json.Marshal(event)
	if _, err = js.Publish(subject[:len(subject)-1]+"created", body); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 7*time.Second, func() bool {
		var n int
		return owner.QueryRow(ctx, `SELECT count(*) FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2 AND notification_type='appointment_created'`, tenant, eventID).Scan(&n) == nil && n == 1
	})
	// The "appointment_created" and "appointment_reminder" rows are planned
	// by two separate inserts within the same handle() call, not one atomic
	// statement, so the reminder row can become visible a moment after the
	// created row above does - this is always true, but only actually
	// observable under real database load (e.g. another integration test
	// package concurrently exercising claim/finish against this same shared
	// database). Poll instead of asserting on a single point-in-time read.
	var reminders, pastReminders int
	var scheduledAt time.Time
	waitFor(t, 7*time.Second, func() bool {
		return owner.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE scheduled_at<=clock_timestamp()),max(scheduled_at) FROM public.email_notifications WHERE tenant_id=$1 AND appointment_id=$2 AND notification_type='appointment_reminder'`, tenant, appointment).Scan(&reminders, &pastReminders, &scheduledAt) == nil && reminders == 1
	})
	if pastReminders != 0 {
		t.Fatalf("reminder scheduling reminders=%d past=%d", reminders, pastReminders)
	}
	if want := payloadStart(payload).Add(-3 * time.Hour); scheduledAt.Sub(want) < -time.Second || scheduledAt.Sub(want) > time.Second {
		t.Fatalf("tenant reminder offset was not used: scheduled=%s want=%s", scheduledAt, want)
	}
	waitFor(t, 7*time.Second, func() bool {
		info, e := js.ConsumerInfo(stream, durable)
		return e == nil && info.NumAckPending == 0 && info.AckFloor.Consumer >= 1
	})

	// Replaying the same durable event has a different JetStream sequence but
	// must remain one logical notification due to the database constraint.
	if _, err = js.Publish(subject[:len(subject)-1]+"created", body); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 7*time.Second, func() bool {
		info, e := js.ConsumerInfo(stream, durable)
		return e == nil && info.AckFloor.Consumer >= 2
	})
	var count int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate planning count=%d err=%v", count, err)
	}
	// The dedup replay above (ON CONFLICT DO NOTHING) must not double-count
	// notification_planned_total: the first "appointment_created" delivery
	// created a row (created=true), the replay did not (created=false).
	if got := counterValueForLabel(t, registry, "notification_planned_total", "notification_type", "appointment_created"); got != 1 {
		t.Fatalf("notification_planned_total{appointment_created} = %v, want 1 (dedup replay must not increment it)", got)
	}

	// Settings are read when each new appointment is planned rather than at
	// consumer startup. Updating the tenant offset changes only future reminder
	// rows and leaves existing delivery plans untouched.
	settings.Set(tenant, []time.Duration{90 * time.Minute})
	secondAppointment, secondEvent := uuid.New(), uuid.New()
	secondStart := time.Now().UTC().Add(10 * time.Hour).Truncate(time.Second)
	secondPayload, _ := json.Marshal(map[string]time.Time{"start_at": secondStart})
	second := domain.Envelope{EventID: secondEvent, EventType: "appointment.created", EventVersion: 1, TenantID: tenant, AggregateID: secondAppointment, OccurredAt: time.Now().UTC(), Payload: secondPayload}
	secondBody, _ := json.Marshal(second)
	if _, err = js.Publish(subject[:len(subject)-1]+"created", secondBody); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 7*time.Second, func() bool {
		var when time.Time
		err := owner.QueryRow(ctx, `SELECT scheduled_at FROM public.email_notifications WHERE tenant_id=$1 AND appointment_id=$2 AND notification_type='appointment_reminder'`, tenant, secondAppointment).Scan(&when)
		if err != nil {
			return false
		}
		return when.Sub(secondStart.Add(-90*time.Minute)) >= -time.Second && when.Sub(secondStart.Add(-90*time.Minute)) <= time.Second
	})

	// A persistence failure must leave the message unacknowledged. This used
	// to REVOKE INSERT on the table from notification_db_app - but that is a
	// role-wide privilege, not scoped to this test's own connection or rows,
	// so it also broke every *other* concurrently-running integration test
	// package's INSERTs (via Plan) for the several real seconds the revoke
	// was in effect, once these tests stopped running serialized (-p 1). A
	// trigger that only rejects rows for this test's own tenant reproduces
	// the same "persistence failure" behavior at the consumer/Nak layer
	// without affecting any other tenant's concurrent inserts.
	if _, err = owner.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.__test_reject_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.tenant_id='%s' THEN RAISE EXCEPTION 'simulated persistence failure'; END IF; RETURN NEW; END $$`, tenant)); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `CREATE TRIGGER __test_reject_tenant_trigger BEFORE INSERT ON public.email_notifications FOR EACH ROW EXECUTE FUNCTION public.__test_reject_tenant()`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = owner.Exec(ctx, `DROP TRIGGER IF EXISTS __test_reject_tenant_trigger ON public.email_notifications`)
	}()
	beforeFailure, err := js.ConsumerInfo(stream, durable)
	if err != nil {
		t.Fatal(err)
	}
	failEvent := event
	failEvent.EventID = uuid.New()
	failBody, _ := json.Marshal(failEvent)
	if _, err = js.Publish(subject[:len(subject)-1]+"failed", failBody); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 7*time.Second, func() bool {
		info, e := js.ConsumerInfo(stream, durable)
		return e == nil && info.Delivered.Consumer > beforeFailure.Delivered.Consumer && info.AckFloor.Consumer == beforeFailure.AckFloor.Consumer
	})

	// The unacknowledged message above is NakWithDelay'd, so JetStream
	// redelivers it (NumDelivered > 1 on the second handling attempt). That
	// second delivery must be observable both as a Prometheus counter and as
	// a recorded consumer span.
	waitFor(t, 7*time.Second, func() bool {
		info, e := js.ConsumerInfo(stream, durable)
		return e == nil && info.Delivered.Consumer >= beforeFailure.Delivered.Consumer+2
	})
	waitFor(t, 7*time.Second, func() bool {
		return counterValue(t, registry, "nats_messages_redelivered_total") >= 1
	})
	consumerSpans := 0
	for _, span := range spanRecorder.Ended() {
		if span.Name() == "notification.consume" {
			consumerSpans++
		}
	}
	if consumerSpans == 0 {
		t.Fatal("expected at least one recorded notification.consume consumer span")
	}
}

// counterValue reads the current total value of a counter (summed across all
// its label combinations) off reg via a scrape (Gather), for assertions
// against a real registry rather than a reference to the internal collector.
func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	return counterValueForLabel(t, reg, name, "", "")
}

// counterValueForLabel reads a single label combination's value from a
// CounterVec metric. Pass labelName == "" to sum across all label values
// (equivalent to counterValue).
func counterValueForLabel(t *testing.T, reg *prometheus.Registry, name, labelName, labelValue string) float64 {
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
			if labelName != "" {
				matched := false
				for _, lp := range m.GetLabel() {
					if lp.GetName() == labelName && lp.GetValue() == labelValue {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			total += m.GetCounter().GetValue()
		}
		return total
	}
	return 0
}

func TestJetStreamConsumerRestartAndRecipientRecoveryIntegration(t *testing.T) {
	appDSN, ownerDSN, natsURL := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_NATS_URL")
	if appDSN == "" || ownerDSN == "" || natsURL == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL, NOTIFICATION_TEST_OWNER_DATABASE_URL, and NOTIFICATION_TEST_NATS_URL are required")
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
	control, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Drain()
	js, err := control.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	stream, durable := "NOTIFY_RESTART_"+uuid.NewString()[:8], "notify-restart-"+uuid.NewString()
	subject := "notify.restart." + uuid.NewString() + ".>"
	if _, err = js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = js.DeleteStream(stream) }()
	tenant, appointment, eventID := uuid.New(), uuid.New(), uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant) }()
	repo := repository.New(pool)
	settings := &tenantOffsets{values: map[uuid.UUID][]time.Duration{tenant: nil}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	startConsumer := func(resolver consumer.RecipientResolver) (context.CancelFunc, <-chan struct{}, *nats.Conn) {
		nc, connectErr := nats.Connect(natsURL)
		if connectErr != nil {
			t.Fatal(connectErr)
		}
		workerJS, jsErr := nc.JetStream()
		if jsErr != nil {
			t.Fatal(jsErr)
		}
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			consumer.NewWithConfigAndReminderOffsets(workerJS, repo, logger, stream, subject, durable, settings, resolver).Run(runCtx)
		}()
		return cancel, done, nc
	}

	cancelFirst, firstDone, firstNC := startConsumer(failingRecipientResolver{})
	event := domain.Envelope{EventID: eventID, EventType: "appointment.created", EventVersion: 1, TenantID: tenant, AggregateID: appointment, OccurredAt: time.Now().UTC()}
	body, _ := json.Marshal(event)
	if _, err = js.Publish(strings.TrimSuffix(subject, ">")+"created", body); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 7*time.Second, func() bool {
		info, infoErr := js.ConsumerInfo(stream, durable)
		return infoErr == nil && info.Delivered.Consumer >= 1 && info.AckFloor.Consumer == 0
	})
	var planned int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID).Scan(&planned); err != nil || planned != 0 {
		t.Fatalf("recipient failure incorrectly persisted/acked work count=%d err=%v", planned, err)
	}
	cancelFirst()
	select {
	case <-firstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("first consumer did not stop")
	}
	_ = firstNC.Drain()

	cancelSecond, secondDone, secondNC := startConsumer(testRecipientResolver{})
	defer func() {
		cancelSecond()
		select {
		case <-secondDone:
		case <-time.After(3 * time.Second):
			t.Error("restarted consumer did not stop")
		}
		_ = secondNC.Drain()
	}()
	waitFor(t, 7*time.Second, func() bool {
		var count int
		return owner.QueryRow(ctx, `SELECT count(*) FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID).Scan(&count) == nil && count == 1
	})
	waitFor(t, 7*time.Second, func() bool {
		info, infoErr := js.ConsumerInfo(stream, durable)
		return infoErr == nil && info.NumAckPending == 0 && info.AckFloor.Consumer >= 1
	})
}

func payloadStart(payload []byte) time.Time {
	var value struct {
		StartAt time.Time `json:"start_at"`
	}
	_ = json.Unmarshal(payload, &value)
	return value.StartAt
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for integration condition")
}
