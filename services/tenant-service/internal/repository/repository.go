package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/tenant-service/generated"
	"github.com/barber-appointment/tenant-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type TenantRepository struct {
	pool  *pgxpool.Pool
	cache *redis.Client
}

func New(pool *pgxpool.Pool, cache *redis.Client) TenantRepository {
	return TenantRepository{pool: pool, cache: cache}
}

var (
	ErrNotFound       = errors.New("tenant resource not found")
	ErrConflict       = errors.New("tenant resource conflict")
	ErrTenantInactive = errors.New("tenant is inactive")
)

// ResolveDomain is deliberately control-plane-only: it calls a constrained
// SECURITY DEFINER function rather than treating the lookup as tenant data.
func (r TenantRepository) ResolveDomain(ctx context.Context, hostname string) (domain.DomainResolution, error) {
	var result domain.DomainResolution
	cacheKey := domainCacheKey(hostname)
	if r.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		value, cacheErr := r.cache.Get(cacheCtx, cacheKey).Result()
		cancel()
		if cacheErr == nil && json.Unmarshal([]byte(value), &result) == nil {
			return result, nil
		}
	}
	err := r.pool.QueryRow(ctx, `SELECT tenant_id, app_type FROM public.resolve_verified_domain($1)`, hostname).Scan(&result.TenantID, &result.AppType)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err == nil && r.cache != nil {
		if value, marshalErr := json.Marshal(result); marshalErr == nil {
			cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			_ = r.cache.Set(cacheCtx, cacheKey, value, 5*time.Minute).Err()
			cancel()
		}
	}
	return result, err
}

func (r TenantRepository) Settings(ctx context.Context, tenantID uuid.UUID) (domain.Settings, error) {
	var result domain.Settings
	cacheKey := settingsCacheKey(tenantID)
	if r.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		value, cacheErr := r.cache.Get(cacheCtx, cacheKey).Result()
		cancel()
		if cacheErr == nil && json.Unmarshal([]byte(value), &result) == nil && result.TenantID == tenantID {
			return result, nil
		}
	}
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).GetTenantSettings(ctx, pgUUID(tenantID))
		if err != nil {
			return translate(err)
		}
		result = settingsFrom(row)
		return nil
	})
	if err == nil {
		r.cacheSettings(ctx, result)
	}
	return result, translate(err)
}

// UpdateSettings always establishes the RLS context inside an explicit
// transaction. Its Redis write-through invalidates stale settings immediately
// without making the cache an authority for tenant configuration.
func (r TenantRepository) UpdateSettings(ctx context.Context, tenantID uuid.UUID, settings domain.Settings) (domain.Settings, error) {
	var result domain.Settings
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).UpdateTenantSettings(ctx, generated.UpdateTenantSettingsParams{
			TenantID:                    pgUUID(tenantID),
			BusinessTimezone:            settings.BusinessTimezone,
			BookingIntervalMinutes:      int32(settings.BookingIntervalMinutes),
			ReminderOffsetsMinutes:      intsToInt32(settings.ReminderOffsetsMinutes),
			CancellationPolicy:          settings.CancellationPolicy,
			CancellationNoticeMinutes:   int32(settings.CancellationNoticeMinutes),
			BookingHorizonDays:          int32(settings.BookingHorizonDays),
			MinimumBookingNoticeMinutes: int32(settings.MinimumBookingNoticeMinutes),
		})
		if err != nil {
			return translate(err)
		}
		result = settingsFromUpdated(row)
		return nil
	})
	if err == nil {
		r.cacheSettings(ctx, result)
	}
	return result, translate(err)
}

// CreateTenant establishes the tenant and its RLS-protected settings in the
// same explicit transaction. set_config(..., true) is transaction-local, so a
// pooled connection never retains tenant context after commit.
func (r TenantRepository) CreateTenant(ctx context.Context, name string, settings domain.Settings) (domain.Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tenant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	created, err := q.CreateTenant(ctx, name)
	if err != nil {
		return domain.Tenant{}, translate(err)
	}
	tenantID := uuid.UUID(created.ID.Bytes)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return domain.Tenant{}, fmt.Errorf("set tenant context: %w", err)
	}
	initialized, err := q.InitializeTenantSettings(ctx, generated.InitializeTenantSettingsParams{
		TenantID:                    pgUUID(tenantID),
		BusinessTimezone:            settings.BusinessTimezone,
		BookingIntervalMinutes:      int32(settings.BookingIntervalMinutes),
		ReminderOffsetsMinutes:      intsToInt32(settings.ReminderOffsetsMinutes),
		CancellationPolicy:          settings.CancellationPolicy,
		CancellationNoticeMinutes:   int32(settings.CancellationNoticeMinutes),
		BookingHorizonDays:          int32(settings.BookingHorizonDays),
		MinimumBookingNoticeMinutes: int32(settings.MinimumBookingNoticeMinutes),
	})
	if err != nil {
		return domain.Tenant{}, translate(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Tenant{}, err
	}
	initializedSettings := settingsFromInitialized(initialized)
	r.cacheSettings(ctx, initializedSettings)
	return domain.Tenant{
		ID:        tenantID,
		Name:      created.Name,
		Status:    created.Status,
		CreatedAt: created.CreatedAt.Time,
		Settings:  initializedSettings,
	}, nil
}

