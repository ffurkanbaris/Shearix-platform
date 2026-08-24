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
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/trace"
)

type Publisher struct {
	repo        outboxRepository
	js          nats.JetStreamContext
	nc          *nats.Conn
	log         *slog.Logger
	tracer      trace.Tracer
	stop        context.CancelFunc
	done        chan struct{}
	once        sync.Once
	startOnce   sync.Once
	ready       atomic.Bool
	lastCleanup time.Time

	publishFailures *prometheus.CounterVec
	publishRetries  *prometheus.CounterVec
	terminalTotal   *prometheus.CounterVec
}

type outboxRepository interface {
	ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error)
	MarkOutboxPublished(context.Context, uuid.UUID) error
	ReleaseOutbox(context.Context, uuid.UUID) (attempts int, terminal bool, err error)
	CleanupRetention(context.Context, int) (int, int, error)
	OutboxBacklogStats(context.Context) (count int, oldestPendingAge time.Duration, err error)
}

// New constructs a Publisher. registerer is optional (a nil registerer skips
// metrics registration, e.g. in unit tests that construct a Publisher
// directly) and, when supplied, must be a private registry unique to this
// service — see platform/obsmetrics.Registry.Registerer(). All metrics
// registered here carry only bounded labels (service-scoped counters/gauges
// with no tenant, event, or appointment ID).
func New(repo outboxRepository, connection *nats.Conn, logger *slog.Logger, tracer trace.Tracer, registerer prometheus.Registerer) (*Publisher, error) {
	js, err := connection.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		return nil, err
	}
	if tracer == nil {
		tracer = trace.NewNoopTracerProvider().Tracer("outbox")
	}
	p := &Publisher{repo: repo, js: js, nc: connection, log: logger, tracer: tracer, done: make(chan struct{})}
	p.registerMetrics(registerer)
	return p, nil
}

func (p *Publisher) registerMetrics(registerer prometheus.Registerer) {
	if registerer == nil {
		return
	}
	p.publishFailures = promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_publish_failures_total",
		Help: "Total outbox publish attempts that failed (JetStream publish error).",
	}, []string{})
	p.publishRetries = promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_publish_retries_total",
		Help: "Total failed outbox publish attempts that were released for another retry (not yet terminal).",
	}, []string{})
	p.terminalTotal = promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_terminal_total",
		Help: "Total outbox rows that crossed the bounded-retry threshold and became terminal (failed_at set).",
	}, []string{})
	promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "outbox_backlog",
		Help: "Count of unpublished, non-terminal outbox rows.",
	}, func() float64 {
		count, _, err := p.repo.OutboxBacklogStats(context.Background())
		if err != nil {
			return 0
		}
		return float64(count)
	})
	promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "outbox_oldest_pending_age_seconds",
		Help: "Age in seconds of the oldest unpublished, non-terminal outbox row (0 when the backlog is empty).",
	}, func() float64 {
		_, age, err := p.repo.OutboxBacklogStats(context.Background())
		if err != nil {
			return 0
		}
		return age.Seconds()
	})
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
		p.publishOne(ctx, event)
	}
}

func (p *Publisher) publishOne(ctx context.Context, event domain.OutboxEvent) {
	ctx, span := p.tracer.Start(ctx, "outbox.publish", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

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
		// PublishMsg (rather than Publish) is used so the W3C traceparent
		// can be injected as a message header, propagating this producer
		// span to the eventual consumer.
		msg := &nats.Msg{Subject: "appointments.v1." + event.Type, Data: envelope, Header: nats.Header{}}
		otelsetup.InjectNATSHeaders(ctx, msg)
		msg.Header.Set("Nats-Msg-Id", event.ID.String())
		_, err = p.js.PublishMsg(msg, nats.Context(ctx))
	}
	if err != nil {
		p.ready.Store(false)
		p.log.Warn("outbox publish failed", "event_id", event.ID, "event_type", event.Type, "error", err)
		p.incCounter(p.publishFailures)
		attempts, terminal, releaseErr := p.repo.ReleaseOutbox(ctx, event.ID)
		if releaseErr != nil {
			p.log.Warn("outbox release failed", "event_id", event.ID, "error", releaseErr)
			return
		}
		if terminal {
			p.log.Warn("outbox event reached terminal bounded-retry state", "event_id", event.ID, "event_type", event.Type, "attempts", attempts)
			p.incCounter(p.terminalTotal)
		} else {
			p.incCounter(p.publishRetries)
		}
		return
	}
	if err = p.repo.MarkOutboxPublished(ctx, event.ID); err != nil {
		p.log.Warn("outbox acknowledgement persistence failed", "event_id", event.ID, "error", err)
	}
}

func (p *Publisher) incCounter(c *prometheus.CounterVec) {
	if c == nil {
		return
	}
	c.WithLabelValues().Inc()
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
