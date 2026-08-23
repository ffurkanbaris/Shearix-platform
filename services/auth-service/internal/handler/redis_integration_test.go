package handler_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/platform/ratelimit"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestAuthRedisRateLimitIntegration(t *testing.T) {
	redisURL := os.Getenv("AUTH_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("AUTH_TEST_REDIS_URL is required")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx := context.Background()
	key := "auth-integration:" + uuid.NewString()
	defer client.Del(context.Background(), key)
	limiter := ratelimit.NewRedis(client)
	allowed, err := limiter.Allow(ctx, key, 1, 150*time.Millisecond)
	if err != nil || !allowed {
		t.Fatalf("first rate-limit request allowed=%v err=%v", allowed, err)
	}
	allowed, err = limiter.Allow(ctx, key, 1, 150*time.Millisecond)
	if err != nil || allowed {
		t.Fatalf("limit was not enforced allowed=%v err=%v", allowed, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		allowed, err = limiter.Allow(ctx, key, 1, 150*time.Millisecond)
		if err == nil && allowed {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("rate-limit window did not expire")
}
