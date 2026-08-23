package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/catalog-service/internal/handler"
	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/barberclient"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
)

func main() {
	migration := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("catalog-service")
	if *migration {
		if err := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "catalog_db_owner"), env("MIGRATIONS_DIR", "migrations")); err != nil {
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
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error { return platformdb.Ready(pool) })
	handler.New(repository.New(pool), internalauth.NewTokenVerifier(cfg.InternalAuthToken), adminauth.New(env("AUTH_SERVICE_URL", "http://auth-service:8080"), cfg.InternalAuthToken), barberclient.New(env("BARBER_SERVICE_URL", "http://barber-service:8080"), cfg.InternalAuthToken)).Register(app)
	if err := httpx.Run(app, cfg.Port, logging.New(cfg.ServiceName)); err != nil {
		log.Fatal(err)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
