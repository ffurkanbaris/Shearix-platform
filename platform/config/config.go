package config

import "os"

import (
	"errors"
	"strings"
)

// ServiceConfig contains only infrastructure settings. Secrets are read from
// environment variables and are intentionally never emitted by logging code.
type ServiceConfig struct {
	ServiceName       string
	Port              string
	InternalAuthToken string
	// ServiceInternalToken authenticates backend-to-backend calls (see
	// platform/internalauth.MultiTokenVerifier) separately from
	// InternalAuthToken, which the gateway presents when proxying
	// browser-facing requests. Falls back to InternalAuthToken when unset
	// (SERVICE_INTERNAL_TOKEN not configured) so single-token dev/test setups
	// keep working unchanged; production sets both, distinctly.
	ServiceInternalToken string
	DatabaseURL          string
	RedisURL             string
	NATSURL              string
}

func (c ServiceConfig) Validate() error {
	if c.InternalAuthToken == "" {
		return errors.New("INTERNAL_AUTH_TOKEN is required")
	}
	if c.DatabaseURL == "" && c.ServiceName != "gateway-service" {
		return errors.New("DATABASE_URL is required")
	}
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		values := []string{strings.ToLower(c.InternalAuthToken), strings.ToLower(c.ServiceInternalToken), strings.ToLower(c.DatabaseURL), strings.ToLower(c.RedisURL), strings.ToLower(c.NATSURL)}
		for _, unsafe := range []string{"development", "dev-password", "dev-user", "changeme", "replace-with", "default"} {
			for _, value := range values {
				if strings.Contains(value, unsafe) {
					return errors.New("unsafe production secret configuration")
				}
			}
		}
		// A Redis/NATS URL that carries no credentials at all (no "@"
		// separating userinfo from the host) would otherwise connect
		// anonymously in production even though the server itself requires
		// auth (docker-compose.production.yml) - fail fast instead of
		// discovering that as a connection error at startup.
		if c.RedisURL != "" && !strings.Contains(c.RedisURL, "@") {
			return errors.New("REDIS_URL must include credentials in production")
		}
		if c.NATSURL != "" && !strings.Contains(c.NATSURL, "@") {
			return errors.New("NATS_URL must include credentials in production")
		}
	}
	return nil
}

func Load(serviceName string) ServiceConfig {
	internalAuthToken := os.Getenv("INTERNAL_AUTH_TOKEN")
	serviceInternalToken := value("SERVICE_INTERNAL_TOKEN", internalAuthToken)
	return ServiceConfig{
		ServiceName:          serviceName,
		Port:                 value("PORT", "8080"),
		InternalAuthToken:    internalAuthToken,
		ServiceInternalToken: serviceInternalToken,
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		RedisURL:             os.Getenv("REDIS_URL"),
		NATSURL:              os.Getenv("NATS_URL"),
	}
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
