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

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/server"
	"github.com/hngprojects/personal-trainer-be/pkg/logger"
	"github.com/hngprojects/personal-trainer-be/pkg/redis"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("failed to load environment variables: %v", err)
	}
	redisClient := redis.NewClient()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to laod configuration varialbe %v", err)
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat, cfg.Env)
	slog.SetDefault(log)

	var db *sql.DB
	if cfg.DatabaseURL != "" {
		db, err = sql.Open("postgres", cfg.DatabaseURL)
		if err != nil {
			log.Error("failed to open database", "err", err)
			os.Exit(1)
		}
		defer db.Close()

		if err := db.Ping(); err != nil {
			log.Error("failed to connect to database", "err", err)
			os.Exit(1)
		}
		log.Info("database connected")
	} else {
		log.Warn("DATABASE_URL not set — starting without database connection")
	}

	srv := server.New(cfg, log, db)

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