func (r TenantRepository) Tenant(ctx context.Context, tenantID uuid.UUID) (domain.Tenant, error) {
	row, err := generated.New(r.pool).GetTenant(ctx, pgUUID(tenantID))
	if err != nil {
		return domain.Tenant{}, translate(err)
	}
	settings, err := r.Settings(ctx, tenantID)
	if err != nil {
		return domain.Tenant{}, err
	}
	return domain.Tenant{ID: uuid.UUID(row.ID.Bytes), Name: row.Name, Status: row.Status, CreatedAt: row.CreatedAt.Time, Settings: settings}, nil
}

func (r TenantRepository) CreateDomain(ctx context.Context, tenantID uuid.UUID, hostname, domainType string, tokenHash []byte) (domain.TenantDomain, error) {
	if _, err := r.activeTenant(ctx, tenantID); err != nil {
		return domain.TenantDomain{}, err
	}
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).CreateTenantDomain(ctx, generated.CreateTenantDomainParams{
			TenantID: pgUUID(tenantID), Hostname: hostname, DomainType: domainType, VerificationTokenHash: tokenHash,
		})
		if err != nil {
			return translate(err)
		}
		result = domainFromCreate(row)
		return nil
	})
	return result, translate(err)
}

func (r TenantRepository) Domains(ctx context.Context, tenantID uuid.UUID) ([]domain.TenantDomain, error) {
	if _, err := r.activeTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	var result []domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := generated.New(tx).ListTenantDomains(ctx, pgUUID(tenantID))
		if err != nil {
			return translate(err)
		}
		result = make([]domain.TenantDomain, 0, len(rows))
		for _, row := range rows {
			result = append(result, domainFromList(row))
		}
		return nil
	})
	return result, translate(err)
}

func (r TenantRepository) Domain(ctx context.Context, tenantID, domainID uuid.UUID) (domain.TenantDomain, error) {
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).GetTenantDomain(ctx, generated.GetTenantDomainParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = domainFromGet(row)
		return nil
	})
	return result, translate(err)
}

type VerificationDomain struct {
	Domain    domain.TenantDomain
	TokenHash []byte
}

func (r TenantRepository) DomainForVerification(ctx context.Context, tenantID, domainID uuid.UUID) (VerificationDomain, error) {
	var result VerificationDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).GetTenantDomainForVerification(ctx, generated.GetTenantDomainForVerificationParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = VerificationDomain{Domain: domainFromVerification(row), TokenHash: append([]byte(nil), row.VerificationTokenHash...)}
		return nil
	})
	return result, translate(err)
}

func (r TenantRepository) MarkVerificationSucceeded(ctx context.Context, tenantID, domainID uuid.UUID) (domain.TenantDomain, error) {
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).MarkDomainVerificationSucceeded(ctx, generated.MarkDomainVerificationSucceededParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = domainFromMarkVerified(row)
		return nil
	})
	if err == nil {
		r.invalidateDomain(ctx, result.Hostname)
	}
	return result, translate(err)
}

func (r TenantRepository) MarkVerificationFailed(ctx context.Context, tenantID, domainID uuid.UUID) (domain.TenantDomain, error) {
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).MarkDomainVerificationFailed(ctx, generated.MarkDomainVerificationFailedParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = domainFromMarkFailed(row)
		return nil
	})
	if err == nil {
		r.invalidateDomain(ctx, result.Hostname)
	}
	return result, translate(err)
}

func (r TenantRepository) ActivateDomain(ctx context.Context, tenantID, domainID uuid.UUID) (domain.TenantDomain, error) {
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).ActivateTenantDomain(ctx, generated.ActivateTenantDomainParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = domainFromActivate(row)
		return nil
	})
	if err == nil {
		r.invalidateDomain(ctx, result.Hostname)
	}
	return result, translate(err)
}

func (r TenantRepository) DeactivateDomain(ctx context.Context, tenantID, domainID uuid.UUID) (domain.TenantDomain, error) {
	var result domain.TenantDomain
	err := db.WithTenantTx(ctx, r.pool, tenantID, func(tx pgx.Tx) error {
		row, err := generated.New(tx).DeactivateTenantDomain(ctx, generated.DeactivateTenantDomainParams{TenantID: pgUUID(tenantID), ID: pgUUID(domainID)})
		if err != nil {
			return translate(err)
		}
		result = domainFromDeactivate(row)
		return nil
	})
	if err == nil {
		r.invalidateDomain(ctx, result.Hostname)
	}
	return result, translate(err)
}

