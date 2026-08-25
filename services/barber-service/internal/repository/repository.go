package repository

import (
	"context"
	"errors"
	"github.com/barber-appointment/barber-service/generated"
	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) Repository { return Repository{pool} }
func (r Repository) Assigned(ctx context.Context, t, branchID, barberID uuid.UUID, result *bool) error {
	return db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.barber_branch_assignments WHERE tenant_id=$1 AND branch_id=$2 AND barber_id=$3 AND active=true)`, t, branchID, barberID).Scan(result)
	})
}
func (r Repository) Branches(ctx context.Context, t uuid.UUID, activeOnly bool) ([]domain.Branch, error) {
	out := make([]domain.Branch, 0)
	err := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		if !activeOnly {
			rows, e := generated.New(tx).ListBranches(ctx, pgtype.UUID{Bytes: [16]byte(t), Valid: true})
			if e != nil {
				return e
			}
			for _, row := range rows {
				out = append(out, domain.Branch{ID: uuid.UUID(row.ID.Bytes), Name: row.Name, Address: row.Address, Active: row.Active})
			}
			return nil
		}
		q := `SELECT id,name,address,active FROM public.branches WHERE tenant_id=$1 AND active=true ORDER BY name`
		rows, e := tx.Query(ctx, q, t)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Branch
			if e = rows.Scan(&v.ID, &v.Name, &v.Address, &v.Active); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (r Repository) Branch(ctx context.Context, t, id uuid.UUID) (domain.Branch, error) {
	var v domain.Branch
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		e := tx.QueryRow(ctx, `SELECT id,name,address,active FROM public.branches WHERE tenant_id=$1 AND id=$2`, t, id).Scan(&v.ID, &v.Name, &v.Address, &v.Active)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return e
	})
	return v, e
}
func (r Repository) SaveBranch(ctx context.Context, t, id uuid.UUID, in domain.BranchInput) (domain.Branch, error) {
	var v domain.Branch
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		active := true
		if in.Active != nil {
			active = *in.Active
		}
		q := `INSERT INTO public.branches(tenant_id,name,address,active)VALUES($1,$2,$3,$4) RETURNING id,name,address,active`
		args := []any{t, in.Name, in.Address, active}
		if id != uuid.Nil {
			q = `UPDATE public.branches SET name=$3,address=$4,active=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING id,name,address,active`
			args = []any{t, id, in.Name, in.Address, active}
		}
		e := tx.QueryRow(ctx, q, args...).Scan(&v.ID, &v.Name, &v.Address, &v.Active)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return e
	})
	return v, e
}
func (r Repository) Barbers(ctx context.Context, t uuid.UUID, public bool) ([]domain.Barber, error) {
	out := make([]domain.Barber, 0)
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		q := `SELECT b.id,b.display_name,p.bio,b.active,b.identity_id FROM public.barbers b JOIN public.barber_profiles p ON p.barber_id=b.id AND p.tenant_id=b.tenant_id WHERE b.tenant_id=$1`
		if public {
			q += ` AND b.active=true AND EXISTS(SELECT 1 FROM public.barber_branch_assignments a JOIN public.branches br ON br.id=a.branch_id AND br.tenant_id=a.tenant_id WHERE a.tenant_id=b.tenant_id AND a.barber_id=b.id AND a.active=true AND br.active=true)`
		}
		q += ` ORDER BY b.display_name`
		rows, x := tx.Query(ctx, q, t)
		if x != nil {
			return x
		}
		for rows.Next() {
			var v domain.Barber
			if x = rows.Scan(&v.ID, &v.DisplayName, &v.Bio, &v.Active, &v.IdentityID); x != nil {
				rows.Close()
				return x
			}
			if public {
				v.IdentityID = nil
			}
			out = append(out, v)
		}
		if x = rows.Err(); x != nil {
			rows.Close()
			return x
		}
		rows.Close()
		// Branch assignments are public booking data: exposing tenant-local IDs
		// lets a booking client avoid offering a barber at an unrelated branch.
		// Identity links remain withheld above.
		for index := range out {
			if out[index].BranchIDs, x = branchIDs(ctx, tx, t, out[index].ID); x != nil {
				return x
			}
		}
		return nil
	})
	return out, e
}
func (r Repository) Barber(ctx context.Context, t, id uuid.UUID) (domain.Barber, error) {
	var v domain.Barber
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		x := tx.QueryRow(ctx, `SELECT b.id,b.display_name,p.bio,b.active,b.identity_id FROM public.barbers b JOIN public.barber_profiles p ON p.barber_id=b.id AND p.tenant_id=b.tenant_id WHERE b.tenant_id=$1 AND b.id=$2`, t, id).Scan(&v.ID, &v.DisplayName, &v.Bio, &v.Active, &v.IdentityID)
		if errors.Is(x, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if x == nil {
			v.BranchIDs, x = branchIDs(ctx, tx, t, id)
		}
		return x
	})
	return v, e
}
func (r Repository) SaveBarber(ctx context.Context, t, id uuid.UUID, in domain.BarberInput) (domain.Barber, error) {
	var v domain.Barber
	e := db.WithTenantTx(ctx, r.pool, t, func(tx pgx.Tx) error {
		a := true
		if in.Active != nil {
			a = *in.Active
		}
		if id == uuid.Nil {
			x := tx.QueryRow(ctx, `INSERT INTO public.barbers(tenant_id,display_name,active)VALUES($1,$2,$3)RETURNING id,display_name,active,identity_id`, t, in.DisplayName, a).Scan(&v.ID, &v.DisplayName, &v.Active, &v.IdentityID)
			if x != nil {
				return x
			}
			if _, x := tx.Exec(ctx, `INSERT INTO public.barber_profiles(barber_id,tenant_id,bio)VALUES($1,$2,$3)`, v.ID, t, in.Bio); x != nil {
				return x
			}
		} else {
			v.ID = id
			x := tx.QueryRow(ctx, `UPDATE public.barbers SET display_name=$3,active=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING display_name,active,identity_id`, t, id, in.DisplayName, a).Scan(&v.DisplayName, &v.Active, &v.IdentityID)
			if errors.Is(x, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if x != nil {
				return x
			}
			_, x = tx.Exec(ctx, `UPDATE public.barber_profiles SET bio=$3,updated_at=now() WHERE tenant_id=$1 AND barber_id=$2`, t, id, in.Bio)
			if x != nil {
				return x
			}
		}
		v.Bio = in.Bio
		assignedBranchIDs, branchErr := branchIDs(ctx, tx, t, v.ID)
		if branchErr != nil {
			return branchErr
		}
		v.BranchIDs = assignedBranchIDs
		if in.BranchIDs != nil {
			for _, branch := range in.BranchIDs {
				var ok bool
				if x := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.branches WHERE tenant_id=$1 AND id=$2)`, t, branch).Scan(&ok); x != nil {
					return x
				}
				if !ok {
					return ErrNotFound
				}
			}
			if _, x := tx.Exec(ctx, `UPDATE public.barber_branch_assignments SET active=false WHERE tenant_id=$1 AND barber_id=$2`, t, v.ID); x != nil {
				return x
			}
			for _, branch := range in.BranchIDs {
				if _, x := tx.Exec(ctx, `INSERT INTO public.barber_branch_assignments(tenant_id,barber_id,branch_id,active)VALUES($1,$2,$3,true) ON CONFLICT(tenant_id,barber_id,branch_id)DO UPDATE SET active=true`, t, v.ID, branch); x != nil {
					return x
				}
			}
			v.BranchIDs = append([]uuid.UUID(nil), in.BranchIDs...)
		}
		return nil
	})
	return v, e
}

func branchIDs(ctx context.Context, tx pgx.Tx, tenantID, barberID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT branch_id::text FROM public.barber_branch_assignments WHERE tenant_id=$1 AND barber_id=$2 AND active=true ORDER BY branch_id`, tenantID, barberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return nil, parseErr
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LinkIdentity records only the tenant-local association. Eligibility is
// validated first through auth-service; barber-service never reads auth_db.
func (r Repository) LinkIdentity(ctx context.Context, tenantID, barberID, identityID uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var linked *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT identity_id FROM public.barbers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, barberID).Scan(&linked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if linked != nil {
			if *linked == identityID {
				return nil
			}
			return ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE public.barbers SET identity_id=$3,updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, tenantID, barberID, identityID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrConflict
			}
			return err
		}
		return nil
	})
}

func (r Repository) UnlinkIdentity(ctx context.Context, tenantID, barberID uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.barbers SET identity_id=NULL,updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, tenantID, barberID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
}
