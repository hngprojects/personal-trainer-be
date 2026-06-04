package auth

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

type LogoutHandler struct {
	redis appredis.RedisClient
	log   *slog.Logger
}

func NewLogoutHandler(redis appredis.RedisClient, log *slog.Logger) *LogoutHandler {
	return &LogoutHandler{redis: redis, log: log}
}

func (h *LogoutHandler) HandleLogout(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := c.ShouldBindJSON(&body); err != nil || body.RefreshToken == "" {
		h.log.Warn("HandleLogout: missing refresh token in body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("refresh token is required", api.CodeBadRequest))
		return
	}

	token, err := ValidateToken(body.RefreshToken)
	if err != nil || !token.Valid {
		h.log.Warn("HandleLogout: invalid refresh token", "err", err)
		c.JSON(http.StatusUnauthorized, api.NewError("invalid refresh token", api.CodeUnauthorized))
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		h.log.Warn("HandleLogout: invalid token claims (not MapClaims)")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid token claims", api.CodeUnauthorized))
		return
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != string(RefreshToken) {
		h.log.Warn("HandleLogout: wrong token type", "expected", RefreshToken, "got", tokenType)
		c.JSON(http.StatusBadRequest, api.NewError("invalid token type", api.CodeBadRequest))
		return
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		h.log.Warn("HandleLogout: token missing jti claim")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
		return
	}

	exp, _ := claims["exp"].(float64)
	remainingTTL := time.Until(time.Unix(int64(exp), 0))
	if err := h.redis.Set(c.Request.Context(), common.RedisKeyBlocklist+jti, 1, remainingTTL); err != nil {
		h.log.Error("failed to blocklist token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccessResponse("logout successful", api.CodeOK, nil, nil))
}
