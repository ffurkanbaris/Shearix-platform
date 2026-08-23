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

// RequestIDLocalsKey is where RequestID() stores the resolved request ID so
// downstream handlers, logging, and outbound-client wiring can retrieve it
// without re-parsing headers. maxRequestIDLen bounds a caller-supplied
// X-Request-ID so an unbounded value can never flow into logs/metrics/traces
// or downstream headers.
const RequestIDLocalsKey = "request_id"

const maxRequestIDLen = 128

func RequestID() fiber.Handler {
	return func(c fiber.Ctx) error {
		requestID := c.Get("X-Request-ID")
		if requestID == "" || len(requestID) > maxRequestIDLen || !isBoundedToken(requestID) {
			requestID = uuid.NewString()
		}
		c.Set("X-Request-ID", requestID)
		c.Locals(RequestIDLocalsKey, requestID)
		return c.Next()
	}
}

// isBoundedToken rejects control characters and anything but a small safe
// character set, so a caller-supplied request ID can never inject a header,
// break log formatting, or carry unbounded/binary content.
func isBoundedToken(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}

// ResolveStatus takes the error c.Next() returned and — matching the exact
// pattern fiber's own bundled logger middleware uses — explicitly applies
// the app's configured ErrorHandler before reading the final response
// status. Fiber does not eagerly write an error's mapped status code onto
// the response as it unwinds through nested c.Next() calls; that mapping
// only happens once something actually invokes the ErrorHandler. Reading
// c.Response().StatusCode() right after c.Next() without doing this first
// silently observes a stale pre-error status (commonly 200) for every
// request that ends in an error — including the common case of a request
// that matched no route at all. Any middleware that records a completion
// status (logging, metrics, tracing) must call this immediately after
// c.Next() rather than reading c.Response().StatusCode() directly.
func ResolveStatus(c fiber.Ctx, chainErr error) int {
	if chainErr != nil {
		if err := c.App().ErrorHandler(c, chainErr); err != nil {
			_ = c.SendStatus(fiber.StatusInternalServerError)
		}
	}
	return c.Response().StatusCode()
}

// RoutePattern returns the bounded route *pattern* fiber matched for this
// request (e.g. "/widgets/:id"), never the raw request path. Pass the error
// c.Next() returned (before ResolveStatus mutates the response). When no
// registered endpoint matched, fiber's router returns fiber.ErrNotFound or
// fiber.ErrMethodNotAllowed as that error — critically, c.Route() in that
// case does NOT report a "no match" sentinel; it reports whatever wrapping
// app.Use() middleware last ran (including this very middleware's own
// registration, e.g. Path "/"), which would otherwise leak the illusion of
// a real route match, or worse — for the true zero-match struct fiber falls
// back to internally — the literal, unbounded request path. Checking the
// router's own not-found/method-not-allowed sentinel errors is the only
// reliable signal; this keeps every consumer (logging, tracing, metrics)
// safe from an unbounded-cardinality leak via probed/garbage paths.
func RoutePattern(c fiber.Ctx, chainErr error) string {
	if errors.Is(chainErr, fiber.ErrNotFound) || errors.Is(chainErr, fiber.ErrMethodNotAllowed) {
		return "unmatched"
	}
	r := c.Route()
	if r == nil || r.Path == "" {
		return "unmatched"
	}
	return r.Path
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
