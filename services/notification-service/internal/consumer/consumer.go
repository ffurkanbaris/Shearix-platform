package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Consumer struct {
	js         nats.JetStreamContext
	r          repository.Repository
	log        *slog.Logger
	stream     string
	subject    string
	durable    string
	templates  map[string]string
	language   string
	offsets    reminderOffsetsProvider
	recipients RecipientResolver
}

// reminderOffsetsProvider keeps reminder configuration tenant-aware without
// coupling notification-service to tenant_db. The environment-backed default
// is retained only for isolated tests; production injects tenant-service.
type reminderOffsetsProvider interface {
	ReminderOffsets(context.Context, uuid.UUID) ([]time.Duration, error)
}

type staticOffsets []time.Duration

func (offsets staticOffsets) ReminderOffsets(_ context.Context, _ uuid.UUID) ([]time.Duration, error) {
	return append([]time.Duration(nil), offsets...), nil
}

func New(js nats.JetStreamContext, r repository.Repository, l *slog.Logger) Consumer {
	return NewWithConfig(js, r, l, "APPOINTMENTS", "appointments.v1.appointment.*", "notification-email-v1")
}

// NewWithConfig keeps the production durable fixed while letting integration
// tests use an isolated stream and durable on a real JetStream server.
func NewWithConfig(js nats.JetStreamContext, r repository.Repository, l *slog.Logger, stream, subject, durable string) Consumer {
	return NewWithConfigAndReminderOffsets(js, r, l, stream, subject, durable, staticOffsets(offsets()))
}

// NewWithConfigAndReminderOffsets is used by production to obtain offsets
// from tenant-service and by integration tests to inject deterministic values.
func NewWithConfigAndReminderOffsets(js nats.JetStreamContext, r repository.Repository, l *slog.Logger, stream, subject, durable string, provider reminderOffsetsProvider, resolvers ...RecipientResolver) Consumer {
	if provider == nil {
		provider = staticOffsets(offsets())
	}
	consumer := Consumer{js: js, r: r, log: l, stream: stream, subject: subject, durable: durable, templates: map[string]string{"appointment.created": env("EMAIL_TEMPLATE_APPOINTMENT_CREATED", "appointment_created"), "appointment.rescheduled": env("EMAIL_TEMPLATE_APPOINTMENT_RESCHEDULED", "appointment_rescheduled"), "appointment.cancelled": env("EMAIL_TEMPLATE_APPOINTMENT_CANCELLED", "appointment_cancelled"), "appointment.confirmed": env("EMAIL_TEMPLATE_APPOINTMENT_CONFIRMED", "appointment_confirmed"), "appointment.reminder_due": env("EMAIL_TEMPLATE_APPOINTMENT_REMINDER", "appointment_reminder")}, language: env("EMAIL_TEMPLATE_LANGUAGE", "en"), offsets: provider}
	if len(resolvers) > 0 {
		consumer.recipients = resolvers[0]
	}
	return consumer
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func offsets() []time.Duration {
	out := []time.Duration{}
	for _, x := range strings.Split(env("REMINDER_OFFSETS", "24h,2h"), ",") {
		if d, e := time.ParseDuration(strings.TrimSpace(x)); e == nil && d > 0 {
			out = append(out, d)
		}
	}
	return out
}
func (c Consumer) Run(ctx context.Context) {
	var sub *nats.Subscription
	for sub == nil {
		var err error
		sub, err = c.js.PullSubscribe(c.subject, c.durable, nats.BindStream(c.stream), nats.ManualAck())
		if err == nil {
			break
		}
		c.log.Warn("notification subscription unavailable", "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	for {
		if ctx.Err() != nil {
			return
		}
		// Fetch has one bounded timeout only. Passing a request context as well
		// conflicts with MaxWait in nats.go and previously caused idle failures.
		msgs, e := sub.Fetch(10, nats.MaxWait(time.Second))
		if e != nil {
			if errors.Is(e, nats.ErrTimeout) {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			if ctx.Err() != nil {
				return
			}
			c.log.Warn("notification fetch failed", "error", e)
			time.Sleep(time.Second)
			continue
		}
		for _, msg := range msgs {
			if c.handle(ctx, msg) == nil {
				_ = msg.Ack()
			} else {
				_ = msg.NakWithDelay(time.Second)
			}
		}
	}
}
func (c Consumer) handle(ctx context.Context, m *nats.Msg) error {
	var e domain.Envelope
	if err := json.Unmarshal(m.Data, &e); err != nil || e.EventID.String() == "00000000-0000-0000-0000-000000000000" || e.TenantID.String() == "00000000-0000-0000-0000-000000000000" || e.AggregateID.String() == "00000000-0000-0000-0000-000000000000" || e.EventVersion != 1 {
		return errors.New("malformed appointment envelope")
	}
	tmpl, ok := c.templates[e.EventType]
	if !ok {
		return nil
	}
	var payload struct {
		StartAt time.Time `json:"start_at"`
	}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &payload)
	}
	// Resolve the tenant-scoped contact snapshot at delivery planning time. The
	// durable event remains free of customer PII.
	if c.recipients == nil {
		return errors.New("appointment recipient resolver unavailable")
	}
	recipient, err := c.recipients.Email(ctx, e.TenantID, e.AggregateID)
	if err != nil {
		return err
	}
	if _, err := c.r.Plan(ctx, e.TenantID, e, mapType(e.EventType), recipient, tmpl, c.language, time.Now().UTC()); err != nil {
		return err
	}
	if e.EventType == "appointment.rescheduled" || e.EventType == "appointment.cancelled" {
		if err := c.r.CancelReminders(ctx, e.TenantID, e.AggregateID); err != nil {
			return err
		}
	}
	if (e.EventType == "appointment.created" || e.EventType == "appointment.rescheduled") && !payload.StartAt.IsZero() {
		now := time.Now().UTC()
		offsets, err := c.offsets.ReminderOffsets(ctx, e.TenantID)
		if err != nil {
			return err
		}
		for _, off := range offsets {
			scheduledAt := payload.StartAt.UTC().Add(-off)
			// A missed offset is not an immediate reminder. The appointment event
			// itself provides the immediate confirmation; only future reminders are
			// persisted for the delivery worker.
			if !scheduledAt.After(now) {
				continue
			}
			rem := e
			rem.EventID = uuid.NewSHA1(e.EventID, []byte(off.String()))
			_, err := c.r.Plan(ctx, e.TenantID, rem, "appointment_reminder", recipient, c.templates["appointment.reminder_due"], c.language, scheduledAt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func mapType(t string) string { return strings.ReplaceAll(t, ".", "_") }
