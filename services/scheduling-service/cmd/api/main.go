package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/scheduling-service/internal/application"
	"github.com/barber-appointment/scheduling-service/internal/handler"
	"github.com/barber-appointment/scheduling-service/internal/repository"
	"github.com/barber-appointment/scheduling-service/internal/service"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
)

func main() {
	migration := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("scheduling-service")
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "scheduling_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
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
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	httpx.Health(app, func() error {
		err := platformdb.Ready(pool)
		metrics.SetDependencyUp("postgres", err == nil)
		return err
	})
	app.Get("/metrics", metrics.Handler())
	deps := schedulingdeps.New(env("BARBER_SERVICE_URL", "http://barber-service:8080"), env("CATALOG_SERVICE_URL", "http://catalog-service:8080"), env("TENANT_SERVICE_URL", "http://tenant-service:8080"), cfg.ServiceInternalToken)
	scheduling := application.New(repository.New(pool), deps, service.NewHTTPOccupancy(env("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), cfg.ServiceInternalToken), handler.Interval(env("BOOKING_INTERVAL_MINUTES", "15")))
	handler.New(scheduling, internalauth.NewMultiTokenVerifier(cfg.InternalAuthToken, cfg.ServiceInternalToken), adminauth.New(env("AUTH_SERVICE_URL", "http://auth-service:8080"), cfg.ServiceInternalToken)).Register(app)
	if err := httpx.Run(app, cfg.Port, logger); err != nil {
		log.Fatal(err)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
