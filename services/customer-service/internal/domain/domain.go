package domain

import (
	"github.com/google/uuid"
	"strings"
	"time"
)

type Customer struct {
	ID                    uuid.UUID  `json:"id"`
	TenantID              uuid.UUID  `json:"-"`
	Name                  string     `json:"name"`
	Email                 string     `json:"email"`
	Phone                 string     `json:"phone"`
	Status                string     `json:"status"`
	MustChangePassword    bool       `json:"must_change_password"`
	InitialDeliveryStatus string     `json:"-"`
	PasswordChangedAt     *time.Time `json:"-"`
}
type Appointment struct {
	ID              uuid.UUID `json:"id"`
	BranchID        uuid.UUID `json:"branch_id"`
	BarberID        uuid.UUID `json:"barber_id"`
	ServiceID       uuid.UUID `json:"service_id"`
	CustomerName    string    `json:"customer_name"`
	CustomerContact string    `json:"customer_contact"`
	StartAt         time.Time `json:"start_at"`
	EndAt           time.Time `json:"end_at"`
	Status          string    `json:"status"`
}

func NormalizePhone(v string) (string, error) {
	v = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(v))
	if len(v) < 9 || len(v) > 16 || !strings.HasPrefix(v, "+") {
		return "", &ValidationError{"invalid phone"}
	}
	for _, r := range v[1:] {
		if r < '0' || r > '9' {
			return "", &ValidationError{"invalid phone"}
		}
	}
	return v, nil
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
