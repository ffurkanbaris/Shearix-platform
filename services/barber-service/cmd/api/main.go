package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/barber-service/internal/handler"
	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
)

func main() {
	migration := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("barber-service")
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "barber_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
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
	handler.New(repository.New(pool), internalauth.NewMultiTokenVerifier(cfg.InternalAuthToken, cfg.ServiceInternalToken), adminauth.New(env("AUTH_SERVICE_URL", "http://auth-service:8080"), cfg.ServiceInternalToken)).Register(app)
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
