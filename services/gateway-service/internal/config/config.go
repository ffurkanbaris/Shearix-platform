package config

import (
	base "github.com/barber-appointment/platform/config"
	"os"
	"strconv"
	"time"
)

// defaultOutboundTimeoutMS is the fallback timeout for the gateway's outbound
// HTTP client used for every proxied call to internal services. It is kept
// close to but at or under the Next.js frontend proxy's default
// GATEWAY_REQUEST_TIMEOUT_MS (10s, see apps/shared/server-proxy.ts) so that a
// slow backend call is reported by the gateway as a clear 504 timeout before
// the frontend's own request budget is exhausted, instead of the frontend
// racing (and usually losing) against a gateway that gives up too early.
const defaultOutboundTimeoutMS = 9000

type Config struct {
	base.ServiceConfig
	TenantServiceURL       string
	AuthServiceURL         string
	BarberServiceURL       string
	CatalogServiceURL      string
	SchedulingServiceURL   string
	AppointmentServiceURL  string
	NotificationServiceURL string
	CustomerServiceURL     string
	AdminWebURL            string
	BookingWebURL          string
	PlatformAdminToken     string
	OutboundTimeout        time.Duration
}

func Load() Config {
	return Config{ServiceConfig: base.Load("gateway-service"), TenantServiceURL: value("TENANT_SERVICE_URL", "http://tenant-service:8080"), AuthServiceURL: value("AUTH_SERVICE_URL", "http://auth-service:8080"), BarberServiceURL: value("BARBER_SERVICE_URL", "http://barber-service:8080"), CatalogServiceURL: value("CATALOG_SERVICE_URL", "http://catalog-service:8080"), SchedulingServiceURL: value("SCHEDULING_SERVICE_URL", "http://scheduling-service:8080"), AppointmentServiceURL: value("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), NotificationServiceURL: value("NOTIFICATION_SERVICE_URL", "http://notification-service:8080"), CustomerServiceURL: value("CUSTOMER_SERVICE_URL", "http://customer-service:8080"), AdminWebURL: value("ADMIN_WEB_URL", "http://admin-web:3000"), BookingWebURL: value("BOOKING_WEB_URL", "http://booking-web:3000"), PlatformAdminToken: value("PLATFORM_ADMIN_TOKEN", ""), OutboundTimeout: outboundTimeout("GATEWAY_OUTBOUND_TIMEOUT_MS", defaultOutboundTimeoutMS)}
}
func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// outboundTimeout reads a positive millisecond value from the environment,
// falling back to fallbackMS (also in milliseconds) when unset or invalid.
func outboundTimeout(key string, fallbackMS int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return time.Duration(fallbackMS) * time.Millisecond
}
