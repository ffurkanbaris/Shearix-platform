package domain

import (
	"time"

	"github.com/google/uuid"
)

type CreateInput struct {
	BranchID        uuid.UUID  `json:"branch_id"`
	BarberID        uuid.UUID  `json:"barber_id"`
	ServiceID       uuid.UUID  `json:"service_id"`
	CustomerName    string     `json:"customer_name"`
	CustomerContact string     `json:"customer_contact"`
	StartAt         time.Time  `json:"start_at"`
	CustomerID      *uuid.UUID `json:"-"`
}

// Appointment timing is always calculated from catalog metadata by the
// appointment service; clients cannot submit end or occupied ranges.
type Appointment struct {
	ID                   uuid.UUID  `json:"id"`
	BranchID             uuid.UUID  `json:"branch_id"`
	BarberID             uuid.UUID  `json:"barber_id"`
	ServiceID            uuid.UUID  `json:"service_id"`
	CustomerName         string     `json:"customer_name"`
	CustomerContact      string     `json:"customer_contact"`
	StartAt              time.Time  `json:"start_at"`
	EndAt                time.Time  `json:"end_at"`
	OccupiedStartAt      time.Time  `json:"occupied_start_at,omitempty"`
	OccupiedEndAt        time.Time  `json:"occupied_end_at,omitempty"`
	Status               string     `json:"status"`
	CanCancel            bool       `json:"can_cancel,omitempty"`
	CancellationDeadline *time.Time `json:"cancellation_deadline,omitempty"`
}

type OutboxEvent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	AggregateID uuid.UUID
	Type        string
	Payload     []byte
	OccurredAt  time.Time
}
