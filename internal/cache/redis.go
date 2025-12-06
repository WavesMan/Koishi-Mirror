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

func (r *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
    if r == nil || r.cli == nil { return 0, redis.Nil }
    return r.cli.TTL(ctx, key).Result()
}

func (r *Redis) SetMany(ctx context.Context, kv map[string]string) error {
    if r == nil || r.cli == nil || len(kv) == 0 { return nil }
    pipe := r.cli.Pipeline()
    for k, v := range kv {
        pipe.Set(ctx, k, v, r.ttl)
    }
    _, err := pipe.Exec(ctx)
    return err
}
