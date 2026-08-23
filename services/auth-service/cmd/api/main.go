package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/auth-service/internal/config"
	"github.com/barber-appointment/auth-service/internal/handler"
	"github.com/barber-appointment/auth-service/internal/repository"
	"github.com/barber-appointment/auth-service/internal/service"
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
	cfg := config.Load()
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "auth_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
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
	emailSender, err := platformemail.FromEnvironment(logger)
	if err != nil {
		log.Fatal(err)
	}
	authService := service.New(repository.New(pool), cfg.SessionTTL, emailSender)
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error {
		if err := platformdb.Ready(pool); err != nil {
			return err
		}
		if err := infra.RedisReady(redisClient); err != nil {
			return err
		}
		return nil
	})
	handler.New(authService, internalauth.NewTokenVerifier(cfg.InternalAuthToken), cfg.CookieName, cfg.CookieSecure, ratelimit.NewRedis(redisClient)).Register(app)
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
