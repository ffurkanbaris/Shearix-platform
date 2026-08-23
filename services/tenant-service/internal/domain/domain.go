package domain

import (
	"time"

	"github.com/google/uuid"
)

type DomainResolution struct {
	TenantID uuid.UUID `json:"tenant_id"`
	AppType  string    `json:"app_type"`
}
type Settings struct {
	TenantID                    uuid.UUID `json:"tenant_id"`
	BusinessTimezone            string    `json:"business_timezone"`
	BookingIntervalMinutes      int       `json:"booking_interval_minutes"`
	ReminderOffsetsMinutes      []int     `json:"reminder_offsets_minutes"`
	CancellationPolicy          string    `json:"cancellation_policy"`
	CancellationNoticeMinutes   int       `json:"cancellation_notice_minutes"`
	BookingHorizonDays          int       `json:"booking_horizon_days"`
	MinimumBookingNoticeMinutes int       `json:"minimum_booking_notice_minutes"`
}

// UpdateSettingsInput is deliberately partial because the tenant-admin API is
// a PATCH endpoint. Units are part of every duration field name so callers
// cannot accidentally submit an ambiguous value. tenant_id is derived from
// the trusted hostname context and is never part of this input.
type UpdateSettingsInput struct {
	Timezone                    *string `json:"timezone,omitempty"`
	BookingIntervalMinutes      *int    `json:"booking_interval_minutes,omitempty"`
	ReminderOffsetsMinutes      *[]int  `json:"reminder_offsets_minutes,omitempty"`
	CancellationPolicy          *string `json:"cancellation_policy,omitempty"`
	BookingHorizonDays          *int    `json:"booking_horizon_days,omitempty"`
	MinimumBookingNoticeMinutes *int    `json:"minimum_booking_notice_minutes,omitempty"`
}

const (
	DomainTypeAdmin   = "admin"
	DomainTypeBooking = "booking"

	DomainVerificationPending  = "pending"
	DomainVerificationVerified = "verified"
	DomainVerificationFailed   = "failed"
)

type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Settings  Settings  `json:"settings"`
}

type CreateTenantInput struct {
	Name     string   `json:"name"`
	Settings Settings `json:"settings"`
}

type CreateDomainInput struct {
	Hostname   string `json:"hostname"`
	DomainType string `json:"domain_type"`
}

// TenantDomain never serializes the token hash. VerificationToken is returned
// only by the create-domain operation so an operator can publish its TXT
// record; subsequent reads cannot recover it.
type TenantDomain struct {
	ID                        uuid.UUID  `json:"id"`
	TenantID                  uuid.UUID  `json:"tenant_id"`
	Hostname                  string     `json:"hostname"`
	DomainType                string     `json:"domain_type"`
	Verified                  bool       `json:"verified"`
	Active                    bool       `json:"active"`
	VerificationState         string     `json:"verification_state"`
	VerificationRecord        string     `json:"verification_record"`
	VerificationToken         string     `json:"verification_token,omitempty"`
	VerificationValue         string     `json:"verification_value,omitempty"`
	VerificationRequestedAt   *time.Time `json:"verification_requested_at,omitempty"`
	LastVerificationAttemptAt *time.Time `json:"last_verification_attempt_at,omitempty"`
	VerifiedAt                *time.Time `json:"verified_at,omitempty"`
	ActivatedAt               *time.Time `json:"activated_at,omitempty"`
	DeactivatedAt             *time.Time `json:"deactivated_at,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
}
