package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/barber-appointment/platform/adminauth"
	platformdb "github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/infra"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/tenant-service/internal/config"
	"github.com/barber-appointment/tenant-service/internal/handler"
	"github.com/barber-appointment/tenant-service/internal/repository"
	"github.com/barber-appointment/tenant-service/internal/service"
	"github.com/gofiber/fiber/v3"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations")
	flag.Parse()
	cfg := config.Load()
	if *migrateOnly {
		if err := migrate.Apply(context.Background(), cfg.MigrationDatabaseURL, cfg.DatabaseOwnerRole, cfg.MigrationsDir); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := cfg.ServiceConfig.Validate(); err != nil {
		log.Fatal(err)
	}
	logger := logging.New(cfg.ServiceName)
	tracer, shutdown := otelsetup.Init(context.Background(), cfg.ServiceName, logger)
	defer shutdown(context.Background())
	metrics := obsmetrics.New(cfg.ServiceName)
	pool, err := platformdb.OpenPool(context.Background(), cfg.DatabaseURL, platformdb.WithTracer(otelsetup.PGXTracer(tracer)))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	metrics.RegisterPgxPool(pool)
	cache, err := infra.ConnectRedis(context.Background(), cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	if cache != nil {
		defer cache.Close()
		metrics.RegisterRedis(cache)
	}
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	app.Get("/metrics", metrics.Handler())
	httpx.Health(app, func() error {
		dbErr := platformdb.Ready(pool)
		metrics.SetDependencyUp("postgres", dbErr == nil)
		if dbErr != nil {
			return dbErr
		}
		if cache != nil {
			redisErr := infra.RedisReady(cache)
			metrics.SetDependencyUp("redis", redisErr == nil)
			return redisErr
		}
		return nil
	})
	handler.New(service.New(repository.New(pool, cache), service.Dependencies{AllowLocalhostDomains: cfg.AllowLocalhostDomains}), internalauth.NewMultiTokenVerifier(cfg.InternalAuthToken, cfg.ServiceInternalToken), adminauth.New(cfg.AuthServiceURL, cfg.ServiceInternalToken)).Register(app)
	if err := httpx.Run(app, cfg.Port, logger); err != nil {
		os.Exit(1)
	}
}
