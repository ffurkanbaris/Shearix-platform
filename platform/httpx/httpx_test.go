package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestUnexpectedListenerErrorPropagates(t *testing.T) {
	want := errors.New("address already in use")
	err := run(fiber.New(), func() error { return want }, slog.New(slog.NewTextHandler(io.Discard, nil)), make(chan os.Signal))
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v", err)
	}
}

func TestHealthIsLivenessAndReadyChecksDependencies(t *testing.T) {
	app := fiber.New()
	Health(app, func() error { return errors.New("database unavailable") })
	health, err := app.Test(httptest.NewRequest("GET", "/health", nil))
	if err != nil || health.StatusCode != 200 {
		t.Fatalf("health status=%v err=%v", health.StatusCode, err)
	}
	ready, err := app.Test(httptest.NewRequest("GET", "/ready", nil))
	if err != nil || ready.StatusCode != 503 {
		t.Fatalf("ready status=%v err=%v", ready.StatusCode, err)
	}
}
