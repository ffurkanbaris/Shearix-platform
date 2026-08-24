package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/customer-service/internal/handler"
	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/infra"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/ratelimit"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
)

func main() {
	migration := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("customer-service")
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "customer_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
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
	if cfg.RedisURL == "" {
		log.Fatal("REDIS_URL is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	redisClient, err := infra.ConnectRedis(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	defer redisClient.Close()
	metrics.RegisterRedis(redisClient)
	sender, err := platformemail.FromEnvironment(logger)
	if err != nil {
		log.Fatal(err)
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
		redisErr := infra.RedisReady(redisClient)
		metrics.SetDependencyUp("redis", redisErr == nil)
		return redisErr
	})
	handler.New(repository.New(pool), internalauth.NewTokenVerifier(cfg.InternalAuthToken), sender, cfg.InternalAuthToken, env("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), metrics, ratelimit.NewRedis(redisClient)).Register(app)
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
