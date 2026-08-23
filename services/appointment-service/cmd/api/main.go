package main

import (
	"context"
	"errors"
	"flag"
	"github.com/barber-appointment/appointment-service/internal/application"
	appointmentclient "github.com/barber-appointment/appointment-service/internal/client"
	"github.com/barber-appointment/appointment-service/internal/handler"
	"github.com/barber-appointment/appointment-service/internal/outbox"
	"github.com/barber-appointment/appointment-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/infra"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
	"time"
)

func main() {
	migration := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("appointment-service")
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "appointment_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	pool, err := platformdb.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	logger := logging.New(cfg.ServiceName)
	if cfg.NATSURL == "" {
		log.Fatal("NATS_URL is required")
	}
	connection, err := infra.ConnectNATS(cfg.NATSURL)
	if err != nil {
		log.Fatal(err)
	}
	publisher, err := outbox.New(repository.New(pool), connection, logger)
	if err != nil {
		log.Fatal(err)
	}
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	publisher.Start(runtimeCtx)
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		if !publisher.Ready() {
			return errors.New("NATS outbox publisher is not ready")
		}
		return nil
	})
	repo := repository.New(pool)
	deps := appointmentclient.New(
		schedulingdeps.New(env("BARBER_SERVICE_URL", "http://barber-service:8080"), env("CATALOG_SERVICE_URL", "http://catalog-service:8080"), env("TENANT_SERVICE_URL", "http://tenant-service:8080"), cfg.InternalAuthToken),
		env("SCHEDULING_SERVICE_URL", "http://scheduling-service:8080"), env("CUSTOMER_SERVICE_URL", "http://customer-service:8080"), cfg.InternalAuthToken,
	)
	handler.New(application.NewBooking(repo, deps), application.NewLifecycle(repo, deps), application.NewQuery(repo, deps), internalauth.NewTokenVerifier(cfg.InternalAuthToken), adminauth.New(env("AUTH_SERVICE_URL", "http://auth-service:8080"), cfg.InternalAuthToken)).Register(app)
	runErr := httpx.Run(app, cfg.Port, logger)
	stopRuntime()
	publisher.Close()
	connection.Close()
	if runErr != nil {
		log.Print(runErr)
		os.Exit(1)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