func (r TenantRepository) activeTenant(ctx context.Context, tenantID uuid.UUID) (generated.GetTenantRow, error) {
	row, err := generated.New(r.pool).GetTenant(ctx, pgUUID(tenantID))
	if err != nil {
		return generated.GetTenantRow{}, translate(err)
	}
	if row.Status != "active" {
		return generated.GetTenantRow{}, ErrTenantInactive
	}
	return row, nil
}

func (r TenantRepository) invalidateDomain(ctx context.Context, hostname string) {
	if r.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		_ = r.cache.Del(cacheCtx, domainCacheKey(hostname)).Err()
		cancel()
	}
}

func domainCacheKey(hostname string) string      { return "tenant-domain:" + hostname }
func settingsCacheKey(tenantID uuid.UUID) string { return "tenant-settings:" + tenantID.String() }

func (r TenantRepository) cacheSettings(ctx context.Context, settings domain.Settings) {
	if r.cache == nil || settings.TenantID == uuid.Nil {
		return
	}
	if value, err := json.Marshal(settings); err == nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		_ = r.cache.Set(cacheCtx, settingsCacheKey(settings.TenantID), value, 5*time.Minute).Err()
		cancel()
	}
}

func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func pgUUID(value uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: value, Valid: true} }

func intsToInt32(values []int) []int32 {
	result := make([]int32, len(values))
	for i, value := range values {
		result[i] = int32(value)
	}
	return result
}

func intsFromInt32(values []int32) []int {
	result := make([]int, len(values))
	for i, value := range values {
		result[i] = int(value)
	}
	return result
}

func settingsFrom(row generated.GetTenantSettingsRow) domain.Settings {
	return domain.Settings{TenantID: uuid.UUID(row.TenantID.Bytes), BusinessTimezone: row.BusinessTimezone, BookingIntervalMinutes: int(row.BookingIntervalMinutes), ReminderOffsetsMinutes: intsFromInt32(row.ReminderOffsetsMinutes), CancellationPolicy: row.CancellationPolicy, CancellationNoticeMinutes: int(row.CancellationNoticeMinutes), BookingHorizonDays: int(row.BookingHorizonDays), MinimumBookingNoticeMinutes: int(row.MinimumBookingNoticeMinutes)}
}

func settingsFromInitialized(row generated.InitializeTenantSettingsRow) domain.Settings {
	return domain.Settings{TenantID: uuid.UUID(row.TenantID.Bytes), BusinessTimezone: row.BusinessTimezone, BookingIntervalMinutes: int(row.BookingIntervalMinutes), ReminderOffsetsMinutes: intsFromInt32(row.ReminderOffsetsMinutes), CancellationPolicy: row.CancellationPolicy, CancellationNoticeMinutes: int(row.CancellationNoticeMinutes), BookingHorizonDays: int(row.BookingHorizonDays), MinimumBookingNoticeMinutes: int(row.MinimumBookingNoticeMinutes)}
}

func settingsFromUpdated(row generated.UpdateTenantSettingsRow) domain.Settings {
	return domain.Settings{TenantID: uuid.UUID(row.TenantID.Bytes), BusinessTimezone: row.BusinessTimezone, BookingIntervalMinutes: int(row.BookingIntervalMinutes), ReminderOffsetsMinutes: intsFromInt32(row.ReminderOffsetsMinutes), CancellationPolicy: row.CancellationPolicy, CancellationNoticeMinutes: int(row.CancellationNoticeMinutes), BookingHorizonDays: int(row.BookingHorizonDays), MinimumBookingNoticeMinutes: int(row.MinimumBookingNoticeMinutes)}
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func newDomain(id, tenantID pgtype.UUID, hostname, domainType string, verified, active bool, verificationState string, requested, attempted, verifiedAt, activated, deactivated, created pgtype.Timestamptz) domain.TenantDomain {
	return domain.TenantDomain{ID: uuid.UUID(id.Bytes), TenantID: uuid.UUID(tenantID.Bytes), Hostname: hostname, DomainType: domainType, Verified: verified, Active: active, VerificationState: verificationState, VerificationRecord: "_barber-verify." + hostname, VerificationRequestedAt: nullableTime(requested), LastVerificationAttemptAt: nullableTime(attempted), VerifiedAt: nullableTime(verifiedAt), ActivatedAt: nullableTime(activated), DeactivatedAt: nullableTime(deactivated), CreatedAt: created.Time}
}

func domainFromCreate(row generated.CreateTenantDomainRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromGet(row generated.GetTenantDomainRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromVerification(row generated.GetTenantDomainForVerificationRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromList(row generated.ListTenantDomainsRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromMarkVerified(row generated.MarkDomainVerificationSucceededRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromMarkFailed(row generated.MarkDomainVerificationFailedRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromActivate(row generated.ActivateTenantDomainRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
func domainFromDeactivate(row generated.DeactivateTenantDomainRow) domain.TenantDomain {
	return newDomain(row.ID, row.TenantID, row.Hostname, row.DomainType, row.Verified, row.Active, row.VerificationState, row.VerificationRequestedAt, row.LastVerificationAttemptAt, row.VerifiedAt, row.ActivatedAt, row.DeactivatedAt, row.CreatedAt)
}
