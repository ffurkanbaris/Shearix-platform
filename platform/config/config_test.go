package config

import "testing"

func TestProductionRejectsDevelopmentToken(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	if err := (ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "development-token"}).Validate(); err == nil {
		t.Fatal("unsafe production token was accepted")
	}
}

func TestProductionRejectsDevelopmentDatabaseCredential(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://app:app-dev-password@db/test", InternalAuthToken: "production-token-value"}
	if err := config.Validate(); err == nil {
		t.Fatal("unsafe production database credential was accepted")
	}
}
