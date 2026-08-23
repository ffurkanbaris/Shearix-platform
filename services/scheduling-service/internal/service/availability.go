package service

import (
	"context"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/google/uuid"
	"time"
)

// OccupancyProvider intentionally decouples scheduling from appointment storage.
type OccupancyProvider interface {
	Occupied(ctx context.Context, tenant tenantctx.Context, barberID uuid.UUID, start, end time.Time) ([]domain.BlockedPeriod, error)
}
type EmptyOccupancy struct{}

func (EmptyOccupancy) Occupied(context.Context, tenantctx.Context, uuid.UUID, time.Time, time.Time) ([]domain.BlockedPeriod, error) {
	return nil, nil
}
func ValidIntervals(in []domain.Interval) bool {
	prev := time.Duration(-1)
	for _, v := range in {
		s, e, ok := wall(v)
		if !ok || s >= e || s < prev {
			return false
		}
		prev = e
	}
	return true
}
func wall(v domain.Interval) (time.Duration, time.Duration, bool) {
	s, e := parseWall(v.Start), parseWall(v.End)
	return s, e, s >= 0 && e >= 0
}
func parseWall(v string) time.Duration {
	t, e := time.Parse("15:04", v)
	if e != nil {
		return -1
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
}
func IntervalsForDate(hours domain.WorkingHoursInput, over domain.Override, date time.Time, zone *time.Location) []domain.Slot {
	var in []domain.Interval
	if over.ID.String() != "00000000-0000-0000-0000-000000000000" {
		if over.Kind != "custom_hours" {
			return nil
		}
		in = over.Intervals
	} else {
		for _, d := range hours {
			if d.Weekday == int(date.Weekday()) {
				in = d.Intervals
				break
			}
		}
	}
	out := make([]domain.Slot, 0, len(in))
	for _, v := range in {
		s, e, ok := wall(v)
		if !ok {
			continue
		}
		base := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, zone)
		out = append(out, domain.Slot{StartAt: base.Add(s), EndAt: base.Add(e)})
	}
	return out
}
func Available(intervals []domain.Slot, blocked []domain.BlockedPeriod, occupied []domain.BlockedPeriod, duration, before, after, step int, now time.Time) []domain.Slot {
	out := []domain.Slot{}
	for _, i := range intervals {
		for start := i.StartAt; !start.Add(time.Duration(duration+after) * time.Minute).After(i.EndAt); start = start.Add(time.Duration(step) * time.Minute) {
			occStart := start.Add(-time.Duration(before) * time.Minute)
			occEnd := start.Add(time.Duration(duration+after) * time.Minute)
			if occStart.Before(i.StartAt) || !start.After(now) {
				continue
			}
			bad := false
			for _, x := range append(append([]domain.BlockedPeriod{}, blocked...), occupied...) {
				if occStart.Before(x.EndAt) && occEnd.After(x.StartAt) {
					bad = true
					break
				}
			}
			if !bad {
				out = append(out, domain.Slot{StartAt: start, EndAt: start.Add(time.Duration(duration) * time.Minute)})
			}
		}
	}
	return out
}
