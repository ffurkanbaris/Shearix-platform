package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type Publisher struct {
	repo        outboxRepository
	js          nats.JetStreamContext
	nc          *nats.Conn
	log         *slog.Logger
	stop        context.CancelFunc
	done        chan struct{}
	once        sync.Once
	startOnce   sync.Once
	ready       atomic.Bool
	lastCleanup time.Time
}

type outboxRepository interface {
	ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error)
	MarkOutboxPublished(context.Context, uuid.UUID) error
	ReleaseOutbox(context.Context, uuid.UUID) error
	CleanupRetention(context.Context, int) (int, int, error)
}

func New(repo outboxRepository, connection *nats.Conn, logger *slog.Logger) (*Publisher, error) {
	js, err := connection.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		return nil, err
	}
	return &Publisher{repo: repo, js: js, nc: connection, log: logger, done: make(chan struct{})}, nil
}

func (p *Publisher) Start(parent context.Context) {
	p.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		p.stop = cancel
		go func() {
			defer close(p.done)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if p.ensureReady(ctx) {
					p.publishBatch(ctx)
					p.cleanupRetention(ctx)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (p *Publisher) cleanupRetention(ctx context.Context) {
	if !p.lastCleanup.IsZero() && time.Since(p.lastCleanup) < time.Hour {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := p.repo.CleanupRetention(cleanupCtx, 500); err != nil {
		p.log.Warn("retention cleanup failed", "error", err)
		return
	}
	p.lastCleanup = time.Now()
}

func (p *Publisher) ensureReady(ctx context.Context) bool {
	if ctx.Err() != nil || !p.nc.IsConnected() {
		p.ready.Store(false)
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := p.js.StreamInfo("APPOINTMENTS", nats.Context(probeCtx)); err != nil {
		if _, err = p.js.AddStream(&nats.StreamConfig{Name: "APPOINTMENTS", Subjects: []string{"appointments.>"}, Retention: nats.LimitsPolicy}, nats.Context(probeCtx)); err != nil && !strings.Contains(err.Error(), "stream name already in use") {
			p.ready.Store(false)
			return false
		}
	}
	p.ready.Store(true)
	return true
}

func (p *Publisher) Ready() bool { return p != nil && p.ready.Load() && p.nc.IsConnected() }

func (p *Publisher) publishBatch(ctx context.Context) {
	events, err := p.repo.ClaimOutbox(ctx, 50)
	if err != nil {
		p.log.Warn("outbox claim failed", "error", err)
		return
	}
	for _, event := range events {
		envelope, err := json.Marshal(struct {
			EventID      string          `json:"event_id"`
			EventType    string          `json:"event_type"`
			EventVersion int             `json:"event_version"`
			TenantID     string          `json:"tenant_id"`
			AggregateID  string          `json:"aggregate_id"`
			OccurredAt   time.Time       `json:"occurred_at"`
			Payload      json.RawMessage `json:"payload"`
		}{event.ID.String(), event.Type, 1, event.TenantID.String(), event.AggregateID.String(), event.OccurredAt.UTC(), event.Payload})
		if err == nil {
			// MsgId enables JetStream's server-side publish dedup window so a
			// double-publish of the same logical event (e.g. publish succeeds
			// but MarkOutboxPublished fails, and the row is reclaimed and
			// republished next cycle) is absorbed by the broker instead of
			// producing two distinct stream messages. The outbox event's own
			// durable UUID is a stable, per-event identity for this purpose.
			_, err = p.js.Publish("appointments.v1."+event.Type, envelope, nats.Context(ctx), nats.MsgId(event.ID.String()))
		}
		if err != nil {
			p.ready.Store(false)
			p.log.Warn("outbox publish failed", "event_id", event.ID, "event_type", event.Type, "error", err)
			_ = p.repo.ReleaseOutbox(ctx, event.ID)
			continue
		}
		if err = p.repo.MarkOutboxPublished(ctx, event.ID); err != nil {
			p.log.Warn("outbox acknowledgement persistence failed", "event_id", event.ID, "error", err)
		}
	}
}

func (p *Publisher) Close() {
	p.once.Do(func() {
		p.ready.Store(false)
		if p.stop != nil {
			p.stop()
			<-p.done
		}
	})
}
