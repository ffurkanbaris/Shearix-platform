package service

import (
	"errors"
	"testing"

	"github.com/barber-appointment/tenant-service/internal/domain"
)

func TestNormalizeHostname(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		allowLocalhost    bool
		valid             bool
	}{
		{name: "normalizes case whitespace and root dot", input: "  Panel.Example.COM. ", want: "panel.example.com", valid: true},
		{name: "development localhost", input: "Admin.Localhost", want: "admin.localhost", allowLocalhost: true, valid: true},
		{name: "production rejects localhost", input: "admin.localhost", allowLocalhost: false},
		{name: "rejects scheme", input: "https://panel.example.com"},
		{name: "rejects path", input: "panel.example.com/path"},
		{name: "rejects port", input: "panel.example.com:8443"},
		{name: "rejects wildcard", input: "*.example.com"},
		{name: "rejects invalid label", input: "-panel.example.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeHostname(test.input, test.allowLocalhost)
			if test.valid && (err != nil || got != test.want) {
				t.Fatalf("NormalizeHostname(%q)=%q,%v want %q,nil", test.input, got, err, test.want)
			}
			if !test.valid && err == nil {
				t.Fatalf("NormalizeHostname(%q) unexpectedly succeeded", test.input)
			}
		})
	}
}

func TestDefaultSettingsAreExplicitAndSensible(t *testing.T) {
	settings := DefaultSettings()
	if settings.BusinessTimezone != "Europe/Istanbul" || settings.BookingIntervalMinutes != 15 || settings.BookingHorizonDays != 60 || settings.MinimumBookingNoticeMinutes != 60 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	if len(settings.ReminderOffsetsMinutes) != 2 || settings.ReminderOffsetsMinutes[0] != 1440 || settings.ReminderOffsetsMinutes[1] != 120 {
		t.Fatalf("unexpected reminder defaults: %+v", settings.ReminderOffsetsMinutes)
	}
}

func TestSettingsValidationRejectsInvalidOperationalValues(t *testing.T) {
	base := DefaultSettings()
	for _, test := range []struct {
		name  string
		input domain.UpdateSettingsInput
	}{
		{name: "invalid timezone", input: domain.UpdateSettingsInput{Timezone: stringPointer("Not/AZone")}},
		{name: "local timezone is not IANA", input: domain.UpdateSettingsInput{Timezone: stringPointer("Local")}},
		{name: "invalid interval", input: domain.UpdateSettingsInput{BookingIntervalMinutes: intPointer(0)}},
		{name: "duplicate reminders", input: domain.UpdateSettingsInput{ReminderOffsetsMinutes: intsPointer([]int{120, 120})}},
		{name: "non-positive reminder", input: domain.UpdateSettingsInput{ReminderOffsetsMinutes: intsPointer([]int{0})}},
		{name: "invalid horizon", input: domain.UpdateSettingsInput{BookingHorizonDays: intPointer(366)}},
		{name: "invalid notice", input: domain.UpdateSettingsInput{MinimumBookingNoticeMinutes: intPointer(-1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mergeSettings(base, test.input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("mergeSettings error=%v, want invalid input", err)
			}
		})
	}
}

func TestSettingsMergePreservesUntouchedFields(t *testing.T) {
	base := DefaultSettings()
	result, err := mergeSettings(base, domain.UpdateSettingsInput{BookingIntervalMinutes: intPointer(30), ReminderOffsetsMinutes: intsPointer([]int{2880, 180})})
	if err != nil {
		t.Fatal(err)
	}
	if result.BookingIntervalMinutes != 30 || len(result.ReminderOffsetsMinutes) != 2 || result.ReminderOffsetsMinutes[0] != 2880 || result.BusinessTimezone != base.BusinessTimezone || result.BookingHorizonDays != base.BookingHorizonDays {
		t.Fatalf("unexpected merged settings: %+v", result)
	}
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }
func intsPointer(value []int) *[]int     { return &value }
