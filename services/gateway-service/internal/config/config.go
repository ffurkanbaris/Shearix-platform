package config

import (
	base "github.com/barber-appointment/platform/config"
	"os"
)

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
}

func Load() Config {
	return Config{ServiceConfig: base.Load("gateway-service"), TenantServiceURL: value("TENANT_SERVICE_URL", "http://tenant-service:8080"), AuthServiceURL: value("AUTH_SERVICE_URL", "http://auth-service:8080"), BarberServiceURL: value("BARBER_SERVICE_URL", "http://barber-service:8080"), CatalogServiceURL: value("CATALOG_SERVICE_URL", "http://catalog-service:8080"), SchedulingServiceURL: value("SCHEDULING_SERVICE_URL", "http://scheduling-service:8080"), AppointmentServiceURL: value("APPOINTMENT_SERVICE_URL", "http://appointment-service:8080"), NotificationServiceURL: value("NOTIFICATION_SERVICE_URL", "http://notification-service:8080"), CustomerServiceURL: value("CUSTOMER_SERVICE_URL", "http://customer-service:8080"), AdminWebURL: value("ADMIN_WEB_URL", "http://admin-web:3000"), BookingWebURL: value("BOOKING_WEB_URL", "http://booking-web:3000"), PlatformAdminToken: value("PLATFORM_ADMIN_TOKEN", "")}
}
func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
