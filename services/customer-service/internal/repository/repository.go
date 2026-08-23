package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/barber-appointment/customer-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate")

type Repository struct{ Pool *pgxpool.Pool }

func New(p *pgxpool.Pool) Repository { return Repository{p} }
func hashToken(v string) string      { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (r Repository) Create(ctx context.Context, t uuid.UUID, name, email, phone, passwordHash string) (domain.Customer, error) {
	var c domain.Customer
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO public.customers(tenant_id,name,normalized_email,normalized_phone,password_hash,password_changed_at,must_change_password,initial_delivery_status,initial_delivery_updated_at) VALUES($1,$2,$3,NULLIF($4,''),$5,now(),true,'pending',now()) RETURNING id,name,normalized_email,COALESCE(normalized_phone,''),status,must_change_password,initial_delivery_status,password_changed_at`, t, name, email, phone, passwordHash).Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Status, &c.MustChangePassword, &c.InitialDeliveryStatus, &c.PasswordChangedAt)
	})
	if err != nil {
		return c, err
	}
	c.TenantID = t
	return c, nil
}
func (r Repository) EnsureGuest(ctx context.Context, t uuid.UUID, name, phone string) (domain.Customer, error) {
	var c domain.Customer
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO public.customers(tenant_id,name,normalized_phone,password_hash) VALUES($1,$2,$3,'!guest-no-password!') ON CONFLICT(tenant_id,normalized_phone) DO UPDATE SET name=public.customers.name RETURNING id,name,normalized_phone,status,password_changed_at`, t, name, phone).Scan(&c.ID, &c.Name, &c.Phone, &c.Status, &c.PasswordChangedAt)
	})
	c.TenantID = t
	return c, err
}
func (r Repository) Find(ctx context.Context, t uuid.UUID, phone string) (domain.Customer, string, error) {
	var c domain.Customer
	var hash string
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,name,normalized_phone,password_hash,status,password_changed_at FROM public.customers WHERE tenant_id=$1 AND normalized_phone=$2`, t, phone).Scan(&c.ID, &c.Name, &c.Phone, &hash, &c.Status, &c.PasswordChangedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", ErrNotFound
	}
	c.TenantID = t
	return c, hash, err
}
func (r Repository) FindByEmail(ctx context.Context, t uuid.UUID, email string) (domain.Customer, string, error) {
	var c domain.Customer
	var hash string
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,name,normalized_email,COALESCE(normalized_phone,''),password_hash,status,must_change_password,initial_delivery_status,password_changed_at FROM public.customers WHERE tenant_id=$1 AND normalized_email=$2`, t, email).Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &hash, &c.Status, &c.MustChangePassword, &c.InitialDeliveryStatus, &c.PasswordChangedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", ErrNotFound
	}
	c.TenantID = t
	return c, hash, err
}
func (r Repository) ClaimInitialDelivery(ctx context.Context, t, id uuid.UUID) (uuid.UUID, bool, error) {
	token := uuid.New()
	claimed := false
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.customers SET initial_delivery_status='sending',initial_delivery_claim_token=$3,initial_delivery_claim_until=now()+interval '2 minutes',initial_delivery_updated_at=now() WHERE tenant_id=$1 AND id=$2 AND (initial_delivery_status IN ('pending','failed') OR (initial_delivery_status='sending' AND initial_delivery_claim_until<now()))`, t, id, token)
		claimed = err == nil && tag.RowsAffected() == 1
		return err
	})
	return token, claimed, err
}

func (r Repository) MarkInitialDelivery(ctx context.Context, t, id, claim uuid.UUID, status, problem string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.customers SET initial_delivery_status=$4,initial_delivery_attempts=initial_delivery_attempts+1,initial_delivery_last_error=NULLIF($5,''),initial_delivery_claim_token=NULL,initial_delivery_claim_until=NULL,initial_delivery_updated_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND initial_delivery_claim_token=$3`, t, id, claim, status, problem)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}
func (r Repository) ActivateRetriedCredential(ctx context.Context, t, id, claim uuid.UUID, hash string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.customers SET password_hash=$4,must_change_password=true,initial_delivery_status='sent',initial_delivery_attempts=initial_delivery_attempts+1,initial_delivery_last_error=NULL,initial_delivery_claim_token=NULL,initial_delivery_claim_until=NULL,initial_delivery_updated_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND initial_delivery_claim_token=$3 AND initial_delivery_status='sending'`, t, id, claim, hash)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}
func (r Repository) ByID(ctx context.Context, t, id uuid.UUID) (domain.Customer, error) {
	var c domain.Customer
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id,name,COALESCE(normalized_email,''),COALESCE(normalized_phone,''),status,must_change_password,password_changed_at FROM public.customers WHERE tenant_id=$1 AND id=$2`, t, id).Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Status, &c.MustChangePassword, &c.PasswordChangedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	c.TenantID = t
	return c, err
}
func (r Repository) UpdateName(ctx context.Context, t, id uuid.UUID, name string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET name=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, t, id, name)
		return e
	})
}
func (r Repository) UpdatePassword(ctx context.Context, t, id uuid.UUID, hash string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET password_hash=$3,password_changed_at=now(),must_change_password=false,updated_at=now() WHERE tenant_id=$1 AND id=$2`, t, id, hash)
		return e
	})
}
func (r Repository) ResetPassword(ctx context.Context, t, id uuid.UUID, hash string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET password_hash=$3,password_changed_at=now(),must_change_password=true,updated_at=now() WHERE tenant_id=$1 AND id=$2`, t, id, hash)
		return e
	})
}
func (r Repository) CreateSession(ctx context.Context, t, cid uuid.UUID, token string, expires time.Time) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO public.customer_sessions(tenant_id,customer_id,token_hash,expires_at) VALUES($1,$2,$3,$4)`, t, cid, hashToken(token), expires)
		return e
	})
}
func (r Repository) Session(ctx context.Context, t uuid.UUID, token string) (domain.Customer, error) {
	var id uuid.UUID
	err := db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT customer_id FROM public.customer_sessions WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now()`, t, hashToken(token)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Customer{}, ErrNotFound
	}
	if err != nil {
		return domain.Customer{}, err
	}
	return r.ByID(ctx, t, id)
}
func (r Repository) Revoke(ctx context.Context, t uuid.UUID, token string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customer_sessions SET revoked_at=now() WHERE tenant_id=$1 AND token_hash=$2`, t, hashToken(token))
		return e
	})
}
func (r Repository) RevokeOthers(ctx context.Context, t, cid uuid.UUID, token string) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customer_sessions SET revoked_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND token_hash<>$3 AND revoked_at IS NULL`, t, cid, hashToken(token))
		return e
	})
}
func (r Repository) RevokeAll(ctx context.Context, t, cid uuid.UUID) error {
	return db.WithTenantTx(ctx, r.Pool, t, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customer_sessions SET revoked_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND revoked_at IS NULL`, t, cid)
		return e
	})
}
