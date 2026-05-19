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

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/observability"
	"github.com/hngprojects/personal-trainer-be/internal/routes"
	"github.com/hngprojects/personal-trainer-be/pkg/logger"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

func main() {
	_ = godotenv.Load() // dev convenience; env vars may be injected by the platform
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration variable %v", err)
	}

	if cfg.Env != "development" {
		slog.Error("seed script can only run in development")
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat, cfg.Env)
	slog.SetDefault(log)

	auth.Configure(cfg.JwtSecret)

	if cfg.OTelEnabled {
		traceCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
		tracerProvider, err := observability.InitTracer(traceCtx, cfg.ServiceName, cfg.Env, cfg.OTelEndpoint)
		traceCancel()
		if err != nil {
			log.Error("failed to initialize tracing", "err", err)
			os.Exit(1)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
				log.Error("failed to shutdown tracer provider", "err", err)
			}
		}()
		log.Info("tracing initialized", "service", cfg.ServiceName, "endpoint", cfg.OTelEndpoint)
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		log.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	log.Info("database connected")

	redisClient, err := appredis.New(cfg.RedisURL)
	if err != nil {
		log.Error("failed to connect to redis", "err", err)
		os.Exit(1)
	}
	log.Info("redis connected")
	defer func() { _ = redisClient.Close() }()

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

	srv.Close()
	log.Info("server stopped")
}
