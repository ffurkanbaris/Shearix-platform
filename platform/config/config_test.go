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

func TestProductionRejectsDevelopmentServiceInternalToken(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "production-token-value", ServiceInternalToken: "replace-with-a-token"}
	if err := config.Validate(); err == nil {
		t.Fatal("unsafe production ServiceInternalToken was accepted")
	}
}

func TestProductionRejectsRedisURLWithoutCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "production-token-value", RedisURL: "redis://redis:6379/0"}
	if err := config.Validate(); err == nil {
		t.Fatal("a credential-less production REDIS_URL was accepted")
	}
}

func TestProductionAcceptsRedisURLWithCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "production-token-value", RedisURL: "redis://:a-real-password@redis:6379/0"}
	if err := config.Validate(); err != nil {
		t.Fatalf("a credentialed production REDIS_URL was rejected: %v", err)
	}
}

func TestProductionRejectsNATSURLWithoutCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "production-token-value", NATSURL: "nats://nats:4222"}
	if err := config.Validate(); err == nil {
		t.Fatal("a credential-less production NATS_URL was accepted")
	}
}

func TestProductionAcceptsNATSURLWithCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	config := ServiceConfig{ServiceName: "test", DatabaseURL: "postgres://example", InternalAuthToken: "production-token-value", NATSURL: "nats://svc:a-real-password@nats:4222"}
	if err := config.Validate(); err != nil {
		t.Fatalf("a credentialed production NATS_URL was rejected: %v", err)
	}
}

func TestLoadDefaultsServiceInternalTokenToInternalAuthToken(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_TOKEN", "shared-secret")
	t.Setenv("SERVICE_INTERNAL_TOKEN", "")
	config := Load("test")
	if config.ServiceInternalToken != "shared-secret" {
		t.Fatalf("ServiceInternalToken = %q, want it to default to InternalAuthToken", config.ServiceInternalToken)
	}
}

func TestLoadKeepsServiceInternalTokenDistinctWhenSet(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_TOKEN", "gateway-secret")
	t.Setenv("SERVICE_INTERNAL_TOKEN", "peer-secret")
	config := Load("test")
	if config.ServiceInternalToken != "peer-secret" {
		t.Fatalf("ServiceInternalToken = %q, want the explicitly configured value", config.ServiceInternalToken)
	}
	if config.InternalAuthToken != "gateway-secret" {
		t.Fatalf("InternalAuthToken = %q, want it left untouched", config.InternalAuthToken)
	}
}
