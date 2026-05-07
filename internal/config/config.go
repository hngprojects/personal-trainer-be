package config

import (
	"errors"
	"os"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	LogLevel    string
	LogFormat   string

	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	RedisAddress  string
	RedisPassword string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:       getenv("APP_ENV", "development"),
		Port:      getenv("PORT", "8080"),
		LogLevel:  getenv("LOG_LEVEL", "info"),
		LogFormat: os.Getenv("LOG_FORMAT"),

		DatabaseURL: os.Getenv("DATABASE_URL"),

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     getenv("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),

		RedisAddress:  os.Getenv("REDIS_ADDR"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	if cfg.RedisAddress == "" {
		return nil, errors.New("REDIS_ADDR is required")
	}

	if cfg.RedisPassword == "" {
		return nil, errors.New("REDIS_PASSWORD is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
