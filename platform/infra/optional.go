package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// Optional integrations are connected only when enabled by the consuming
// service. Their absence does not become a readiness dependency accidentally.
func ConnectRedis(ctx context.Context, url string) (*redis.Client, error) {
	if url == "" {
		return nil, nil
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	options.ContextTimeoutEnabled = true
	client := redis.NewClient(options)
	ping, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(ping).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func RedisReady(client *redis.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}

func ConnectNATS(url string, options ...nats.Option) (*nats.Conn, error) {
	if url == "" {
		return nil, nil
	}
	base := []nats.Option{nats.Timeout(2 * time.Second), nats.Name("barber-appointment"), nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second)}
	connection, err := nats.Connect(url, append(base, options...)...)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	return connection, nil
}
