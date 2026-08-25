package repository

import (
	"context"
	"errors"
	"github.com/barber-appointment/catalog-service/generated"
	"github.com/barber-appointment/catalog-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) Repository { return Repository{p} }
func (r Repository) Services(ctx context.Context, t uuid.UUID, activeOnly bool) ([]domain.Service, error) {
	out := make([]domain.Service, 0)
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		if activeOnly {
			rows, x := generated.New(tx).ListActiveServices(ctx, pgtype.UUID{Bytes: [16]byte(t), Valid: true})
			if x != nil {
				return x
			}
			for _, row := range rows {
				out = append(out, domain.Service{ID: uuid.UUID(row.ID.Bytes), Name: row.Name, DurationMinutes: int(row.DurationMinutes), BufferBeforeMinutes: int(row.BufferBeforeMinutes), BufferAfterMinutes: int(row.BufferAfterMinutes), Price: row.Price, Currency: row.Currency, Active: true})
			}
			return nil
		}
		q := `SELECT s.id,s.name,s.duration_minutes,s.buffer_before_minutes,s.buffer_after_minutes,p.amount::text,p.currency,s.active FROM public.services s JOIN public.pricing p ON p.service_id=s.id AND p.tenant_id=s.tenant_id WHERE s.tenant_id=$1`
		q += ` ORDER BY s.name`
		rows, x := tx.Query(ctx, q, t)
		if x != nil {
			return x
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Service
			if x = rows.Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Price, &v.Currency, &v.Active); x != nil {
				return x
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}

func (r Repository) PublicBarberServices(ctx context.Context, t uuid.UUID) ([]domain.PublicBarberServices, error) {
	out := make([]domain.PublicBarberServices, 0)
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT bs.barber_id, bs.service_id
			FROM public.barber_services bs
			JOIN public.services s ON s.tenant_id=bs.tenant_id AND s.id=bs.service_id
			WHERE bs.tenant_id=$1 AND bs.active=true AND s.active=true
			ORDER BY bs.barber_id, bs.service_id`, t)
		if err != nil {
			return err
		}
		defer rows.Close()
		byBarber := make(map[uuid.UUID]int)
		for rows.Next() {
			var barberID, serviceID uuid.UUID
			if err = rows.Scan(&barberID, &serviceID); err != nil {
				return err
			}
			idx, ok := byBarber[barberID]
			if !ok {
				out = append(out, domain.PublicBarberServices{BarberID: barberID, ServiceIDs: []uuid.UUID{serviceID}})
				byBarber[barberID] = len(out) - 1
			} else {
				out[idx].ServiceIDs = append(out[idx].ServiceIDs, serviceID)
			}
		}
		return rows.Err()
	})
	return out, e
}
func (r Repository) Service(ctx context.Context, t, id uuid.UUID) (domain.Service, error) {
	var v domain.Service
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		x := tx.QueryRow(ctx, `SELECT s.id,s.name,s.duration_minutes,s.buffer_before_minutes,s.buffer_after_minutes,p.amount::text,p.currency,s.active FROM public.services s JOIN public.pricing p ON p.service_id=s.id AND p.tenant_id=s.tenant_id WHERE s.tenant_id=$1 AND s.id=$2`, t, id).Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Price, &v.Currency, &v.Active)
		if errors.Is(x, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return x
	})
	return v, e
}
func (r Repository) Save(ctx context.Context, t, id uuid.UUID, in domain.ServiceInput) (domain.Service, error) {
	var v domain.Service
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		a := true
		if in.Active != nil {
			a = *in.Active
		}
		if id == uuid.Nil {
			x := tx.QueryRow(ctx, `INSERT INTO public.services(tenant_id,name,duration_minutes,buffer_before_minutes,buffer_after_minutes,active)VALUES($1,$2,$3,$4,$5,$6)RETURNING id,name,duration_minutes,buffer_before_minutes,buffer_after_minutes,active`, t, in.Name, in.DurationMinutes, in.BufferBeforeMinutes, in.BufferAfterMinutes, a).Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Active)
			if x != nil {
				return x
			}
			if _, x := tx.Exec(ctx, `INSERT INTO public.pricing(service_id,tenant_id,amount,currency)VALUES($1,$2,$3,$4)`, v.ID, t, in.Price, in.Currency); x != nil {
				return x
			}
		} else {
			v.ID = id
			x := tx.QueryRow(ctx, `UPDATE public.services SET name=$3,duration_minutes=$4,buffer_before_minutes=$5,buffer_after_minutes=$6,active=$7,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING name,duration_minutes,buffer_before_minutes,buffer_after_minutes,active`, t, id, in.Name, in.DurationMinutes, in.BufferBeforeMinutes, in.BufferAfterMinutes, a).Scan(&v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Active)
			if errors.Is(x, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if x != nil {
				return x
			}
			if _, x := tx.Exec(ctx, `UPDATE public.pricing SET amount=$3,currency=$4,updated_at=now() WHERE tenant_id=$1 AND service_id=$2`, t, id, in.Price, in.Currency); x != nil {
				return x
			}
		}
		v.Price = in.Price
		v.Currency = in.Currency
		return nil
	})
	return v, e
}
func (r Repository) Assign(ctx context.Context, t, b, s uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.services WHERE tenant_id=$1 AND id=$2)`, t, s).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return ErrNotFound
		}
		_, e := tx.Exec(ctx, `INSERT INTO public.barber_services(tenant_id,barber_id,service_id)VALUES($1,$2,$3) ON CONFLICT(tenant_id,barber_id,service_id)DO UPDATE SET active=true`, t, b, s)
		return e
	})
}
func (r Repository) Assignments(ctx context.Context, t, b uuid.UUID) ([]domain.Service, error) {
	out := make([]domain.Service, 0)
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		rows, x := tx.Query(ctx, `SELECT s.id,s.name,s.duration_minutes,s.buffer_before_minutes,s.buffer_after_minutes,p.amount::text,p.currency,s.active FROM public.barber_services bs JOIN public.services s ON s.id=bs.service_id AND s.tenant_id=bs.tenant_id JOIN public.pricing p ON p.service_id=s.id AND p.tenant_id=s.tenant_id WHERE bs.tenant_id=$1 AND bs.barber_id=$2 AND bs.active=true ORDER BY s.name`, t, b)
		if x != nil {
			return x
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Service
			if x = rows.Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Price, &v.Currency, &v.Active); x != nil {
				return x
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (r Repository) Unassign(ctx context.Context, t, b, s uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `DELETE FROM public.barber_services WHERE tenant_id=$1 AND barber_id=$2 AND service_id=$3`, t, b, s)
		if e == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return e
	})
}

// ActiveAssignedService is a tenant-local projection for scheduling. It never
// reads barber-service data; the caller separately validates the barber there.
func (r Repository) ActiveAssignedService(ctx context.Context, t, b, s uuid.UUID) (domain.Service, error) {
	var v domain.Service
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		x := tx.QueryRow(ctx, `SELECT s.id,s.name,s.duration_minutes,s.buffer_before_minutes,s.buffer_after_minutes,p.amount::text,p.currency,s.active FROM public.barber_services bs JOIN public.services s ON s.id=bs.service_id AND s.tenant_id=bs.tenant_id JOIN public.pricing p ON p.service_id=s.id AND p.tenant_id=s.tenant_id WHERE bs.tenant_id=$1 AND bs.barber_id=$2 AND bs.service_id=$3 AND bs.active=true AND s.active=true`, t, b, s).Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferBeforeMinutes, &v.BufferAfterMinutes, &v.Price, &v.Currency, &v.Active)
		if errors.Is(x, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return x
	})
	return v, e
}
