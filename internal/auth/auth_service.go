package auth

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// jwtSecret is set once at process startup via Configure. Production callers
// invoke Configure from main.go after config.Load() succeeds. Tests that don't
// call Configure fall back to the JWT_SECRET env var (see resolveSecret),
// preserving the older test fixture pattern without requiring every test to
// thread Configure through.
var jwtSecret []byte

// Configure stores the JWT signing secret loaded from config. Call once at
// startup. Panics on empty secret — config.Load already validates non-empty,
// so reaching here with empty indicates a programming error.
func Configure(s string) {
	if s == "" {
		panic("auth.Configure: empty JWT secret")
	}
	jwtSecret = []byte(s)
}

func resolveSecret() []byte {
	if len(jwtSecret) > 0 {
		return jwtSecret
	}
	return []byte(os.Getenv("JWT_SECRET"))
}

func GenerateJWTToken(userId string, tokenType TokenType) (string, error) {
	ttl := 15 * time.Minute
	if tokenType == RefreshToken {
		ttl = 7 * 24 * time.Hour
	}

	claims := jwt.MapClaims{
		"sub":  userId,
		"exp":  time.Now().Add(ttl).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "api.fitcall",
		"type": string(tokenType),
		"jti":  uuid.NewString(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(resolveSecret())
}

// ValidateToken parses and verifies a JWT, enforcing the HMAC signing method.
// To enforce a particular token type (access vs refresh), use
// ValidateAccessToken / ValidateRefreshToken instead.
func ValidateToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid signing method: %s", token.Method.Alg())
		}
		return resolveSecret(), nil
	})
}

// ValidateAccessToken parses, verifies, and asserts the token's "type" claim
// is "access". Refresh tokens cannot be substituted as access tokens.
func ValidateAccessToken(tokenString string) (*jwt.Token, error) {
	return validateTyped(tokenString, AccessToken)
}

// ValidateRefreshToken parses, verifies, and asserts the token's "type" claim
// is "refresh".
func ValidateRefreshToken(tokenString string) (*jwt.Token, error) {
	return validateTyped(tokenString, RefreshToken)
}

func validateTyped(tokenString string, expected TokenType) (*jwt.Token, error) {
	tok, err := ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	if t, _ := claims["type"].(string); t != string(expected) {
		return nil, fmt.Errorf("invalid token type: expected %s", expected)
	}
	return tok, nil
}
