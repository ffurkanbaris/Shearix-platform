package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/platformauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
)

// DefaultOutboundTimeout is used whenever New is called with a non-positive
// outboundTimeout (in particular, the zero value from callers that don't
// care). See services/gateway-service/internal/config for the environment
// variable (GATEWAY_OUTBOUND_TIMEOUT_MS) that normally drives this in
// production, and its rationale relative to the frontend proxy's own timeout.
const DefaultOutboundTimeout = 9 * time.Second

type Handler struct {
	resolver                                                                                                      service.Resolver
	tenantURL, authURL, barberURL, catalogURL, schedulingURL, appointmentURL, notificationURL, customerURL, token string
	adminWebURL, bookingWebURL                                                                                    string
	platformAuth                                                                                                  platformauth.Verifier
	client                                                                                                        *http.Client
}

func New(resolver service.Resolver, tenantURL, authURL, barberURL, catalogURL, schedulingURL, appointmentURL, notificationURL, customerURL, token, platformAdminToken string, outboundTimeout time.Duration, frontendURLs ...string) Handler {
	adminWebURL, bookingWebURL := "http://admin-web:3000", "http://booking-web:3000"
	if len(frontendURLs) > 0 && frontendURLs[0] != "" {
		adminWebURL = frontendURLs[0]
	}
	if len(frontendURLs) > 1 && frontendURLs[1] != "" {
		bookingWebURL = frontendURLs[1]
	}
	if outboundTimeout <= 0 {
		outboundTimeout = DefaultOutboundTimeout
	}
	return Handler{resolver: resolver, tenantURL: strings.TrimRight(tenantURL, "/"), authURL: strings.TrimRight(authURL, "/"), barberURL: strings.TrimRight(barberURL, "/"), catalogURL: strings.TrimRight(catalogURL, "/"), schedulingURL: strings.TrimRight(schedulingURL, "/"), appointmentURL: strings.TrimRight(appointmentURL, "/"), notificationURL: strings.TrimRight(notificationURL, "/"), customerURL: strings.TrimRight(customerURL, "/"), adminWebURL: strings.TrimRight(adminWebURL, "/"), bookingWebURL: strings.TrimRight(bookingWebURL, "/"), token: token, platformAuth: platformauth.NewTokenVerifier(platformAdminToken), client: &http.Client{Timeout: outboundTimeout, Transport: otelsetup.WrapTransport(nil)}}
}

// requestID returns the single, final resolved request ID for this inbound
// request: the value httpx.RequestID() computed (either the caller's own
// validated X-Request-ID or a freshly generated one) and already returned on
// the response header, as stored in c.Locals by that middleware. Every
// outbound proxy call must forward this exact value — never a fresh read of
// the inbound X-Request-ID header, which is empty whenever the caller sent
// no header at all (the case httpx.RequestID() exists to paper over by
// generating one). Falls back to the raw inbound header only when the
// RequestID middleware was not installed in front of this handler (e.g. a
// handler exercised directly in a test without the full app.Use chain), so
// behavior degrades gracefully rather than silently dropping the header.
func requestID(c fiber.Ctx) string {
	if id, ok := c.Locals(httpx.RequestIDLocalsKey).(string); ok && id != "" {
		return id
	}
	return c.Get(tenantctx.RequestIDHeader)
}

