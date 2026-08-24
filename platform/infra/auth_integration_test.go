package infra_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/platform/infra"
	"github.com/nats-io/nats.go"
)

// TestRedisRequiresCredentialsIntegration exercises ConnectRedis against a
// real, already-running Redis instance that requires authentication (see
// docker-compose.yml's redis --requirepass), proving all three credential
// states this repo's production hardening depends on: missing credentials
// are rejected, wrong credentials are rejected, and valid credentials
// succeed. This is infrastructure configuration, not application logic, so
// it is gated on the dev/test Redis instance's own real address/password
// rather than a mock.
func TestRedisRequiresCredentialsIntegration(t *testing.T) {
	addr, password := os.Getenv("INFRA_TEST_REDIS_ADDR"), os.Getenv("INFRA_TEST_REDIS_PASSWORD")
	if addr == "" || password == "" {
		t.Skip("INFRA_TEST_REDIS_ADDR and INFRA_TEST_REDIS_PASSWORD are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("missing credentials are rejected", func(t *testing.T) {
		client, err := infra.ConnectRedis(ctx, "redis://"+addr+"/0")
		if err == nil {
			_ = client.Close()
			t.Fatal("expected an anonymous connection to a password-protected Redis to fail")
		}
	})
	t.Run("wrong credentials are rejected", func(t *testing.T) {
		client, err := infra.ConnectRedis(ctx, "redis://:wrong-password-entirely@"+addr+"/0")
		if err == nil {
			_ = client.Close()
			t.Fatal("expected a wrong-password connection to fail")
		}
	})
	t.Run("valid credentials succeed", func(t *testing.T) {
		client, err := infra.ConnectRedis(ctx, "redis://:"+password+"@"+addr+"/0")
		if err != nil {
			t.Fatalf("expected a correctly-authenticated connection to succeed: %v", err)
		}
		defer client.Close()
		if err := infra.RedisReady(client); err != nil {
			t.Fatalf("RedisReady on an authenticated client: %v", err)
		}
	})
}

// TestNATSRequiresCredentialsIntegration is the same three-state proof for
// ConnectNATS against a real, already-running NATS server that requires
// authentication (see infrastructure/nats/nats-server.conf).
func TestNATSRequiresCredentialsIntegration(t *testing.T) {
	addr, user, password := os.Getenv("INFRA_TEST_NATS_ADDR"), os.Getenv("INFRA_TEST_NATS_USER"), os.Getenv("INFRA_TEST_NATS_PASSWORD")
	if addr == "" || user == "" || password == "" {
		t.Skip("INFRA_TEST_NATS_ADDR, INFRA_TEST_NATS_USER, and INFRA_TEST_NATS_PASSWORD are required")
	}

	// RetryOnFailedConnect(false) overrides ConnectNATS' own default (true):
	// these two cases need Connect to fail synchronously so the test can
	// observe the error, rather than handing back a connection that retries
	// in the background.
	t.Run("missing credentials are rejected", func(t *testing.T) {
		conn, err := infra.ConnectNATS("nats://"+addr, nats.RetryOnFailedConnect(false))
		if err == nil {
			conn.Close()
			t.Fatal("expected an anonymous connection to an authenticated NATS server to fail")
		}
	})
	t.Run("wrong credentials are rejected", func(t *testing.T) {
		conn, err := infra.ConnectNATS("nats://"+user+":wrong-password-entirely@"+addr, nats.RetryOnFailedConnect(false))
		if err == nil {
			conn.Close()
			t.Fatal("expected a wrong-password connection to fail")
		}
	})
	t.Run("valid credentials succeed", func(t *testing.T) {
		conn, err := infra.ConnectNATS("nats://" + user + ":" + password + "@" + addr)
		if err != nil {
			t.Fatalf("expected a correctly-authenticated connection to succeed: %v", err)
		}
		defer conn.Close()
		if !conn.IsConnected() {
			t.Fatal("authenticated NATS connection reports not connected")
		}
	})
}
