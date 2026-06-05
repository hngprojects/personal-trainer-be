package config

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
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

	// Server-to-server (org-account) credentials — used when
	// ZoomMeetingHost == "org" (default) and as the fallback when a
	// trainer hasn't connected their own account in trainer mode.
	ZoomAccountID    string
	ZoomClientID     string
	ZoomClientSecret string

	// Per-user OAuth credentials for the trainer-hosted flow. A
	// separate Zoom Marketplace app from the server-to-server one;
	// declare scopes meeting:write, meeting:read, user:read.
	ZoomOAuthClientID     string
	ZoomOAuthClientSecret string
	// ZoomOAuthRedirectURL is the absolute URL Zoom redirects the
	// trainer back to after authorising — must match an entry on
	// the Zoom app's "Redirect URL for OAuth" list. Defaults to a
	// dev value so local boot works without env config.
	ZoomOAuthRedirectURL string

	// Base64-encoded 32-byte AES key for encrypting Zoom OAuth tokens
	// at rest. Generate with `openssl rand -base64 32`. Rotation is a
	// re-encrypt migration — out of scope for v1.
	ZoomTokenEncryptionKey string

	// Meeting SDK credentials (a third Zoom Marketplace app of type
	// "Meeting SDK"). Used by /sessions/{id}/join-info to sign the
	// short-lived JWT the client SDK needs to join a meeting. NEVER
	// shipped to the client — signing is server-side.
	ZoomSDKKey    string
	ZoomSDKSecret string

	// Feature flag: who hosts the Zoom meeting created at booking
	// time. "org" (default) uses the server-to-server account; "trainer"
	// uses the trainer's connected OAuth grant, falling back to org
	// when the trainer hasn't connected yet.
	ZoomMeetingHost string

	// Feature flag: what the booking-confirmation email's "Join" link
	// points to. "link" (default) is the raw Zoom URL — opens the Zoom
	// app or browser. "sdk" is a universal-link URL on UniversalLinkDomain
	// that opens our app, which joins via the Meeting SDK.
	ZoomJoinMode string

	// UniversalLinkDomain is the host used in the email "Join" button
	// when ZoomJoinMode == "sdk". Must match the AppleAppSiteAssociation
	// + assetlinks.json files we serve, and must match the AASA host
	// the mobile app's Associated Domains entitlement declares.
	UniversalLinkDomain string

	// ── Google Meet (org-account only) ─────────────────────────────────
	// All Meet rooms are minted by a single Workspace user — usually a
	// dedicated `meet-bot@<domain>` account. Trainers do NOT connect
	// their own Google account; that path was deliberately dropped in
	// favour of one org-owned refresh token, which is much simpler to
	// operate. See docs/MEET_INTEGRATION.md for the one-time bootstrap
	// flow that produces MeetRefreshToken.
	//
	// MeetEnabled is the master switch. When false (default), the Meet
	// platform option is hidden from clients and any inbound booking
	// with session_platform=google_meet returns 503 — letting you ship
	// this code before the Workspace user is provisioned.
	MeetEnabled         bool
	MeetOAuthClientID   string
	MeetOAuthClientSecret string
	MeetRefreshToken    string
	// MeetHostEmail is the address of the Workspace user the refresh
	// token belongs to. The Meet API itself doesn't need it; we only
	// use it for log lines so an operator chasing "why are Meet rooms
	// failing" can tell which mailbox to re-auth.
	MeetHostEmail       string

	// IOSAppBundleID + IOSAppTeamID + IOSAppPath are baked into the
	// AASA file at /.well-known/apple-app-site-association. Without
	// these the iOS app won't claim our /sessions/*/join paths.
	IOSAppBundleID string
	IOSAppTeamID   string

	// AndroidAppPackage + AndroidAppSHA256 are baked into
	// /.well-known/assetlinks.json. The SHA-256 fingerprint must match
	// the signing key on the production APK exactly — get it via
	// `keytool -list -v -keystore <keystore>`.
	AndroidAppPackage string
	AndroidAppSHA256  string

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
	FCMCredentialsJSON []byte
	FCMProjectID       string

	// Apple IAP receipt validation.
	AppleSharedSecret string // APPLE_SHARED_SECRET — App Store Connect shared secret
	AppleBundleID     string // APPLE_BUNDLE_ID — e.g. com.fitcal.app

	// AppleSignInBundleIDs are the `aud` values we accept on the
	// identity token returned by Sign in with Apple. Comma-separated so
	// one server can serve the iOS app bundle id AND the web Services
	// ID (those have different aud values — the iOS native flow uses
	// the bundle id, the web flow uses the Services ID configured in
	// the Apple Developer portal). Empty falls back to AppleBundleID
	// so single-platform deployments don't need a second env var.
	AppleSignInBundleIDs []string // APPLE_SIGN_IN_BUNDLE_IDS=com.fitcal.app,com.fitcal.app.web

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

		ZoomOAuthClientID:      os.Getenv("ZOOM_OAUTH_CLIENT_ID"),
		ZoomOAuthClientSecret:  os.Getenv("ZOOM_OAUTH_CLIENT_SECRET"),
		ZoomOAuthRedirectURL:   getenv("ZOOM_OAUTH_REDIRECT_URL", "http://localhost:8080/api/v1/trainers/me/zoom/callback"),
		ZoomTokenEncryptionKey: os.Getenv("ZOOM_TOKEN_ENCRYPTION_KEY"),

		ZoomSDKKey:    os.Getenv("ZOOM_SDK_KEY"),
		ZoomSDKSecret: os.Getenv("ZOOM_SDK_SECRET"),

		ZoomMeetingHost: getenv("ZOOM_MEETING_HOST", "org"),
		ZoomJoinMode:    getenv("ZOOM_JOIN_MODE", "link"),

		UniversalLinkDomain: os.Getenv("UNIVERSAL_LINK_DOMAIN"),

		MeetEnabled:           getenv("MEET_ENABLED", "false") == "true",
		MeetOAuthClientID:     os.Getenv("MEET_OAUTH_CLIENT_ID"),
		MeetOAuthClientSecret: os.Getenv("MEET_OAUTH_CLIENT_SECRET"),
		MeetRefreshToken:      os.Getenv("MEET_REFRESH_TOKEN"),
		MeetHostEmail:         os.Getenv("MEET_HOST_EMAIL"),

		IOSAppBundleID:      os.Getenv("IOS_APP_BUNDLE_ID"),
		IOSAppTeamID:        os.Getenv("IOS_APP_TEAM_ID"),
		AndroidAppPackage:   os.Getenv("ANDROID_APP_PACKAGE"),
		AndroidAppSHA256:    os.Getenv("ANDROID_APP_SHA256"),

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
		// Encode the JSON file with: base64 -w0 /path/to/fcm-service-account.json
		FCMCredentialsJSON: decodeBase64Env("FCM_CREDENTIALS_JSON"),
		FCMProjectID:       os.Getenv("FCM_PROJECT_ID"),

		AppleSharedSecret: os.Getenv("APPLE_SHARED_SECRET"),
		AppleBundleID:     getenv("APPLE_BUNDLE_ID", "com.fitcal.app"),

		AppleSignInBundleIDs: splitCSV(os.Getenv("APPLE_SIGN_IN_BUNDLE_IDS")),

		GooglePackageName:        getenv("GOOGLE_PACKAGE_NAME", "com.fitcal.app"),
		GoogleServiceAccountJSON: os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON"),

		// IAPSkipVerification skips Apple/Google receipt verification in dev/test.
		IAPSkipVerification: getenv("IAP_SKIP_VERIFICATION", "false") == "true",
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.JwtSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}

	// Production must NEVER silently skip receipt verification. This
	// stays fatal — getting it wrong means a malicious client can claim
	// to have paid without ever paying.
	if cfg.IAPSkipVerification && cfg.Env == "production" {
		return nil, errors.New("IAP_SKIP_VERIFICATION must not be enabled in production")
	}

	// Missing IAP credentials used to be a boot-fatal too, but it took
	// down environments (staging, local dev, CI) that don't run the
	// Apple/Google flows. Matches the pattern used elsewhere in this
	// codebase (MinIO, Zoom, ffmpeg): degrade with a loud warn, let the
	// subscription handlers reject at request time when their backing
	// secret is empty. Production deployments are expected to set both
	// and should treat these warnings as deploy-blocking.
	if !cfg.IAPSkipVerification {
		if cfg.AppleSharedSecret == "" {
			slog.Warn("APPLE_SHARED_SECRET is not set — apple IAP verification will reject every receipt at request time")
		}
		if cfg.GoogleServiceAccountJSON == "" {
			slog.Warn("GOOGLE_SERVICE_ACCOUNT_JSON is not set — google IAP verification will reject every purchase at request time")
		}
	}

	// Single-platform deployments don't need a separate
	// APPLE_SIGN_IN_BUNDLE_IDS knob — fall back to the IAP bundle id
	// so Sign in with Apple "just works" once the iOS app's
	// AppleBundleID is configured.
	if len(cfg.AppleSignInBundleIDs) == 0 && cfg.AppleBundleID != "" {
		cfg.AppleSignInBundleIDs = []string{cfg.AppleBundleID}
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

// splitCSV parses a comma-separated env value into a trimmed, non-empty
// slice. Empty input returns nil so callers can `len() == 0` test for
// "unset" the same way as a missing variable.
func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeBase64Env(key string) []byte {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		slog.Warn(key + " is not valid base64 — push notifications will be disabled")
		return nil
	}
	return data
}
