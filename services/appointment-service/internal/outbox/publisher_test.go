package outbox

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/appointment-service/internal/repository"
	"github.com/barber-appointment/platform/infra"
	"github.com/google/uuid"
	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStartIsIdempotentAndShutdownIsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &Publisher{done: make(chan struct{})}
	p.Start(ctx)
	p.Start(ctx)
	p.Close()
}

type fakeOutbox struct {
	mu        sync.Mutex
	events    []domain.OutboxEvent
	published int
}

func (f *fakeOutbox) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.OutboxEvent(nil), f.events...), nil
}
func (f *fakeOutbox) MarkOutboxPublished(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.events {
		if f.events[i].ID == id {
			f.events = append(f.events[:i], f.events[i+1:]...)
			f.published++
			break
		}
	}
	return nil
}
func (f *fakeOutbox) ReleaseOutbox(context.Context, uuid.UUID) (int, bool, error) {
	return 0, false, nil
}
func (f *fakeOutbox) CleanupRetention(context.Context, int) (int, int, error) {
	return 0, 0, nil
}
func (f *fakeOutbox) OutboxBacklogStats(context.Context) (int, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events), 0, nil
}
func (f *fakeOutbox) add() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, domain.OutboxEvent{ID: uuid.New(), TenantID: uuid.New(), AggregateID: uuid.New(), Type: "appointment.created", Payload: []byte(`{}`), OccurredAt: time.Now()})
}
func (f *fakeOutbox) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.published }

