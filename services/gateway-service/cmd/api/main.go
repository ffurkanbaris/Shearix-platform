package main

import (
	"context"
	"github.com/barber-appointment/gateway-service/internal/config"
	"github.com/barber-appointment/gateway-service/internal/handler"
	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	"log"
	"os"
)

func main() {
	cfg := config.Load()
	if err := cfg.ServiceConfig.Validate(); err != nil {
		log.Fatal(err)
	}
	if cfg.InternalAuthToken == "" || cfg.PlatformAdminToken == "" {
		log.Fatal("INTERNAL_AUTH_TOKEN and PLATFORM_ADMIN_TOKEN are required")
	}
	logger := logging.New(cfg.ServiceName)
	tracer, shutdown := otelsetup.Init(context.Background(), cfg.ServiceName, logger)
	defer shutdown(context.Background())
	metrics := obsmetrics.New(cfg.ServiceName)
	resolver := service.NewResolver(cfg.TenantServiceURL, cfg.InternalAuthToken)
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	app.Get("/metrics", metrics.Handler())
	httpx.Health(app, func() error { return resolver.Ready(context.Background()) })
	handler.New(resolver, cfg.TenantServiceURL, cfg.AuthServiceURL, cfg.BarberServiceURL, cfg.CatalogServiceURL, cfg.SchedulingServiceURL, cfg.AppointmentServiceURL, cfg.NotificationServiceURL, cfg.CustomerServiceURL, cfg.InternalAuthToken, cfg.PlatformAdminToken, cfg.OutboundTimeout, cfg.AdminWebURL, cfg.BookingWebURL).Register(app)
	if err := httpx.Run(app, cfg.Port, logger); err != nil {
		os.Exit(1)
	}
}
