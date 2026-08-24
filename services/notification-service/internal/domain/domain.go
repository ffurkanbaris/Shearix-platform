package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Envelope struct {
	EventID      uuid.UUID       `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventVersion int             `json:"event_version"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	AggregateID  uuid.UUID       `json:"aggregate_id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
}
type Notification struct {
	ID            uuid.UUID
	ClaimToken    uuid.UUID
	TenantID      uuid.UUID
	AppointmentID *uuid.UUID
	Type          string
	Email         string
	Template      string
	Language      string
	Attempts      int
	// Recovered is true when this claim reclaimed a row whose previous lease
	// had already expired (i.e. a prior worker crashed or stalled mid-send),
	// and false for a claim of a fresh 'pending' row. Populated by
	// claim_email_notifications' RETURNING clause; observability-only, never
	// used for delivery/fencing decisions.
	Recovered bool
}
