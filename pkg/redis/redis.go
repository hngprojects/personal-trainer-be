package redis

import (
	"context"
	"log/slog"
	"os"

	goredis "github.com/redis/go-redis/v9"
)

var Ctx = context.Background()

func NewClient() *goredis.Client {
	redisAddress := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	if redisAddress == "" || redisPassword == "" {
		slog.Error("failed to get redis environment variables")
		os.Exit(1)
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:     redisAddress,
		Password: redisPassword,
		DB:       0,
	})

	_, err := client.Ping(Ctx).Result()
	if err != nil {
		slog.Error("failed to connect to redis", "err", err)
		os.Exit(1)
	}

	slog.Info("redis connected")

	return client
}
