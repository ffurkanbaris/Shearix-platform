package logging

import (
	"log/slog"
	"os"
	"time"

	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
)

func New(service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})).With("service", service)
}

// HTTPCompletionMiddleware emits exactly one structured log line per
// completed request, after the handler chain runs, with a fixed allowlist
// of fields: request_id, trace_id, method, the normalized route pattern
// (never the raw path or query string), status and duration. tenant_id is
// included only when tenantctx has already validated one for this request.
//
// This is a strict allowlist, not a redaction filter: nothing else about
// the request — headers, cookies, the body, query parameters, or any other
// value a handler might place in c.Locals — is ever read or logged here.
// Passwords, credentials, session cookies, and internal/auth tokens are
// never in scope of this middleware because it never inspects them.
func HTTPCompletionMiddleware(logger *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		route := httpx.RoutePattern(c, err)

		requestID, _ := c.Locals(httpx.RequestIDLocalsKey).(string)

		attrs := []any{
			"request_id", requestID,
			"trace_id", otelsetup.TraceID(c.Context()),
			"method", c.Method(),
			"route", route,
			"status", httpx.ResolveStatus(c, err),
			"duration_ms", time.Since(start).Milliseconds(),
		}
		// The tenant header is only ever trusted for authorization decisions
		// once the gateway (or, for internal contract tests, a caller
		// presenting a valid internal token) has set it; here it is used
		// purely as a diagnostic label, so reading it directly off the
		// request is safe even before per-handler validation runs.
		if tenantID := c.Get(tenantctx.TenantIDHeader); tenantID != "" {
			attrs = append(attrs, "tenant_id", tenantID)
		}

		logger.Info("http_request", attrs...)
		return err
	}
}
