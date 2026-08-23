package config

import (
	"os"
	"strconv"
	"time"

	base "github.com/barber-appointment/platform/config"
)

type Config struct {
	base.ServiceConfig
	CookieName   string
	CookieSecure bool
	SessionTTL   time.Duration
}

func Load() Config {
	return Config{
		ServiceConfig: base.Load("auth-service"),
		CookieName:    value("AUTH_COOKIE_NAME", "__Host-barber_session"),
		CookieSecure:  boolValue("AUTH_COOKIE_SECURE", true),
		SessionTTL:    time.Duration(intValue("AUTH_SESSION_TTL_HOURS", 24)) * time.Hour,
	}
}
func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func boolValue(key string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
func intValue(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
