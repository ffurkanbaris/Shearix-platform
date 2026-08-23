package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/scheduling-service/generated"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) Repository { return Repository{p} }
func (r Repository) ReplaceHours(ctx context.Context, t, b uuid.UUID, in domain.WorkingHoursInput) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `DELETE FROM public.barber_working_hours WHERE tenant_id=$1 AND barber_id=$2`, t, b); e != nil {
			return e
		}
		for _, d := range in {
			for _, v := range d.Intervals {
				if _, e := tx.Exec(ctx, `INSERT INTO public.barber_working_hours(tenant_id,barber_id,weekday,start_time,end_time)VALUES($1,$2,$3,$4::time,$5::time)`, t, b, d.Weekday, v.Start, v.End); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (r Repository) Hours(ctx context.Context, t, b uuid.UUID) (domain.WorkingHoursInput, error) {
	out := domain.WorkingHoursInput{}
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		rows, e := generated.New(tx).ListWorkingHours(ctx, generated.ListWorkingHoursParams{TenantID: pgtype.UUID{Bytes: [16]byte(t), Valid: true}, BarberID: pgtype.UUID{Bytes: [16]byte(b), Valid: true}})
		if e != nil {
			return e
		}
		idx := map[int]int{}
		for _, row := range rows {
			d := int(row.Weekday)
			v := domain.Interval{Start: timeText(row.StartTime), End: timeText(row.EndTime)}
			i, ok := idx[d]
			if !ok {
				out = append(out, struct {
					Weekday   int               `json:"weekday"`
					Intervals []domain.Interval `json:"intervals"`
				}{Weekday: d})
				i = len(out) - 1
				idx[d] = i
			}
			out[i].Intervals = append(out[i].Intervals, v)
		}
		return nil
	})
	return out, e
}
func timeText(v pgtype.Time) string {
	minutes := v.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
func (r Repository) Overrides(ctx context.Context, t, b uuid.UUID) ([]domain.Override, error) {
	out := []domain.Override{}
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id,override_date::text,kind FROM public.schedule_overrides WHERE tenant_id=$1 AND barber_id=$2 ORDER BY override_date`, t, b)
		if e != nil {
			return e
		}
		for rows.Next() {
			var v domain.Override
			if e = rows.Scan(&v.ID, &v.Date, &v.Kind); e != nil {
				rows.Close()
				return e
			}
			out = append(out, v)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()

		// pgx permits one active result set per connection. Collect the override
		// headers first, then issue the interval queries on the same tenant-scoped
		// transaction after the parent result set is closed.
		for i := range out {
			ir, e := tx.Query(ctx, `SELECT start_time,end_time FROM public.schedule_override_intervals WHERE tenant_id=$1 AND override_id=$2 ORDER BY start_time`, t, out[i].ID)
			if e != nil {
				return e
			}
			for ir.Next() {
				var start, end pgtype.Time
				if e = ir.Scan(&start, &end); e != nil {
					ir.Close()
					return e
				}
				out[i].Intervals = append(out[i].Intervals, domain.Interval{Start: timeText(start), End: timeText(end)})
			}
			if e = ir.Err(); e != nil {
				ir.Close()
				return e
			}
			ir.Close()
		}
		return nil
	})
	return out, e
}
func (r Repository) SaveOverride(ctx context.Context, t, b, id uuid.UUID, in domain.OverrideInput) (domain.Override, error) {
	var o domain.Override
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		q := `INSERT INTO public.schedule_overrides(tenant_id,barber_id,override_date,kind)VALUES($1,$2,$3::date,$4)RETURNING id,override_date::text,kind`
		args := []any{t, b, in.Date, in.Kind}
		if id != uuid.Nil {
			q = `UPDATE public.schedule_overrides SET override_date=$3::date,kind=$4,updated_at=now() WHERE tenant_id=$1 AND barber_id=$2 AND id=$5 RETURNING id,override_date::text,kind`
			args = []any{t, b, in.Date, in.Kind, id}
		}
		if e := tx.QueryRow(ctx, q, args...).Scan(&o.ID, &o.Date, &o.Kind); errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		} else if e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `DELETE FROM public.schedule_override_intervals WHERE tenant_id=$1 AND override_id=$2`, t, o.ID); e != nil {
			return e
		}
		for _, v := range in.Intervals {
			if _, e := tx.Exec(ctx, `INSERT INTO public.schedule_override_intervals(tenant_id,override_id,barber_id,override_date,start_time,end_time)VALUES($1,$2,$3,$4::date,$5::time,$6::time)`, t, o.ID, b, in.Date, v.Start, v.End); e != nil {
				return e
			}
			o.Intervals = append(o.Intervals, v)
		}
		return nil
	})
	return o, e
}
func (r Repository) DeleteOverride(ctx context.Context, t, b, id uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `DELETE FROM public.schedule_overrides WHERE tenant_id=$1 AND barber_id=$2 AND id=$3`, t, b, id)
		if e == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return e
	})
}
func (r Repository) Blocks(ctx context.Context, t, b uuid.UUID) ([]domain.BlockedPeriod, error) {
	out := []domain.BlockedPeriod{}
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id,start_at,end_at FROM public.blocked_periods WHERE tenant_id=$1 AND barber_id=$2 ORDER BY start_at`, t, b)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.BlockedPeriod
			if e = rows.Scan(&v.ID, &v.StartAt, &v.EndAt); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (r Repository) SaveBlock(ctx context.Context, t, b uuid.UUID, in domain.BlockedPeriodInput) (domain.BlockedPeriod, error) {
	var v domain.BlockedPeriod
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO public.blocked_periods(tenant_id,barber_id,start_at,end_at)VALUES($1,$2,$3,$4)RETURNING id,start_at,end_at`, t, b, in.StartAt, in.EndAt).Scan(&v.ID, &v.StartAt, &v.EndAt)
	})
	return v, e
}
func (r Repository) DeleteBlock(ctx context.Context, t, b, id uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `DELETE FROM public.blocked_periods WHERE tenant_id=$1 AND barber_id=$2 AND id=$3`, t, b, id)
		if e == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return e
	})
}
func (r Repository) Day(ctx context.Context, t, b uuid.UUID, date time.Time) (domain.Override, []domain.BlockedPeriod, error) {
	var o domain.Override
	var blocks []domain.BlockedPeriod
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		e := tx.QueryRow(ctx, `SELECT id,override_date::text,kind FROM public.schedule_overrides WHERE tenant_id=$1 AND barber_id=$2 AND override_date=$3`, t, b, date).Scan(&o.ID, &o.Date, &o.Kind)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e == nil {
			rows, e := tx.Query(ctx, `SELECT start_time,end_time FROM public.schedule_override_intervals WHERE tenant_id=$1 AND override_id=$2 ORDER BY start_time`, t, o.ID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var start, end pgtype.Time
				if e = rows.Scan(&start, &end); e != nil {
					rows.Close()
					return e
				}
				o.Intervals = append(o.Intervals, domain.Interval{Start: timeText(start), End: timeText(end)})
			}
			rows.Close()
		}
		rows, e := tx.Query(ctx, `SELECT id,start_at,end_at FROM public.blocked_periods WHERE tenant_id=$1 AND barber_id=$2 AND start_at<$4 AND end_at>$3`, t, b, date, date.AddDate(0, 0, 1))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.BlockedPeriod
			if e = rows.Scan(&v.ID, &v.StartAt, &v.EndAt); e != nil {
				return e
			}
			blocks = append(blocks, v)
		}
		return rows.Err()
	})
	return o, blocks, e
}
