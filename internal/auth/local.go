package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
)

const (
	codeExpiry         = 15 * time.Minute
	refreshTokenExpiry = 7 * 24 * time.Hour
	accessTokenTTL     = 10 * time.Minute
)

type LocalHandler struct {
	users           UserRepository
	sessions        SessionRepository
	codes           VerificationCodeRepository
	localAuth       LocalAuthRepository
	mailer          email.Mailer
	log             *slog.Logger
	verifyLimiter   ratelimit.RateLimiter
	registerLimiter ratelimit.RateLimiter
	otpSecret       string
}

func NewLocalHandler(
	users UserRepository,
	sessions SessionRepository,
	codes VerificationCodeRepository,
	localAuth LocalAuthRepository,
	mailer email.Mailer,
	log *slog.Logger,
	otpSecret string,
	verifyLimiter ratelimit.RateLimiter,
	registerLimiter ratelimit.RateLimiter,
) *LocalHandler {
	if otpSecret == "" {
		log.Warn("OTP_SECRET is not set — OTP hashes have no secret protection; set OTP_SECRET in production")
	}
	return &LocalHandler{
		users:           users,
		sessions:        sessions,
		codes:           codes,
		localAuth:       localAuth,
		mailer:          mailer,
		log:             log,
		verifyLimiter:   verifyLimiter,
		registerLimiter: registerLimiter,
		otpSecret:       otpSecret,
	}
}

func generateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (h *LocalHandler) hashOTP(code string) string {
	mac := hmac.New(sha256.New, []byte(h.otpSecret))
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}
