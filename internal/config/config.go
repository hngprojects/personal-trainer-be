package config

import (
	"fmt"
	"os"
	"time"

	"github.com/lpernett/godotenv"
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

	SessionTTL time.Duration
}

func Load() (*Config, error) {
	godotenv.Load()

	sessionTTL := 7 * 24 * time.Hour
	if v := os.Getenv("SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			sessionTTL = d
		}
	}

	cfg := &Config{
		Env:         getenv("APP_ENV", "development"),
		LogLevel:    os.Getenv("LOG_LEVEL"),
		Port:        os.Getenv("PORT"),
		LogFormat:   os.Getenv("LOG_FORMAT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     getenv("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),

		SessionTTL: sessionTTL,
	}

	required := map[string]string{
		"PORT":                 cfg.Port,
		"DATABASE_URL":         cfg.DatabaseURL,
		"GOOGLE_CLIENT_ID":     cfg.GoogleClientID,
		"GOOGLE_CLIENT_SECRET": cfg.GoogleClientSecret,
		"GOOGLE_REDIRECT_URL":  cfg.GoogleRedirectURL,
	}
	for key, val := range required {
		if val == "" {
			return nil, fmt.Errorf("%s is required", key)
		}
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
