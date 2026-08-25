package repository

import (
	"context"
	"errors"
	"time"

	"github.com/barber-appointment/auth-service/generated"
	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) Repository { return Repository{pool: pool} }

type Credential struct {
	IdentityID         uuid.UUID
	Name               string
	Phone              string
	Email              string
	PasswordHash       string
	MustChangePassword bool
	DeliveryStatus     string
	Active             bool
}

type RegistrationResult struct {
	IdentityID        uuid.UUID
	CreatedIdentity   bool
	CreatedMembership bool
	Role              domain.Role
}

func (r Repository) CredentialByEmail(ctx context.Context, email string) (Credential, error) {
	var value Credential
	var status string
	err := r.pool.QueryRow(ctx, `SELECT i.id,COALESCE(i.name,''),COALESCE(i.phone,''),i.email,c.password_hash,c.must_change_password,c.initial_delivery_status,i.status FROM public.identities i JOIN public.credentials c ON c.identity_id=i.id WHERE lower(i.email)=lower($1)`, email).
		Scan(&value.IdentityID, &value.Name, &value.Phone, &value.Email, &value.PasswordHash, &value.MustChangePassword, &value.DeliveryStatus, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}
	value.Active = status == "active"
	return value, nil
}

// Register creates only the least-privileged tenant membership. An existing
// global identity retains its name and credential; registration merely adds a
// new tenant membership when needed.
func (r Repository) Register(ctx context.Context, tenantID uuid.UUID, name, email, phone string, role domain.Role, passwordHash string) (RegistrationResult, error) {
	var result RegistrationResult
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var identityID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO public.identities(name,email,phone,status)
			VALUES($1,$2,NULLIF($3,''),'active')
			ON CONFLICT DO NOTHING
			RETURNING id`, name, email, phone).Scan(&identityID)
		if errors.Is(err, pgx.ErrNoRows) {
			if err = tx.QueryRow(ctx, `SELECT id FROM public.identities WHERE lower(email)=lower($1)`, email).Scan(&identityID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			result.CreatedIdentity = true
			if _, err = tx.Exec(ctx, `INSERT INTO public.credentials(identity_id,password_hash,password_changed_at,must_change_password,initial_delivery_status,initial_delivery_updated_at) VALUES($1,$2,clock_timestamp(),true,'pending',clock_timestamp())`, identityID, passwordHash); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO public.tenant_memberships(tenant_id,identity_id,role,status) VALUES($1,$2,$3,'active') ON CONFLICT(tenant_id,identity_id) DO NOTHING`, tenantID, identityID, role)
		if err != nil {
			return err
		}
		result.CreatedMembership = tag.RowsAffected() == 1
		if err = tx.QueryRow(ctx, `SELECT role FROM public.tenant_memberships WHERE tenant_id=$1 AND identity_id=$2`, tenantID, identityID).Scan(&result.Role); err != nil {
			return err
		}
		result.IdentityID = identityID
		return nil
	})
	return result, err
}

func (r Repository) ClaimInitialDelivery(ctx context.Context, identityID uuid.UUID) (uuid.UUID, bool, error) {
	token := uuid.New()
	tag, err := r.pool.Exec(ctx, `UPDATE public.credentials SET initial_delivery_status='sending',initial_delivery_claim_token=$2,initial_delivery_claim_until=clock_timestamp()+interval '2 minutes',initial_delivery_updated_at=clock_timestamp() WHERE identity_id=$1 AND (initial_delivery_status IN ('pending','failed') OR (initial_delivery_status='sending' AND initial_delivery_claim_until<clock_timestamp()))`, identityID, token)
	return token, err == nil && tag.RowsAffected() == 1, err
}

