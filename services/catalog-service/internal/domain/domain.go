package domain

import "github.com/google/uuid"

type Service struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	DurationMinutes     int       `json:"duration_minutes"`
	BufferBeforeMinutes int       `json:"buffer_before_minutes"`
	BufferAfterMinutes  int       `json:"buffer_after_minutes"`
	Price               string    `json:"price"`
	Currency            string    `json:"currency"`
	Active              bool      `json:"active"`
}
type ServiceInput struct {
	Name                string `json:"name"`
	DurationMinutes     int    `json:"duration_minutes"`
	BufferBeforeMinutes int    `json:"buffer_before_minutes"`
	BufferAfterMinutes  int    `json:"buffer_after_minutes"`
	Price               string `json:"price"`
	Currency            string `json:"currency"`
	Active              *bool  `json:"active,omitempty"`
}
type AssignmentInput struct {
	ServiceID uuid.UUID `json:"service_id"`
}

// PublicBarberServices is the tenant-scoped assignment projection used by
// public booking clients. It contains no catalog internals beyond active
// service relationships.
type PublicBarberServices struct {
	BarberID   uuid.UUID   `json:"barber_id"`
	ServiceIDs []uuid.UUID `json:"service_ids"`
}
