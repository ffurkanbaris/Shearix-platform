package main

import (
	"context"
	"flag"
	"github.com/barber-appointment/notification-service/internal/consumer"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/barber-appointment/notification-service/internal/worker"
	"github.com/barber-appointment/platform/config"
	platformdb "github.com/barber-appointment/platform/db"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/infra"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/migrate"
	"github.com/barber-appointment/platform/tenantsettings"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "")
	flag.Parse()
	cfg := config.Load("notification-service")
	if *migrateOnly {
		if e := migrate.Apply(context.Background(), os.Getenv("MIGRATION_DATABASE_URL"), env("DATABASE_OWNER_ROLE", "notification_db_owner"), env("MIGRATIONS_DIR", "migrations")); e != nil {
			log.Fatal(e)
		}
		return
	}
	if e := cfg.Validate(); e != nil {
		log.Fatal(e)
	}
	pool, e := platformdb.OpenPool(context.Background(), cfg.DatabaseURL)
	if e != nil {
		log.Fatal(e)
	}
	defer pool.Close()
	if cfg.NATSURL == "" {
		log.Fatal("NATS_URL is required")
	}
	if cfg.InternalAuthToken == "" {
		log.Fatal("INTERNAL_AUTH_TOKEN is required")
	}
	nc, e := infra.ConnectNATS(cfg.NATSURL)
	if e != nil {
		log.Fatal(e)
	}
	js, e := nc.JetStream()
	if e != nil {
		log.Fatal(e)
	}
	logger := logging.New(cfg.ServiceName)
	repo := repository.New(pool)
	provider, e := platformemail.FromEnvironment(logger)
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reminderSettings := tenantsettings.New(env("TENANT_SERVICE_URL", "http://tenant-service:8080"), cfg.InternalAuthToken)
	recipients := consumer.NewAppointmentRecipientClient(env("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), cfg.InternalAuthToken)
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumer.NewWithConfigAndReminderOffsets(js, repo, logger, "APPOINTMENTS", "appointments.v1.appointment.*", "notification-email-v1", reminderSettings, recipients).Run(ctx)
	}()
	workerInstance := worker.New(repo, provider, logger)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); workerInstance.Run(ctx) }()
	app := fiber.New()
	app.Use(httpx.RequestID())
	httpx.Health(app, func() error {
		if e := platformdb.Ready(pool); e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, e = js.AccountInfo(nats.Context(ctx)); e != nil {
			return e
		}
		return nil
	})
	runErr := httpx.Run(app, cfg.Port, logger)
	cancel()
	shutdown, stopShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopShutdown()
	for consumerDone != nil || workerDone != nil {
		select {
		case <-consumerDone:
			consumerDone = nil
		case <-workerDone:
			workerDone = nil
		case <-shutdown.Done():
			consumerDone, workerDone = nil, nil
		}
	}
	nc.Close()
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
