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
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

type TokenRole string

const (
	Admin   TokenRole = "admin"
	User    TokenRole = "user"
	Trainer TokenRole = "trainer"
)

func GenerateJWTToken(userId string, tokenType TokenType, role TokenRole) (string, error) {
	ttl := 10 * time.Minute
	if tokenType == RefreshToken {
		ttl = 7 * 24 * time.Hour
	}

	claims := jwt.MapClaims{
		"sub":  userId,
		"exp":  time.Now().Add(ttl).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "api.fitcall",
		"type": string(tokenType),
		"role": string(role),
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
