package worker

import (
	"context"
	"errors"
	"fmt"
	"github.com/barber-appointment/notification-service/internal/domain"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	metrics     *metrics
}

// metrics holds this package's domain Prometheus collectors. All labels are
// bounded (notification_type is drawn from a fixed, small enum) — never a
// tenant, event, or notification ID.
type metrics struct {
	sent           *prometheus.CounterVec
	failed         *prometheus.CounterVec
	retried        *prometheus.CounterVec
	claimRecovered prometheus.Counter
}

// newMetrics registers this package's counters on reg. Callers must invoke
// this at most once per registerer (e.g. once per process, via
// Worker.WithObservability during startup) — registering the same metric
// name twice on the same registry panics.
func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		sent: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "notification_sent_total",
			Help: "Notifications successfully delivered, by notification_type.",
		}, []string{"notification_type"}),
		failed: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "notification_failed_total",
			Help: "Notifications permanently failed after exhausting retries, by notification_type.",
		}, []string{"notification_type"}),
		retried: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "notification_retried_total",
			Help: "Notifications scheduled for a retry after a temporary send failure, by notification_type.",
		}, []string{"notification_type"}),
		claimRecovered: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "notification_claim_recovered_total",
			Help: "Notification claims that recovered a row whose lease had expired (a previous worker crashed/stalled mid-processing).",
		}),
	}
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

// WithObservability attaches a Prometheus registerer for this worker's
// domain counters (sent/failed/retried/claim-recovered). Optional: an
// unconfigured Worker keeps working exactly as before, recording no domain
// metrics.
func (w Worker) WithObservability(reg prometheus.Registerer) Worker {
	w.metrics = newMetrics(reg)
	return w
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
		if item.Recovered && w.metrics != nil {
			w.metrics.claimRecovered.Inc()
		}
		sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
		result, sendErr := w.p.Send(sendCtx, platformemail.Message{To: item.Email, Subject: fmt.Sprintf("Appointment update: %s", item.Type), Text: fmt.Sprintf("There is an update to your appointment: %s.", item.Type), Template: item.Template, IdempotencyKey: item.ID.String()})
		cancel()
		if sendErr == nil {
			if err = w.r.Finish(ctx, item.ID, item.ClaimToken, result.ProviderMessageID, "", nil); err != nil {
				w.log.Warn("notification completion persistence failed", "notification_id", item.ID, "error", err)
			}
			if w.metrics != nil {
				w.metrics.sent.WithLabelValues(item.Type).Inc()
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
			if w.metrics != nil {
				w.metrics.retried.WithLabelValues(item.Type).Inc()
			}
		} else {
			_ = w.r.Finish(ctx, item.ID, item.ClaimToken, "", problem, nil)
			if w.metrics != nil {
				w.metrics.failed.WithLabelValues(item.Type).Inc()
			}
		}
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
