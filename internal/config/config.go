package config

import (
	"fmt"
	"os"

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
}

func Load() (*Config, error) {
	godotenv.Load()
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
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
	}

	required := map[string]string{
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
