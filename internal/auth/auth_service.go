package auth

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TokenType string

const (
	AccessToken     TokenType = "access"
	RefreshToken    TokenType = "refresh"
	AccessTokenTTL            = 10 * time.Minute
	RefreshTokenTTL           = 7 * 24 * time.Hour
)

func GenerateJWTToken(userId string, tokenType TokenType) (string, error) {
	ttl := AccessTokenTTL
	if tokenType == RefreshToken {
		ttl = RefreshTokenTTL
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
	return token.SignedString([]byte(os.Getenv("JWT_SECRET")))
}

func ValidateToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("invalid signing method")
		}

		return []byte(os.Getenv("JWT_SECRET")), nil
	})
}
