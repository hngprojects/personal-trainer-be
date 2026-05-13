package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	LogLevel    string
	LogFormat   string
	FrontendURL string

	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	ResendAPIKey string
	ResendFrom   string

	GoogleClientID        string
	GoogleClientSecret    string
	GoogleRedirectURL     string
	GoogleAndroidClientID string
	GoogleIOSClientID     string

	ZoomAccountID        string
	ZoomClientID         string
	ZoomClientSecret     string
	ZoomUserID           string
	ZoomTokenURL         string
	ZoomAPIBaseURL       string
	ZoomRetryMaxAttempts int
	ZoomRetryBaseDelayMS int
	ZoomRetryMaxDelayMS  int

	OTPSecret string
	RedisURL  string
	JwtSecret string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:         getenv("APP_ENV", "development"),
		Port:        getenv("PORT", "8080"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		LogFormat:   os.Getenv("LOG_FORMAT"),
		FrontendURL: getenv("FRONTEND_URL", "http://localhost:3000"),

		DatabaseURL: os.Getenv("DATABASE_URL"),

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     getenv("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),

		ResendAPIKey: os.Getenv("RESEND_API_KEY"),
		ResendFrom:   os.Getenv("RESEND_FROM"),

		GoogleClientID:        os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:    os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:     getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
		GoogleAndroidClientID: os.Getenv("GOOGLE_ANDROID_CLIENT_ID"),
		GoogleIOSClientID:     os.Getenv("GOOGLE_IOS_CLIENT_ID"),

		ZoomAccountID:        os.Getenv("ZOOM_ACCOUNT_ID"),
		ZoomClientID:         os.Getenv("ZOOM_CLIENT_ID"),
		ZoomClientSecret:     os.Getenv("ZOOM_CLIENT_SECRET"),
		ZoomUserID:           getenv("ZOOM_USER_ID", "me"),
		ZoomTokenURL:         getenv("ZOOM_TOKEN_URL", "https://zoom.us/oauth/token"),
		ZoomAPIBaseURL:       getenv("ZOOM_API_BASE_URL", "https://api.zoom.us/v2"),
		ZoomRetryMaxAttempts: getenvInt("ZOOM_RETRY_MAX_ATTEMPTS", 4),
		ZoomRetryBaseDelayMS: getenvInt("ZOOM_RETRY_BASE_DELAY_MS", 250),
		ZoomRetryMaxDelayMS:  getenvInt("ZOOM_RETRY_MAX_DELAY_MS", 1000),

		OTPSecret: getenv("OTP_SECRET", os.Getenv("JWT_SECRET")),
		RedisURL:  getenv("REDIS_URL", "redis://localhost:6379"),
		JwtSecret: os.Getenv("JWT_SECRET"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.JwtSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
