package config

import (
	"encoding/base64"
	"encoding/json"
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

	// Apple IAP receipt validation. APPLE_SHARED_SECRET is retained for
	// any callers still on the legacy verifyReceipt flow, but the
	// primary path is StoreKit 2 signed JWS transactions which need
	// only AppleBundleID + the pinned Apple Root CA (in pkg/iap).
	AppleSharedSecret string // APPLE_SHARED_SECRET — legacy, unused by StoreKit 2 path
	AppleBundleID     string // APPLE_BUNDLE_ID — e.g. com.fitcal.app

	// AppleIAPEnvironment pins which Apple environment is acceptable
	// for incoming JWS transactions: "production", "sandbox", or empty
	// (accept either). Production deployments should set this to
	// "production" so a leaked sandbox transaction can't unlock
	// entitlements. Staging / TestFlight typically leave it empty.
	AppleIAPEnvironment string // APPLE_IAP_ENVIRONMENT=production|sandbox

	// Apple App Store Server API credentials. Not consumed by purchase
	// verification today (that's local JWS verification) — present so
	// future server-to-server flows (refund processing, subscription
	// status checks, App Store Server Notifications V2 ack) can pick
	// them up without another config migration. All three must be set
	// together; partial config logs a warn at boot.
	//
	// APPLE_API_KEY_P8 accepts either raw PEM (`-----BEGIN PRIVATE KEY-----`)
	// or base64-encoded PEM for env-quoting convenience.
	AppleAPIKeyID    string // APPLE_API_KEY_ID — 10-char key id from Apple Developer → Keys
	AppleAPIKeyP8    string // APPLE_API_KEY_P8 — raw or base64 of the .p8 file
	AppleAPIIssuerID string // APPLE_API_ISSUER_ID — UUID from App Store Connect → Integrations

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

		AppleSharedSecret:   os.Getenv("APPLE_SHARED_SECRET"),
		AppleBundleID:       getenv("APPLE_BUNDLE_ID", "com.fitcal.app"),
		AppleIAPEnvironment: strings.ToLower(strings.TrimSpace(os.Getenv("APPLE_IAP_ENVIRONMENT"))),

		AppleAPIKeyID:    os.Getenv("APPLE_API_KEY_ID"),
		AppleAPIKeyP8:    decodePossibleBase64PEM("APPLE_API_KEY_P8"),
		AppleAPIIssuerID: os.Getenv("APPLE_API_ISSUER_ID"),

		AppleSignInBundleIDs: splitCSV(os.Getenv("APPLE_SIGN_IN_BUNDLE_IDS")),

		GooglePackageName:        getenv("GOOGLE_PACKAGE_NAME", "com.fitcal.app"),
		GoogleServiceAccountJSON: loadServiceAccountJSON("GOOGLE_SERVICE_ACCOUNT_JSON"),

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
		// AppleBundleID is the new required input — StoreKit 2 JWS
		// verification needs it to validate the bundleId claim. The
		// legacy AppleSharedSecret is no longer consulted; warning on
		// its absence would be noise.
		if cfg.AppleBundleID == "" {
			slog.Warn("APPLE_BUNDLE_ID is not set — apple IAP verification will reject every transaction at request time")
		}
		if cfg.GoogleServiceAccountJSON == "" {
			slog.Warn("GOOGLE_SERVICE_ACCOUNT_JSON is not set — google IAP verification will reject every purchase at request time")
		}
	}

	// App Store Server API knobs are all-or-nothing — a half-configured
	// trio (e.g. key id without issuer id) silently degrades to "the
	// API client refuses every call", which is worse than not having
	// it at all. Warn loudly so operators catch it at deploy time.
	apiAll := cfg.AppleAPIKeyID != "" && cfg.AppleAPIKeyP8 != "" && cfg.AppleAPIIssuerID != ""
	apiAny := cfg.AppleAPIKeyID != "" || cfg.AppleAPIKeyP8 != "" || cfg.AppleAPIIssuerID != ""
	if apiAny && !apiAll {
		slog.Warn("partial App Store Server API config — need all of APPLE_API_KEY_ID, APPLE_API_KEY_P8, APPLE_API_ISSUER_ID; server-to-server calls will be disabled")
	}

	// APPLE_IAP_ENVIRONMENT defends prod against sandbox transactions.
	// In production, missing or non-"production" config logs a warn
	// because either is probably wrong — but we don't force-fail so a
	// transitional period (mixed builds) can be diagnosed.
	if cfg.Env == "production" && cfg.AppleIAPEnvironment != "production" {
		slog.Warn("APPLE_IAP_ENVIRONMENT is not set to 'production' in a production build — sandbox transactions will be accepted",
			"current", cfg.AppleIAPEnvironment)
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

// loadServiceAccountJSON reads a Google service-account key from the
// environment, accepting either format:
//
//   - Raw JSON (value starts with `{`) — what you get if you paste the
//     key file's contents straight into the env var.
//   - Base64-encoded JSON — convenient when raw JSON is awkward to
//     embed (Docker Compose YAML, k8s ConfigMaps, .env quoting).
//
// Auto-detected by the leading character so existing deployments
// using raw JSON keep working. We also validate the decoded payload
// is actually JSON before accepting it — base64 of garbage decodes
// fine but would only fail much later inside pkg/iap.googleAccessToken
// during purchase verification. Surfacing it at boot makes misconfigs
// obvious immediately. An invalid value warns and resolves to empty
// so subscription handlers reject at request time rather than the
// server failing to boot.
func loadServiceAccountJSON(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "{") {
		// Best-effort validity check: catches the common "paste
		// went weird" case where the env var looks JSON-y but
		// isn't quite. Same warn-and-empty fallthrough as base64.
		if !json.Valid([]byte(v)) {
			slog.Warn(key + " starts with '{' but is not valid JSON — google IAP verification will reject every purchase at request time")
			return ""
		}
		return v
	}
	decoded, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		slog.Warn(key + " is neither raw JSON nor valid base64 — google IAP verification will reject every purchase at request time")
		return ""
	}
	if !json.Valid(decoded) {
		slog.Warn(key + " decoded from base64 but the payload is not valid JSON — google IAP verification will reject every purchase at request time")
		return ""
	}
	return string(decoded)
}

// decodePossibleBase64PEM reads a PEM key from the environment that
// may have been base64-encoded for env-quoting convenience. Detection:
// if the trimmed value contains a `-----BEGIN` marker we treat it as
// raw PEM; otherwise we attempt base64 decode and check the decoded
// bytes are PEM. Invalid input warns and resolves to empty so the
// caller can degrade rather than the server failing to boot.
func decodePossibleBase64PEM(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return ""
	}
	if strings.Contains(v, "-----BEGIN") {
		return v
	}
	decoded, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		slog.Warn(key + " is neither raw PEM nor valid base64 — server-to-server calls will be disabled")
		return ""
	}
	if !strings.Contains(string(decoded), "-----BEGIN") {
		slog.Warn(key + " decoded from base64 but the payload is not PEM — server-to-server calls will be disabled")
		return ""
	}
	return string(decoded)
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