// isTimeout reports whether err represents the outbound HTTP client's own
// timeout firing (context deadline exceeded while waiting on the backend),
// as opposed to a connection-level failure such as connection refused or a
// DNS lookup failure. http.Client wraps timeout errors in a *url.Error whose
// Timeout() method returns true; errors.As unwraps to find it.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// upstreamError maps a failed h.client.Do call to a distinct, clear response:
// a timed-out backend is reported as 504 with a JSON body identifying the
// timeout, while any other network failure (connection refused, DNS
// failure, and similar) is reported as 502 with a JSON body identifying it
// as an unavailable upstream. The two failure modes never share a status
// code or body shape, so callers (and the frontend proxy's own timeout
// handling) can always tell them apart.
func upstreamError(c fiber.Ctx, err error) error {
	if isTimeout(err) {
		return c.Status(fiber.StatusGatewayTimeout).JSON(fiber.Map{"error": "gateway_timeout", "message": "The upstream service did not respond in time."})
	}
	return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "gateway_unavailable", "message": "The upstream service is unavailable."})
}
func (h Handler) Register(app *fiber.App) {
	// Meta callbacks are public but do not resolve a hostname or add internal
	// tenant/auth headers. Authenticity is validated by notification-service.
	// Platform endpoints deliberately bypass hostname tenant resolution. They
	// are protected by a distinct platform-admin credential and cannot be
	// reached as tenant-admin APIs.
	app.Post("/api/v1/platform/tenants", h.platformProxy("/internal/v1/platform/tenants"))
	app.Get("/api/v1/platform/tenants/:id", h.platformProxy("/internal/v1/platform/tenants/:id"))
	app.Post("/api/v1/platform/tenants/:id/domains", h.platformProxy("/internal/v1/platform/tenants/:id/domains"))
	app.Get("/api/v1/platform/tenants/:id/domains", h.platformProxy("/internal/v1/platform/tenants/:id/domains"))
	app.Post("/api/v1/platform/tenants/:id/domains/:domain_id/verify", h.platformProxy("/internal/v1/platform/tenants/:id/domains/:domain_id/verify"))
	app.Post("/api/v1/platform/tenants/:id/domains/:domain_id/activate", h.platformProxy("/internal/v1/platform/tenants/:id/domains/:domain_id/activate"))
	app.Post("/api/v1/platform/tenants/:id/domains/:domain_id/deactivate", h.platformProxy("/internal/v1/platform/tenants/:id/domains/:domain_id/deactivate"))
	app.Get("/api/v1/public/config", h.config)
	h.registerCustomerRoutes(app)
	h.registerAuthRoutes(app)
	h.registerTenantRoutes(app)
	h.registerAppointmentRoutes(app)
	h.registerBarberRoutes(app)
	h.registerCatalogRoutes(app)
	h.registerSchedulingRoutes(app)
	// Caddy forwards all non-API tenant page traffic here. The gateway resolves
	// the hostname first, then selects the correct frontend by the authoritative
	// domain app type. This keeps dynamic custom-domain routing out of Caddy.
	app.Use(h.frontend)
}

func (h Handler) platformProxy(path string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if h.platformAuth == nil || h.platformAuth.Verify(c.Get(platformauth.HeaderName)) != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		target := strings.ReplaceAll(path, ":id", c.Params("id"))
		target = strings.ReplaceAll(target, ":domain_id", c.Params("domain_id"))
		request, err := http.NewRequestWithContext(c.Context(), c.Method(), h.tenantURL+target, bytes.NewReader(c.Body()))
		if err != nil {
			return c.SendStatus(fiber.StatusBadGateway)
		}
		// The gateway derives the only trusted internal credential. Client
		// supplied X-Internal-Token or tenant-context values are never copied.
		request.Header.Set(internalauth.HeaderName, h.token)
		request.Header.Set(tenantctx.RequestIDHeader, requestID(c))
		request.Header.Set("Content-Type", c.Get("Content-Type"))
		response, err := h.client.Do(request)
		if err != nil {
			return upstreamError(c, err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return c.SendStatus(fiber.StatusBadGateway)
		}
		return c.Status(response.StatusCode).Send(body)
	}
}

