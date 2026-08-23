package outbox

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/infra"
	"github.com/google/uuid"
	server "github.com/nats-io/nats-server/v2/server"
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
func (f *fakeOutbox) ReleaseOutbox(context.Context, uuid.UUID) error { return nil }
func (f *fakeOutbox) CleanupRetention(context.Context, int) (int, int, error) {
	return 0, 0, nil
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
	publisher, err := New(repo, nc, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