func TestPublisherRecoversAcrossNATSAvailability(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	url := "nats://127.0.0.1:" + strconv.Itoa(port)
	nc, err := infra.ConnectNATS(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	repo := &fakeOutbox{}
	repo.add()
	publisher, err := New(repo, nc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher.Start(ctx)
	publisher.Start(ctx)
	if publisher.Ready() {
		t.Fatal("publisher ready before NATS starts")
	}

	ns := runServer(t, port)
	waitFor(t, 8*time.Second, func() bool { return publisher.Ready() && repo.count() == 1 })
	ns.Shutdown()
	ns.WaitForShutdown()
	waitFor(t, 8*time.Second, func() bool { return !publisher.Ready() })
	repo.add()
	ns = runServer(t, port)
	defer ns.Shutdown()
	waitFor(t, 8*time.Second, func() bool { return publisher.Ready() && repo.count() == 2 })
	publisher.Close()
}

// staticOutbox always re-offers the same single event from ClaimOutbox and
// never removes it, regardless of MarkOutboxPublished outcome. This models
// the ambiguous "publish succeeded but the DB ack/mark-published step
// failed" window: the outbox row gets reclaimed and republished on the next
// cycle even though a JetStream message for it already exists.
type staticOutbox struct {
	event domain.OutboxEvent
	marks int32
}

func (s *staticOutbox) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	return []domain.OutboxEvent{s.event}, nil
}
func (s *staticOutbox) MarkOutboxPublished(context.Context, uuid.UUID) error {
	s.marks++
	return nil
}
func (s *staticOutbox) ReleaseOutbox(context.Context, uuid.UUID) (int, bool, error) {
	return 0, false, nil
}
func (s *staticOutbox) CleanupRetention(context.Context, int) (int, int, error) {
	return 0, 0, nil
}
func (s *staticOutbox) OutboxBacklogStats(context.Context) (int, time.Duration, error) {
	return 0, 0, nil
}

// TestPublishDeduplicatesRepublishedEvent exercises the same-event-published-
// twice ambiguity directly: it forces two publishBatch cycles over the exact
// same outbox event (as would happen if MarkOutboxPublished failed after a
// successful publish) and asserts JetStream's server-side MsgId dedup window
// collapses them into a single stream message rather than two.
func TestPublishDeduplicatesRepublishedEvent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	url := "nats://127.0.0.1:" + strconv.Itoa(port)
	ns := runServer(t, port)
	defer ns.Shutdown()

	nc, err := infra.ConnectNATS(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	repo := &staticOutbox{event: domain.OutboxEvent{
		ID: uuid.New(), TenantID: uuid.New(), AggregateID: uuid.New(),
		Type: "appointment.created", Payload: []byte(`{}`), OccurredAt: time.Now(),
	}}
	publisher, err := New(repo, nc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !publisher.ensureReady(ctx) {
		t.Fatal("publisher did not become ready against a running NATS server")
	}

	publisher.publishBatch(ctx)
	publisher.publishBatch(ctx)

	if repo.marks != 2 {
		t.Fatalf("expected both publish attempts to be reported successful, marks=%d", repo.marks)
	}
	info, err := publisher.js.StreamInfo("APPOINTMENTS", nats.Context(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("expected JetStream dedup to collapse the republished event into 1 message, got %d", info.State.Msgs)
	}
}

// TestPublishPropagatesTraceparentAndMsgId asserts that publishing an event
// with an active span in ctx produces a JetStream message carrying a
// traceparent header (via otelsetup.InjectNATSHeaders/PublishMsg) and the
// expected Nats-Msg-Id header (set directly on msg.Header rather than via
// the nats.MsgId PubOpt).
func TestPublishPropagatesTraceparentAndMsgId(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	url := "nats://127.0.0.1:" + strconv.Itoa(port)
	ns := runServer(t, port)
	defer ns.Shutdown()

	nc, err := infra.ConnectNATS(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	event := domain.OutboxEvent{
		ID: uuid.New(), TenantID: uuid.New(), AggregateID: uuid.New(),
		Type: "appointment.created", Payload: []byte(`{}`), OccurredAt: time.Now(),
	}
	repo := &staticOutbox{event: event}

	tp := sdktrace.NewTracerProvider()
	defer tp.Shutdown(context.Background())
	tracer := tp.Tracer("test")

	publisher, err := New(repo, nc, slog.New(slog.NewTextHandler(io.Discard, nil)), tracer, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !publisher.ensureReady(ctx) {
		t.Fatal("publisher did not become ready against a running NATS server")
	}

	// publishBatch/publishOne starts its own producer span internally from
	// the publisher's tracer, so any context is sufficient here — the
	// resulting message header is asserted below.
	publisher.publishBatch(ctx)

	sub, err := publisher.js.PullSubscribe("appointments.v1.appointment.created", "test-consumer", nats.BindStream("APPOINTMENTS"))
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0]
	if msg.Header.Get("traceparent") == "" {
		t.Fatal("expected traceparent header to be set on the published message")
	}
	if got := msg.Header.Get("Nats-Msg-Id"); got != event.ID.String() {
		t.Fatalf("expected Nats-Msg-Id header %q, got %q", event.ID.String(), got)
	}
}

// TestOutboxTerminalAndRetryCounters drives the real publishOne failure path
// (JetStream publish forced to fail by closing the connection) against a
// repo stub that reproduces release_outbox_event's own bounded-retry
// bookkeeping (terminal once publish_attempts reaches 20), and asserts
// outbox_publish_retries_total increments on every non-terminal failure
// while outbox_terminal_total increments exactly once, on the attempt that
// crosses the threshold.
func TestOutboxTerminalAndRetryCounters(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	url := "nats://127.0.0.1:" + strconv.Itoa(port)
	ns := runServer(t, port)
	defer ns.Shutdown()

	nc, err := infra.ConnectNATS(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	event := domain.OutboxEvent{ID: uuid.New(), TenantID: uuid.New(), AggregateID: uuid.New(), Type: "appointment.created", Payload: []byte(`{}`), OccurredAt: time.Now()}
	repo := &countingOutbox{event: event}

	registerer := prometheus.NewRegistry()
	publisher, err := New(repo, nc, slog.New(slog.NewTextHandler(io.Discard, nil)), trace.NewNoopTracerProvider().Tracer("test"), registerer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !publisher.ensureReady(ctx) {
		t.Fatal("publisher did not become ready against a running NATS server")
	}

	// Close the client connection (rather than merely shutting the broker
	// down) so every subsequent PublishMsg fails fast with "connection
	// closed" instead of blocking on nats.Context(ctx) with a background
	// (deadline-less) context while the client silently retries to
	// reconnect, forcing publishOne down the ReleaseOutbox/failure branch
	// every time.
	nc.Close()

	for i := 0; i < 25; i++ {
		publisher.publishBatch(ctx)
	}

	if repo.terminalAt != 20 {
		t.Fatalf("expected terminal at attempt 20, got %d", repo.terminalAt)
	}
	if got := testCounterTotal(t, registerer, "outbox_terminal_total"); got != 1 {
		t.Fatalf("expected outbox_terminal_total to increment exactly once, got %v", got)
	}
	if got := testCounterTotal(t, registerer, "outbox_publish_retries_total"); got != 19 {
		t.Fatalf("expected outbox_publish_retries_total to increment 19 times (attempts 1-19), got %v", got)
	}
	if got := testCounterTotal(t, registerer, "outbox_publish_failures_total"); got != 20 {
		t.Fatalf("expected outbox_publish_failures_total to increment once per failed attempt (20, since the row is never reclaimed once terminal), got %v", got)
	}
}

// countingOutbox always re-offers the same event (regardless of terminal
// state — a real repo would stop via failed_at IS NULL in
// claim_outbox_events, but this stub only needs to drive publishOne's
// failure branch repeatedly) and reproduces release_outbox_event's
// bounded-retry bookkeeping in Go.
type countingOutbox struct {
	mu         sync.Mutex
	event      domain.OutboxEvent
	attempts   int
	terminalAt int
}

func (c *countingOutbox) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Mirror claim_outbox_events' own WHERE failed_at IS NULL filter: once
	// this row has gone terminal it is never reclaimed/republished again.
	if c.terminalAt != 0 {
		return nil, nil
	}
	return []domain.OutboxEvent{c.event}, nil
}
func (c *countingOutbox) MarkOutboxPublished(context.Context, uuid.UUID) error { return nil }
func (c *countingOutbox) ReleaseOutbox(context.Context, uuid.UUID) (int, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts++
	terminal := c.attempts >= 20
	if terminal && c.terminalAt == 0 {
		c.terminalAt = c.attempts
	}
	return c.attempts, terminal, nil
}
func (c *countingOutbox) CleanupRetention(context.Context, int) (int, int, error) {
	return 0, 0, nil
}
func (c *countingOutbox) OutboxBacklogStats(context.Context) (int, time.Duration, error) {
	return 0, 0, nil
}

func testCounterTotal(t *testing.T, gatherer prometheus.Gatherer, name string) float64 {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		var total float64
		for _, metric := range family.GetMetric() {
			total += metric.GetCounter().GetValue()
		}
		return total
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

// TestOutboxBacklogStatsIntegration seeds a couple of outbox rows with known
// created_at values against a real Postgres and asserts the backlog count
// and oldest-pending age come back correctly. Skipped unless
// APPOINTMENT_TEST_DATABASE_URL / APPOINTMENT_TEST_OWNER_DATABASE_URL are
// set, matching the convention in internal/repository's integration tests.
func TestOutboxBacklogStatsIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("APPOINTMENT_TEST_DATABASE_URL"), os.Getenv("APPOINTMENT_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("APPOINTMENT_TEST_DATABASE_URL and APPOINTMENT_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE appointment_db_owner"); err != nil {
		t.Fatal(err)
	}

	tenant := uuid.New()
	older := uuid.New()
	newer := uuid.New()
	terminal := uuid.New()
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.outbox_events WHERE id = ANY($1)`, []uuid.UUID{older, newer, terminal})
	}()

	oldCreatedAt := time.Now().Add(-90 * time.Second).UTC()
	newCreatedAt := time.Now().Add(-5 * time.Second).UTC()
	if _, err = owner.Exec(ctx, `INSERT INTO public.outbox_events (id, tenant_id, aggregate_id, type, payload, created_at) VALUES ($1,$2,$2,'appointment.created','{}',$3)`, older, tenant, oldCreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `INSERT INTO public.outbox_events (id, tenant_id, aggregate_id, type, payload, created_at) VALUES ($1,$2,$2,'appointment.created','{}',$3)`, newer, tenant, newCreatedAt); err != nil {
		t.Fatal(err)
	}
	// A terminal row must not count toward the backlog.
	if _, err = owner.Exec(ctx, `INSERT INTO public.outbox_events (id, tenant_id, aggregate_id, type, payload, created_at, failed_at) VALUES ($1,$2,$2,'appointment.created','{}',$3,now())`, terminal, tenant, oldCreatedAt); err != nil {
		t.Fatal(err)
	}

	repo := repository.New(pool)
	count, age, err := repo.OutboxBacklogStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("expected at least the 2 seeded pending rows in the backlog, got %d", count)
	}
	if age < 80*time.Second {
		t.Fatalf("expected oldest-pending age to reflect the older seeded row (>=80s), got %v", age)
	}
}

func runServer(t *testing.T, port int) *server.Server {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server did not start")
	}
	return ns
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
	t.Fatal("timed out waiting for condition")
}
