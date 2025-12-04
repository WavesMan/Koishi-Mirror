package cache

import (
    "context"
    "time"
    "github.com/redis/go-redis/v9"
)

type Redis struct {
    cli *redis.Client
    ttl time.Duration
}

func NewRedis(addr, password string, db int, ttl time.Duration) *Redis {
    if addr == "" { return nil }
    cli := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
    return &Redis{cli: cli, ttl: ttl}
}

func (r *Redis) Get(ctx context.Context, key string) (string, error) {
    if r == nil || r.cli == nil { return "", redis.Nil }
    return r.cli.Get(ctx, key).Result()
}

func (r *Redis) Set(ctx context.Context, key, val string) error {
    if r == nil || r.cli == nil { return nil }
    return r.cli.Set(ctx, key, val, r.ttl).Err()
}

func (r *Redis) Del(ctx context.Context, key string) error {
    if r == nil || r.cli == nil { return nil }
    return r.cli.Del(ctx, key).Err()
}

