package config

import (
	"errors"
	"os"
)

type Config struct {
	Env                string
	Port               string
	DatabaseURL        string
	LogLevel           string
	LogFormat          string
	GoogleClientID     string
	GoogleRedirectURL  string
	GoogleClientSecret string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:                getenv("APP_ENV", "development"),
		Port:               getenv("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		LogLevel:           getenv("LOG_LEVEL", "info"),
		LogFormat:          getenv("LOG_FORMAT", "text"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.GoogleClientID == "" {
		return nil, errors.New("GOOGLE_CLIENT_ID is required")
	}
	if cfg.GoogleClientSecret == "" {
		return nil, errors.New("GOOGLE_CLIENT_SECRET is required")
	}

	if cfg.GoogleRedirectURL == "" {
		return nil, errors.New("GOOGLE_REDIRECT_URL is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