func hostname(c fiber.Ctx) string {
	host := strings.ToLower(strings.TrimSpace(c.Hostname()))
	return strings.TrimSuffix(host, ".")
}
func (h Handler) config(c fiber.Ctx) error {
	// Deliberately ignore all client-supplied trusted context headers.
	resolution, err := h.resolver.Resolve(c.Context(), hostname(c))
	if err == service.ErrNotFound {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusBadGateway)
	}
	request, err := http.NewRequestWithContext(c.Context(), http.MethodGet, h.tenantURL+"/internal/v1/public/config", nil)
	if err != nil {
		return c.SendStatus(fiber.StatusBadGateway)
	}
	request.Header.Set(internalauth.HeaderName, h.token)
	request.Header.Set(tenantctx.TenantIDHeader, resolution.TenantID)
	request.Header.Set(tenantctx.AppTypeHeader, resolution.AppType)
	request.Header.Set(tenantctx.RequestIDHeader, requestID(c))
	response, err := h.client.Do(request)
	if err != nil {
		return upstreamError(c, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return c.SendStatus(fiber.StatusBadGateway)
	}
	return c.Status(response.StatusCode).Send(body)
}

func (h Handler) auth(path string) fiber.Handler {
	return func(c fiber.Ctx) error {
		resolution, err := h.resolver.Resolve(c.Context(), hostname(c))
		if err == service.ErrNotFound {
			return c.SendStatus(fiber.StatusNotFound)
		}
		if err != nil {
			return c.SendStatus(fiber.StatusBadGateway)
		}
		if resolution.AppType != "admin" {
			return c.SendStatus(fiber.StatusForbidden)
		}
		path = strings.ReplaceAll(path, ":id", c.Params("id"))
		request, err := http.NewRequestWithContext(c.Context(), c.Method(), h.authURL+"/internal/v1/auth"+path, bytes.NewReader(c.Body()))
		if err != nil {
			return c.SendStatus(fiber.StatusBadGateway)
		}
		request.Header.Set(internalauth.HeaderName, h.token)
		request.Header.Set(tenantctx.TenantIDHeader, resolution.TenantID)
		request.Header.Set(tenantctx.AppTypeHeader, resolution.AppType)
		request.Header.Set(tenantctx.RequestIDHeader, requestID(c))
		request.Header.Set("Content-Type", c.Get("Content-Type"))
		if cookie := c.Get("Cookie"); cookie != "" {
			request.Header.Set("Cookie", cookie)
		}
		response, err := h.client.Do(request)
		if err != nil {
			return upstreamError(c, err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return c.SendStatus(fiber.StatusBadGateway)
		}
		for _, cookie := range response.Header.Values("Set-Cookie") {
			c.Append("Set-Cookie", cookie)
		}
		return c.Status(response.StatusCode).Send(body)
	}
}

func (h Handler) proxy(base, path, appType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		resolution, err := h.resolver.Resolve(c.Context(), hostname(c))
		if err == service.ErrNotFound {
			return c.SendStatus(404)
		}
		if err != nil {
			return c.SendStatus(502)
		}
		if resolution.AppType != appType {
			return c.SendStatus(403)
		}
		target := strings.ReplaceAll(path, ":id", c.Params("id"))
		target = strings.ReplaceAll(target, ":barber_id", c.Params("barber_id"))
		target = strings.ReplaceAll(target, ":service_id", c.Params("service_id"))
		if query := string(c.Request().URI().QueryString()); query != "" {
			target += "?" + query
		}
		req, err := http.NewRequestWithContext(c.Context(), c.Method(), base+target, bytes.NewReader(c.Body()))
		if err != nil {
			return c.SendStatus(502)
		}
		req.Header.Set(internalauth.HeaderName, h.token)
		req.Header.Set(tenantctx.TenantIDHeader, resolution.TenantID)
		req.Header.Set(tenantctx.AppTypeHeader, resolution.AppType)
		req.Header.Set(tenantctx.RequestIDHeader, requestID(c))
		req.Header.Set("Content-Type", c.Get("Content-Type"))
		if key := c.Get("Idempotency-Key"); key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if cookie := c.Get("Cookie"); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		// Resolve an optional customer session at the trusted edge. Guest
		// bookings continue without this header; appointment-service never
		// accepts customer ownership from the browser body.
		if base == h.appointmentURL && strings.HasPrefix(path, "/internal/v1/public/appointments") {
			session := c.Cookies("__Host-barber_customer_session")
			if session == "" {
				session = c.Cookies("barber_customer_session")
			}
			if session != "" {
				check, checkErr := http.NewRequestWithContext(c.Context(), http.MethodGet, h.customerURL+"/internal/v1/internal/customer/session", nil)
				if checkErr == nil {
					check.Header.Set(internalauth.HeaderName, h.token)
					check.Header.Set(tenantctx.TenantIDHeader, resolution.TenantID)
					check.Header.Set(tenantctx.AppTypeHeader, resolution.AppType)
					check.Header.Set("X-Customer-Session", session)
					if checkResponse, checkDoErr := h.client.Do(check); checkDoErr == nil {
						defer checkResponse.Body.Close()
						if checkResponse.StatusCode >= 200 && checkResponse.StatusCode < 300 {
							var customer struct {
								ID string `json:"id"`
							}
							if json.NewDecoder(checkResponse.Body).Decode(&customer) == nil && customer.ID != "" {
								req.Header.Set("X-Customer-ID", customer.ID)
							}
						}
					}
				}
			}
		}
		res, err := h.client.Do(req)
		if err != nil {
			return upstreamError(c, err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return c.SendStatus(502)
		}
		for _, cookie := range res.Header.Values("Set-Cookie") {
			c.Append("Set-Cookie", cookie)
		}
		return c.Status(res.StatusCode).Send(body)
	}
}

// frontend is a narrowly scoped reverse proxy for tenant page/static traffic.
// It does not create or forward tenant context: resolution comes from Host and
// the selected Next.js application receives only ordinary browser headers.
func (h Handler) frontend(c fiber.Ctx) error {
	if c.Method() != http.MethodGet && c.Method() != http.MethodHead {
		return c.SendStatus(http.StatusNotFound)
	}
	// Only page/static traffic belongs to a browser app. Keeping unmatched API
	// and webhook paths at the gateway avoids an accidental proxy recursion
	// through a Next.js same-origin API route.
	if strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/webhooks/") {
		return c.SendStatus(http.StatusNotFound)
	}
	resolution, err := h.resolver.Resolve(c.Context(), hostname(c))
	if err == service.ErrNotFound {
		return c.SendStatus(http.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(http.StatusBadGateway)
	}
	base := ""
	switch resolution.AppType {
	case "admin":
		base = h.adminWebURL
	case "booking":
		base = h.bookingWebURL
	default:
		return c.SendStatus(http.StatusNotFound)
	}
	target := base + c.OriginalURL()
	req, err := http.NewRequestWithContext(c.Context(), c.Method(), target, nil)
	if err != nil {
		return c.SendStatus(http.StatusBadGateway)
	}
	for _, header := range []string{"Accept", "Accept-Language", "Cookie", "Next-Router-Prefetch", "Next-Router-State-Tree", "Next-Url", "Rsc"} {
		if value := c.Get(header); value != "" {
			req.Header.Set(header, value)
		}
	}
	// Request.Host controls the actual Host header in net/http. Preserve it so
	// frontend same-origin API proxies retain tenant hostname semantics.
	req.Host = c.Get("Host")
	res, err := h.client.Do(req)
	if err != nil {
		return upstreamError(c, err)
	}
	defer res.Body.Close()
	for _, header := range []string{"Content-Type", "Cache-Control", "Location", "Vary"} {
		if value := res.Header.Get(header); value != "" {
			c.Set(header, value)
		}
	}
	for _, cookie := range res.Header.Values("Set-Cookie") {
		c.Append("Set-Cookie", cookie)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return c.SendStatus(http.StatusBadGateway)
	}
	return c.Status(res.StatusCode).Send(body)
}
