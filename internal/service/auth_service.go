package auth

import (
	"fmt"
	"log"
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
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("no env variable 'JWT_SECRET'")
	}
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
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString string, expectedType TokenType) (*jwt.Token, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("no env variable 'JWT_SECRET'")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("invalid signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	tokenType, ok := claims["type"].(string)
	if !ok || TokenType(tokenType) != expectedType {
		return nil, fmt.Errorf("invalid token type: expected %s", expectedType)
	}

	return token, nil
}
