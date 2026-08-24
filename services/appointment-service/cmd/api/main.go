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
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
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
	if cfg.NATSURL == "" {
		log.Fatal("NATS_URL is required")
	}
	connection, err := infra.ConnectNATS(cfg.NATSURL)
	if err != nil {
		log.Fatal(err)
	}
	publisher, err := outbox.New(repository.New(pool), connection, logger, tracer, metrics.Registerer())
	if err != nil {
		log.Fatal(err)
	}
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	publisher.Start(runtimeCtx)
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	app.Get("/metrics", metrics.Handler())
	httpx.Health(app, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		dbErr := pool.Ping(ctx)
		metrics.SetDependencyUp("postgres", dbErr == nil)
		natsUp := publisher.Ready()
		metrics.SetDependencyUp("nats", natsUp)
		if dbErr != nil {
			return dbErr
		}
		if !natsUp {
			return errors.New("NATS outbox publisher is not ready")
		}
		return nil
	})
	repo := repository.New(pool)
	deps := appointmentclient.New(
		schedulingdeps.New(env("BARBER_SERVICE_URL", "http://barber-service:8080"), env("CATALOG_SERVICE_URL", "http://catalog-service:8080"), env("TENANT_SERVICE_URL", "http://tenant-service:8080"), cfg.ServiceInternalToken),
		env("SCHEDULING_SERVICE_URL", "http://scheduling-service:8080"), env("CUSTOMER_SERVICE_URL", "http://customer-service:8080"), cfg.ServiceInternalToken,
	)
	handler.New(application.NewBooking(repo, deps), application.NewLifecycle(repo, deps), application.NewQuery(repo, deps), internalauth.NewMultiTokenVerifier(cfg.InternalAuthToken, cfg.ServiceInternalToken), adminauth.New(env("AUTH_SERVICE_URL", "http://auth-service:8080"), cfg.ServiceInternalToken)).Register(app)
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
