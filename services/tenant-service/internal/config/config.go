package config

import base "github.com/barber-appointment/platform/config"

type Config struct {
	base.ServiceConfig
	MigrationDatabaseURL  string
	DatabaseOwnerRole     string
	MigrationsDir         string
	AllowLocalhostDomains bool
	AuthServiceURL        string
}

func Load() Config {
	c := base.Load("tenant-service")
	return Config{ServiceConfig: c, MigrationDatabaseURL: env("MIGRATION_DATABASE_URL"), DatabaseOwnerRole: envDefault("DATABASE_OWNER_ROLE", "tenant_db_owner"), MigrationsDir: envDefault("MIGRATIONS_DIR", "migrations"), AllowLocalhostDomains: envDefault("APP_ENV", "development") == "development", AuthServiceURL: envDefault("AUTH_SERVICE_URL", "http://auth-service:8080")}
}
