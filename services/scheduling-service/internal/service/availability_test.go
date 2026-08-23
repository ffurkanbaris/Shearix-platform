package service

import (
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestAvailabilityIntervalsAndBuffers(t *testing.T) {
	zone := time.FixedZone("test", 0)
	date := time.Date(2030, 1, 7, 0, 0, 0, 0, zone)
	hours := domain.WorkingHoursInput{{Weekday: 1, Intervals: []domain.Interval{{Start: "09:00", End: "10:00"}, {Start: "11:00", End: "12:00"}}}}
	slots := IntervalsForDate(hours, domain.Override{}, date, zone)
	got := Available(slots, nil, nil, 30, 5, 10, 15, time.Date(2029, 1, 1, 0, 0, 0, 0, zone))
	if len(got) != 2 {
		t.Fatalf("expected 2 boundary-safe slots, got %d", len(got))
	}
	if !got[0].StartAt.Equal(time.Date(2030, 1, 7, 9, 15, 0, 0, zone)) {
		t.Fatal("buffer before not applied")
	}
}
func TestOverridesBlockedPastAndValidation(t *testing.T) {
	zone, _ := time.LoadLocation("Europe/Istanbul")
	date := time.Date(2030, 3, 31, 0, 0, 0, 0, zone)
	hours := domain.WorkingHoursInput{{Weekday: int(date.Weekday()), Intervals: []domain.Interval{{Start: "09:00", End: "17:00"}}}}
	vac := domain.Override{ID: uuid.New(), Kind: "vacation"}
	if len(IntervalsForDate(hours, vac, date, zone)) != 0 {
		t.Fatal("vacation must remove hours")
	}
	custom := domain.Override{ID: uuid.New(), Kind: "custom_hours", Intervals: []domain.Interval{{Start: "10:00", End: "11:00"}}}
	if x := IntervalsForDate(hours, custom, date, zone); len(x) != 1 || x[0].StartAt.Hour() != 10 {
		t.Fatal("custom hours must replace weekly hours")
	}
	if ValidIntervals([]domain.Interval{{Start: "10:00", End: "09:00"}}) || ValidIntervals([]domain.Interval{{Start: "09:00", End: "11:00"}, {Start: "10:00", End: "12:00"}}) {
		t.Fatal("invalid intervals accepted")
	}
	slots := Available(IntervalsForDate(hours, custom, date, zone), []domain.BlockedPeriod{{StartAt: time.Date(2030, 3, 31, 10, 20, 0, 0, zone), EndAt: time.Date(2030, 3, 31, 10, 40, 0, 0, zone)}}, nil, 20, 0, 0, 10, time.Date(2029, 1, 1, 0, 0, 0, 0, zone))
	if len(slots) != 2 {
		t.Fatalf("blocked range expected 2 slots, got %d", len(slots))
	}
}
