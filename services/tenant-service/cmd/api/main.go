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
	pool, err := platformdb.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	cache, err := infra.ConnectRedis(context.Background(), cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	if cache != nil {
		defer cache.Close()
	}
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error {
		if err := platformdb.Ready(pool); err != nil {
			return err
		}
		if cache != nil {
			return infra.RedisReady(cache)
		}
		return nil
	})
	handler.New(service.New(repository.New(pool, cache), service.Dependencies{AllowLocalhostDomains: cfg.AllowLocalhostDomains}), internalauth.NewTokenVerifier(cfg.InternalAuthToken), adminauth.New(cfg.AuthServiceURL, cfg.InternalAuthToken)).Register(app)
	if err := httpx.Run(app, cfg.Port, logging.New(cfg.ServiceName)); err != nil {
		os.Exit(1)
	}
}
