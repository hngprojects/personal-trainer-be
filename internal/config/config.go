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

	OTPSecret string
	RedisURL  string
	JwtSecret string

	ZoomAccountID    string
	ZoomClientID     string
	ZoomClientSecret string

	NotificationEmail string

	MinioEndpoint      string // e.g. "localhost:9000" or "minio.staging.fitcall.me"
	MinioAccessKey     string
	MinioSecretKey     string
	MinioBucket        string // bucket for avatar and video storage
	MinioUseSSL        bool
	MinioPublicBaseURL string // public URL prefix used to build asset URLs returned to clients

	// VideoTempDir is where the video-upload handler writes incoming files
	// before the worker transcodes them. Empty = os.TempDir. Set this to a
	// roomy volume in prod — worst case (workers + buffer) × 500MiB ≈ 11GB.
	VideoTempDir string

	StripeSecretKey string
	ServiceName     string
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

		OTPSecret: getenv("OTP_SECRET", os.Getenv("JWT_SECRET")),
		RedisURL:  getenv("REDIS_URL", "redis://localhost:6379"),
		JwtSecret: os.Getenv("JWT_SECRET"),

		ZoomAccountID:    os.Getenv("ZOOM_ACCOUNT_ID"),
		ZoomClientID:     os.Getenv("ZOOM_CLIENT_ID"),
		ZoomClientSecret: os.Getenv("ZOOM_CLIENT_SECRET"),

		NotificationEmail: os.Getenv("NOTIFICATION_EMAIL"),

		MinioEndpoint:      os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey:     os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey:     os.Getenv("MINIO_SECRET_KEY"),
		MinioBucket:        getenv("MINIO_BUCKET", "fitcall-avatars"),
		MinioUseSSL:        getenv("MINIO_USE_SSL", "false") == "true",
		MinioPublicBaseURL: os.Getenv("MINIO_PUBLIC_BASE_URL"),

		// Default to os.TempDir() so consumers can rely on the field
		// always being a usable path rather than empty-equals-default.
		// Matches the comment on the field and the previous behaviour in
		// streamUploadToTemp (which still defends if the value is empty).
		VideoTempDir: getenv("VIDEO_TEMP_DIR", os.TempDir()),

		StripeSecretKey: os.Getenv("STRIPE_SECRET_KEY"),
		ServiceName:     getenv("SERVICE_NAME", "personal-trainer-be"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.JwtSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}
	if cfg.StripeSecretKey == "" {
		return nil, errors.New("STRIPE_SECRET_KEY is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
