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
	ServiceName string

	OTelEnabled  bool
	OTelEndpoint string

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

	// TrainerSetupTokenExpiryHours bounds how long a trainer activation link
	// is valid. Default 168h (7 days) so an admin can invite a trainer on
	// Monday and the trainer can act on it during the following week.
	// Override via TRAINER_SETUP_TOKEN_EXPIRY_HOURS.
	TrainerSetupTokenExpiryHours int

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
	VideoTempDir       string
	FCMCredentialsFile string
	FCMProjectID       string

	// Apple IAP receipt validation.
	AppleSharedSecret string // APPLE_SHARED_SECRET — App Store Connect shared secret
	AppleBundleID     string // APPLE_BUNDLE_ID — e.g. com.fitcal.app

	// Google Play billing.
	GooglePackageName        string // GOOGLE_PACKAGE_NAME — e.g. com.fitcal.app
	GoogleServiceAccountJSON string // GOOGLE_SERVICE_ACCOUNT_JSON — full JSON key file contents

	// IAPSkipVerification skips Apple/Google receipt verification in dev/test.
	IAPSkipVerification bool // IAP_SKIP_VERIFICATION=true
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:         getenv("APP_ENV", "development"),
		Port:        getenv("PORT", "8080"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		LogFormat:   os.Getenv("LOG_FORMAT"),
		FrontendURL: getenv("FRONTEND_URL", "http://localhost:3000"),
		ServiceName: getenv("SERVICE_NAME", "personal-trainer-be"),

		OTelEnabled:  getenv("OTEL_ENABLED", "true") != "false",
		OTelEndpoint: getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:4317"),

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

		TrainerSetupTokenExpiryHours: parsePositiveIntEnv("TRAINER_SETUP_TOKEN_EXPIRY_HOURS", 168),

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

		// FCM credentials for push notifications to trainers
		FCMCredentialsFile: os.Getenv("FCM_CREDENTIALS_FILE"),
		FCMProjectID:       os.Getenv("FCM_PROJECT_ID"),

		AppleSharedSecret: os.Getenv("APPLE_SHARED_SECRET"),
		AppleBundleID:     getenv("APPLE_BUNDLE_ID", "com.fitcal.app"),

		GooglePackageName:        getenv("GOOGLE_PACKAGE_NAME", "com.fitcal.app"),
		GoogleServiceAccountJSON: os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON"),

		IAPSkipVerification: getenv("IAP_SKIP_VERIFICATION", "false") == "true",
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.JwtSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	if cfg.IAPSkipVerification && cfg.Env == "production" {
		return nil, errors.New("IAP_SKIP_VERIFICATION must not be enabled in production")
	}
	if !cfg.IAPSkipVerification {
		if cfg.AppleSharedSecret == "" {
			return nil, errors.New("APPLE_SHARED_SECRET is required when IAP_SKIP_VERIFICATION is false")
		}
		if cfg.GoogleServiceAccountJSON == "" {
			return nil, errors.New("GOOGLE_SERVICE_ACCOUNT_JSON is required when IAP_SKIP_VERIFICATION is false")
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

// parsePositiveIntEnv reads an env var as a positive int. Empty, unset, or
// invalid values fall back to the supplied default — preferring a sane
// default over hard-failing boot on a malformed knob.
func parsePositiveIntEnv(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
