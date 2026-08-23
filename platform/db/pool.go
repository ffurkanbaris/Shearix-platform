package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

func PoolConfigFromEnvironment() (PoolConfig, error) {
	c := PoolConfig{MaxConns: 10, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: 15 * time.Minute, HealthCheckPeriod: 30 * time.Second, ConnectTimeout: 5 * time.Second}
	var err error
	if c.MaxConns, err = int32Env("DB_MAX_CONNS", c.MaxConns); err != nil {
		return c, err
	}
	if c.MinConns, err = int32Env("DB_MIN_CONNS", c.MinConns); err != nil {
		return c, err
	}
	if c.MaxConnLifetime, err = durationEnv("DB_MAX_CONN_LIFETIME", c.MaxConnLifetime); err != nil {
		return c, err
	}
	if c.MaxConnIdleTime, err = durationEnv("DB_MAX_CONN_IDLE_TIME", c.MaxConnIdleTime); err != nil {
		return c, err
	}
	if c.HealthCheckPeriod, err = durationEnv("DB_HEALTH_CHECK_PERIOD", c.HealthCheckPeriod); err != nil {
		return c, err
	}
	if c.ConnectTimeout, err = durationEnv("DB_CONNECT_TIMEOUT", c.ConnectTimeout); err != nil {
		return c, err
	}
	if c.MaxConns <= 0 || c.MinConns < 0 || c.MinConns > c.MaxConns || c.MaxConnLifetime <= 0 || c.MaxConnIdleTime <= 0 || c.HealthCheckPeriod <= 0 || c.ConnectTimeout <= 0 {
		return c, errors.New("invalid database pool configuration")
	}
	return c, nil
}

func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	settings, err := PoolConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	parsed, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	parsed.MaxConns = settings.MaxConns
	parsed.MinConns = settings.MinConns
	parsed.MaxConnLifetime = settings.MaxConnLifetime
	parsed.MaxConnIdleTime = settings.MaxConnIdleTime
	parsed.HealthCheckPeriod = settings.HealthCheckPeriod
	parsed.ConnConfig.ConnectTimeout = settings.ConnectTimeout
	connectCtx, cancel := context.WithTimeout(ctx, settings.ConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, parsed)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func Ready(pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return pool.Ping(ctx)
}

func int32Env(name string, fallback int32) (int32, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return int32(parsed), nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration", name)
	}
	return parsed, nil
}
