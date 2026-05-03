package config

import (
	"os"
)

type Config struct {
	Env                string
	Port               string
	DBURL              string
	LogLevel           string
	LogFormat          string
	DatabaseURL        string
	GoogleClientID     string
	GoogleRedirectURL  string
	GoogleClientSecret string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:                getenv("APP_ENV", "development"),
		Port:               getenv("PORT", "8080"),
		DBURL:              os.Getenv("DB_URL"),
		LogLevel:           getenv("LOG_LEVEL", "info"),
		LogFormat:          os.Getenv("LOG_FORMAT"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
