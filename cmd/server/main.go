package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
	"github.com/hngprojects/personal-trainer-be/pkg/logger"
)

func main() {
	_ = godotenv.Load() // dev convenience; env vars may be injected by the platform
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration variable %v", err)
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat, cfg.Env)
	slog.SetDefault(log)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		log.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	log.Info("database connected")

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Error("failed to parse REDIS_URL", "err", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	redisPingCtx, redisPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisPingCancel()
	if err := redisClient.Ping(redisPingCtx).Err(); err != nil {
		log.Error("failed to connect to redis", "err", err)
		os.Exit(1)
	}
	log.Info("redis connected")

	srv := routes.New(cfg, log, db, redisClient)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}