func (r Repository) MarkInitialDelivery(ctx context.Context, identityID, claim uuid.UUID, status, problem string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE public.credentials SET initial_delivery_status=$3,initial_delivery_attempts=initial_delivery_attempts+1,initial_delivery_last_error=NULLIF($4,''),initial_delivery_claim_token=NULL,initial_delivery_claim_until=NULL,initial_delivery_updated_at=clock_timestamp() WHERE identity_id=$1 AND initial_delivery_claim_token=$2`, identityID, claim, status, problem)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
func (r Repository) ActivateRetriedCredential(ctx context.Context, identityID, claim uuid.UUID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE public.credentials SET password_hash=$3,must_change_password=true,initial_delivery_status='sent',initial_delivery_attempts=initial_delivery_attempts+1,initial_delivery_last_error=NULL,initial_delivery_claim_token=NULL,initial_delivery_claim_until=NULL,initial_delivery_updated_at=clock_timestamp(),updated_at=clock_timestamp() WHERE identity_id=$1 AND initial_delivery_claim_token=$2 AND initial_delivery_status='sending'`, identityID, claim, passwordHash)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (r Repository) Members(ctx context.Context, tenantID uuid.UUID) ([]domain.Member, error) {
	members := []domain.Member{}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT i.id, COALESCE(i.name,''), COALESCE(i.email,''), COALESCE(i.phone,''), m.role, m.status, m.created_at
			FROM public.tenant_memberships AS m
			JOIN public.identities AS i ON i.id=m.identity_id
			WHERE m.tenant_id=$1
			ORDER BY i.name, i.email`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var member domain.Member
			if err = rows.Scan(&member.IdentityID, &member.Name, &member.Email, &member.Phone, &member.Role, &member.Status, &member.CreatedAt); err != nil {
				return err
			}
			members = append(members, member)
		}
		return rows.Err()
	})
	return members, err
}

func (r Repository) Owners(ctx context.Context, tenantID uuid.UUID) ([]domain.OwnerState, error) {
	result := []domain.OwnerState{}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT i.id,COALESCE(i.name,''),i.email,i.status,m.status,c.must_change_password,c.initial_delivery_status FROM public.tenant_memberships m JOIN public.identities i ON i.id=m.identity_id JOIN public.credentials c ON c.identity_id=i.id WHERE m.tenant_id=$1 AND m.role='OWNER' ORDER BY i.email`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value domain.OwnerState
			if err = rows.Scan(&value.IdentityID, &value.Name, &value.Email, &value.IdentityStatus, &value.MembershipStatus, &value.MustChangePassword, &value.DeliveryStatus); err != nil {
				return err
			}
			result = append(result, value)
		}
		return rows.Err()
	})
	return result, err
}

func (r Repository) Member(ctx context.Context, tenantID, identityID uuid.UUID) (domain.Member, error) {
	var member domain.Member
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT i.id, COALESCE(i.name,''), COALESCE(i.email,''), COALESCE(i.phone,''), m.role, m.status, m.created_at
			FROM public.tenant_memberships AS m
			JOIN public.identities AS i ON i.id=m.identity_id
			WHERE m.tenant_id=$1 AND m.identity_id=$2`, tenantID, identityID).
			Scan(&member.IdentityID, &member.Name, &member.Email, &member.Phone, &member.Role, &member.Status, &member.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return member, err
}

func (r Repository) ChangeMemberRole(ctx context.Context, tenantID, identityID uuid.UUID, role domain.Role) (domain.Member, error) {
	var member domain.Member
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE public.tenant_memberships AS m
			SET role=$3
			FROM public.identities AS i
			WHERE m.tenant_id=$1 AND m.identity_id=$2 AND i.id=m.identity_id
			RETURNING i.id, COALESCE(i.name,''), COALESCE(i.email,''), COALESCE(i.phone,''), m.role, m.status, m.created_at`, tenantID, identityID, role).
			Scan(&member.IdentityID, &member.Name, &member.Email, &member.Phone, &member.Role, &member.Status, &member.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return member, err
}

func (r Repository) SetMemberStatus(ctx context.Context, tenantID, identityID uuid.UUID, status string) (domain.Member, error) {
	var member domain.Member
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE public.tenant_memberships AS m
			SET status=$3
			FROM public.identities AS i
			WHERE m.tenant_id=$1 AND m.identity_id=$2 AND i.id=m.identity_id
			RETURNING i.id, COALESCE(i.name,''), COALESCE(i.email,''), COALESCE(i.phone,''), m.role, m.status, m.created_at`, tenantID, identityID, status).
			Scan(&member.IdentityID, &member.Name, &member.Email, &member.Phone, &member.Role, &member.Status, &member.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return member, err
}

// BarberEligible is the service-to-service membership check used before a
// barber business record can be linked. It stays tenant-scoped and therefore
// cannot be repurposed as a global identity lookup.
func (r Repository) BarberEligible(ctx context.Context, tenantID, identityID uuid.UUID) (bool, error) {
	eligible := false
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM public.tenant_memberships
				WHERE tenant_id=$1 AND identity_id=$2 AND role='BARBER' AND status='active'
			)`, tenantID, identityID).Scan(&eligible)
	})
	return eligible, err
}

