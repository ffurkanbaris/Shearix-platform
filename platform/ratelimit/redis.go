// Package ratelimit provides small fixed-window infrastructure rate limits.
package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct{ client redis.UniversalClient }

func NewRedis(client redis.UniversalClient) Redis { return Redis{client: client} }

var allow = redis.NewScript(`
local n=redis.call('INCR',KEYS[1])
if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end
if n>tonumber(ARGV[2]) then return 0 end
return 1`)

func (r Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	result, err := allow.Run(ctx, r.client, []string{key}, window.Milliseconds(), limit).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
