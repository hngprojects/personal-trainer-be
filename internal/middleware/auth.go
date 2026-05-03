package middleware

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const UserIDKey = "userID"

type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func Auth(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "UNAUTHORIZED", "message": "missing authorization header"},
			})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "UNAUTHORIZED", "message": "invalid authorization header"},
			})
			c.Abort()
			return
		}

		tokenString := parts[1]


		// TODO: Still unsure if it's JWT token we're using or sessions
		
		// secret := []byte(os.Getenv("JWT_SECRET"))
		// token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		// 	if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
		// 		return nil, jwt.ErrSignatureInvalid
		// 	}
		// 	return secret, nil
		// })
		// if err != nil || !token.Valid {
		// 	c.JSON(http.StatusUnauthorized, gin.H{
		// 		"error": gin.H{"code": "UNAUTHORIZED", "message": "invalid or expired token"},
		// 	})
		// 	c.Abort()
		// 	return
		// }

		var userID int64
		err := db.QueryRowContext(c.Request.Context(),
			"SELECT user_id FROM sessions WHERE token = $1 AND expires_at > NOW()",
			tokenString,
		).Scan(&userID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "UNAUTHORIZED", "message": "session not found or expired"},
			})
			c.Abort()
			return
		}

		c.Set(UserIDKey, userID)
		c.Next()
	}
}