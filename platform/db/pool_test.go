package db

import "testing"

func TestPoolConfigurationValidation(t *testing.T) {
	t.Setenv("DB_MAX_CONNS", "0")
	if _, err := PoolConfigFromEnvironment(); err == nil {
		t.Fatal("zero max connections must fail")
	}
	t.Setenv("DB_MAX_CONNS", "2")
	t.Setenv("DB_MIN_CONNS", "3")
	if _, err := PoolConfigFromEnvironment(); err == nil {
		t.Fatal("min greater than max must fail")
	}
	t.Setenv("DB_MIN_CONNS", "1")
	t.Setenv("DB_CONNECT_TIMEOUT", "-1s")
	if _, err := PoolConfigFromEnvironment(); err == nil {
		t.Fatal("negative timeout must fail")
	}
}

func TestPoolConfigurationDevelopmentDefaults(t *testing.T) {
	for _, name := range []string{"DB_MAX_CONNS", "DB_MIN_CONNS", "DB_MAX_CONN_LIFETIME", "DB_MAX_CONN_IDLE_TIME", "DB_HEALTH_CHECK_PERIOD", "DB_CONNECT_TIMEOUT"} {
		t.Setenv(name, "")
	}
	config, err := PoolConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxConns != 10 || config.MinConns != 1 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}
