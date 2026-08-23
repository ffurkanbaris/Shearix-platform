package domain

import (
	"github.com/google/uuid"
	"time"
)

type Interval struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type WorkingHoursInput []struct {
	Weekday   int        `json:"weekday"`
	Intervals []Interval `json:"intervals"`
}
type OverrideInput struct {
	Date      string     `json:"date"`
	Kind      string     `json:"kind"`
	Intervals []Interval `json:"intervals"`
}
type Override struct {
	ID        uuid.UUID  `json:"id"`
	Date      string     `json:"date"`
	Kind      string     `json:"kind"`
	Intervals []Interval `json:"intervals,omitempty"`
}
type BlockedPeriodInput struct {
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}
type BlockedPeriod struct {
	ID      uuid.UUID `json:"id"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}
type Schedule struct {
	WorkingHours   WorkingHoursInput `json:"working_hours"`
	Overrides      []Override        `json:"overrides"`
	BlockedPeriods []BlockedPeriod   `json:"blocked_periods"`
}
type Slot struct {
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}
