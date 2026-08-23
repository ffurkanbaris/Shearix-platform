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
	DatabaseURL       string
	RedisURL          string
	NATSURL           string
}

func (c ServiceConfig) Validate() error {
	if c.InternalAuthToken == "" {
		return errors.New("INTERNAL_AUTH_TOKEN is required")
	}
	if c.DatabaseURL == "" && c.ServiceName != "gateway-service" {
		return errors.New("DATABASE_URL is required")
	}
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		values := []string{strings.ToLower(c.InternalAuthToken), strings.ToLower(c.DatabaseURL)}
		for _, unsafe := range []string{"development", "dev-password", "changeme", "replace-with", "default"} {
			for _, value := range values {
				if strings.Contains(value, unsafe) {
					return errors.New("unsafe production secret configuration")
				}
			}
		}
	}
	return nil
}

func Load(serviceName string) ServiceConfig {
	return ServiceConfig{
		ServiceName:       serviceName,
		Port:              value("PORT", "8080"),
		InternalAuthToken: os.Getenv("INTERNAL_AUTH_TOKEN"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          os.Getenv("REDIS_URL"),
		NATSURL:           os.Getenv("NATS_URL"),
	}
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
