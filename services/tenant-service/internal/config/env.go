package config

import "os"

func env(key string) string { return os.Getenv(key) }
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
