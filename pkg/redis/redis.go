package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)
}

type Client struct {
	rdb *redis.Client
}

var errNoRedis = errors.New("redis not configured")

func New(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

func (c *Client) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if c.rdb == nil {
		return errNoRedis
	}
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if c.rdb == nil {
		return false, errNoRedis
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

func (c *Client) Get(ctx context.Context, key string) *redis.StringCmd {
	if c.rdb == nil {
		cmd := redis.NewStringCmd(ctx, "GET", key)
		cmd.SetErr(errNoRedis)
		return cmd
	}
	return c.rdb.Get(ctx, key)
}

func (c *Client) Delete(ctx context.Context, key string) *redis.IntCmd {
	if c.rdb == nil {
		cmd := redis.NewIntCmd(ctx, "DEL", key)
		cmd.SetErr(errNoRedis)
		return cmd
	}
	return c.rdb.Del(ctx, key)
}

func (c *Client) Close() error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// Raw returns the underlying *redis.Client from go-redis/v9.
func (c *Client) Raw() *redis.Client {
	return c.rdb
}
