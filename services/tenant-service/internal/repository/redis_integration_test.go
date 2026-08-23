package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/platform/infra"
	"github.com/barber-appointment/tenant-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestTenantRedisCacheAndRecoveryIntegration(t *testing.T) {
	appDSN, ownerDSN, redisURL := os.Getenv("TENANT_TEST_DATABASE_URL"), os.Getenv("TENANT_TEST_OWNER_DATABASE_URL"), os.Getenv("TENANT_TEST_REDIS_URL")
	if appDSN == "" || ownerDSN == "" || redisURL == "" {
		t.Skip("TENANT_TEST_DATABASE_URL, TENANT_TEST_OWNER_DATABASE_URL, and TENANT_TEST_REDIS_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE tenant_db_owner"); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	options.ContextTimeoutEnabled = true
	cache := redis.NewClient(options)
	defer cache.Close()
	if err = cache.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	tenant, domainID := uuid.New(), uuid.New()
	hostname := "cache-" + uuid.NewString() + ".example.test"
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenant_domains WHERE tenant_id=$1`, tenant)
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenant_settings WHERE tenant_id=$1`, tenant)
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenants WHERE id=$1`, tenant)
		_ = cache.Del(context.Background(), "tenant-domain:"+hostname).Err()
	}()
	if _, err = owner.Exec(ctx, `INSERT INTO public.tenants(id,name,status) VALUES($1,'Redis Test','active')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `INSERT INTO public.tenant_domains(id,tenant_id,hostname,domain_type,verified,active,verification_state) VALUES($1,$2,$3,'booking',true,true,'verified')`, domainID, tenant, hostname); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool, cache)

	first, err := repo.ResolveDomain(ctx, hostname)
	if err != nil || first.TenantID != tenant || first.AppType != "booking" {
		t.Fatalf("source resolution=%+v err=%v", first, err)
	}
	if exists, err := cache.Exists(ctx, "tenant-domain:"+hostname).Result(); err != nil || exists != 1 {
		t.Fatalf("cache was not populated exists=%d err=%v", exists, err)
	}
	if _, err = owner.Exec(ctx, `UPDATE public.tenant_domains SET domain_type='admin' WHERE id=$1`, domainID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ResolveDomain(ctx, hostname)
	if err != nil || second.AppType != "booking" {
		t.Fatalf("expected cache hit result=%+v err=%v", second, err)
	}
	if err = cache.Expire(ctx, "tenant-domain:"+hostname, time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2500*time.Millisecond, func() bool { return cache.Exists(ctx, "tenant-domain:"+hostname).Val() == 0 })
	afterExpiry, err := repo.ResolveDomain(ctx, hostname)
	if err != nil || afterExpiry.AppType != "admin" {
		t.Fatalf("expired cache did not refresh result=%+v err=%v", afterExpiry, err)
	}

	if err = cache.Del(ctx, "tenant-domain:"+hostname).Err(); err != nil {
		t.Fatal(err)
	}
	if err = cache.Do(ctx, "CLIENT", "PAUSE", 3500, "ALL").Err(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	fallback, err := repo.ResolveDomain(ctx, hostname)
	if err != nil || fallback.TenantID != tenant || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("Redis outage did not fall back promptly result=%+v elapsed=%s err=%v", fallback, time.Since(started), err)
	}
	if err = infra.RedisReady(cache); err == nil {
		t.Fatal("Redis readiness remained healthy during outage")
	}
	waitUntil(t, 5*time.Second, func() bool { return infra.RedisReady(cache) == nil })
	if err = infra.RedisReady(cache); err != nil {
		t.Fatalf("Redis readiness did not recover: %v", err)
	}
	if _, err = repo.ResolveDomain(ctx, hostname); err != nil && !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("resolution after Redis recovery: %v", err)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Redis integration condition")
}
