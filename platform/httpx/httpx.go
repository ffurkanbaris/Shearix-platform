package httpx

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

func RequestID() fiber.Handler {
	return func(c fiber.Ctx) error {
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Set("X-Request-ID", requestID)
		return c.Next()
	}
}

func Health(app *fiber.App, ready func() error) {
	app.Get("/health", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/ready", func(c fiber.Ctx) error {
		if err := ready(); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "not_ready"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
}

func Run(app *fiber.App, port string, logger *slog.Logger) error {
	return run(app, func() error { return app.Listen(":" + port) }, logger, nil)
}

func run(app *fiber.App, listen func() error, logger *slog.Logger, testSignals <-chan os.Signal) error {
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- listen()
	}()
	signals := testSignals
	if signals == nil {
		owned := make(chan os.Signal, 1)
		signal.Notify(owned, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(owned)
		signals = owned
	}
	select {
	case err := <-serverResult:
		if err == nil || errors.Is(err, fiber.ErrNotRunning) {
			return nil
		}
		return err
	case <-signals:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.ShutdownWithContext(ctx); err != nil && !errors.Is(err, fiber.ErrNotRunning) {
		return err
	}
	select {
	case err := <-serverResult:
		if err != nil && !errors.Is(err, fiber.ErrNotRunning) {
			logger.Debug("server closed", "error", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
