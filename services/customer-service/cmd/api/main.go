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
	pool, err := platformdb.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
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
	logger := logging.New(cfg.ServiceName)
	sender, err := platformemail.FromEnvironment(logger)
	if err != nil {
		log.Fatal(err)
	}
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error {
		if err := platformdb.Ready(pool); err != nil {
			return err
		}
		return infra.RedisReady(redisClient)
	})
	handler.New(repository.New(pool), internalauth.NewTokenVerifier(cfg.InternalAuthToken), sender, cfg.InternalAuthToken, env("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), ratelimit.NewRedis(redisClient)).Register(app)
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
