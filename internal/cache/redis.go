package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	c *redis.Client
}

func New(addr string) *Cache {
	return &Cache{c: redis.NewClient(&redis.Options{Addr: addr})}
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.c.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.c.Close()
}

func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.c.Set(ctx, key, b, ttl).Err()
}

func (c *Cache) GetJSON(ctx context.Context, key string, v any) error {
	b, err := c.c.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.c.Del(ctx, key).Err()
}