func (r Repository) CreateSession(ctx context.Context, tenantID, identityID uuid.UUID, tokenHash []byte, expiresAt time.Time) (domain.Principal, error) {
	var principal domain.Principal
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		membership, err := generated.New(tx).FindActiveMembership(ctx, generated.FindActiveMembershipParams{TenantID: pgtype.UUID{Bytes: tenantID, Valid: true}, IdentityID: pgtype.UUID{Bytes: identityID, Valid: true}})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT COALESCE(i.name,''),COALESCE(i.email,''),COALESCE(i.phone,''),c.must_change_password FROM public.identities i JOIN public.credentials c ON c.identity_id=i.id WHERE i.id=$1 AND i.status='active'`, identityID).Scan(&principal.Name, &principal.Email, &principal.Phone, &principal.MustChangePassword); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var sessionID uuid.UUID
		if err = tx.QueryRow(ctx, `INSERT INTO public.sessions (tenant_id, identity_id, token_hash, expires_at) VALUES ($1,$2,$3,$4) RETURNING id`, tenantID, identityID, tokenHash, expiresAt).Scan(&sessionID); err != nil {
			return err
		}
		principal.IdentityID = identityID
		principal.TenantID = tenantID
		principal.SessionID = sessionID
		principal.Role = domain.Role(membership.Role)
		return nil
	})
	return principal, err
}

func (r Repository) Session(ctx context.Context, tenantID uuid.UUID, tokenHash []byte) (domain.Principal, error) {
	var principal domain.Principal
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx, `
			SELECT s.id,s.identity_id,COALESCE(i.name,''),COALESCE(i.email,''),COALESCE(i.phone,''),c.must_change_password,m.role
			FROM public.sessions s
			JOIN public.identities i ON i.id=s.identity_id
			JOIN public.credentials c ON c.identity_id=i.id
			JOIN public.tenant_memberships m ON m.tenant_id=s.tenant_id AND m.identity_id=s.identity_id
			WHERE s.tenant_id=$1 AND s.token_hash=$2 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
			  AND i.status='active' AND m.status='active'`, tenantID, tokenHash).Scan(&principal.SessionID, &principal.IdentityID, &principal.Name, &principal.Email, &principal.Phone, &principal.MustChangePassword, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		principal.TenantID = tenantID
		principal.Role = domain.Role(role)
		return nil
	})
	return principal, err
}

func (r Repository) RevokeSession(ctx context.Context, tenantID uuid.UUID, tokenHash []byte) error {
	return db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE public.sessions SET revoked_at=clock_timestamp() WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL`, tenantID, tokenHash)
		return err
	})
}

func (r Repository) HasActiveMembership(ctx context.Context, tenantID, identityID uuid.UUID) (bool, error) {
	var active bool
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.tenant_memberships WHERE tenant_id=$1 AND identity_id=$2 AND status='active')`, tenantID, identityID).Scan(&active)
	})
	return active, err
}

func (r Repository) ChangePassword(ctx context.Context, tenantID, identityID, keepSessionID uuid.UUID, passwordHash string) error {
	return db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.tenant_memberships WHERE tenant_id=$1 AND identity_id=$2 AND status='active')`, tenantID, identityID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE public.credentials SET password_hash=$2,password_changed_at=clock_timestamp(),must_change_password=false,updated_at=clock_timestamp() WHERE identity_id=$1`, identityID, passwordHash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SELECT public.revoke_identity_sessions($1,$2)`, identityID, keepSessionID)
		return err
	})
}

// ResetPassword creates a future password-delivery outbox entry in the same
// transaction as the password replacement and all-session revocation.
func (r Repository) ResetPassword(ctx context.Context, tenantID, identityID uuid.UUID, passwordHash string) (bool, error) {
	reset := false
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.tenant_memberships WHERE tenant_id=$1 AND identity_id=$2 AND status='active')`, tenantID, identityID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE public.credentials SET password_hash=$2,password_changed_at=clock_timestamp(),must_change_password=true,updated_at=clock_timestamp() WHERE identity_id=$1`, identityID, passwordHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT public.revoke_identity_sessions($1,NULL)`, identityID); err != nil {
			return err
		}
		reset = true
		return nil
	})
	return reset, err
}
